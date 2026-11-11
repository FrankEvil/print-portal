package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/printmode"
)

const printModeMaxUploadMB = 200

// handlePrintModeIDCard composes a front/back pair into one A4 PDF and
// registers it as a preview session. The print call then reuses preview_id.
func (s *Server) handlePrintModeIDCard(w http.ResponseWriter, r *http.Request) {
	maxBytes := int64(printModeMaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+10<<20)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "文件超过上传上限")
		return
	}
	front, frontHeader, err := r.FormFile("front")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file", "缺少身份证正面")
		return
	}
	defer front.Close()
	back, backHeader, err := r.FormFile("back")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file", "缺少身份证反面")
		return
	}
	defer back.Close()

	previewsDir := filepath.Join(s.cfg.DataDir, "previews")
	if err := os.MkdirAll(previewsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	dir, err := os.MkdirTemp(previewsDir, "idcard-")
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dir)
		}
	}()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	frontPath := filepath.Join(dir, "front"+imageExt(frontHeader.Filename))
	backPath := filepath.Join(dir, "back"+imageExt(backHeader.Filename))
	if err := saveMultipartFile(front, frontPath); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	if err := saveMultipartFile(back, backPath); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}

	opts := printmode.IDCardOptions{
		Layout: defaultStr(r.FormValue("layout"), "vertical"),
		Border: r.FormValue("border") == "1",
	}
	out := filepath.Join(dir, "idcard.pdf")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if err := printmode.ComposeIDCard(ctx, frontPath, backPath, out, dir, opts); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "compose_failed", err.Error())
		return
	}
	previewID := s.registerPreviewFile(out, "身份证.pdf")
	success = true
	writeJSON(w, http.StatusOK, map[string]any{
		"preview_id": previewID,
		"page_count": 1,
	})
}

// handlePrintModeInvoice composes one or more PDF/image invoices into A4
// sheets and registers the result as a preview session.
func (s *Server) handlePrintModeInvoice(w http.ResponseWriter, r *http.Request) {
	maxBytes := int64(printModeMaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+10<<20)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "文件超过上传上限")
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file", "请至少选择一个发票文件")
		return
	}
	var sizes []printmode.PageSize
	if raw := strings.TrimSpace(r.FormValue("image_sizes")); raw != "" {
		_ = json.Unmarshal([]byte(raw), &sizes)
	}

	previewsDir := filepath.Join(s.cfg.DataDir, "previews")
	if err := os.MkdirAll(previewsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	dir, err := os.MkdirTemp(previewsDir, "invoice-")
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dir)
		}
	}()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	var inputs []printmode.InvoiceInput
	for index, header := range files {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext != ".pdf" && ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
			writeError(w, http.StatusUnprocessableEntity, "invalid_file_type", "发票仅支持 PDF、JPG、PNG")
			return
		}
		path := filepath.Join(dir, fmt.Sprintf("invoice-%03d%s", index, ext))
		file, err := header.Open()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		if err := saveMultipartFile(file, path); err != nil {
			file.Close()
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		file.Close()
		kind := "image"
		if ext == ".pdf" {
			kind = "pdf"
		}
		input := printmode.InvoiceInput{Path: path, Name: header.Filename, Kind: kind}
		if kind == "image" {
			input.WidthMM, input.HeightMM = 210, 140
			if index < len(sizes) && sizes[index].WidthMM > 0 && sizes[index].HeightMM > 0 {
				input.WidthMM, input.HeightMM = sizes[index].WidthMM, sizes[index].HeightMM
			}
		}
		inputs = append(inputs, input)
	}

	opts := printmode.InvoiceOptions{
		PerSheet: atoiDefault(r.FormValue("per_sheet"), 1),
		Trim:     r.FormValue("trim") != "0",
		CutLines: r.FormValue("cut_lines") == "1",
	}
	out := filepath.Join(dir, "invoice.pdf")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	result, err := printmode.ComposeInvoice(ctx, inputs, out, dir, opts)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "compose_failed", err.Error())
		return
	}
	previewID := s.registerPreviewFile(out, "发票.pdf")
	success = true
	writeJSON(w, http.StatusOK, map[string]any{
		"preview_id": previewID,
		"page_count": result.PageCount,
		"items":      result.Items,
		"sheets":     result.Sheets,
	})
}

// handlePrintModeInspect returns page sizes so the UI can show invoice
// metadata before composing.
func (s *Server) handlePrintModeInspect(w http.ResponseWriter, r *http.Request) {
	maxBytes := int64(printModeMaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+10<<20)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "文件超过上传上限")
		return
	}
	files := r.MultipartForm.File["files"]
	previewsDir := filepath.Join(s.cfg.DataDir, "previews")
	if err := os.MkdirAll(previewsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	dir, err := os.MkdirTemp(previewsDir, "inspect-")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	defer os.RemoveAll(dir)

	var out []printmode.InvoiceItemInfo
	for index, header := range files {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		kind := "image"
		if ext == ".pdf" {
			kind = "pdf"
		}
		path := filepath.Join(dir, fmt.Sprintf("item-%03d%s", index, ext))
		file, err := header.Open()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		if err := saveMultipartFile(file, path); err != nil {
			file.Close()
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		file.Close()
		pages, err := printmode.InspectInvoice(path, kind)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "inspect_failed", err.Error())
			return
		}
		out = append(out, printmode.InvoiceItemInfo{Name: header.Filename, Kind: kind, Pages: pages})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handlePrintModePreview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.previewMu.Lock()
	session, ok := s.previewSessions[id]
	if ok {
		session.lastUsed = time.Now()
	}
	s.previewMu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "预览不存在或已过期")
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, session.path)
}

func (s *Server) registerPreviewFile(path, name string) string {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	s.previewMu.Lock()
	s.previewSessions[id] = &previewSession{path: path, name: name, lastUsed: time.Now()}
	s.previewMu.Unlock()
	return id
}

func saveMultipartFile(file multipart.File, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, file)
	return err
}

func imageExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png":
		return ext
	default:
		return ".jpg"
	}
}
