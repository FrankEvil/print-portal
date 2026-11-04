package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/scan"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	running, err := s.cups.SchedulerRunning(ctx)
	cupsState := "unknown"
	if err == nil {
		if running {
			cupsState = "running"
		} else {
			cupsState = "stopped"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": s.cfg.Version,
		"time":    time.Now().Format(time.RFC3339),
		"cups":    cupsState,
	})
}

type publicPrinter struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Model        string   `json:"model"`
	Queue        string   `json:"queue"`
	IsDefault    bool     `json:"is_default"`
	State        string   `json:"state"`
	StateReasons []string `json:"state_reasons"`
}

func (s *Server) handleListPrinters(w http.ResponseWriter, r *http.Request) {
	printers, err := s.store.ListPrinters()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	statuses, _ := s.cups.ListPrinters(ctx)
	statusByQueue := map[string]string{}
	for _, st := range statuses {
		statusByQueue[st.Name] = st.State
	}
	out := make([]publicPrinter, 0, len(printers))
	for _, p := range printers {
		state := statusByQueue[p.PPDQueue]
		if state == "" {
			state = "unknown"
		}
		out = append(out, publicPrinter{
			ID: p.ID, Name: p.Name, Model: p.Model, Queue: p.PPDQueue,
			IsDefault: p.IsDefault, State: state, StateReasons: []string{},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"printers": out})
}

func (s *Server) handlePublicPPDOptions(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	p, err := s.store.GetPrinter(id)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "not_found", "printer not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	opts, err := s.cups.PPDOptions(ctx, p.PPDQueue)
	if err != nil {
		writeError(w, http.StatusConflict, "ppd_options_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": opts})
}

var allowedExt = map[string]bool{
	".pdf": true, ".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
	".ps": true, ".odt": true, ".ods": true, ".odp": true,
}

func (s *Server) handlePrint(w http.ResponseWriter, r *http.Request) {
	maxMB := s.cfg.MaxUploadMB
	if v, ok, _ := s.store.GetSetting("max_upload_mb"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxMB = n
		}
	}
	maxBytes := int64(maxMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+10<<20)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "file exceeds the upload limit")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file", "file is required")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedExt[ext] {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file_type", "仅支持 PDF、JPEG、PNG、WebP、Office 文档、PostScript")
		return
	}
	printerID := int64(atoiDefault(r.FormValue("printer_id"), 0))
	if printerID <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_printer", "printer_id is required")
		return
	}
	p, err := s.store.GetPrinter(printerID)
	if err != nil || p == nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_printer", "printer not found")
		return
	}

	filename := sanitizeFilename(header.Filename)
	_ = os.MkdirAll(filepath.Join(s.cfg.DataDir, "uploads"), 0o755)
	dst := filepath.Join(s.cfg.DataDir, "uploads", fmt.Sprintf("%d-%s", time.Now().UnixNano(), filename))
	out, err := os.Create(dst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		_ = os.Remove(dst)
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	out.Close()

	copies := atoiDefault(r.FormValue("copies"), 1)
	if copies < 1 {
		copies = 1
	}
	if copies > 99 {
		copies = 99
	}
	j := &storage.Job{
		PrinterID:   printerID,
		Source:      "web",
		Filename:    filename,
		SourceIP:    clientIP(r),
		PageRange:   strings.TrimSpace(r.FormValue("page_range")),
		Duplex:      defaultStr(r.FormValue("duplex"), "one-sided"),
		Copies:      copies,
		PaperSize:   defaultStr(r.FormValue("paper_size"), "A4"),
		Orientation: defaultStr(r.FormValue("orientation"), "portrait"),
		ColorMode:   defaultStr(r.FormValue("color_mode"), "monochrome"),
		Scaling:     defaultStr(r.FormValue("scaling"), "auto"),
		Status:      "pending",
		StoredPath:  dst,
	}
	if err := s.store.CreateJob(j); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	s.jobs.Wake()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id":   j.ID,
		"status":   "pending",
		"filename": filename,
		"printer":  p.Name,
	})
}

type jobResponse struct {
	storage.Job
	PrinterName string `json:"printer_name"`
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	limit := atoiDefault(r.URL.Query().Get("limit"), 100)
	offset := atoiDefault(r.URL.Query().Get("offset"), 0)
	jobs, err := s.store.ListJobs(storage.JobFilter{
		Status: r.URL.Query().Get("status"),
		Source: r.URL.Query().Get("source"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.enrichJobs(jobs)})
}

func (s *Server) enrichJobs(jobs []storage.Job) []jobResponse {
	printers, _ := s.store.ListPrinters()
	names := map[int64]string{}
	for _, p := range printers {
		names[p.ID] = p.Name
	}
	out := make([]jobResponse, 0, len(jobs))
	for _, j := range jobs {
		j.StoredPath = ""
		out = append(out, jobResponse{Job: j, PrinterName: names[j.PrinterID]})
	}
	return out
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	j, err := s.store.GetJob(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if j == nil {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	j.StoredPath = ""
	resp := s.enrichJobs([]storage.Job{*j})
	writeJSON(w, http.StatusOK, resp[0])
}

func (s *Server) handleJobPreview(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	j, err := s.store.GetJob(id)
	if err != nil || j == nil {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	path := filepath.Join(s.cfg.DataDir, "jobs", fmt.Sprintf("%d", id), "preview.png")
	if _, err := os.Stat(path); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "preview not available")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := s.jobs.Cancel(r.Context(), id); err != nil {
		writeError(w, http.StatusConflict, "cancel_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	j, err := s.jobs.Retry(id)
	if err != nil {
		writeError(w, http.StatusConflict, "retry_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": j.ID, "status": "pending"})
}

func (s *Server) handleScanners(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	scanners, err := s.cups.ListScanners(ctx)
	if err != nil {
		writeError(w, http.StatusConflict, "scanner_discovery_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scanners": scanners})
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	var params scan.Params
	if err := decodeJSON(r, &params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	job, err := s.scans.Scan(ctx, params)
	if err != nil {
		writeError(w, http.StatusConflict, "scan_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) handleGetScan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	job, err := s.store.GetScanJob(id)
	if err != nil || job == nil {
		writeError(w, http.StatusNotFound, "not_found", "scan not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleDownloadScan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	job, path, err := s.scans.File(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if r.URL.Query().Get("inline") != "1" {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+job.Filename+"\"")
	}
	http.ServeFile(w, r, path)
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		name = "upload"
	}
	return name
}

func defaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func atoiDefault(v string, fallback int) int {
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return fallback
	}
	return n
}
