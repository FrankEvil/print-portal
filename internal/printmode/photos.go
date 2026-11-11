package printmode

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
)

// PhotoInput is one cropped image for photo printing.
type PhotoInput struct {
	Path string
	Name string
}

// PhotoOptions controls photo sheet composition.
type PhotoOptions struct {
	PaperWidthMM  float64
	PaperHeightMM float64
	PerSheet      int
	Arrangement   string // auto / horizontal / vertical
	Scaling       string // fit / fill
	Borderless    bool
	MarginMM      float64
	CutLines      bool
}

// PhotoResult is returned by ComposePhotos.
type PhotoResult struct {
	PageCount int `json:"page_count"`
}

// ComposePhotos places images in a grid on the selected paper size.
func ComposePhotos(ctx context.Context, inputs []PhotoInput, outPDF, workDir string, opts PhotoOptions) (PhotoResult, error) {
	if len(inputs) == 0 {
		return PhotoResult{}, fmt.Errorf("没有图片")
	}
	if opts.PerSheet != 1 && opts.PerSheet != 2 && opts.PerSheet != 4 && opts.PerSheet != 6 && opts.PerSheet != 8 {
		opts.PerSheet = 4
	}
	if opts.PaperWidthMM <= 0 || opts.PaperHeightMM <= 0 {
		return PhotoResult{}, fmt.Errorf("纸张尺寸无效")
	}
	if opts.MarginMM < 0 {
		opts.MarginMM = 0
	}
	if opts.Borderless {
		opts.MarginMM = 0
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return PhotoResult{}, err
	}
	images := make([]image.Image, 0, len(inputs))
	for _, input := range inputs {
		img, err := decodeImage(input.Path)
		if err != nil {
			return PhotoResult{}, fmt.Errorf("%s: %w", input.Name, err)
		}
		images = append(images, img)
	}
	var sheets []string
	var sizes []PageSize
	for start := 0; start < len(images); start += opts.PerSheet {
		end := start + opts.PerSheet
		if end > len(images) {
			end = len(images)
		}
		canvas := composePhotoSheet(images[start:end], opts)
		path := filepath.Join(workDir, fmt.Sprintf("photo-%03d.jpg", len(sheets)+1))
		if err := writeJPEG(canvas, path); err != nil {
			return PhotoResult{}, err
		}
		sheets = append(sheets, path)
		sizes = append(sizes, PageSize{WidthMM: opts.PaperWidthMM, HeightMM: opts.PaperHeightMM})
	}
	if err := mergeImageSheets(sheets, sizes, outPDF); err != nil {
		return PhotoResult{}, err
	}
	return PhotoResult{PageCount: len(sheets)}, nil
}

func composePhotoSheet(images []image.Image, opts PhotoOptions) image.Image {
	pageW, pageH := mmToPx(opts.PaperWidthMM), mmToPx(opts.PaperHeightMM)
	canvas := image.NewRGBA(image.Rect(0, 0, pageW, pageH))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	cols, rows := choosePhotoGrid(images, opts.PerSheet, opts.PaperWidthMM, opts.PaperHeightMM, opts.PaperWidthMM > opts.PaperHeightMM)
	if opts.Arrangement == "horizontal" && cols < rows {
		cols, rows = rows, cols
	}
	if opts.Arrangement == "vertical" && rows < cols {
		cols, rows = rows, cols
	}
	margin := mmToPx(opts.MarginMM)
	gap := mmToPx(2)
	if opts.Borderless {
		gap = 0
	}
	cellW := (pageW - 2*margin - (cols-1)*gap) / cols
	cellH := (pageH - 2*margin - (rows-1)*gap) / rows
	if cellW <= 0 || cellH <= 0 {
		return canvas
	}
	for index, img := range images {
		if index >= cols*rows {
			break
		}
		col, row := index%cols, index/cols
		x := margin + col*(cellW+gap)
		y := margin + row*(cellH+gap)
		rect := image.Rect(x, y, x+cellW, y+cellH)
		if opts.Scaling == "fill" {
			drawCover(canvas, img, rect)
		} else {
			drawFit(canvas, img, rect)
		}
		if opts.CutLines && !opts.Borderless {
			drawDashedRect(canvas, expand(rect, mmToPx(1.5)), color.RGBA{150, 160, 175, 255}, 7)
		}
	}
	return canvas
}

// choosePhotoGrid scores candidate grids against every image on the sheet,
// not against one averaged aspect ratio. Mixed landscape/portrait batches then
// pick a grid that balances the actual images.
func choosePhotoGrid(images []image.Image, perSheet int, paperW, paperH float64, landscape bool) (int, int) {
	if perSheet <= 1 || len(images) == 0 {
		return 1, 1
	}
	count := perSheet
	if len(images) < count {
		count = len(images)
	}
	bestScore := math.MaxFloat64
	bestCols, bestRows := 1, count
	for rows := 1; rows <= count; rows++ {
		cols := (count + rows - 1) / rows
		cellAspect := (paperW / float64(cols)) / (paperH / float64(rows))
		fillSum := 0.0
		for _, img := range images {
			b := img.Bounds()
			if b.Dy() == 0 {
				continue
			}
			aspect := float64(b.Dx()) / float64(b.Dy())
			fill := ratioFit(aspect, cellAspect)
			if inverse := ratioFit(1/aspect, cellAspect); inverse > fill {
				fill = inverse
			}
			fillSum += fill
		}
		unused := cols*rows - count
		score := (float64(len(images)) - fillSum) + float64(unused)*0.75
		if score < bestScore-1e-9 {
			bestScore = score
			bestCols, bestRows = cols, rows
		} else if math.Abs(score-bestScore) < 1e-9 {
			if (landscape && cols > bestCols) || (!landscape && rows > bestRows) {
				bestCols, bestRows = cols, rows
			}
		}
	}
	return bestCols, bestRows
}

func ratioFit(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > b {
		return b / a
	}
	return a / b
}

func drawCover(dst *image.RGBA, src image.Image, rect image.Rectangle) {
	b := src.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return
	}
	scale := math.Max(float64(rect.Dx())/float64(b.Dx()), float64(rect.Dy())/float64(b.Dy()))
	w := float64(b.Dx()) * scale
	h := float64(b.Dy()) * scale
	sx := float64(b.Min.X) + (w-float64(rect.Dx()))/2/scale
	sy := float64(b.Min.Y) + (h-float64(rect.Dy()))/2/scale
	srcRect := image.Rect(int(sx), int(sy), int(sx)+int(float64(rect.Dx())/scale), int(sy)+int(float64(rect.Dy())/scale))
	xdraw.CatmullRom.Scale(dst, rect, src, srcRect, draw.Over, nil)
}
