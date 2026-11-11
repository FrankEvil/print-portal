package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/convert"
	"github.com/liuzhaojun/print-portal/internal/layout"
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

func (s *Server) handlePublicPaperAreas(w http.ResponseWriter, r *http.Request) {
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
	areas, err := s.cups.PaperAreas(ctx, p.PPDQueue)
	if err != nil {
		writeError(w, http.StatusConflict, "paper_areas_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"areas": areas})
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

	// A file already uploaded for the preview session is reused instead of
	// being sent again. On a slow link the document can be tens of megabytes,
	// so a second upload would double the wait before printing starts.
	var filename string
	var ext string
	var srcPath string
	var header *multipart.FileHeader
	var file multipart.File
	if previewID := strings.TrimSpace(r.FormValue("preview_id")); previewID != "" {
		s.previewMu.Lock()
		session, ok := s.previewSessions[previewID]
		if ok {
			session.lastUsed = time.Now()
			srcPath = session.path
			filename = sanitizeFilename(session.name)
		}
		s.previewMu.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "preview_not_found", "preview session not found or expired")
			return
		}
		ext = strings.ToLower(filepath.Ext(srcPath))
	} else {
		uploaded, fileHeader, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_file", "file or preview_id is required")
			return
		}
		defer uploaded.Close()
		file = uploaded
		header = fileHeader
		filename = sanitizeFilename(header.Filename)
		ext = strings.ToLower(filepath.Ext(header.Filename))
		if !allowedExt[ext] {
			writeError(w, http.StatusUnprocessableEntity, "invalid_file_type", "仅支持 PDF、JPEG、PNG、WebP、Office 文档、PostScript")
			return
		}
	}
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

	_ = os.MkdirAll(filepath.Join(s.cfg.DataDir, "uploads"), 0o755)
	dst := filepath.Join(s.cfg.DataDir, "uploads", fmt.Sprintf("%d-%s", time.Now().UnixNano(), filename))
	if srcPath != "" {
		// Copy from the preview cache so the job keeps its own file even after
		// the preview session expires.
		if err := copyLocalFile(srcPath, dst); err != nil {
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
	} else {
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
	}
	_ = header

	copies := atoiDefault(r.FormValue("copies"), 1)
	if copies < 1 {
		copies = 1
	}
	if copies > 99 {
		copies = 99
	}
	j := &storage.Job{
		PrinterID:     printerID,
		Source:        "web",
		Filename:      filename,
		SourceIP:      clientIP(r),
		PageRange:     strings.TrimSpace(r.FormValue("page_range")),
		Duplex:        defaultStr(r.FormValue("duplex"), "one-sided"),
		Copies:        copies,
		PaperSize:     defaultStr(r.FormValue("paper_size"), "A4"),
		Orientation:   defaultStr(r.FormValue("orientation"), "portrait"),
		ColorMode:     defaultStr(r.FormValue("color_mode"), "monochrome"),
		Scaling:       defaultStr(r.FormValue("scaling"), "auto"),
		PPDOptions:    normalizePPDOptions(r.FormValue("ppd_options")),
		LayoutOptions: normalizeLayoutOptions(r.FormValue("layout_options")),
		Status:        "pending",
		StoredPath:    dst,
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

// handlePreviewSession stores one uploaded document for repeated preview
// renders. Uploading and converting a PDF/Office file once makes parameter
// changes much faster than re-uploading it for every preview request.
func (s *Server) handlePreviewSession(w http.ResponseWriter, r *http.Request) {
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
	dir := filepath.Join(s.cfg.DataDir, "previews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	rawPath := filepath.Join(dir, id+ext)
	tmp, err := os.Create(rawPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		os.Remove(rawPath)
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(rawPath)
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	previewPath := rawPath
	if ext != ".pdf" {
		previewPath = filepath.Join(dir, id+".pdf")
		conv := convert.New(dir)
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		if err := conv.ToPDF(ctx, rawPath, previewPath); err != nil {
			os.Remove(rawPath)
			writeError(w, http.StatusUnprocessableEntity, "preview_failed", err.Error())
			return
		}
		_ = os.Remove(rawPath)
	}
	s.previewMu.Lock()
	s.previewSessions[id] = &previewSession{path: previewPath, name: header.Filename, lastUsed: time.Now()}
	s.previewMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"preview_id": id})
}

// handlePreview renders a single-page print-layout preview. It accepts a
// preview session id, an uploaded file (multipart "file") or an authorized NAS
// path ("nas_path"), then applies paper size, orientation and color mode.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
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
	var src string
	var cleanup func()
	if previewID := strings.TrimSpace(r.FormValue("preview_id")); previewID != "" {
		s.previewMu.Lock()
		session, ok := s.previewSessions[previewID]
		if ok {
			session.lastUsed = time.Now()
			src = session.path
		}
		s.previewMu.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "preview_not_found", "preview session not found or expired")
			return
		}
	} else if file, header, err := r.FormFile("file"); err == nil {
		defer file.Close()
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if !allowedExt[ext] {
			writeError(w, http.StatusUnprocessableEntity, "invalid_file_type", "仅支持 PDF、JPEG、PNG、WebP、Office 文档、PostScript")
			return
		}
		dir := filepath.Join(s.cfg.DataDir, "previews")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		tmp, err := os.CreateTemp(dir, "src-*"+ext)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		if _, err := io.Copy(tmp, file); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		if err := tmp.Close(); err != nil {
			os.Remove(tmp.Name())
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		src = tmp.Name()
		cleanup = func() { _ = os.Remove(src) }
	} else if raw := strings.TrimSpace(r.FormValue("nas_path")); raw != "" {
		abs, err := filepath.Abs(filepath.Clean(raw))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_path", "invalid path")
			return
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil || !s.allowedNASPath(real) {
			writeError(w, http.StatusForbidden, "path_forbidden", "path is outside the configured NAS roots")
			return
		}
		info, err := os.Stat(real)
		if err != nil || !info.Mode().IsRegular() {
			writeError(w, http.StatusNotFound, "not_found", "file not found")
			return
		}
		if !allowedExt[strings.ToLower(filepath.Ext(real))] {
			writeError(w, http.StatusUnprocessableEntity, "invalid_file_type", "unsupported file type")
			return
		}
		src = real
	} else {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file", "file or nas_path is required")
		return
	}
	if cleanup != nil {
		defer cleanup()
	}
	opts := convert.PreviewOptions{
		PaperSize:   defaultStr(r.FormValue("paper_size"), "A4"),
		Orientation: defaultStr(r.FormValue("orientation"), "portrait"),
		ColorMode:   defaultStr(r.FormValue("color_mode"), "monochrome"),
		Scaling:     defaultStr(r.FormValue("scaling"), "auto"),
		Page:        atoiDefault(r.FormValue("page"), 1),
	}
	outDir := filepath.Join(s.cfg.DataDir, "previews")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	out := filepath.Join(outDir, fmt.Sprintf("preview-%d.png", time.Now().UnixNano()))
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	conv := convert.New(outDir)
	if err := conv.PreviewPNG(ctx, src, out, opts); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "preview_failed", err.Error())
		return
	}
	defer os.Remove(out)
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, out)
}

func (s *Server) handleNASPreview(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "invalid_path", "path is required")
		return
	}
	abs, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "invalid path")
		return
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil || !s.allowedNASPath(real) {
		writeError(w, http.StatusForbidden, "path_forbidden", "path is outside the configured NAS roots")
		return
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	ext := strings.ToLower(filepath.Ext(real))
	if !allowedExt[ext] {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file_type", "unsupported file type")
		return
	}
	f, err := os.Open(real)
	if err != nil {
		writeError(w, http.StatusForbidden, "read_failed", "cannot read file")
		return
	}
	defer f.Close()
	if ct := mime.TypeByExtension(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, filepath.Base(real), info.ModTime(), f)
}

func (s *Server) handlePrintNAS(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PrinterID     int64  `json:"printer_id"`
		Path          string `json:"path"`
		PageRange     string `json:"page_range"`
		Duplex        string `json:"duplex"`
		Copies        int    `json:"copies"`
		PaperSize     string `json:"paper_size"`
		Orientation   string `json:"orientation"`
		ColorMode     string `json:"color_mode"`
		Scaling       string `json:"scaling"`
		PPDOptions    string `json:"ppd_options"`
		LayoutOptions string `json:"layout_options"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if body.PrinterID <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_printer", "printer_id is required")
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_path", "path is required")
		return
	}
	p, err := s.store.GetPrinter(body.PrinterID)
	if err != nil || p == nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_printer", "printer not found")
		return
	}
	src, err := filepath.Abs(filepath.Clean(body.Path))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_path", "invalid path")
		return
	}
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_path", "file not found")
		return
	}
	if !s.allowedNASPath(real) {
		writeError(w, http.StatusForbidden, "path_forbidden", "path is outside the configured NAS roots")
		return
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusUnprocessableEntity, "invalid_path", "file not found")
		return
	}
	filename := sanitizeFilename(filepath.Base(real))
	ext := strings.ToLower(filepath.Ext(filename))
	if !allowedExt[ext] {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file_type", "仅支持 PDF、JPEG、PNG、WebP、Office 文档、PostScript")
		return
	}
	srcFile, err := os.Open(real)
	if err != nil {
		writeError(w, http.StatusForbidden, "read_failed", "cannot read selected file")
		return
	}
	defer srcFile.Close()
	_ = os.MkdirAll(filepath.Join(s.cfg.DataDir, "uploads"), 0o755)
	dst := filepath.Join(s.cfg.DataDir, "uploads", fmt.Sprintf("%d-%s", time.Now().UnixNano(), filename))
	out, err := os.Create(dst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	if _, err := io.Copy(out, srcFile); err != nil {
		out.Close()
		_ = os.Remove(dst)
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	out.Close()

	copies := body.Copies
	if copies < 1 {
		copies = 1
	}
	if copies > 99 {
		copies = 99
	}
	j := &storage.Job{
		PrinterID: body.PrinterID, Source: "web", Filename: filename, SourceIP: clientIP(r),
		PageRange: strings.TrimSpace(body.PageRange), Duplex: defaultStr(body.Duplex, "one-sided"),
		Copies: copies, PaperSize: defaultStr(body.PaperSize, "A4"),
		Orientation: defaultStr(body.Orientation, "portrait"), ColorMode: defaultStr(body.ColorMode, "monochrome"),
		Scaling: defaultStr(body.Scaling, "auto"), PPDOptions: normalizePPDOptions(body.PPDOptions),
		LayoutOptions: normalizeLayoutOptions(body.LayoutOptions), Status: "pending", StoredPath: dst,
	}
	if err := s.store.CreateJob(j); err != nil {
		_ = os.Remove(dst)
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	s.jobs.Wake()
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": j.ID, "status": "pending", "filename": filename, "printer": p.Name})
}

func normalizeLayoutOptions(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "{}"
	}
	var values layout.Options
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return "{}"
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func normalizePPDOptions(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "{}"
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return "{}"
	}
	clean := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			clean[key] = value
		}
	}
	data, err := json.Marshal(clean)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func copyLocalFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func (s *Server) allowedNASPath(path string) bool {
	roots := strings.Split(s.cfg.NASRoots, ",")
	if len(roots) == 0 {
		return false
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(filepath.Clean(root))
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absRoot, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
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

// handleJobFile serves the converted PDF exactly as it was sent to CUPS.
func (s *Server) handleJobFile(w http.ResponseWriter, r *http.Request) {
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
	if j.Source != "web" {
		writeError(w, http.StatusNotFound, "not_found", "本地任务没有原始文件")
		return
	}
	// Serve exactly what was sent to CUPS: the laid-out PDF when layout was
	// applied, otherwise the plain conversion result.
	path := filepath.Join(s.cfg.DataDir, "jobs", fmt.Sprintf("%d", id), "layout.pdf")
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(s.cfg.DataDir, "jobs", fmt.Sprintf("%d", id), "converted.pdf")
	}
	if _, err := os.Stat(path); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "converted file not available")
		return
	}
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, max-age=300")
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
