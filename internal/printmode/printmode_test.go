package printmode

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	if pages, err := api.PageCountFile(out); err != nil || pages != 1 {
		t.Fatalf("actual PDF pages = %d, err=%v, want 1", pages, err)
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
	if pages, err := api.PageCountFile(out); err != nil || pages != 1 {
		t.Fatalf("actual PDF pages = %d, err=%v, want 1", pages, err)
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
	if pages, err := api.PageCountFile(out); err != nil || pages != 1 {
		t.Fatalf("actual PDF pages = %d, err=%v, want 1", pages, err)
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

func TestComposeInvoiceFromTwoPDFs(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not installed")
	}
	dir := t.TempDir()
	mk := func(name string, c color.Color) string {
		pngPath := filepath.Join(dir, name+".png")
		writePNG(t, pngPath, 600, 400, c)
		img, err := decodeImage(pngPath)
		if err != nil {
			t.Fatal(err)
		}
		jpgPath := filepath.Join(dir, name+".jpg")
		if err := writeJPEG(img, jpgPath); err != nil {
			t.Fatal(err)
		}
		pdfPath := filepath.Join(dir, name+".pdf")
		if err := imageJPEGToPDF([]string{jpgPath}, 210, 140, pdfPath); err != nil {
			t.Fatal(err)
		}
		return pdfPath
	}
	a := mk("a", color.RGBA{255, 255, 255, 255})
	b := mk("b", color.RGBA{250, 250, 250, 255})
	out := filepath.Join(dir, "out.pdf")
	res, err := ComposeInvoice(context.Background(), []InvoiceInput{
		{Path: a, Name: "a.pdf", Kind: "pdf"},
		{Path: b, Name: "b.pdf", Kind: "pdf"},
	}, out, filepath.Join(dir, "work"), InvoiceOptions{PerSheet: 2, Trim: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.PageCount == 0 {
		t.Fatal("result page_count is zero")
	}
	if pages, err := api.PageCountFile(out); err != nil || pages != res.PageCount {
		t.Fatalf("actual pages = %d, err=%v, result page_count=%d", pages, err, res.PageCount)
	}
}

func TestComposePhotos(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	out := filepath.Join(dir, "photos.pdf")
	writePNG(t, a, 800, 600, color.RGBA{255, 200, 200, 255})
	writePNG(t, b, 600, 800, color.RGBA{200, 220, 255, 255})
	res, err := ComposePhotos(context.Background(), []PhotoInput{{Path: a, Name: "a.png"}, {Path: b, Name: "b.png"}}, out, filepath.Join(dir, "work"), PhotoOptions{
		PaperWidthMM: 102, PaperHeightMM: 152, PerSheet: 2, Arrangement: "auto", Scaling: "fit", Borderless: true, CutLines: true,
	})
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
	if abs(wmm-102) > 1 || abs(hmm-152) > 1 {
		t.Fatalf("page = %.1f x %.1f mm, want 6 inch", wmm, hmm)
	}
}

func TestComposeBooklet(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not installed")
	}
	dir := t.TempDir()
	var jpegs []string
	for i := 0; i < 8; i++ {
		pngPath := filepath.Join(dir, fmt.Sprintf("p%d.png", i))
		writePNG(t, pngPath, 600, 800, color.RGBA{255, 255, 255, 255})
		img, err := decodeImage(pngPath)
		if err != nil {
			t.Fatal(err)
		}
		jpgPath := filepath.Join(dir, fmt.Sprintf("p%d.jpg", i))
		if err := writeJPEG(img, jpgPath); err != nil {
			t.Fatal(err)
		}
		jpegs = append(jpegs, jpgPath)
	}
	src := filepath.Join(dir, "src.pdf")
	if err := imageJPEGToPDF(jpegs, 210, 297, src); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "book.pdf")
	front := filepath.Join(dir, "front.pdf")
	back := filepath.Join(dir, "back.pdf")
	res, err := ComposeBooklet(context.Background(), src, out, front, back, filepath.Join(dir, "work"), BookletOptions{PaperWidthMM: 297, PaperHeightMM: 210})
	if err != nil {
		t.Fatal(err)
	}
	if res.SheetCount != 2 || res.PageCount != 4 {
		t.Fatalf("sheets=%d pages=%d, want 2/4", res.SheetCount, res.PageCount)
	}
	for _, p := range []string{out, front, back} {
		pages, err := api.PageCountFile(p)
		if err != nil || pages == 0 {
			t.Fatalf("%s pages=%d err=%v", p, pages, err)
		}
	}
}

func TestBuildBookletGeometry(t *testing.T) {
	g := BuildBookletGeometry(8, nil, 0)
	if g.PaddedPages != 8 || g.SheetCount != 2 || g.SideCount != 4 {
		t.Fatalf("geometry = %+v", g)
	}
	if !reflect.DeepEqual(g.Sheets[0].Front, []int{8, 1}) || !reflect.DeepEqual(g.Sheets[0].Back, []int{2, 7}) {
		t.Fatalf("first sheet = %+v", g.Sheets[0])
	}
	if !reflect.DeepEqual(g.Sheets[1].Front, []int{6, 3}) || !reflect.DeepEqual(g.Sheets[1].Back, []int{4, 5}) {
		t.Fatalf("second sheet = %+v", g.Sheets[1])
	}
	// 38 pages become 40 with two blank slots on the first/last sheet.
	g = BuildBookletGeometry(38, nil, 0)
	if g.PaddedPages != 40 || g.SheetCount != 10 || g.Sheets[0].Front[0] != 0 {
		t.Fatalf("38-page geometry = %+v", g.Sheets[0])
	}
}
