package layout

import "strings"

// Slot is where one source page sits on a physical sheet.
//
// The browser renders the preview from these numbers and the print path builds
// its PDF from the same geometry, so there is exactly one layout calculation
// instead of one per renderer.
type Slot struct {
	Page   int `json:"page"`
	Col    int `json:"col"`
	Row    int `json:"row"`
	Rotate int `json:"rotate"` // 0 or 90
}

// Sheet is one physical sheet of paper and the pages placed on it.
type Sheet struct {
	Index int    `json:"index"`
	Slots []Slot `json:"slots"`
}

// Geometry describes the whole preview: paper size, printable margins, the grid
// and the placement of every page.
type Geometry struct {
	Cols          int     `json:"cols"`
	Rows          int     `json:"rows"`
	PaperWidth    float64 `json:"paper_width"`
	PaperHeight   float64 `json:"paper_height"`
	MarginLeft    float64 `json:"margin_left"`
	MarginTop     float64 `json:"margin_top"`
	MarginRight   float64 `json:"margin_right"`
	MarginBottom  float64 `json:"margin_bottom"`
	PageWidth     float64 `json:"page_width"`
	PageHeight    float64 `json:"page_height"`
	PagesPerSheet int     `json:"pages_per_sheet"`
	Border        bool    `json:"border"`
	Reverse       bool    `json:"reverse"`
	Flip          bool    `json:"flip_horizontal"`
	Sheets        []Sheet `json:"sheets"`
}

// Margins is the non-printable border declared by the PPD for one paper size.
type Margins struct {
	Left   float64
	Top    float64
	Right  float64
	Bottom float64
}

// ComputeGeometry works out how the selected pages land on each sheet.
//
// pageAspect is the width/height ratio of a source page. It decides whether a
// page needs to be rotated to fill its cell, which is what keeps portrait pages
// from being squeezed into landscape cells.
func ComputeGeometry(pages []int, opts Options, margins Margins, pageAspect float64) Geometry {
	perSheet := opts.PagesPerSheet
	if perSheet < 1 {
		perSheet = 1
	}
	if pageAspect <= 0 {
		pageAspect = 0.7071
	}
	landscape := strings.EqualFold(opts.Orientation, "landscape")
	paperWidth, paperHeight, _ := PaperSizePoints(opts.PaperSize, landscape)

	// The printable box is what the printer can actually reach; the grid is
	// chosen against it so the preview shows the same blank borders.
	boxWidth := paperWidth - margins.Left - margins.Right
	boxHeight := paperHeight - margins.Top - margins.Bottom
	if boxWidth <= 1 || boxHeight <= 1 {
		boxWidth, boxHeight = paperWidth, paperHeight
	}

	cols, rows := chooseGrid(perSheet, boxWidth, boxHeight, pageAspect, orientationName(landscape))
	if cols < 1 || rows < 1 {
		cols, rows = 1, 1
	}
	cellAspect := (boxWidth / float64(cols)) / (boxHeight / float64(rows))
	rotate := 0
	if cellAspect > 1.15 && pageAspect < 0.9 {
		rotate = 90
	} else if cellAspect < 0.87 && pageAspect > 1.15 {
		rotate = 90
	}

	geometry := Geometry{
		Cols:          cols,
		Rows:          rows,
		PaperWidth:    paperWidth,
		PaperHeight:   paperHeight,
		MarginLeft:    margins.Left,
		MarginTop:     margins.Top,
		MarginRight:   margins.Right,
		MarginBottom:  margins.Bottom,
		PageWidth:     paperWidth,
		PageHeight:    paperHeight,
		PagesPerSheet: perSheet,
		Border:        opts.Border,
		Reverse:       opts.Reverse,
		Flip:          opts.FlipHorizontal,
	}

	for start := 0; start < len(pages); start += perSheet {
		end := start + perSheet
		if end > len(pages) {
			end = len(pages)
		}
		sheet := Sheet{Index: len(geometry.Sheets)}
		for offset, page := range pages[start:end] {
			col, row := cellPosition(offset, cols, rows, opts.Direction)
			sheet.Slots = append(sheet.Slots, Slot{Page: page, Col: col, Row: row, Rotate: rotate})
		}
		geometry.Sheets = append(geometry.Sheets, sheet)
	}
	return geometry
}

// Rotate reports the rotation applied to every page. All slots share the same
// value because the grid and the source aspect are uniform for a document.
func (g Geometry) Rotate() int {
	for _, sheet := range g.Sheets {
		for _, slot := range sheet.Slots {
			return slot.Rotate
		}
	}
	return 0
}

func orientationName(landscape bool) string {
	if landscape {
		return "landscape"
	}
	return "portrait"
}

// cellPosition maps the n-th page of a sheet to a grid cell, honouring the
// requested reading direction.
func cellPosition(offset, cols, rows int, direction string) (col, row int) {
	switch strings.ToLower(direction) {
	case "rltb": // right to left, top to bottom
		row = offset / cols
		col = cols - 1 - offset%cols
	case "tblr": // top to bottom, left to right
		col = offset / rows
		row = offset % rows
	case "btlr": // bottom to top, left to right
		col = offset / rows
		row = rows - 1 - offset%rows
	default: // left to right, top to bottom
		row = offset / cols
		col = offset % cols
	}
	if col >= cols {
		col = cols - 1
	}
	if row >= rows {
		row = rows - 1
	}
	return col, row
}
