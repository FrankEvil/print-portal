package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/liuzhaojun/print-portal/internal/config"
	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/discovery"
	"github.com/liuzhaojun/print-portal/internal/driver"
	"github.com/liuzhaojun/print-portal/internal/job"
	"github.com/liuzhaojun/print-portal/internal/printer"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

// Server wires HTTP handlers to the application services.
type Server struct {
	cfg       config.Config
	store     *storage.Store
	cups      *cups.Client
	jobs      *job.Manager
	printers  *printer.Service
	drivers   *driver.Service
	discovery *discovery.Service
	logger    *log.Logger
	mux       *http.ServeMux
}

func New(
	cfg config.Config,
	store *storage.Store,
	cupsClient *cups.Client,
	jobs *job.Manager,
	printers *printer.Service,
	drivers *driver.Service,
	discoverySvc *discovery.Service,
	logger *log.Logger,
) *Server {
	s := &Server{
		cfg:       cfg,
		store:     store,
		cups:      cupsClient,
		jobs:      jobs,
		printers:  printers,
		drivers:   drivers,
		discovery: discoverySvc,
		logger:    logger,
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Public API.
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /api/printers", s.handleListPrinters)
	s.mux.HandleFunc("GET /api/printers/{id}/ppd-options", s.handlePublicPPDOptions)
	s.mux.HandleFunc("POST /api/print", s.handlePrint)
	s.mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	s.mux.HandleFunc("GET /api/jobs/{id}", s.handleGetJob)
	s.mux.HandleFunc("GET /api/jobs/{id}/preview", s.handleJobPreview)
	s.mux.HandleFunc("DELETE /api/jobs/{id}", s.handleCancelJob)
	s.mux.HandleFunc("POST /api/jobs/{id}/retry", s.handleRetryJob)
	s.mux.HandleFunc("GET /api/scanners", s.handleScanners)
	s.mux.HandleFunc("POST /api/scan", s.handleScan)

	// Admin API (ADMIN_TOKEN).
	s.admin("GET /api/admin/overview", s.handleOverview)
	s.admin("GET /api/admin/printers", s.handleAdminPrinters)
	s.admin("POST /api/admin/printers", s.handleAdminPrinters)
	s.admin("GET /api/admin/printers/{name}", s.handleAdminPrinter)
	s.admin("PUT /api/admin/printers/{name}", s.handleAdminPrinter)
	s.admin("DELETE /api/admin/printers/{name}", s.handleAdminPrinter)
	s.admin("POST /api/admin/printers/{name}/pause", s.handlePrinterAction)
	s.admin("POST /api/admin/printers/{name}/resume", s.handlePrinterAction)
	s.admin("POST /api/admin/printers/{name}/accept", s.handlePrinterAction)
	s.admin("POST /api/admin/printers/{name}/reject", s.handlePrinterAction)
	s.admin("POST /api/admin/printers/{name}/purge", s.handlePrinterAction)
	s.admin("POST /api/admin/printers/{name}/test", s.handlePrinterAction)
	s.admin("GET /api/admin/printers/{name}/ppd-options", s.handlePPDOptions)
	s.admin("PUT /api/admin/printers/{name}/ppd-options", s.handlePPDOptions)
	s.admin("GET /api/admin/discovery/printers", s.handleDiscoveryPrinters)
	s.admin("GET /api/admin/discovery/scanners", s.handleDiscoveryScanners)
	s.admin("POST /api/admin/printers/discover", s.handleDiscoverAndCreate)
	s.admin("GET /api/admin/drivers", s.handleDrivers)
	s.admin("POST /api/admin/drivers/install", s.handleDriverInstall)
	s.admin("POST /api/admin/drivers/{id}/uninstall", s.handleDriverUninstall)
	s.admin("POST /api/admin/drivers/{id}/refresh", s.handleDriverRefresh)
	s.admin("GET /api/admin/drivers/models", s.handleDriverModels)
	s.admin("GET /api/admin/drivers/{id}/models", s.handleDriverModels)
	s.admin("GET /api/admin/cups/status", s.handleCUPSStatus)
	s.admin("GET /api/admin/cups/settings", s.handleCUPSSettings)
	s.admin("PUT /api/admin/cups/settings", s.handleCUPSSettings)
	s.admin("GET /api/admin/cups/config", s.handleCUPSConfig)
	s.admin("PUT /api/admin/cups/config", s.handleCUPSConfig)
	s.admin("POST /api/admin/cups/reload", s.handleCUPSReload)
	s.admin("GET /api/admin/cups/logs", s.handleCUPSLogs)
	s.admin("GET /api/admin/jobs", s.handleAdminJobs)
	s.admin("POST /api/admin/jobs/{id}/cancel", s.handleAdminJobAction)
	s.admin("POST /api/admin/jobs/{id}/hold", s.handleAdminJobAction)
	s.admin("POST /api/admin/jobs/{id}/release", s.handleAdminJobAction)
	s.admin("POST /api/admin/jobs/{id}/restart", s.handleAdminJobAction)
	s.admin("GET /api/admin/settings", s.handleSettings)
	s.admin("PUT /api/admin/settings", s.handleSettings)
	s.admin("GET /api/admin/logs", s.handleLogs)
	s.admin("GET /api/admin/backup", s.handleBackup)
	s.admin("POST /api/admin/restore", s.handleBackup)

	// Static SPA files (must be last).
	s.mux.Handle("/", s.staticHandler())
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler {
	return s.loggingMiddleware(s.mux)
}

func (s *Server) admin(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.requireAdmin(h))
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Admin-Token")
		if token == "" {
			auth := r.Header.Get("Authorization")
			token = strings.TrimPrefix(auth, "Bearer ")
		}
		if s.cfg.AdminToken == "" {
			// Development mode: no token configured. Log once per request at most.
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AdminToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing ADMIN_TOKEN")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.logger.Printf("%s %s %s", clientIP(r), r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) staticHandler() http.Handler {
	if s.cfg.StaticDir == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
				http.NotFound(w, r)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"service": "print-portal", "message": "frontend not built"})
		})
	}
	fileServer := http.FileServer(http.Dir(s.cfg.StaticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			http.NotFound(w, r)
			return
		}
		clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if clean == "." {
			clean = ""
		}
		full := filepath.Join(s.cfg.StaticDir, clean)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback.
		index := filepath.Join(s.cfg.StaticDir, "index.html")
		if _, err := os.Stat(index); err == nil {
			http.ServeFile(w, r, index)
			return
		}
		http.NotFound(w, r)
	})
}

// ---- Helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func parseID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// splitLines is used by settings and log handlers.
func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "printer"
	}
	return out
}
