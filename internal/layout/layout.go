// Package layout applies print-page layout options to a PDF before it is
// rendered into printer-native data. It is deliberately independent of PPDs:
// N-up, borders, rotation and mirroring are application-level print layout
// features, not printer capabilities.
package layout

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Options describes the print layout selected on the print page.
type Options struct {
	PagesPerSheet  int    `json:"pages_per_sheet"` // 1, 2, 3, 4, 6, 8, 9, 12, 16
	Direction      string `json:"direction"`       // lrtb / rltb / tblr / btlr
	Border         bool   `json:"border"`
	Reverse        bool   `json:"reverse"`
	FlipHorizontal bool   `json:"flip_horizontal"`
	PaperSize      string `json:"paper_size"`
	Orientation    string `json:"orientation"`
	PageRange      string `json:"-"`
	PageParity     string `json:"-"` // all / odd / even
	// Margins is the PPD printable border. It is not part of the JSON contract
	// because the client never sends it; the server fills it in so the grid is
	// scored against the same box the preview uses.
	Margins Margins `json:"-"`
}

func (o Options) active() bool {
	return o.PagesPerSheet > 1 || o.Border || o.Reverse || o.FlipHorizontal
}

// PaperSizePoints returns the paper size in PostScript points. When landscape
// is true the width and height are swapped.
func PaperSizePoints(name string, landscape bool) (float64, float64, bool) {
	dim, ok := paperDimensions[strings.ToUpper(strings.TrimSpace(name))]
	if !ok {
		dim, ok = paperDimensions["A4"]
	}
	if landscape {
		return dim.Height, dim.Width, ok
	}
	return dim.Width, dim.Height, ok
}

// Grid returns the rows x cols grid that wastes the least paper for the given
// page count, paper size and source page aspect ratio.
func Grid(pages int, paperWidth, paperHeight, pageAspect float64, landscape bool) (cols, rows int) {
	orientation := "portrait"
	if landscape {
		orientation = "landscape"
	}
	return chooseGrid(pages, paperWidth, paperHeight, pageAspect, orientation)
}

var paperDimensions = map[string]types.Dim{
	"A4":     {Width: 595.28, Height: 841.89},
	"A5":     {Width: 419.53, Height: 595.28},
	"A6":     {Width: 297.64, Height: 419.53},
	"B5":     {Width: 498.90, Height: 708.66},
	"B6":     {Width: 354.33, Height: 498.90},
	"Letter": {Width: 612.00, Height: 792.00},
	"Legal":  {Width: 612.00, Height: 1008.00},
}

// Apply writes a layout-processed PDF to dst. If no layout option is active,
// the source is copied unchanged.
func Apply(ctx context.Context, src, dst string, opts Options) error {
	if !opts.active() {
		return copyFile(src, dst)
	}
	tmpDir, err := os.MkdirTemp("", "print-portal-layout-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	current := src
	conf := model.NewDefaultConfiguration()

	if opts.PagesPerSheet > 1 || (opts.PagesPerSheet == 1 && opts.Border) {
		var nup *model.NUp
		var err error
		if opts.PagesPerSheet > 1 {
			nup, err = api.PDFNUpConfig(opts.PagesPerSheet, "", conf)
		} else {
			nup, err = api.PDFGridConfig(1, 1, "", conf)
		}
		if err != nil {
			return fmt.Errorf("invalid pages per sheet: %w", err)
		}
		pageSize := strings.TrimSpace(opts.PaperSize)
		if pageSize == "" {
			pageSize = "A4"
		}
		landscape := strings.EqualFold(opts.Orientation, "landscape")
		if landscape {
			pageSize += "L"
		} else {
			pageSize += "P"
		}
		nup.PageSize = pageSize
		if dim, ok := paperDimensions[strings.ToUpper(strings.TrimSuffix(strings.TrimSuffix(pageSize, "L"), "P"))]; ok {
			if landscape {
				dim = types.Dim{Width: dim.Height, Height: dim.Width}
			}
			nup.PageDim = &dim
		}
		// Build the placement with the same function the preview uses, then let
		// pdfcpu only perform the mechanics. Enforce stays off because the
		// rotation was already decided by ComputeGeometry.
		selected := selectedPageNumbers(current, opts.PageRange, opts.PageParity)
		pageAspect := averagePageAspect(current)
		geometry := ComputeGeometry(selected, opts, opts.Margins, pageAspect)
		nup.Grid = &types.Dim{Width: float64(geometry.Cols), Height: float64(geometry.Rows)}
		nup.Enforce = false
		nup.Border = opts.Border
		applyOrientation(nup, opts.Direction)
		if geometry.Rotate() == 90 {
			rotated := filepath.Join(tmpDir, "rotated-90.pdf")
			if err := api.RotateFile(current, rotated, 90, nil, conf); err != nil {
				return fmt.Errorf("page rotation failed: %w", err)
			}
			current = rotated
		}
		next := filepath.Join(tmpDir, "nup.pdf")
		var rangeArg []string
		if strings.TrimSpace(opts.PageRange) != "" || pageParityActive(opts.PageParity) {
			rangeArg = []string{CompactPages(selected)}
		}
		if err := api.NUpFile([]string{current}, next, rangeArg, nup, conf); err != nil {
			return fmt.Errorf("N-up layout failed: %w", err)
		}
		current = next
	} else if strings.TrimSpace(opts.PageRange) != "" || pageParityActive(opts.PageParity) {
		selected := selectedPageNumbers(current, opts.PageRange, opts.PageParity)
		next := filepath.Join(tmpDir, "selected.pdf")
		if err := api.CollectFile(current, next, []string{CompactPages(selected)}, conf); err != nil {
			return fmt.Errorf("page selection failed: %w", err)
		}
		current = next
	}

	if opts.Reverse {
		next := filepath.Join(tmpDir, "rotated.pdf")
		if err := api.RotateFile(current, next, 180, nil, conf); err != nil {
			return fmt.Errorf("reverse orientation failed: %w", err)
		}
		current = next
	}
	if opts.FlipHorizontal {
		next := filepath.Join(tmpDir, "flipped.pdf")
		if err := flipPDF(ctx, current, next, tmpDir); err != nil {
			return fmt.Errorf("horizontal flip failed: %w", err)
		}
		current = next
	}
	if current == src {
		return copyFile(src, dst)
	}
	return copyFile(current, dst)
}

func paperDimensionsFor(pageSize string) (float64, float64) {
	name := strings.ToUpper(strings.TrimSuffix(strings.TrimSuffix(pageSize, "L"), "P"))
	dim, ok := paperDimensions[name]
	if !ok {
		dim = paperDimensions["A4"]
	}
	if strings.HasSuffix(pageSize, "L") {
		return dim.Height, dim.Width
	}
	return dim.Width, dim.Height
}

// selectedPageNumbers expands a page range like "1-3,5" into page numbers.
// An empty range means every page.
func selectedPageNumbers(pdf, pageRange, parity string) []int {
	return SelectPages(pageCountOf(pdf), pageRange, parity)
}

// SelectPages expands a page range and applies the odd/even filter. An empty
// range means every page.
func SelectPages(total int, pageRange, parity string) []int {
	text := strings.TrimSpace(pageRange)
	seen := map[int]bool{}
	if text == "" {
		for page := 1; page <= total; page++ {
			seen[page] = true
		}
	} else {
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
			if total > 0 && end > total {
				end = total
			}
			for page := start; page <= end; page++ {
				seen[page] = true
			}
		}
	}
	pages := make([]int, 0, len(seen))
	for page := range seen {
		if parity == "odd" && page%2 == 0 {
			continue
		}
		if parity == "even" && page%2 == 1 {
			continue
		}
		pages = append(pages, page)
	}
	sort.Ints(pages)
	return pages
}

// ReversePages writes the pages of src in reverse order to dst. This is used
// before chunking so "reverse paper order" applies to the whole document.
func ReversePages(src, dst string) error {
	dims, err := api.PageDimsFile(src)
	if err != nil {
		return err
	}
	pages := make([]string, 0, len(dims))
	for page := len(dims); page >= 1; page-- {
		pages = append(pages, strconv.Itoa(page))
	}
	return api.CollectFile(src, dst, pages, nil)
}

// CompactPages turns [1,3,5,7,9] into "1,3,5,7,9" and [1,2,3,5] into "1-3,5".
func CompactPages(pages []int) string {
	if len(pages) == 0 {
		return ""
	}
	sorted := append([]int(nil), pages...)
	sort.Ints(sorted)
	var parts []string
	start, prev := sorted[0], sorted[0]
	flush := func() {
		if start == prev {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, strconv.Itoa(start)+"-"+strconv.Itoa(prev))
		}
	}
	for _, page := range sorted[1:] {
		if page == prev+1 {
			prev = page
			continue
		}
		flush()
		start, prev = page, page
	}
	flush()
	return strings.Join(parts, ",")
}

func pageParityActive(parity string) bool {
	return parity == "odd" || parity == "even"
}

func pageCountOf(pdf string) int {
	dims, err := api.PageDimsFile(pdf)
	if err != nil {
		return 0
	}
	return len(dims)
}

func averagePageAspect(path string) float64 {
	dims, err := api.PageDimsFile(path)
	if err != nil || len(dims) == 0 {
		return 0.7071
	}
	var total float64
	var count int
	for _, dim := range dims {
		if dim.Width > 0 && dim.Height > 0 {
			total += dim.Width / dim.Height
			count++
		}
	}
	if count == 0 {
		return 0.7071
	}
	return total / float64(count)
}

// chooseGrid enumerates all rows x cols candidates and minimizes unused area
// plus unused cells. Page rotation (0/90) is allowed when scoring the fit.
func chooseGrid(pages int, paperWidth, paperHeight, pageAspect float64, orientation string) (cols, rows int) {
	if pages <= 1 {
		return 1, 1
	}
	if pageAspect <= 0 {
		pageAspect = 0.7071
	}
	portrait := !strings.EqualFold(orientation, "landscape")
	bestScore := math.MaxFloat64
	bestCols, bestRows := 0, 0
	for r := 1; r <= pages; r++ {
		c := (pages + r - 1) / r
		cellW := paperWidth / float64(c)
		cellH := paperHeight / float64(r)
		cellAspect := cellW / cellH
		fill := math.Max(fillRatio(pageAspect, cellAspect), fillRatio(1/pageAspect, cellAspect))
		unused := c*r - pages
		score := (1-fill)*float64(pages) + float64(unused)*0.75
		if score < bestScore-1e-9 {
			bestScore = score
			bestCols, bestRows = c, r
		} else if math.Abs(score-bestScore) < 1e-9 {
			// Ties: portrait prefers a taller grid, landscape prefers a wider grid.
			if (portrait && r > bestRows) || (!portrait && c > bestCols) {
				bestCols, bestRows = c, r
			}
		}
	}
	return bestCols, bestRows
}

func fillRatio(pageAspect, cellAspect float64) float64 {
	if pageAspect > cellAspect {
		return cellAspect / pageAspect
	}
	return pageAspect / cellAspect
}

func applyOrientation(nup *model.NUp, direction string) {
	switch strings.ToLower(direction) {
	case "rltb":
		nup.Orient = model.LeftDown
	case "tblr":
		nup.Orient = model.DownRight
	case "btlr":
		nup.Orient = model.DownLeft
	default:
		nup.Orient = model.RightDown
	}
}

// flipPDF is a small raster fallback for horizontal mirroring: pdfcpu does not
// expose a page-mirroring transform, so render pages at 300 dpi, mirror them
// with ImageMagick, and rebuild the PDF with img2pdf.
func flipPDF(ctx context.Context, src, dst, workDir string) error {
	prefix := filepath.Join(workDir, "page")
	if err := run(ctx, "pdftoppm", "-png", "-r", "300", src, prefix); err != nil {
		return err
	}
	pages, err := filepath.Glob(prefix + "-*.png")
	if err != nil || len(pages) == 0 {
		return fmt.Errorf("no pages rendered for flip")
	}
	sort.Slice(pages, func(i, j int) bool {
		return pageNumber(pages[i]) < pageNumber(pages[j])
	})
	flippedDir := filepath.Join(workDir, "flipped")
	if err := os.MkdirAll(flippedDir, 0o755); err != nil {
		return err
	}
	flipped := make([]string, 0, len(pages))
	for _, page := range pages {
		out := filepath.Join(flippedDir, filepath.Base(page))
		if err := run(ctx, "convert", page, "-flop", out); err != nil {
			return err
		}
		flipped = append(flipped, out)
	}
	args := append([]string{"-o", dst}, flipped...)
	return run(ctx, "img2pdf", args...)
}

func pageNumber(path string) int {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if idx := strings.LastIndex(base, "-"); idx >= 0 {
		if n, err := strconv.Atoi(base[idx+1:]); err == nil {
			return n
		}
	}
	return 0
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %s: %w", name, strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
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
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return out.Sync()
}
