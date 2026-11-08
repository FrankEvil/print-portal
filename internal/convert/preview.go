package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PreviewOptions describes the print layout used for a visual preview.
type PreviewOptions struct {
	PaperSize   string
	Orientation string
	ColorMode   string
	Scaling     string
	Page        int
}

var paperSizePoints = map[string][2]float64{
	"A4":        {595.28, 841.89},
	"A5":        {419.53, 595.28},
	"A6":        {297.64, 419.53},
	"B5":        {498.90, 708.66},
	"B6":        {354.33, 498.90},
	"L":         {252.28, 360.00},
	"Postcard":  {283.46, 419.53},
	"TPostcard": {419.53, 283.46},
	"ENV10":     {297.00, 684.00},
	"EnvDL":     {311.81, 623.62},
	"ENVC6":     {323.15, 459.21},
	"Letter":    {612.00, 792.00},
	"Legal":     {612.00, 1008.00},
}

// PreviewPNG renders one page of src to dst as a PNG, using the selected
// paper size, orientation and color mode. It is intentionally a layout
// preview: the printer's PPD/filter chain remains the source of truth for the
// actual output, but this gives users a faithful page/paper simulation.
func (c *Converter) PreviewPNG(ctx context.Context, src, dst string, opts PreviewOptions) error {
	pdfPath := src
	if Detect(src) != KindPDF {
		if err := os.MkdirAll(c.WorkDir, 0o755); err != nil {
			return err
		}
		pdfPath = filepath.Join(c.WorkDir, fmt.Sprintf("preview-%d.pdf", time.Now().UnixNano()))
		if err := c.ToPDF(ctx, src, pdfPath); err != nil {
			return err
		}
		defer os.Remove(pdfPath)
	}
	page := opts.Page
	if page < 1 {
		page = 1
	}
	width, height := paperSizePoints["A4"][0], paperSizePoints["A4"][1]
	if size, ok := paperSizePoints[opts.PaperSize]; ok {
		width, height = size[0], size[1]
	}
	if strings.EqualFold(opts.Orientation, "landscape") {
		width, height = height, width
	}
	device := "png16m"
	if strings.EqualFold(opts.ColorMode, "monochrome") {
		device = "pnggray"
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	args := []string{
		"-q", "-dNOPAUSE", "-dBATCH", "-dSAFER",
		"-sDEVICE=" + device,
		"-r72",
		"-dFirstPage=" + strconv.Itoa(page),
		"-dLastPage=" + strconv.Itoa(page),
		"-dFIXEDMEDIA",
		"-dPDFFitPage",
		"-dAutoRotatePages=/None",
		fmt.Sprintf("-dDEVICEWIDTHPOINTS=%s", strconv.FormatFloat(width, 'f', 2, 64)),
		fmt.Sprintf("-dDEVICEHEIGHTPOINTS=%s", strconv.FormatFloat(height, 'f', 2, 64)),
		"-sOutputFile=" + dst,
		pdfPath,
	}
	return run(ctx, "gs", args...)
}
