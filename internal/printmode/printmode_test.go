package printmode

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func writePNG(t *testing.T, path string, width, height int, c color.Color) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestComposeIDCard(t *testing.T) {
	dir := t.TempDir()
	front := filepath.Join(dir, "front.png")
	back := filepath.Join(dir, "back.png")
	out := filepath.Join(dir, "idcard.pdf")
	writePNG(t, front, 900, 568, color.RGBA{200, 220, 255, 255})
	writePNG(t, back, 900, 568, color.RGBA{255, 235, 210, 255})

	if err := ComposeIDCard(context.Background(), front, back, out, filepath.Join(dir, "work"), IDCardOptions{Layout: "vertical", Border: true}); err != nil {
		t.Fatal(err)
	}
	dims, err := api.PageDimsFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(dims) != 1 {
		t.Fatalf("pages = %d, want 1", len(dims))
	}
	wmm, hmm := dims[0].Width/72*25.4, dims[0].Height/72*25.4
	if abs(wmm-210) > 1 || abs(hmm-297) > 1 {
		t.Fatalf("page = %.1f x %.1f mm, want A4", wmm, hmm)
	}
}

func TestComposeInvoiceTwoUp(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	out := filepath.Join(dir, "invoice.pdf")
	writePNG(t, a, 600, 400, color.RGBA{255, 255, 255, 255})
	writePNG(t, b, 600, 400, color.RGBA{255, 255, 255, 255})

	res, err := ComposeInvoice(context.Background(), []InvoiceInput{
		{Path: a, Name: "a.png", Kind: "image", WidthMM: 210, HeightMM: 140},
		{Path: b, Name: "b.png", Kind: "image", WidthMM: 210, HeightMM: 140},
	}, out, filepath.Join(dir, "work"), InvoiceOptions{PerSheet: 2, CutLines: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.PageCount != 1 {
		t.Fatalf("page count = %d, want 1", res.PageCount)
	}
	dims, err := api.PageDimsFile(out)
	if err != nil {
		t.Fatal(err)
	}
	wmm, hmm := dims[0].Width/72*25.4, dims[0].Height/72*25.4
	if abs(wmm-210) > 1 || abs(hmm-297) > 1 {
		t.Fatalf("page = %.1f x %.1f mm, want A4 portrait", wmm, hmm)
	}
}

func TestComposeInvoiceLandscape(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	out := filepath.Join(dir, "invoice.pdf")
	writePNG(t, a, 800, 460, color.RGBA{255, 255, 255, 255})

	res, err := ComposeInvoice(context.Background(), []InvoiceInput{
		{Path: a, Name: "a.png", Kind: "image", WidthMM: 241, HeightMM: 140},
	}, out, filepath.Join(dir, "work"), InvoiceOptions{PerSheet: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.PageCount != 1 {
		t.Fatalf("page count = %d, want 1", res.PageCount)
	}
	dims, err := api.PageDimsFile(out)
	if err != nil {
		t.Fatal(err)
	}
	wmm, hmm := dims[0].Width/72*25.4, dims[0].Height/72*25.4
	if abs(wmm-297) > 1 || abs(hmm-210) > 1 {
		t.Fatalf("page = %.1f x %.1f mm, want A4 landscape", wmm, hmm)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
