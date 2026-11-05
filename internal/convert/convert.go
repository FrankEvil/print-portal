package convert

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Kind is a detected input file type.
type Kind string

const (
	KindPDF        Kind = "pdf"
	KindImage      Kind = "image"
	KindOffice     Kind = "office"
	KindPostscript Kind = "postscript"
	KindUnknown    Kind = "unknown"
)

// Detect identifies a file by magic bytes, falling back to extension.
func Detect(path string) Kind {
	f, err := os.Open(path)
	if err != nil {
		return KindUnknown
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch {
	case bytes.HasPrefix(head, []byte("%PDF")):
		return KindPDF
	case bytes.HasPrefix(head, []byte("%!PS")):
		return KindPostscript
	case bytes.HasPrefix(head, []byte{0x89, 'P', 'N', 'G'}):
		return KindImage
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return KindImage
	case bytes.HasPrefix(head, []byte("RIFF")) && len(head) > 12 && string(head[8:12]) == "WEBP":
		return KindImage
	case bytes.HasPrefix(head, []byte("II*\x00")) || bytes.HasPrefix(head, []byte("MM\x00*")):
		return KindImage
	case bytes.HasPrefix(head, []byte("GIF8")):
		return KindImage
	case bytes.HasPrefix(head, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		return KindOffice
	case bytes.HasPrefix(head, []byte("PK\x03\x04")):
		switch strings.ToLower(filepath.Ext(path)) {
		case ".docx", ".xlsx", ".pptx", ".odt", ".ods", ".odp":
			return KindOffice
		}
		return KindUnknown
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return KindPDF
	case ".jpg", ".jpeg", ".png", ".webp", ".tif", ".tiff", ".gif", ".bmp", ".heic":
		return KindImage
	case ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".odt", ".ods", ".odp":
		return KindOffice
	case ".ps":
		return KindPostscript
	}
	return KindUnknown
}

// Converter converts input documents to PDF and generates previews.
type Converter struct {
	// WorkDir is the root for scratch files.
	WorkDir string
}

func New(workDir string) *Converter { return &Converter{WorkDir: workDir} }

// ToPDF writes a PDF version of src to dst.
func (c *Converter) ToPDF(ctx context.Context, src, dst string) error {
	switch Detect(src) {
	case KindPDF:
		return copyFile(src, dst)
	case KindImage:
		return c.imageToPDF(ctx, src, dst)
	case KindOffice:
		return c.officeToPDF(ctx, src, dst)
	case KindPostscript:
		return run(ctx, "ps2pdf", src, dst)
	default:
		return fmt.Errorf("unsupported file type: %s", filepath.Base(src))
	}
}

func (c *Converter) imageToPDF(ctx context.Context, src, dst string) error {
	// img2pdf gives lossless, policy-friendly image -> PDF conversion.
	if _, err := exec.LookPath("img2pdf"); err == nil {
		return run(ctx, "img2pdf", src, "-o", dst)
	}
	bin := "convert"
	if _, err := exec.LookPath("magick"); err == nil {
		bin = "magick"
	} else if _, err := exec.LookPath("convert"); err != nil {
		return fmt.Errorf("image conversion tool not found (need img2pdf or ImageMagick)")
	}
	return run(ctx, bin, src, dst)
}

func (c *Converter) officeToPDF(ctx context.Context, src, dst string) error {
	bin := ""
	for _, candidate := range []string{"soffice", "libreoffice"} {
		if _, err := exec.LookPath(candidate); err == nil {
			bin = candidate
			break
		}
	}
	if bin == "" {
		return fmt.Errorf("LibreOffice not found (need soffice/libreoffice to convert Office documents)")
	}
	outDir := filepath.Dir(dst)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := run(ctx, bin, "--headless", "--norestore", "--convert-to", "pdf", "--outdir", outDir, src); err != nil {
		return err
	}
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)) + ".pdf"
	produced := filepath.Join(outDir, base)
	if produced != dst {
		if err := os.Rename(produced, dst); err != nil {
			if _, statErr := os.Stat(dst); statErr != nil {
				return fmt.Errorf("converted PDF not found: %w", err)
			}
		}
	}
	return nil
}

var pagesRE = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)

// PageCount returns the number of pages in a PDF using pdfinfo.
func (c *Converter) PageCount(ctx context.Context, pdfPath string) (int, error) {
	out, err := output(ctx, "pdfinfo", pdfPath)
	if err != nil {
		return 0, err
	}
	m := pagesRE.FindStringSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("could not determine page count")
	}
	return strconv.Atoi(m[1])
}

// SplitPDF splits a PDF into small PDFs so large jobs can be fed to the
// printer in controlled chunks instead of one huge USB transfer.
func (c *Converter) SplitPDF(ctx context.Context, src, dstDir string, chunkPages int) ([]string, error) {
	pages, err := c.PageCount(ctx, src)
	if err != nil {
		return nil, err
	}
	if chunkPages <= 0 || pages <= chunkPages {
		return []string{src}, nil
	}
	if _, err := exec.LookPath("gs"); err != nil {
		return nil, fmt.Errorf("PDF split requires ghostscript (gs)")
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, err
	}
	var chunks []string
	for start := 1; start <= pages; start += chunkPages {
		end := start + chunkPages - 1
		if end > pages {
			end = pages
		}
		out := filepath.Join(dstDir, fmt.Sprintf("chunk-%03d-%03d.pdf", start, end))
		if err := run(ctx, "gs", "-q", "-dNOPAUSE", "-dBATCH", "-sDEVICE=pdfwrite",
			fmt.Sprintf("-dFirstPage=%d", start), fmt.Sprintf("-dLastPage=%d", end),
			fmt.Sprintf("-sOutputFile=%s", out), src); err != nil {
			return nil, err
		}
		chunks = append(chunks, out)
	}
	return chunks, nil
}

// Thumbnail renders the first page of a PDF to a PNG.
func (c *Converter) Thumbnail(ctx context.Context, pdfPath, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return run(ctx, "pdftoppm", "-png", "-singlefile", "-f", "1", "-l", "1", "-r", "72", pdfPath, strings.TrimSuffix(dst, ".png"))
}

// ThumbnailImage renders an image thumbnail using ImageMagick.
func (c *Converter) ThumbnailImage(ctx context.Context, src, dst string) error {
	bin := "convert"
	if _, err := exec.LookPath("magick"); err == nil {
		bin = "magick"
	}
	return run(ctx, bin, src, "-thumbnail", "800x800", dst)
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %s: %w", name, strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

func output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %s: %w", name, strings.TrimSpace(stderr.String()), err)
	}
	return stdout.String(), nil
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
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
