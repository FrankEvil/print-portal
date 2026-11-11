package printmode

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/liuzhaojun/print-portal/internal/layout"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// BookletOptions controls saddle-stitch booklet imposition.
type BookletOptions struct {
	// PaperWidthMM and PaperHeightMM describe the landscape sheet that is
	// folded (A4 297x210 or A3 420x297).
	PaperWidthMM   float64
	PaperHeightMM  float64
	SignaturePages int // 0/4/8/16/32; 0 means one signature
	CutLines       bool
	PageRange      string
}

// BookletResult describes a composed booklet.
type BookletResult struct {
	PageCount  int    `json:"page_count"`  // printed sides
	SheetCount int    `json:"sheet_count"` // physical sheets
	PreviewPDF string `json:"-"`
	FrontPDF   string `json:"-"`
	BackPDF    string `json:"-"`
}

// BookletSheet describes the two source pages on one printed side.
type BookletSheet struct {
	Front []int `json:"front"`
	Back  []int `json:"back"`
}

// BookletGeometry is the pure page-order calculation shared by the geometry
// API and the server-side compositor. Page 0 means a blank padding page.
type BookletGeometry struct {
	PaddedPages int            `json:"padded_pages"`
	SheetCount  int            `json:"sheet_count"`
	SideCount   int            `json:"side_count"`
	Sheets      []BookletSheet `json:"sheets"`
}

// BuildBookletGeometry calculates saddle-stitch page order. selectedPages maps
// the selected source pages in order; nil means 1..pageCount.
func BuildBookletGeometry(pageCount int, selectedPages []int, signaturePages int) BookletGeometry {
	if len(selectedPages) == 0 {
		selectedPages = make([]int, pageCount)
		for i := 1; i <= pageCount; i++ {
			selectedPages[i-1] = i
		}
	}
	selectedCount := len(selectedPages)
	padded := (selectedCount + 3) / 4 * 4
	signature := signaturePages
	if signature != 4 && signature != 8 && signature != 16 && signature != 32 {
		signature = padded
	}
	var sheets []BookletSheet
	for start := 1; start <= padded; start += signature {
		groupLen := signature
		if start+groupLen-1 > padded {
			groupLen = padded - start + 1
		}
		groupLen = (groupLen + 3) / 4 * 4
		if groupLen <= 0 {
			break
		}
		count := groupLen / 4
		for i := 0; i < count; i++ {
			front := []int{pageValue(groupLen-2*i, selectedPages), pageValue(1+2*i, selectedPages)}
			back := []int{pageValue(2+2*i, selectedPages), pageValue(groupLen-1-2*i, selectedPages)}
			sheets = append(sheets, BookletSheet{Front: front, Back: back})
		}
	}
	return BookletGeometry{
		PaddedPages: padded,
		SheetCount:  len(sheets),
		SideCount:   len(sheets) * 2,
		Sheets:      sheets,
	}
}

func pageValue(page int, selectedPages []int) int {
	if page < 1 || page > len(selectedPages) {
		return 0
	}
	return selectedPages[page-1]
}

// ComposeBooklet imposes a PDF as an A4/A3 saddle-stitch booklet.
func ComposeBooklet(ctx context.Context, srcPDF, outPDF, frontPDF, backPDF, workDir string, opts BookletOptions) (BookletResult, error) {
	dims, err := api.PageDimsFile(srcPDF)
	if err != nil {
		return BookletResult{}, err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return BookletResult{}, err
	}
	if strings.TrimSpace(opts.PageRange) != "" {
		selected := layout.SelectPages(len(dims), opts.PageRange, "all")
		if len(selected) == 0 {
			return BookletResult{}, fmt.Errorf("页码范围没有匹配的页面")
		}
		selectedPDF := filepath.Join(workDir, "booklet-source-selected.pdf")
		if err := api.CollectFile(srcPDF, selectedPDF, []string{layout.CompactPages(selected)}, nil); err != nil {
			return BookletResult{}, err
		}
		srcPDF = selectedPDF
		dims, err = api.PageDimsFile(srcPDF)
		if err != nil {
			return BookletResult{}, err
		}
	}
	sourceCount := len(dims)
	if sourceCount == 0 {
		return BookletResult{}, fmt.Errorf("PDF 没有页面")
	}
	pageFiles, err := renderAllBookletPages(ctx, srcPDF, workDir)
	if err != nil {
		return BookletResult{}, err
	}
	if opts.PaperWidthMM <= 0 || opts.PaperHeightMM <= 0 {
		return BookletResult{}, fmt.Errorf("纸张尺寸无效")
	}
	selectedPages := make([]int, sourceCount)
	for i := 1; i <= sourceCount; i++ {
		selectedPages[i-1] = i
	}
	geometry := BuildBookletGeometry(sourceCount, selectedPages, opts.SignaturePages)
	var frontFiles, backFiles, combined []string
	var frontSizes, backSizes, sizes []PageSize
	for sheetIndex, sheet := range geometry.Sheets {
		frontImg := renderBookletSide(pageFiles, sourceCount, sheet.Front, opts)
		backImg := renderBookletSide(pageFiles, sourceCount, sheet.Back, opts)
		frontPath := filepath.Join(workDir, fmt.Sprintf("book-front-%03d.jpg", sheetIndex+1))
		backPath := filepath.Join(workDir, fmt.Sprintf("book-back-%03d.jpg", sheetIndex+1))
		if err := writeJPEG(frontImg, frontPath); err != nil {
			return BookletResult{}, err
		}
		if err := writeJPEG(backImg, backPath); err != nil {
			return BookletResult{}, err
		}
		frontFiles = append(frontFiles, frontPath)
		backFiles = append(backFiles, backPath)
		combined = append(combined, frontPath, backPath)
		frontSizes = append(frontSizes, PageSize{WidthMM: opts.PaperWidthMM, HeightMM: opts.PaperHeightMM})
		backSizes = append(backSizes, PageSize{WidthMM: opts.PaperWidthMM, HeightMM: opts.PaperHeightMM})
		sizes = append(sizes, PageSize{WidthMM: opts.PaperWidthMM, HeightMM: opts.PaperHeightMM})
		sizes = append(sizes, PageSize{WidthMM: opts.PaperWidthMM, HeightMM: opts.PaperHeightMM})
	}
	if err := mergeImageSheets(frontFiles, frontSizes, frontPDF); err != nil {
		return BookletResult{}, err
	}
	if err := mergeImageSheets(backFiles, backSizes, backPDF); err != nil {
		return BookletResult{}, err
	}
	if err := mergeImageSheets(combined, sizes, outPDF); err != nil {
		return BookletResult{}, err
	}
	return BookletResult{PageCount: len(combined), SheetCount: len(frontFiles), PreviewPDF: outPDF, FrontPDF: frontPDF, BackPDF: backPDF}, nil
}

func renderAllBookletPages(ctx context.Context, srcPDF, workDir string) (map[int]string, error) {
	prefix := filepath.Join(workDir, "book-source")
	matches, _ := filepath.Glob(prefix + "-*.jpg")
	for _, match := range matches {
		_ = os.Remove(match)
	}
	// JPEG at 200 dpi is dramatically faster than PNG at 300 dpi for large
	// documents and is still plenty for A4-to-A5 booklet output.
	const bookletDPI = 200
	cmd := exec.CommandContext(ctx, "pdftoppm", "-r", strconv.Itoa(bookletDPI), "-jpeg", "-jpegopt", "quality=90", srcPDF, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %v: %s", err, strings.TrimSpace(string(out)))
	}
	files, err := filepath.Glob(prefix + "-*.jpg")
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		left, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(files[i], prefix+"-"), ".jpg"))
		right, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(files[j], prefix+"-"), ".jpg"))
		return left < right
	})
	out := make(map[int]string, len(files))
	for index, file := range files {
		out[index+1] = file
	}
	return out, nil
}

func renderBookletSide(pageFiles map[int]string, sourceCount int, pages []int, opts BookletOptions) image.Image {
	pageW, pageH := mmToPx(opts.PaperWidthMM), mmToPx(opts.PaperHeightMM)
	canvas := image.NewRGBA(image.Rect(0, 0, pageW, pageH))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	margin := mmToPx(5)
	gap := 0
	cellW := (pageW - 2*margin - gap) / 2
	cellH := pageH - 2*margin
	for i, pageNo := range pages {
		rect := image.Rect(margin+i*(cellW+gap), margin, margin+i*(cellW+gap)+cellW, margin+cellH)
		if pageNo >= 1 && pageNo <= sourceCount {
			if path, ok := pageFiles[pageNo]; ok {
				if img, err := decodeImage(path); err == nil {
					drawFit(canvas, img, rect)
				}
			}
		}
		if opts.CutLines {
			drawDashedRect(canvas, expand(rect, mmToPx(1.5)), color.RGBA{150, 160, 175, 255}, 7)
		}
	}
	// Fold line in the middle.
	drawDashedLine(canvas, pageW/2, margin, pageW/2, pageH-margin, color.RGBA{190, 198, 210, 255}, 5)
	return canvas
}

func drawDashedLine(dst *image.RGBA, x0, y0, x1, y1 int, c color.Color, dash int) {
	uniform := image.NewUniform(c)
	if x0 == x1 {
		for y := y0; y < y1; y += dash * 2 {
			end := y + dash
			if end > y1 {
				end = y1
			}
			draw.Draw(dst, image.Rect(x0, y, x0+1, end), uniform, image.Point{}, draw.Src)
		}
		return
	}
	for x := x0; x < x1; x += dash * 2 {
		end := x + dash
		if end > x1 {
			end = x1
		}
		draw.Draw(dst, image.Rect(x, y0, end, y0+1), uniform, image.Point{}, draw.Src)
	}
}
