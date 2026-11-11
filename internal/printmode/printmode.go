// Package printmode composes special print layouts (ID card, invoice) into
// ready-to-print A4 PDFs. The result is a rasterized 300dpi page, which keeps
// placement independent of the printer's own scaling behaviour.
package printmode

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	xdraw "golang.org/x/image/draw"
)

func init() {
	// pdfcpu otherwise creates a config directory under the user's home,
	// which is not available in the container sandbox.
	api.DisableConfigDir()
}

const (
	DPI = 300

	// A4 dimensions in millimetres.
	A4WidthMM  = 210.0
	A4HeightMM = 297.0

	// Chinese second-generation ID card.
	IDCardWidthMM  = 85.6
	IDCardHeightMM = 54.0
)

func mmToPx(mm float64) int { return int(math.Round(mm / 25.4 * DPI)) }

// IDCardOptions controls the ID card composition.
type IDCardOptions struct {
	Layout string // "vertical" (default) or "horizontal"
	Border bool
}

// ComposeIDCard renders front and back images on one A4 page at their real
// physical size.
func ComposeIDCard(ctx context.Context, frontPath, backPath, outPDF, workDir string, opts IDCardOptions) error {
	front, err := decodeImage(frontPath)
	if err != nil {
		return fmt.Errorf("读取身份证正面失败: %w", err)
	}
	back, err := decodeImage(backPath)
	if err != nil {
		return fmt.Errorf("读取身份证反面失败: %w", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}

	pageW, pageH := mmToPx(A4WidthMM), mmToPx(A4HeightMM)
	canvas := image.NewRGBA(image.Rect(0, 0, pageW, pageH))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	cardW, cardH := mmToPx(IDCardWidthMM), mmToPx(IDCardHeightMM)
	gap := mmToPx(10)
	var frontRect, backRect image.Rectangle
	if strings.EqualFold(opts.Layout, "horizontal") {
		totalW := cardW*2 + gap
		x := (pageW - totalW) / 2
		y := (pageH - cardH) / 2
		frontRect = image.Rect(x, y, x+cardW, y+cardH)
		backRect = image.Rect(x+cardW+gap, y, x+cardW+gap+cardW, y+cardH)
	} else {
		totalH := cardH*2 + gap
		x := (pageW - cardW) / 2
		y := (pageH - totalH) / 2
		frontRect = image.Rect(x, y, x+cardW, y+cardH)
		backRect = image.Rect(x, y+cardH+gap, x+cardW, y+cardH+gap+cardH)
	}
	drawFit(canvas, front, frontRect)
	drawFit(canvas, back, backRect)
	if opts.Border {
		drawDashedRect(canvas, expand(frontRect, mmToPx(2)), color.RGBA{120, 130, 145, 255}, 9)
		drawDashedRect(canvas, expand(backRect, mmToPx(2)), color.RGBA{120, 130, 145, 255}, 9)
	}

	jpgPath := filepath.Join(workDir, "idcard.jpg")
	if err := writeJPEG(canvas, jpgPath); err != nil {
		return err
	}
	return imageJPEGToPDF([]string{jpgPath}, A4WidthMM, A4HeightMM, outPDF)
}

// InvoiceInput is one uploaded invoice file.
type InvoiceInput struct {
	Path     string
	Name     string
	Kind     string // "pdf" or "image"
	WidthMM  float64
	HeightMM float64
}

// InvoiceOptions controls invoice sheet composition.
type InvoiceOptions struct {
	PerSheet int  // 1 or 2
	Trim     bool // trim white margins from PDF pages
	CutLines bool
}

// InvoiceItemInfo describes one input file after inspection.
type InvoiceItemInfo struct {
	Name  string     `json:"name"`
	Kind  string     `json:"kind"`
	Pages []PageSize `json:"pages"`
}

// PageSize is a page or invoice size in millimetres.
type PageSize struct {
	WidthMM  float64 `json:"width_mm"`
	HeightMM float64 `json:"height_mm"`
}

// SheetInfo describes one output A4 sheet.
type SheetInfo struct {
	Index int      `json:"index"`
	Items []string `json:"items"`
}

// InvoiceResult is returned by ComposeInvoice.
type InvoiceResult struct {
	PageCount int               `json:"page_count"`
	Items     []InvoiceItemInfo `json:"items"`
	Sheets    []SheetInfo       `json:"sheets"`
}

// InspectInvoice returns page sizes for a PDF. Image inputs carry their size
// from the UI instead, so they are returned as one entry without dimensions.
func InspectInvoice(path string, kind string) ([]PageSize, error) {
	if kind == "pdf" {
		dims, err := api.PageDimsFile(path)
		if err != nil {
			return nil, err
		}
		out := make([]PageSize, 0, len(dims))
		for _, d := range dims {
			out = append(out, PageSize{WidthMM: d.Width / 72 * 25.4, HeightMM: d.Height / 72 * 25.4})
		}
		return out, nil
	}
	return []PageSize{{}}, nil
}

type invoicePage struct {
	name string
	img  image.Image
	wmm  float64
	hmm  float64
}

type placement struct {
	page invoicePage
	xmm  float64
	ymm  float64
}

type sheetLayout struct {
	widthMM  float64
	heightMM float64
	items    []placement
}

// ComposeInvoice renders every invoice page onto A4 sheets. Pages are placed
// at their real size when they fit; oversized pages are proportionally
// reduced so nothing is clipped.
func ComposeInvoice(ctx context.Context, inputs []InvoiceInput, outPDF, workDir string, opts InvoiceOptions) (InvoiceResult, error) {
	var result InvoiceResult
	if len(inputs) == 0 {
		return result, fmt.Errorf("没有发票文件")
	}
	if opts.PerSheet != 1 && opts.PerSheet != 2 {
		opts.PerSheet = 1
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return result, err
	}

	var pages []invoicePage
	for _, input := range inputs {
		item := InvoiceItemInfo{Name: input.Name, Kind: input.Kind}
		if input.Kind == "pdf" {
			dims, err := api.PageDimsFile(input.Path)
			if err != nil {
				return result, fmt.Errorf("%s: %w", input.Name, err)
			}
			for i, dim := range dims {
				img, err := renderPDFPage(ctx, input.Path, i+1, workDir)
				if err != nil {
					return result, fmt.Errorf("%s 第 %d 页: %w", input.Name, i+1, err)
				}
				wmm, hmm := dim.Width/72*25.4, dim.Height/72*25.4
				if opts.Trim {
					img, wmm, hmm = trimPage(img)
				}
				pages = append(pages, invoicePage{name: input.Name, img: img, wmm: wmm, hmm: hmm})
				item.Pages = append(item.Pages, PageSize{WidthMM: wmm, HeightMM: hmm})
			}
		} else {
			img, err := decodeImage(input.Path)
			if err != nil {
				return result, fmt.Errorf("%s: %w", input.Name, err)
			}
			wmm, hmm := input.WidthMM, input.HeightMM
			if wmm <= 0 || hmm <= 0 {
				wmm, hmm = A4WidthMM, A4HeightMM
			}
			pages = append(pages, invoicePage{name: input.Name, img: img, wmm: wmm, hmm: hmm})
			item.Pages = append(item.Pages, PageSize{WidthMM: wmm, HeightMM: hmm})
		}
		result.Items = append(result.Items, item)
	}

	var sheetFiles []string
	var sheetSizes []PageSize
	for i := 0; i < len(pages); {
		batch := pages[i : i+1]
		if opts.PerSheet == 2 && i+1 < len(pages) {
			batch = pages[i : i+2]
		}
		layout, ok := layoutSheet(batch)
		if !ok && len(batch) == 2 {
			// The pair does not fit side by side; print them on separate sheets.
			batch = batch[:1]
			layout, ok = layoutSheet(batch)
		}
		if !ok {
			return result, fmt.Errorf("发票尺寸过大，无法放入 A4")
		}
		canvas := renderSheet(layout, opts.CutLines)
		jpgPath := filepath.Join(workDir, fmt.Sprintf("invoice-%03d.jpg", len(sheetFiles)+1))
		if err := writeJPEG(canvas, jpgPath); err != nil {
			return result, err
		}
		sheetFiles = append(sheetFiles, jpgPath)
		sheetSizes = append(sheetSizes, PageSize{WidthMM: layout.widthMM, HeightMM: layout.heightMM})
		names := make([]string, 0, len(layout.items))
		for _, p := range layout.items {
			names = append(names, p.page.name)
		}
		result.Sheets = append(result.Sheets, SheetInfo{Index: len(result.Sheets), Items: names})
		i += len(batch)
	}

	if err := mergeImageSheets(sheetFiles, sheetSizes, outPDF); err != nil {
		return result, err
	}
	result.PageCount = len(sheetFiles)
	return result, nil
}

func layoutSheet(pages []invoicePage) (sheetLayout, bool) {
	// Portrait, one or two stacked.
	if len(pages) == 1 {
		if pages[0].wmm <= A4WidthMM && pages[0].hmm <= A4HeightMM {
			x := (A4WidthMM - pages[0].wmm) / 2
			y := (A4HeightMM - pages[0].hmm) / 2
			return sheetLayout{widthMM: A4WidthMM, heightMM: A4HeightMM, items: []placement{{page: pages[0], xmm: x, ymm: y}}}, true
		}
		if pages[0].wmm <= A4HeightMM && pages[0].hmm <= A4WidthMM {
			// Landscape A4.
			x := (A4HeightMM - pages[0].wmm) / 2
			y := (A4WidthMM - pages[0].hmm) / 2
			return sheetLayout{widthMM: A4HeightMM, heightMM: A4WidthMM, items: []placement{{page: pages[0], xmm: x, ymm: y}}}, true
		}
		return sheetLayout{}, false
	}

	maxW := math.Max(pages[0].wmm, pages[1].wmm)
	totalH := pages[0].hmm + pages[1].hmm + 8
	if maxW <= A4WidthMM && totalH <= A4HeightMM {
		y := (A4HeightMM - totalH) / 2
		first := placement{page: pages[0], xmm: (A4WidthMM - pages[0].wmm) / 2, ymm: y}
		second := placement{page: pages[1], xmm: (A4WidthMM - pages[1].wmm) / 2, ymm: y + pages[0].hmm + 8}
		return sheetLayout{widthMM: A4WidthMM, heightMM: A4HeightMM, items: []placement{first, second}}, true
	}
	maxH := math.Max(pages[0].hmm, pages[1].hmm)
	totalW := pages[0].wmm + pages[1].wmm + 8
	if maxH <= A4WidthMM && totalW <= A4HeightMM {
		x := (A4HeightMM - totalW) / 2
		first := placement{page: pages[0], xmm: x, ymm: (A4WidthMM - pages[0].hmm) / 2}
		second := placement{page: pages[1], xmm: x + pages[0].wmm + 8, ymm: (A4WidthMM - pages[1].hmm) / 2}
		return sheetLayout{widthMM: A4HeightMM, heightMM: A4WidthMM, items: []placement{first, second}}, true
	}
	return sheetLayout{}, false
}

func renderSheet(layout sheetLayout, cutLines bool) image.Image {
	w, h := mmToPx(layout.widthMM), mmToPx(layout.heightMM)
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for _, item := range layout.items {
		x0, y0 := mmToPx(item.xmm), mmToPx(item.ymm)
		x1, y1 := x0+mmToPx(item.page.wmm), y0+mmToPx(item.page.hmm)
		rect := image.Rect(x0, y0, x1, y1)
		drawFit(canvas, item.page.img, rect)
		if cutLines {
			drawDashedRect(canvas, expand(rect, mmToPx(1.5)), color.RGBA{150, 160, 175, 255}, 7)
		}
	}
	return canvas
}

func drawFit(dst *image.RGBA, src image.Image, rect image.Rectangle) {
	b := src.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return
	}
	scale := math.Min(float64(rect.Dx())/float64(b.Dx()), float64(rect.Dy())/float64(b.Dy()))
	w := int(math.Round(float64(b.Dx()) * scale))
	h := int(math.Round(float64(b.Dy()) * scale))
	x := rect.Min.X + (rect.Dx()-w)/2
	y := rect.Min.Y + (rect.Dy()-h)/2
	target := image.Rect(x, y, x+w, y+h)
	xdraw.CatmullRom.Scale(dst, target, src, b, draw.Over, nil)
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func writeJPEG(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: 92})
}

// imageJPEGToPDF creates a one-page PDF with the given physical page size.
func imageJPEGToPDF(images []string, widthMM, heightMM float64, outPDF string) error {
	imp := pdfcpu.DefaultImportConfig()
	imp.PageSize = ""
	imp.PageDim = &types.Dim{Width: widthMM / 25.4 * 72, Height: heightMM / 25.4 * 72}
	imp.UserDim = true
	imp.Pos = types.Center
	imp.Scale = 1.0
	imp.DPI = DPI
	imp.InpUnit = types.POINTS
	return api.ImportImagesFile(images, outPDF, imp, nil)
}

// mergeImageSheets turns each sheet JPEG into a one-page PDF and merges them.
func mergeImageSheets(sheets []string, sizes []PageSize, outPDF string) error {
	if len(sheets) == 0 {
		return fmt.Errorf("没有生成任何发票页面")
	}
	var parts []string
	for i, sheet := range sheets {
		part := strings.TrimSuffix(sheet, filepath.Ext(sheet)) + ".pdf"
		size := PageSize{WidthMM: A4WidthMM, HeightMM: A4HeightMM}
		if i < len(sizes) {
			size = sizes[i]
		}
		if err := imageJPEGToPDF([]string{sheet}, size.WidthMM, size.HeightMM, part); err != nil {
			return err
		}
		parts = append(parts, part)
	}
	if len(parts) == 1 {
		return os.Rename(parts[0], outPDF)
	}
	return api.MergeCreateFile(parts, outPDF, false, nil)
}

func renderPDFPage(ctx context.Context, pdfPath string, page int, workDir string) (image.Image, error) {
	prefix := filepath.Join(workDir, fmt.Sprintf("pdf-%d", page))
	// Remove a previous render for the same page to avoid reading a stale file.
	_ = os.Remove(prefix + ".png")
	cmd := exec.CommandContext(ctx, "pdftoppm", "-r", strconv.Itoa(DPI), "-png", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-singlefile", pdfPath, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return decodeImage(prefix + ".png")
}

// trimPage removes uniform white margins from a rendered PDF page and returns
// the new physical size based on the 300dpi render.
func trimPage(img image.Image) (image.Image, float64, float64) {
	rect := contentBounds(img)
	b := img.Bounds()
	if rect == b {
		return img, float64(b.Dx()) / DPI * 25.4, float64(b.Dy()) / DPI * 25.4
	}
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), img, rect.Min, draw.Src)
	return out, float64(rect.Dx()) / DPI * 25.4, float64(rect.Dy()) / DPI * 25.4
}

func contentBounds(img image.Image) image.Rectangle {
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return b
	}
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	found := false
	step := 2
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r >= 0xF000 && g >= 0xF000 && bl >= 0xF000 {
				continue
			}
			found = true
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if !found {
		return b
	}
	pad := mmToPx(2)
	rect := image.Rect(minX-pad, minY-pad, maxX+pad, maxY+pad).Intersect(b)
	if rect.Dx() < b.Dx()/10 || rect.Dy() < b.Dy()/10 {
		return b
	}
	return rect
}

func expand(rect image.Rectangle, n int) image.Rectangle {
	return image.Rect(rect.Min.X-n, rect.Min.Y-n, rect.Max.X+n, rect.Max.Y+n)
}

func drawDashedRect(dst *image.RGBA, rect image.Rectangle, c color.Color, dash int) {
	uniform := image.NewUniform(c)
	drawDash := func(x0, y0, x1, y1 int, horizontal bool) {
		if horizontal {
			for x := x0; x < x1; x += dash * 2 {
				end := x + dash
				if end > x1 {
					end = x1
				}
				draw.Draw(dst, image.Rect(x, y0, end, y0+1), uniform, image.Point{}, draw.Src)
			}
			return
		}
		for y := y0; y < y1; y += dash * 2 {
			end := y + dash
			if end > y1 {
				end = y1
			}
			draw.Draw(dst, image.Rect(x0, y, x0+1, end), uniform, image.Point{}, draw.Src)
		}
	}
	drawDash(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y, true)
	drawDash(rect.Min.X, rect.Max.Y-1, rect.Max.X, rect.Max.Y-1, true)
	drawDash(rect.Min.X, rect.Min.Y, rect.Min.X, rect.Max.Y, false)
	drawDash(rect.Max.X-1, rect.Min.Y, rect.Max.X-1, rect.Max.Y, false)
}
