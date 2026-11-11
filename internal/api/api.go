package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liuzhaojun/print-portal/internal/config"
	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/discovery"
	"github.com/liuzhaojun/print-portal/internal/driver"
	"github.com/liuzhaojun/print-portal/internal/job"
	"github.com/liuzhaojun/print-portal/internal/printer"
	"github.com/liuzhaojun/print-portal/internal/scan"
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
	scans     *scan.Service
	logger    *log.Logger
	mux       *http.ServeMux

	previewMu       sync.Mutex
	previewSessions map[string]*previewSession
}

type previewSession struct {
	path     string
	name     string
	lastUsed time.Time
}

// SweepPreviewSessions drops preview sessions unused for longer than maxAge
// and removes their files. Files in the previews directory that no session
// references are leftovers from a crash or restart and are removed as well.
func (s *Server) SweepPreviewSessions(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	active := map[string]bool{}
	s.previewMu.Lock()
	for id, session := range s.previewSessions {
		if session.lastUsed.Before(cutoff) {
			_ = os.Remove(session.path)
			// Mode previews live in a per-composition directory holding the
			// uploaded source images; remove that as well.
			if base := filepath.Base(filepath.Dir(session.path)); strings.HasPrefix(base, "idcard-") || strings.HasPrefix(base, "invoice-") {
				_ = os.RemoveAll(filepath.Dir(session.path))
			}
			delete(s.previewSessions, id)
			removed++
			continue
		}
		active[filepath.Clean(session.path)] = true
	}
	s.previewMu.Unlock()

	dir := filepath.Join(s.cfg.DataDir, "previews")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return removed
	}
	for _, entry := range entries {
		full := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), "idcard-") || strings.HasPrefix(entry.Name(), "invoice-") || strings.HasPrefix(entry.Name(), "inspect-") {
				if info, err := entry.Info(); err == nil && info.ModTime().Before(cutoff) {
					_ = os.RemoveAll(full)
				}
			}
			continue
		}
		if active[filepath.Clean(full)] {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(full)
	}
	return removed
}

func New(
	cfg config.Config,
	store *storage.Store,
	cupsClient *cups.Client,
	jobs *job.Manager,
	printers *printer.Service,
	drivers *driver.Service,
	discoverySvc *discovery.Service,
	scanSvc *scan.Service,
	logger *log.Logger,
) *Server {
	s := &Server{
		cfg:             cfg,
		store:           store,
		cups:            cupsClient,
		jobs:            jobs,
		printers:        printers,
		drivers:         drivers,
		discovery:       discoverySvc,
		scans:           scanSvc,
		logger:          logger,
		mux:             http.NewServeMux(),
		previewSessions: map[string]*previewSession{},
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Public API.
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /api/auth/config", s.handleAuthConfig)
	s.mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	s.mux.HandleFunc("GET /api/printers", s.handleListPrinters)
	s.mux.HandleFunc("GET /api/printers/{id}/ppd-options", s.handlePublicPPDOptions)
	s.mux.HandleFunc("GET /api/printers/{id}/paper-areas", s.handlePublicPaperAreas)
	s.mux.HandleFunc("POST /api/print", s.handlePrint)
	s.mux.HandleFunc("POST /api/print/nas", s.handlePrintNAS)
	s.mux.HandleFunc("POST /api/preview", s.handlePreview)
	s.mux.HandleFunc("POST /api/preview/session", s.handlePreviewSession)
	s.mux.HandleFunc("POST /api/preview/geometry", s.handlePreviewGeometry)
	s.mux.HandleFunc("POST /api/printmode/idcard", s.handlePrintModeIDCard)
	s.mux.HandleFunc("POST /api/printmode/invoice", s.handlePrintModeInvoice)
	s.mux.HandleFunc("POST /api/printmode/inspect", s.handlePrintModeInspect)
	s.mux.HandleFunc("GET /api/printmode/preview/{id}", s.handlePrintModePreview)
	s.mux.HandleFunc("GET /api/nas/preview", s.handleNASPreview)
	s.mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	s.mux.HandleFunc("GET /api/jobs/{id}", s.handleGetJob)
	s.mux.HandleFunc("GET /api/jobs/{id}/preview", s.handleJobPreview)
	s.mux.HandleFunc("GET /api/jobs/{id}/file", s.handleJobFile)
	s.mux.HandleFunc("DELETE /api/jobs/{id}", s.handleCancelJob)
	s.mux.HandleFunc("POST /api/jobs/{id}/retry", s.handleRetryJob)
	s.mux.HandleFunc("GET /api/scanners", s.handleScanners)
	s.mux.HandleFunc("POST /api/scan", s.handleScan)
	s.mux.HandleFunc("GET /api/scans/{id}", s.handleGetScan)
	s.mux.HandleFunc("GET /api/scans/{id}/download", s.handleDownloadScan)

	// Admin API (ADMIN_TOKEN).
	s.admin("GET /api/admin/overview", s.handleOverview)
	s.admin("GET /api/admin/sharing", s.handleSharing)
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
	s.admin("GET /api/admin/printers/{name}/maintenance", s.handlePrinterMaintenance)
	s.admin("POST /api/admin/printers/{name}/maintenance/{action}", s.handlePrinterMaintenance)
	s.admin("GET /api/admin/discovery/printers", s.handleDiscoveryPrinters)
	s.admin("POST /api/admin/discovery/scan", s.handleDiscoveryScanStart)
	s.admin("GET /api/admin/discovery/scan/{id}", s.handleDiscoveryScanStatus)
	s.admin("GET /api/admin/discovery/scanners", s.handleDiscoveryScanners)
	s.admin("POST /api/admin/printers/discover", s.handleDiscoverAndCreate)
	s.admin("GET /api/admin/drivers", s.handleDrivers)
	s.admin("POST /api/admin/drivers/install", s.handleDriverInstall)
	s.admin("POST /api/admin/drivers/refresh", s.handleDriverRefresh)
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
	s.admin("DELETE /api/admin/jobs", s.handleAdminJobs)
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

// Handler returns the root handler for the TCP listener.
func (s *Server) Handler() http.Handler {
	return s.buildHandler(false)
}

// SocketHandler returns the root handler for the fnOS gateway Unix socket.
// fnOS injects identity headers there, so only this listener may trust them.
func (s *Server) SocketHandler() http.Handler {
	return s.buildHandler(true)
}

func (s *Server) buildHandler(trustedSocket bool) http.Handler {
	return s.loggingMiddleware(s.stripGatewayPrefix(s.authMiddleware(trustedSocket, s.mux)))
}

func (s *Server) stripGatewayPrefix(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthMode == "fnos" && s.cfg.FnOSPrefix != "" {
			prefix := strings.TrimRight(s.cfg.FnOSPrefix, "/")
			if strings.HasPrefix(r.URL.Path, prefix) {
				r2 := r.Clone(r.Context())
				u := *r.URL
				u.Path = strings.TrimPrefix(u.Path, prefix)
				if u.Path == "" {
					u.Path = "/"
				}
				r2.URL = &u
				r = r2
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(trustedSocket bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Empty mode is the legacy/test default; production always sets token
		// or fnos via config.Load.
		if s.cfg.AuthMode == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Static files are public so the login page can load; all API routes
		// except the two auth endpoints require authentication.
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/auth/config" || r.URL.Path == "/api/auth/login" {
			next.ServeHTTP(w, r)
			return
		}
		if s.cfg.AuthMode == "fnos" {
			// X-Trim-* headers are plain HTTP headers. They are only
			// trustworthy when the request comes from the fnOS gateway over a
			// Unix socket; a LAN client could otherwise forge them by calling
			// port 8088 directly.
			if !trustedSocket {
				writeError(w, http.StatusUnauthorized, "fnos_gateway_required", "请从 fnOS 应用中心打开")
				return
			}
			if strings.TrimSpace(r.Header.Get("X-Trim-Userid")) == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized", "fnOS login required")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if s.cfg.AdminToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(tokenFromRequest(r)), []byte(s.cfg.AdminToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func tokenFromRequest(r *http.Request) string {
	token := r.Header.Get("X-Admin-Token")
	if token == "" {
		auth := r.Header.Get("Authorization")
		token = strings.TrimPrefix(auth, "Bearer ")
	}
	return token
}

func (s *Server) admin(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.requireAdmin(h))
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthMode == "fnos" {
			if strings.TrimSpace(r.Header.Get("X-Trim-Userid")) == "" || r.Header.Get("X-Trim-Isadmin") != "true" {
				writeError(w, http.StatusForbidden, "forbidden", "fnOS administrator required")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if s.cfg.AdminToken == "" {
			// Development mode: no token configured.
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(tokenFromRequest(r)), []byte(s.cfg.AdminToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing ADMIN_TOKEN")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":          s.cfg.AuthMode,
		"token_login":   s.cfg.AuthMode != "fnos",
		"auth_required": true,
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AuthMode == "fnos" {
		writeError(w, http.StatusBadRequest, "not_supported", "fnOS authentication is handled by the gateway")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if s.cfg.AdminToken == "" || subtle.ConstantTimeCompare([]byte(body.Token), []byte(s.cfg.AdminToken)) == 1 {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}
	writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
}

func (s *Server) serveStaticFile(w http.ResponseWriter, r *http.Request, path, prefix string) {
	clean := filepath.Clean(strings.TrimPrefix(path, "/"))
	if clean == "." {
		clean = ""
	}
	full := filepath.Join(s.cfg.StaticDir, clean)
	if info, err := os.Stat(full); err == nil && !info.IsDir() {
		http.ServeFile(w, r, full)
		return
	}
	index := filepath.Join(s.cfg.StaticDir, "index.html")
	body, err := os.ReadFile(index)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if prefix != "" {
		body = bytes.ReplaceAll(body, []byte(`"/assets/`), []byte(`"`+prefix+`/assets/`))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	_, _ = w.Write(body)
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.logger.Printf("%s %s %s", clientIP(r), r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) fnosLoginURL(r *http.Request) string {
	base := strings.TrimRight(s.cfg.FnOSBaseURL, "/")
	if base == "" && s.cfg.FnOSPort == "" {
		return ""
	}
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if s.cfg.FnOSPort != "" {
			host = net.JoinHostPort(host, s.cfg.FnOSPort)
		}
		base = scheme + "://" + host
	}
	return base + "/" + strings.TrimLeft(s.cfg.FnOSPrefix, "/")
}

func (s *Server) staticHandler() http.Handler {
	if s.cfg.AuthMode == "fnos" && s.cfg.FnOSPrefix != "" {
		prefix := strings.TrimRight(s.cfg.FnOSPrefix, "/")
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if strings.HasPrefix(path, prefix) {
				path = strings.TrimPrefix(path, prefix)
				if path == "" {
					path = "/"
				}
			}
			if path == "/healthz" || strings.HasPrefix(path, "/api/") {
				http.NotFound(w, r)
				return
			}
			// Static assets are harmless and must load even if the gateway
			// does not forward identity headers on asset requests.
			if strings.HasPrefix(path, "/assets/") {
				s.serveStaticFile(w, r, path, prefix)
				return
			}
			if strings.TrimSpace(r.Header.Get("X-Trim-Userid")) == "" {
				if target := s.fnosLoginURL(r); target != "" {
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
				http.Error(w, "fnOS login required; open from the fnOS app center", http.StatusUnauthorized)
				return
			}
			s.serveStaticFile(w, r, path, prefix)
		})
	}
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
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
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
