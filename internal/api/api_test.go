package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liuzhaojun/print-portal/internal/config"
	"github.com/liuzhaojun/print-portal/internal/convert"
	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/discovery"
	"github.com/liuzhaojun/print-portal/internal/driver"
	"github.com/liuzhaojun/print-portal/internal/job"
	"github.com/liuzhaojun/print-portal/internal/printer"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

type testRunner struct{}

func (testRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	joined := strings.Join(args, " ")
	switch {
	case name == "lpstat" && strings.Contains(joined, "-r"):
		return "scheduler is running", "", nil
	case name == "lpstat" && strings.Contains(joined, "-p"):
		return "printer dcp-ppd is idle.  enabled since now", "", nil
	case name == "lpstat" && strings.Contains(joined, "-v"):
		return "device for dcp-ppd: usb://Brother/DCP-7060D\n", "", nil
	case name == "lpstat" && strings.Contains(joined, "-o"):
		return "", "", nil
	case name == "lp":
		return "request id is dcp-ppd-1 (1 file(s))", "", nil
	default:
		return "", "", nil
	}
}

type testConverter struct{}

func (testConverter) ToPDF(_ context.Context, _ string, dst string) error {
	return os.WriteFile(dst, []byte("%PDF-1.4\n"), 0o644)
}
func (testConverter) PageCount(_ context.Context, _ string) (int, error) { return 1, nil }
func (testConverter) Thumbnail(_ context.Context, _, dst string) error {
	return os.WriteFile(dst, []byte("png"), 0o644)
}
func (testConverter) ThumbnailImage(_ context.Context, _, _ string) error { return nil }

func newTestServer(t *testing.T) (*httptest.Server, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Addr: ":0", DataDir: dir, AdminToken: "secret", MaxUploadMB: 10,
		CUPSServer: "127.0.0.1:631", Version: "test",
	}
	cupsClient := cups.NewClient(testRunner{}, "")
	jobManager := job.New(store, cupsClient, testConverter{}, dir, log.New(io.Discard, "", 0))
	server := New(cfg, store, cupsClient, jobManager,
		printer.NewService(store, cupsClient),
		driver.NewService(store, cupsClient, dir),
		discovery.NewService(store, cupsClient),
		log.New(io.Discard, "", 0))
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(func() { ts.Close(); store.Close() })
	return ts, store
}

func TestHealth(t *testing.T) {
	ts, _ := newTestServer(t)
	res, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("body = %v", body)
	}
}

func TestAdminAuth(t *testing.T) {
	ts, _ := newTestServer(t)
	res, _ := http.Get(ts.URL + "/api/admin/overview")
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("without token status = %d, want 401", res.StatusCode)
	}
	res.Body.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/admin/overview", nil)
	req.Header.Set("X-Admin-Token", "secret")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("with token status = %d, want 200", res.StatusCode)
	}
}

func TestPrintEndpoint(t *testing.T) {
	ts, store := newTestServer(t)
	p := &storage.Printer{Name: "DCP", PPDQueue: "dcp-ppd", RAWQueue: "dcp-raw", DeviceURI: "usb://x", IsDefault: true}
	if err := store.CreatePrinter(p); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", "test.pdf")
	_, _ = part.Write([]byte("%PDF-1.4\n"))
	_ = w.WriteField("printer_id", "1")
	_ = w.WriteField("copies", "2")
	_ = w.WriteField("duplex", "two-sided-long-edge")
	w.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/print", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status = %d, body = %s", res.StatusCode, body)
	}
	var out struct {
		JobID int64 `json:"job_id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if out.JobID == 0 {
		t.Fatal("expected job_id")
	}
	j, _ := store.GetJob(out.JobID)
	if j == nil || j.Status != "pending" || j.Copies != 2 {
		t.Errorf("job = %+v", j)
	}

	// Unsupported file type is rejected.
	var buf2 bytes.Buffer
	w2 := multipart.NewWriter(&buf2)
	p2, _ := w2.CreateFormFile("file", "test.exe")
	_, _ = p2.Write([]byte("MZ"))
	_ = w2.WriteField("printer_id", "1")
	w2.Close()
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/print", &buf2)
	req2.Header.Set("Content-Type", w2.FormDataContentType())
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("exe upload status = %d, want 422", res2.StatusCode)
	}
}

func TestConvertDetectUsedByAPI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.pdf")
	_ = os.WriteFile(path, []byte("%PDF"), 0o644)
	if convert.Detect(path) != convert.KindPDF {
		t.Error("detect failed")
	}
}
