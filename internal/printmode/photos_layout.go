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
)

// PhotoPlacement is one manually positioned image on a sheet.
type PhotoPlacement struct {
	Index    int     `json:"index"`
	XMM      float64 `json:"x_mm"`
	YMM      float64 `json:"y_mm"`
	WMM      float64 `json:"w_mm"`
	HMM      float64 `json:"h_mm"`
	Rotation float64 `json:"rotation"`
	Fit      string  `json:"fit"` // cover / contain
}

// PhotoSheetLayout is one paper sheet in manual collage mode.
type PhotoSheetLayout struct {
	Items []PhotoPlacement `json:"items"`
}

// PhotoLayout is the complete manual collage layout.
type PhotoLayout struct {
	Sheets []PhotoSheetLayout `json:"sheets"`
}

// ComposePhotoLayout renders a manual collage layout into a PDF.
func ComposePhotoLayout(ctx context.Context, inputs []PhotoInput, layout PhotoLayout, outPDF, workDir string, paperWidthMM, paperHeightMM float64) (PhotoResult, error) {
	if len(inputs) == 0 {
		return PhotoResult{}, fmt.Errorf("没有图片")
	}
	if paperWidthMM <= 0 || paperHeightMM <= 0 {
		return PhotoResult{}, fmt.Errorf("纸张尺寸无效")
	}
	if len(layout.Sheets) == 0 {
		return PhotoResult{}, fmt.Errorf("拼贴版面为空")
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return PhotoResult{}, err
	}
	cache := map[int]image.Image{}
	var sheets []string
	var sizes []PageSize
	for index, sheet := range layout.Sheets {
		pageW, pageH := mmToPx(paperWidthMM), mmToPx(paperHeightMM)
		canvas := image.NewRGBA(image.Rect(0, 0, pageW, pageH))
		draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		for _, placement := range sheet.Items {
			if placement.Index < 0 || placement.Index >= len(inputs) {
				return PhotoResult{}, fmt.Errorf("拼贴引用了不存在的图片")
			}
			img, ok := cache[placement.Index]
			if !ok {
				decoded, err := decodeImage(inputs[placement.Index].Path)
				if err != nil {
					return PhotoResult{}, fmt.Errorf("%s: %w", inputs[placement.Index].Name, err)
				}
				img = decoded
				cache[placement.Index] = decoded
			}
			if placement.Rotation != 0 {
				img = rotateImage(img, placement.Rotation)
			}
			visualW, visualH := rotatedSize(placement.WMM, placement.HMM, placement.Rotation)
			rect := image.Rect(
				mmToPx(placement.XMM), mmToPx(placement.YMM),
				mmToPx(placement.XMM+visualW), mmToPx(placement.YMM+visualH),
			)
			if rect.Dx() <= 0 || rect.Dy() <= 0 {
				continue
			}
			if placement.Fit == "contain" {
				drawFit(canvas, img, rect)
			} else {
				drawCover(canvas, img, rect)
			}
		}
		path := filepath.Join(workDir, fmt.Sprintf("collage-%03d.jpg", index+1))
		if err := writeJPEG(canvas, path); err != nil {
			return PhotoResult{}, err
		}
		sheets = append(sheets, path)
		sizes = append(sizes, PageSize{WidthMM: paperWidthMM, HeightMM: paperHeightMM})
	}
	if err := mergeImageSheets(sheets, sizes, outPDF); err != nil {
		return PhotoResult{}, err
	}
	return PhotoResult{PageCount: len(sheets)}, nil
}

func rotatedSize(w, h, degrees float64) (float64, float64) {
	angle := degrees * math.Pi / 180
	return math.Abs(w*math.Cos(angle)) + math.Abs(h*math.Sin(angle)),
		math.Abs(w*math.Sin(angle)) + math.Abs(h*math.Cos(angle))
}

func rotateImage(src image.Image, degrees float64) image.Image {
	angle := degrees * math.Pi / 180
	cos, sin := math.Cos(angle), math.Sin(angle)
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	outW := int(math.Ceil(math.Abs(float64(w)*cos) + math.Abs(float64(h)*sin)))
	outH := int(math.Ceil(math.Abs(float64(w)*sin) + math.Abs(float64(h)*cos)))
	out := image.NewRGBA(image.Rect(0, 0, outW, outH))
	cx, cy := float64(outW)/2, float64(outH)/2
	scx, scy := float64(w)/2, float64(h)/2
	for y := 0; y < outH; y++ {
		for x := 0; x < outW; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			sx := cos*dx + sin*dy + scx
			sy := -sin*dx + cos*dy + scy
			if sx < 0 || sy < 0 || sx >= float64(w) || sy >= float64(h) {
				continue
			}
			out.Set(x, y, src.At(b.Min.X+int(sx), b.Min.Y+int(sy)))
		}
	}
	return out
}
