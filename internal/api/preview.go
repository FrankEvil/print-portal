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

	"github.com/liuzhaojun/print-portal/internal/convert"
	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/layout"
	"github.com/liuzhaojun/print-portal/internal/preview"
)

// handlePreviewPrepare builds the print layout on the server and returns the
// sheet/page mapping. Rendering images is deferred to handlePreviewSheet so the
// first sheet can be displayed without waiting for the whole document.
func (s *Server) handlePreviewPrepare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PreviewID   string          `json:"preview_id"`
		NASPath     string          `json:"nas_path"`
		PrinterID   int64           `json:"printer_id"`
		Layout      json.RawMessage `json:"layout_options"`
		PageRange   string          `json:"page_range"`
		PaperSize   string          `json:"paper_size"`
		Orientation string          `json:"orientation"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	src, cleanup, srcErr := s.previewSource(body.PreviewID, body.NASPath)
	if srcErr != nil {
		writeError(w, srcErr.status, srcErr.code, srcErr.message)
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	opts := layout.Options{}
	if len(body.Layout) > 0 {
		if err := json.Unmarshal(body.Layout, &opts); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_layout", err.Error())
			return
		}
	}
	opts.PaperSize = defaultStr(body.PaperSize, defaultStr(opts.PaperSize, "A4"))
	opts.Orientation = defaultStr(body.Orientation, defaultStr(opts.Orientation, "portrait"))
	opts.PageRange = strings.TrimSpace(body.PageRange)

	var area *preview.Area
	if body.PrinterID > 0 {
		if p, printErr := s.store.GetPrinter(body.PrinterID); printErr == nil && p != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			areas, areaErr := s.cups.PaperAreas(ctx, p.PPDQueue)
			cancel()
			if areaErr == nil {
				area = pickArea(areas, opts.PaperSize, strings.EqualFold(opts.Orientation, "landscape"))
			}
		}
	}

	key, keyErr := fileKey(src)
	if keyErr != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", keyErr.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	result, prepErr := s.previews.Prepare(ctx, preview.Request{
		SourcePath: src,
		SourceKey:  key,
		Layout:     opts,
		Area:       area,
	})
	if prepErr != nil {
		writeError(w, http.StatusUnprocessableEntity, "preview_failed", prepErr.Error())
		return
	}
	s.previewMu.Lock()
	s.previewAreas[result.ID] = area
	s.previewMu.Unlock()
	writeJSON(w, http.StatusOK, result)
}

// handlePreviewSheet renders one sheet and serves it as a PNG.
func (s *Server) handlePreviewSheet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		writeError(w, http.StatusBadRequest, "invalid_index", "invalid sheet index")
		return
	}
	dpi := atoiDefault(r.URL.Query().Get("dpi"), 96)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	s.previewMu.Lock()
	area := s.previewAreas[id]
	s.previewMu.Unlock()
	path, err := s.previews.SheetImage(ctx, id, index, area, dpi)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "preview_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, path)
}

type previewError struct {
	status  int
	code    string
	message string
}

// previewSource resolves the document to preview from an upload session or an
// authorized NAS path.
func (s *Server) previewSource(previewID, nasPath string) (string, func(), *previewError) {
	if id := strings.TrimSpace(previewID); id != "" {
		s.previewMu.Lock()
		session, ok := s.previewSessions[id]
		if ok {
			session.lastUsed = time.Now()
		}
		s.previewMu.Unlock()
		if !ok {
			return "", nil, &previewError{http.StatusNotFound, "preview_not_found", "preview session not found or expired"}
		}
		return session.path, nil, nil
	}
	raw := strings.TrimSpace(nasPath)
	if raw == "" {
		return "", nil, &previewError{http.StatusUnprocessableEntity, "invalid_file", "preview_id or nas_path is required"}
	}
	abs, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", nil, &previewError{http.StatusBadRequest, "invalid_path", "invalid path"}
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil || !s.allowedNASPath(real) {
		return "", nil, &previewError{http.StatusForbidden, "path_forbidden", "path is outside the configured NAS roots"}
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, &previewError{http.StatusNotFound, "not_found", "file not found"}
	}
	if !allowedExt[strings.ToLower(filepath.Ext(real))] {
		return "", nil, &previewError{http.StatusUnprocessableEntity, "invalid_file_type", "unsupported file type"}
	}
	if strings.EqualFold(filepath.Ext(real), ".pdf") {
		return real, nil, nil
	}
	// Convert once into the preview cache so later requests reuse the PDF.
	dir := filepath.Join(s.cfg.DataDir, "previews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, &previewError{http.StatusInternalServerError, "storage_error", err.Error()}
	}
	dst := filepath.Join(dir, fmt.Sprintf("conv-%d.pdf", time.Now().UnixNano()))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := convert.New(dir).ToPDF(ctx, real, dst); err != nil {
		return "", nil, &previewError{http.StatusUnprocessableEntity, "preview_failed", err.Error()}
	}
	return dst, func() { _ = os.Remove(dst) }, nil
}

func pickArea(areas []cups.PaperArea, paperSize string, landscape bool) *preview.Area {
	for _, area := range areas {
		if !strings.EqualFold(area.PageSize, paperSize) {
			continue
		}
		out := &preview.Area{
			Width:  area.Width,
			Height: area.Height,
			Left:   area.Left,
			Bottom: area.Bottom,
			Right:  area.Right,
			Top:    area.Top,
		}
		if landscape {
			out.Width, out.Height = out.Height, out.Width
			out.Left, out.Bottom, out.Right, out.Top = out.Bottom, out.Right, out.Top, out.Left
		}
		return out
	}
	return nil
}

func fileKey(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s|%d|%d", filepath.Base(path), info.Size(), info.ModTime().UnixNano()), nil
}
