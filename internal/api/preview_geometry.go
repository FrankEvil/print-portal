package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/layout"
)

// handlePreviewGeometry returns how the selected pages land on each sheet.
//
// The response is a few kilobytes of numbers, so the browser can render the
// document it already has instead of waiting for the server to lay the file out
// and send it back. The print path calls the same layout.ComputeGeometry, which
// keeps the preview and the printed output on one set of rules.
func (s *Server) handlePreviewGeometry(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PrinterID   int64           `json:"printer_id"`
		Layout      json.RawMessage `json:"layout_options"`
		PageRange   string          `json:"page_range"`
		PageParity  string          `json:"page_parity"`
		PaperSize   string          `json:"paper_size"`
		Orientation string          `json:"orientation"`
		PageCount   int             `json:"page_count"`
		PageAspect  float64         `json:"page_aspect"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if body.PageCount <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid_page_count", "page_count is required")
		return
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
	opts.PageParity = normalizePageParity(body.PageParity)

	margins := s.paperMargins(r, body.PrinterID, opts.PaperSize, opts.Orientation)
	pages := layout.SelectPages(body.PageCount, opts.PageRange, opts.PageParity)
	aspect := body.PageAspect
	if aspect <= 0 {
		aspect = 0.7071
	}
	geometry := layout.ComputeGeometry(pages, opts, margins, aspect)
	writeJSON(w, http.StatusOK, geometry)
}

// paperMargins reads the PPD printable border for the selected paper size.
func (s *Server) paperMargins(r *http.Request, printerID int64, paperSize, orientation string) layout.Margins {
	if printerID <= 0 {
		return layout.Margins{}
	}
	p, err := s.store.GetPrinter(printerID)
	if err != nil || p == nil {
		return layout.Margins{}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	areas, err := s.cups.PaperAreas(ctx, p.PPDQueue)
	if err != nil {
		return layout.Margins{}
	}
	landscape := strings.EqualFold(orientation, "landscape")
	for _, area := range areas {
		if !strings.EqualFold(area.PageSize, paperSize) {
			continue
		}
		margins := layout.Margins{
			Left: area.Left, Top: area.Top,
			Right: area.Right, Bottom: area.Bottom,
		}
		if landscape {
			// The margins rotate with the paper.
			margins = layout.Margins{
				Left: area.Bottom, Top: area.Right,
				Right: area.Top, Bottom: area.Left,
			}
		}
		return margins
	}
	return layout.Margins{}
}

// expandPageRange turns "1-3,5" into page numbers, defaulting to every page.
func expandPageRange(pageRange string, total int) []int {
	text := strings.TrimSpace(pageRange)
	if text == "" {
		pages := make([]int, 0, total)
		for page := 1; page <= total; page++ {
			pages = append(pages, page)
		}
		return pages
	}
	seen := map[int]bool{}
	for _, token := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == ',' }) {
		parts := strings.SplitN(token, "-", 2)
		start, err := strconv.Atoi(parts[0])
		if err != nil || start < 1 {
			continue
		}
		end := start
		if len(parts) == 2 {
			if value, convErr := strconv.Atoi(parts[1]); convErr == nil && value >= start {
				end = value
			}
		}
		if end > total {
			end = total
		}
		for page := start; page <= end; page++ {
			seen[page] = true
		}
	}
	pages := make([]int, 0, len(seen))
	for page := range seen {
		pages = append(pages, page)
	}
	sort.Ints(pages)
	return pages
}
