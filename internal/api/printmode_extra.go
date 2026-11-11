package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/layout"
	"github.com/liuzhaojun/print-portal/internal/printmode"
)

// resolvePhotoInputs builds the ordered photo inputs from uploaded files and
// NAS paths. item_sources keeps the two sources aligned with the frontend's
// item order.
func (s *Server) resolvePhotoInputs(r *http.Request, dir string) ([]printmode.PhotoInput, error) {
	files := r.MultipartForm.File["files"]
	var nasPaths, itemSources []string
	_ = json.Unmarshal([]byte(r.FormValue("nas_paths")), &nasPaths)
	_ = json.Unmarshal([]byte(r.FormValue("item_sources")), &itemSources)
	total := len(files)
	if len(nasPaths) > total {
		total = len(nasPaths)
	}
	if len(itemSources) > total {
		total = len(itemSources)
	}
	if total == 0 {
		return nil, fmt.Errorf("请至少选择一张图片")
	}
	var inputs []printmode.PhotoInput
	fileIndex := 0
	for index := 0; index < total; index++ {
		source := "upload"
		if index < len(itemSources) && strings.ToLower(itemSources[index]) == "nas" {
			source = "nas"
		}
		if source == "nas" {
			path := ""
			if index < len(nasPaths) {
				path = strings.TrimSpace(nasPaths[index])
			}
			if path == "" {
				return nil, fmt.Errorf("缺少 NAS 路径")
			}
			real, err := filepath.EvalSymlinks(path)
			if err != nil || !s.allowedNASPath(real) {
				return nil, fmt.Errorf("NAS 文件不在允许的目录内")
			}
			ext := strings.ToLower(filepath.Ext(real))
			if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
				return nil, fmt.Errorf("图片打印仅支持 JPG、PNG、WebP")
			}
			inputs = append(inputs, printmode.PhotoInput{Path: real, Name: filepath.Base(real)})
			continue
		}
		if fileIndex >= len(files) {
			return nil, fmt.Errorf("缺少图片文件")
		}
		header := files[fileIndex]
		fileIndex++
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
			return nil, fmt.Errorf("图片打印仅支持 JPG、PNG、WebP")
		}
		path := filepath.Join(dir, fmt.Sprintf("source-%03d%s", index, ext))
		file, err := header.Open()
		if err != nil {
			return nil, err
		}
		if err := saveMultipartFile(file, path); err != nil {
			file.Close()
			return nil, err
		}
		file.Close()
		inputs = append(inputs, printmode.PhotoInput{Path: path, Name: header.Filename})
	}
	return inputs, nil
}

// handlePrintModePhotos composes one or more images into photo paper sheets.
func (s *Server) handlePrintModePhotos(w http.ResponseWriter, r *http.Request) {
	maxBytes := int64(printModeMaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+10<<20)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "文件超过上传上限")
		return
	}
	previewsDir := filepath.Join(s.cfg.DataDir, "previews")
	if err := os.MkdirAll(previewsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	dir, err := os.MkdirTemp(previewsDir, "photos-")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dir)
		}
	}()
	inputs, err := s.resolvePhotoInputs(r, dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file", err.Error())
		return
	}
	opts := printmode.PhotoOptions{
		PaperWidthMM:  parseFloat(r.FormValue("paper_width_mm"), 210),
		PaperHeightMM: parseFloat(r.FormValue("paper_height_mm"), 297),
		PerSheet:      atoiDefault(r.FormValue("per_sheet"), 4),
		Arrangement:   defaultStr(r.FormValue("arrangement"), "auto"),
		Scaling:       defaultStr(r.FormValue("scaling"), "fit"),
		Borderless:    r.FormValue("borderless") == "1",
		MarginMM:      parseFloat(r.FormValue("margin_mm"), 4),
		CutLines:      r.FormValue("cut_lines") == "1",
	}
	out := filepath.Join(dir, "photos.pdf")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	result, err := printmode.ComposePhotos(ctx, inputs, out, dir, opts)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "compose_failed", err.Error())
		return
	}
	previewID := s.registerPreviewFile(out, "图片打印.pdf")
	success = true
	writeJSON(w, http.StatusOK, map[string]any{"preview_id": previewID, "page_count": result.PageCount})
}

// handlePrintModePhotosManual composes a manual collage layout.
func (s *Server) handlePrintModePhotosManual(w http.ResponseWriter, r *http.Request) {
	maxBytes := int64(printModeMaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+10<<20)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "文件超过上传上限")
		return
	}
	var layout printmode.PhotoLayout
	if err := json.Unmarshal([]byte(r.FormValue("layout")), &layout); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_layout", "拼贴版面格式错误")
		return
	}
	previewsDir := filepath.Join(s.cfg.DataDir, "previews")
	if err := os.MkdirAll(previewsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	dir, err := os.MkdirTemp(previewsDir, "collage-")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dir)
		}
	}()
	inputs, err := s.resolvePhotoInputs(r, dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file", err.Error())
		return
	}
	out := filepath.Join(dir, "collage.pdf")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	result, err := printmode.ComposePhotoLayout(ctx, inputs, layout, out, dir,
		parseFloat(r.FormValue("paper_width_mm"), 210), parseFloat(r.FormValue("paper_height_mm"), 297))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "compose_failed", err.Error())
		return
	}
	previewID := s.registerPreviewFile(out, "图片拼贴.pdf")
	success = true
	writeJSON(w, http.StatusOK, map[string]any{"preview_id": previewID, "page_count": result.PageCount})
}

// handlePrintModeBookletGeometry calculates booklet page order without
// uploading the PDF. The browser can then render the sheets locally.
func (s *Server) handlePrintModeBookletGeometry(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PageCount      int    `json:"page_count"`
		SignaturePages int    `json:"signature_pages"`
		PageRange      string `json:"page_range"`
		PageParity     string `json:"page_parity"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if body.PageCount <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_page_count", "page_count is required")
		return
	}
	selected := layout.SelectPages(body.PageCount, body.PageRange, normalizePageParity(body.PageParity))
	if len(selected) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "empty_page_range", "页码范围没有匹配的页面")
		return
	}
	geometry := printmode.BuildBookletGeometry(body.PageCount, selected, body.SignaturePages)
	writeJSON(w, http.StatusOK, geometry)
}

// handlePrintModeBooklet composes a PDF into saddle-stitch booklet sides.
func (s *Server) handlePrintModeBooklet(w http.ResponseWriter, r *http.Request) {
	maxBytes := int64(printModeMaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+10<<20)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "文件超过上传上限")
		return
	}
	previewsDir := filepath.Join(s.cfg.DataDir, "previews")
	if err := os.MkdirAll(previewsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	dir, err := os.MkdirTemp(previewsDir, "booklet-")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dir)
		}
	}()
	var src string
	if file, _, err := r.FormFile("file"); err == nil {
		defer file.Close()
		src = filepath.Join(dir, "source.pdf")
		if err := saveMultipartFile(file, src); err != nil {
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
	} else if nasPath := strings.TrimSpace(r.FormValue("nas_path")); nasPath != "" {
		real, err := filepath.EvalSymlinks(nasPath)
		if err != nil || !s.allowedNASPath(real) {
			writeError(w, http.StatusForbidden, "path_forbidden", "NAS 文件不在允许的目录内")
			return
		}
		if strings.ToLower(filepath.Ext(real)) != ".pdf" {
			writeError(w, http.StatusUnprocessableEntity, "invalid_file_type", "书籍打印只支持 PDF")
			return
		}
		src = real
	} else {
		writeError(w, http.StatusUnprocessableEntity, "invalid_file", "请选择一个 PDF")
		return
	}
	opts := printmode.BookletOptions{
		PaperWidthMM:   parseFloat(r.FormValue("paper_width_mm"), 297),
		PaperHeightMM:  parseFloat(r.FormValue("paper_height_mm"), 210),
		SignaturePages: atoiDefault(r.FormValue("signature_pages"), 0),
		CutLines:       r.FormValue("cut_lines") == "1",
		PageRange:      strings.TrimSpace(r.FormValue("page_range")),
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	result, err := printmode.ComposeBooklet(ctx, src, filepath.Join(dir, "booklet.pdf"), filepath.Join(dir, "front.pdf"), filepath.Join(dir, "back.pdf"), dir, opts)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "compose_failed", err.Error())
		return
	}
	previewID := s.registerPreviewFile(result.PreviewPDF, "书籍打印.pdf")
	frontID := s.registerPreviewFile(result.FrontPDF, "书籍正面.pdf")
	backID := s.registerPreviewFile(result.BackPDF, "书籍反面.pdf")
	success = true
	writeJSON(w, http.StatusOK, map[string]any{
		"preview_id": previewID, "front_preview_id": frontID, "back_preview_id": backID,
		"page_count": result.PageCount, "sheet_count": result.SheetCount,
	})
}

func parseFloat(value string, fallback float64) float64 {
	if n, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && n > 0 {
		return n
	}
	return fallback
}
