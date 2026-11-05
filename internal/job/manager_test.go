package job

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

type fakeConverter struct{}

func (fakeConverter) ToPDF(_ context.Context, _ string, dst string) error {
	return os.WriteFile(dst, []byte("%PDF-1.4\n"), 0o644)
}
func (fakeConverter) PageCount(_ context.Context, _ string) (int, error) { return 3, nil }
func (fakeConverter) SplitPDF(_ context.Context, src, _ string, _ int) ([]string, error) {
	return []string{src}, nil
}
func (fakeConverter) Thumbnail(_ context.Context, _, dst string) error {
	return os.WriteFile(dst, []byte("png"), 0o644)
}
func (fakeConverter) ThumbnailImage(_ context.Context, _, _ string) error { return nil }

type fakeRunner struct{}

func (fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	if name == "lp" {
		return "request id is dcp-ppd-9 (1 file(s))", "", nil
	}
	return "", "", nil
}

func TestProcessPendingJob(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	p := &storage.Printer{Name: "DCP", PPDQueue: "dcp-ppd", RAWQueue: "dcp-raw", DeviceURI: "usb://x"}
	if err := store.CreatePrinter(p); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "input.pdf")
	if err := os.WriteFile(src, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	j := &storage.Job{PrinterID: p.ID, Source: "web", Filename: "input.pdf", Status: "pending", StoredPath: src, Copies: 1, Duplex: "one-sided", PaperSize: "A4", Orientation: "portrait", ColorMode: "monochrome", Scaling: "auto"}
	if err := store.CreateJob(j); err != nil {
		t.Fatal(err)
	}
	cupsClient := cups.NewClient(fakeRunner{}, "")
	m := New(store, cupsClient, fakeConverter{}, dir, log.New(io.Discard, "", 0))
	m.processAll(context.Background())

	got, err := store.GetJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "submitted" || got.CupsJobID != "dcp-ppd-9" {
		t.Errorf("job = %+v, want submitted dcp-ppd-9", got)
	}
	if got.Pages != 3 {
		t.Errorf("pages = %d, want 3", got.Pages)
	}
	if got.PreviewPath == "" {
		t.Error("expected preview path")
	}
}

func TestRetryOnlyFailedWebJobs(t *testing.T) {
	dir := t.TempDir()
	store, _ := storage.Open(filepath.Join(dir, "test.db"))
	defer store.Close()
	_ = store.Migrate()
	p := &storage.Printer{Name: "DCP", PPDQueue: "dcp-ppd", RAWQueue: "dcp-raw", DeviceURI: "usb://x"}
	_ = store.CreatePrinter(p)
	j := &storage.Job{PrinterID: p.ID, Source: "web", Filename: "a.pdf", Status: "failed", StoredPath: "/tmp/a.pdf", Copies: 1}
	_ = store.CreateJob(j)
	m := New(store, cups.NewClient(fakeRunner{}, ""), fakeConverter{}, dir, log.New(io.Discard, "", 0))
	clone, err := m.Retry(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if clone.ID == j.ID || clone.Status != "pending" {
		t.Errorf("clone = %+v", clone)
	}
}

func TestValidatePageRange(t *testing.T) {
	cases := []struct {
		value string
		total int
		ok    bool
	}{
		{"", 10, true},
		{"1-2", 10, true},
		{"1-5 8", 10, true},
		{"1-20", 10, false},
		{"abc", 10, false},
	}
	for _, tc := range cases {
		err := validatePageRange(tc.value, tc.total)
		if (err == nil) != tc.ok {
			t.Errorf("validatePageRange(%q, %d) = %v, want ok=%v", tc.value, tc.total, err, tc.ok)
		}
	}
}
