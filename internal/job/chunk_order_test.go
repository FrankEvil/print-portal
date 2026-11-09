package job

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/liuzhaojun/print-portal/internal/layout"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// Layout must happen before chunking: with 2 pages per sheet, pages 3 and 4
// belong on the same sheet. Splitting the source first would separate them.
func TestLayoutBeforeChunkKeepsSheetsIntact(t *testing.T) {
	if _, err := exec.LookPath("gs"); err != nil {
		t.Skip("ghostscript not available")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src.pdf")
	build := exec.Command("gs", "-q", "-dNOPAUSE", "-dBATCH", "-sDEVICE=pdfwrite",
		"-sOutputFile="+src, "-c",
		"/Helvetica findfont 24 scalefont setfont 72 700 moveto (P1) show showpage "+
			"72 700 moveto (P2) show showpage 72 700 moveto (P3) show showpage "+
			"72 700 moveto (P4) show showpage 72 700 moveto (P5) show showpage")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build pdf: %v %s", err, out)
	}

	laid := filepath.Join(dir, "layout.pdf")
	opts := layout.Options{PagesPerSheet: 2, PaperSize: "A4", Orientation: "portrait"}
	if err := layout.Apply(context.Background(), src, laid, opts); err != nil {
		t.Fatalf("layout: %v", err)
	}
	sheets, err := api.PageCountFile(laid)
	if err != nil {
		t.Fatal(err)
	}
	// 5 source pages at 2 per sheet = 3 sheets (2 + 2 + 1).
	if sheets != 3 {
		t.Fatalf("expected 3 sheets, got %d", sheets)
	}

	// Chunking the laid-out PDF must split at sheet boundaries, not page pairs.
	chunkDir := filepath.Join(dir, "chunks")
	if err := os.MkdirAll(chunkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	chunks := []string{
		filepath.Join(chunkDir, "c1.pdf"),
		filepath.Join(chunkDir, "c2.pdf"),
	}
	if err := api.CollectFile(laid, chunks[0], []string{"1-2"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := api.CollectFile(laid, chunks[1], []string{"3"}, nil); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, chunk := range chunks {
		pages, err := api.PageCountFile(chunk)
		if err != nil {
			t.Fatal(err)
		}
		total += pages
	}
	if total != sheets {
		t.Fatalf("chunks cover %d sheets, layout produced %d", total, sheets)
	}
	t.Logf("source=5 pages, layout=%d sheets, chunks=%d+%d", sheets, 2, 1)
}
