// Package preview builds the print layout once on the server and renders it
// into per-sheet images. The same laid-out PDF is reused when the job is
// printed, so what the user sees and what the printer receives come from a
// single source of truth instead of two independent layout implementations.
package preview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"

	"github.com/liuzhaojun/print-portal/internal/layout"
)

// Area is the printable region of one paper size, in PostScript points.
// Left/Bottom/Right/Top are the non-printable margins.
type Area struct {
	Width  float64
	Height float64
	Left   float64
	Bottom float64
	Right  float64
	Top    float64
}

// Request describes one preview build.
type Request struct {
	SourcePath string // source PDF (already converted from Office/images)
	SourceKey  string // identity of the source file for cache invalidation
	Layout     layout.Options
	Area       *Area // nil when the PPD declares no printable area
	DPI        int   // render resolution, defaults to 96
}

// Sheet is one physical sheet of paper in the preview.
type Sheet struct {
	Index int   `json:"index"`
	Pages []int `json:"pages"`
}

// Result is what the print page needs to render its preview grid.
type Result struct {
	ID            string  `json:"id"`
	Sheets        []Sheet `json:"sheets"`
	SheetCount    int     `json:"sheet_count"`
	PageCount     int     `json:"page_count"`
	Selected      int     `json:"selected_pages"`
	PagesPerSheet int     `json:"pages_per_sheet"`
	PaperWidth    float64 `json:"paper_width"`
	PaperHeight   float64 `json:"paper_height"`
	Printable     bool    `json:"printable"`
	// PDFURL points at the laid-out PDF. The browser renders this file
	// directly, so it downloads once instead of requesting one image per
	// sheet, and what it shows is byte-for-byte what gets printed.
	PDFURL string `json:"pdf_url"`
	// PDFPath is the on-disk location used by the print job.
	PDFPath string `json:"-"`
}

type entry struct {
	result  *Result
	dir     string
	lastUse time.Time
}

// maxPreviewWidth caps the pixel width of a sheet image. Previews are only
// shown as thumbnails, so anything wider is wasted bandwidth.
const maxPreviewWidth = 900

// Service caches laid-out PDFs and rendered sheet images on disk.
type Service struct {
	root string
	mu   sync.Mutex
	byID map[string]*entry
	ttl  time.Duration
	// render bounds how many Ghostscript processes run at once. Browsers ask
	// for many sheets in parallel and each render is CPU bound, so without a
	// limit every request slows down and none finishes quickly.
	render chan struct{}
}

// New creates a preview service rooted at <dataDir>/preview-cache.
func New(dataDir string) *Service {
	return &Service{
		root:   filepath.Join(dataDir, "preview-cache"),
		byID:   map[string]*entry{},
		ttl:    time.Hour,
		render: make(chan struct{}, 2),
	}
}

// Prepare lays the document out and records the sheet/page mapping. Sheet
// images are rendered lazily by SheetImage so the first sheet can appear
// without waiting for the whole document.
func (s *Service) Prepare(ctx context.Context, req Request) (*Result, error) {
	if req.DPI <= 0 {
		req.DPI = 96
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, err
	}
	s.cleanup()

	id, err := cacheKey(req)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(s.root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	layoutPDF := filepath.Join(dir, "layout.pdf")
	needsLayout := true
	if info, statErr := os.Stat(layoutPDF); statErr == nil && info.Size() > 0 {
		needsLayout = false
	}
	if needsLayout {
		if err := layout.Apply(ctx, req.SourcePath, layoutPDF, req.Layout); err != nil {
			return nil, fmt.Errorf("layout: %w", err)
		}
	}

	sheetCount, err := pageCount(ctx, layoutPDF)
	if err != nil {
		return nil, err
	}
	selected, err := selectedPages(req.Layout.PageRange, req.SourcePath)
	if err != nil {
		return nil, err
	}
	perSheet := req.Layout.PagesPerSheet
	if perSheet < 1 {
		perSheet = 1
	}
	sheets := make([]Sheet, 0, sheetCount)
	for index := 0; index < sheetCount; index++ {
		start := index * perSheet
		end := start + perSheet
		if end > len(selected) {
			end = len(selected)
		}
		pages := []int{}
		if start < len(selected) {
			pages = append(pages, selected[start:end]...)
		}
		sheets = append(sheets, Sheet{Index: index, Pages: pages})
	}

	paperWidth, paperHeight, _ := layout.PaperSizePoints(req.Layout.PaperSize, strings.EqualFold(req.Layout.Orientation, "landscape"))
	result := &Result{
		ID:            id,
		Sheets:        sheets,
		SheetCount:    len(sheets),
		PageCount:     len(selected),
		Selected:      len(selected),
		PagesPerSheet: perSheet,
		PaperWidth:    paperWidth,
		PaperHeight:   paperHeight,
		Printable:     req.Area != nil,
		PDFURL:        "/api/preview/" + id + "/document.pdf",
		PDFPath:       layoutPDF,
	}

	s.mu.Lock()
	s.byID[id] = &entry{result: result, dir: dir, lastUse: time.Now()}
	s.mu.Unlock()
	return result, nil
}

// DocumentPath returns the laid-out PDF for a prepared preview.
func (s *Service) DocumentPath(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.byID[id]
	if !ok {
		return "", false
	}
	item.lastUse = time.Now()
	return item.result.PDFPath, true
}

// Result returns a previously prepared result.
func (s *Service) Result(id string) (*Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	item.lastUse = time.Now()
	return item.result, true
}

// SheetImage renders one sheet to a PNG file and returns its path. The result
// is cached on disk, so repeated requests for the same sheet are free.
func (s *Service) SheetImage(ctx context.Context, id string, index int, area *Area, dpi int) (string, error) {
	s.mu.Lock()
	item, ok := s.byID[id]
	if ok {
		item.lastUse = time.Now()
	}
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("preview %s not found", id)
	}
	if index < 0 || index >= item.result.SheetCount {
		return "", fmt.Errorf("sheet %d out of range", index)
	}
	if dpi <= 0 {
		dpi = 96
	}
	out := filepath.Join(item.dir, fmt.Sprintf("sheet-%d-%d.jpg", index, dpi))
	if info, err := os.Stat(out); err == nil && info.Size() > 0 {
		return out, nil
	}
	raw := filepath.Join(item.dir, fmt.Sprintf("raw-%d-%d.jpg", index, dpi))
	select {
	case s.render <- struct{}{}:
		defer func() { <-s.render }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := renderPage(ctx, item.result.PDFPath, index, dpi, raw); err != nil {
		return "", err
	}
	defer os.Remove(raw)
	// Without a printable area and within the width cap the render can be
	// served as-is, which skips a full decode/encode round trip.
	if area == nil {
		if width, err := jpegWidth(raw); err == nil && width <= maxPreviewWidth {
			if err := os.Rename(raw, out); err != nil {
				return "", err
			}
			return out, nil
		}
	}
	if err := compose(raw, out, area); err != nil {
		return "", err
	}
	return out, nil
}

func renderPage(ctx context.Context, pdf string, index, dpi int, dst string) error {
	page := index + 1
	args := []string{
		"-q", "-dNOPAUSE", "-dBATCH", "-dSAFER",
		// Let Ghostscript emit JPEG directly: decoding its PNG and re-encoding
		// in Go cost about three times as much as the render itself.
		"-sDEVICE=jpeg",
		"-dJPEGQ=82",
		"-r" + strconv.Itoa(dpi),
		"-dFirstPage=" + strconv.Itoa(page),
		"-dLastPage=" + strconv.Itoa(page),
		"-dAutoRotatePages=/None",
		"-sOutputFile=" + dst,
		pdf,
	}
	cmd := exec.CommandContext(ctx, "gs", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gs: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

// compose places the rendered page inside the printable region of a white
// sheet, so the preview shows the same hardware margins the printer leaves
// blank. Without an area the page fills the sheet.
func compose(src, dst string, area *Area) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	page, err := jpeg.Decode(in)
	in.Close()
	if err != nil {
		return err
	}
	bounds := page.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	left, top := 0.0, 0.0
	targetW, targetH := float64(width), float64(height)
	if area != nil && area.Width > 0 && area.Height > 0 {
		scaleX := float64(width) / area.Width
		scaleY := float64(height) / area.Height
		left = area.Left * scaleX
		top = area.Top * scaleY
		targetW = (area.Width - area.Left - area.Right) * scaleX
		targetH = (area.Height - area.Bottom - area.Top) * scaleY
	}
	if targetW <= 1 || targetH <= 1 {
		return fmt.Errorf("printable area is empty")
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	white := image.NewUniform(image.White)
	draw.Draw(canvas, canvas.Bounds(), white, image.Point{}, draw.Src)
	rect := image.Rect(
		int(left), int(top),
		int(left+targetW), int(top+targetH),
	)
	// Bilinear is several times faster than CatmullRom and the difference is
	// invisible at thumbnail size.
	draw.ApproxBiLinear.Scale(canvas, rect, page, bounds, draw.Over, nil)

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	// Preview images travel over the LAN to the browser, so JPEG at a capped
	// width keeps a sheet around 100 kB instead of the multi-megabyte PNG that
	// Ghostscript's lossless output produced.
	final := canvas
	if canvas.Bounds().Dx() > maxPreviewWidth {
		ratio := float64(maxPreviewWidth) / float64(canvas.Bounds().Dx())
		height := int(float64(canvas.Bounds().Dy()) * ratio)
		scaled := image.NewRGBA(image.Rect(0, 0, maxPreviewWidth, height))
		draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), canvas, canvas.Bounds(), draw.Src, nil)
		final = scaled
	}
	return jpeg.Encode(out, final, &jpeg.Options{Quality: 82})
}

func jpegWidth(path string) (int, error) {
	in, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	cfg, err := jpeg.DecodeConfig(in)
	if err != nil {
		return 0, err
	}
	return cfg.Width, nil
}

func pageCount(ctx context.Context, pdf string) (int, error) {
	out, err := exec.CommandContext(ctx, "pdfinfo", pdf).Output()
	if err != nil {
		return 0, fmt.Errorf("pdfinfo: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "Pages:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "Pages:"))
		count, convErr := strconv.Atoi(value)
		if convErr != nil {
			return 0, fmt.Errorf("parse page count %q: %w", value, convErr)
		}
		return count, nil
	}
	return 0, fmt.Errorf("page count missing")
}

func selectedPages(pageRange, pdf string) ([]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	total, err := pageCount(ctx, pdf)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(pageRange)
	if text == "" {
		pages := make([]int, 0, total)
		for page := 1; page <= total; page++ {
			pages = append(pages, page)
		}
		return pages, nil
	}
	set := map[int]bool{}
	for _, token := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == ',' }) {
		parts := strings.SplitN(token, "-", 2)
		start, convErr := strconv.Atoi(parts[0])
		if convErr != nil || start < 1 {
			return nil, fmt.Errorf("invalid page range %q", pageRange)
		}
		end := start
		if len(parts) == 2 {
			end, convErr = strconv.Atoi(parts[1])
			if convErr != nil || end < start {
				return nil, fmt.Errorf("invalid page range %q", pageRange)
			}
		}
		if end > total {
			return nil, fmt.Errorf("page range exceeds document (%d pages)", total)
		}
		for page := start; page <= end; page++ {
			set[page] = true
		}
	}
	pages := make([]int, 0, len(set))
	for page := range set {
		pages = append(pages, page)
	}
	sort.Ints(pages)
	return pages, nil
}

func cacheKey(req Request) (string, error) {
	parts := []string{
		req.SourceKey,
		req.Layout.PaperSize,
		req.Layout.Orientation,
		strconv.Itoa(req.Layout.PagesPerSheet),
		req.Layout.Direction,
		strconv.FormatBool(req.Layout.Border),
		strconv.FormatBool(req.Layout.Reverse),
		strconv.FormatBool(req.Layout.FlipHorizontal),
		req.Layout.PageRange,
	}
	if req.Area != nil {
		parts = append(parts, fmt.Sprintf("%.2f/%.2f/%.2f/%.2f/%.2f/%.2f",
			req.Area.Width, req.Area.Height, req.Area.Left, req.Area.Bottom, req.Area.Right, req.Area.Top))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:16]), nil
}

func (s *Service) cleanup() {
	s.mu.Lock()
	expired := make([]string, 0)
	for id, item := range s.byID {
		if time.Since(item.lastUse) > s.ttl {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		dir := s.byID[id].dir
		delete(s.byID, id)
		go os.RemoveAll(dir)
	}
	s.mu.Unlock()
}
