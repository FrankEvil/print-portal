package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liuzhaojun/print-portal/internal/config"
	"github.com/liuzhaojun/print-portal/internal/convert"
	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/discovery"
	"github.com/liuzhaojun/print-portal/internal/driver"
	"github.com/liuzhaojun/print-portal/internal/job"
	"github.com/liuzhaojun/print-portal/internal/printer"
	"github.com/liuzhaojun/print-portal/internal/scan"
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
func (testConverter) SplitPDF(_ context.Context, src, _ string, _ int) ([]string, error) {
	return []string{src}, nil
}
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
		scan.NewService(store, dir, testRunner{}),
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

func TestSweepPreviewSessions(t *testing.T) {
	dir := t.TempDir()
	previewDir := filepath.Join(dir, "previews")
	if err := os.MkdirAll(previewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(previewDir, "old.pdf")
	activePath := filepath.Join(previewDir, "active.pdf")
	orphanPath := filepath.Join(previewDir, "orphan.pdf")
	for _, path := range []string{oldPath, activePath, orphanPath} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-3 * time.Hour)
	for _, path := range []string{oldPath, orphanPath} {
		_ = os.Chtimes(path, oldTime, oldTime)
	}
	s := &Server{
		cfg: config.Config{DataDir: dir},
		previewSessions: map[string]*previewSession{
			"old":    {path: oldPath, lastUsed: oldTime},
			"active": {path: activePath, lastUsed: time.Now()},
		},
	}
	if removed := s.SweepPreviewSessions(time.Hour); removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("expired preview file still present")
	}
	if _, err := os.Stat(activePath); err != nil {
		t.Error("active preview file was removed")
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Error("orphan preview file still present")
	}
	if len(s.previewSessions) != 1 {
		t.Errorf("sessions = %d, want 1", len(s.previewSessions))
	}
}

func TestFnOSIdentityHeadersOnlyTrustedFromSocket(t *testing.T) {
	server := &Server{
		cfg:    config.Config{AuthMode: "fnos"},
		logger: log.New(io.Discard, "", 0),
		mux:    http.NewServeMux(),
	}
	server.admin("GET /api/admin/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	request := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/ping", nil)
		req.Header.Set("X-Trim-Userid", "1")
		req.Header.Set("X-Trim-Isadmin", "true")
		return req
	}

	// A LAN client hitting port 8088 can forge these headers; it must be
	// rejected before reaching the handler.
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, request())
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("TCP request status = %d, want 401", rec.Code)
	}

	// A socket request without identity is not logged in.
	noUser := httptest.NewRequest(http.MethodGet, "/api/admin/ping", nil)
	rec = httptest.NewRecorder()
	server.SocketHandler().ServeHTTP(rec, noUser)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("socket no-user status = %d, want 401", rec.Code)
	}

	// A socket request from a non-admin user is forbidden on admin routes.
	nonAdmin := httptest.NewRequest(http.MethodGet, "/api/admin/ping", nil)
	nonAdmin.Header.Set("X-Trim-Userid", "2")
	rec = httptest.NewRecorder()
	server.SocketHandler().ServeHTTP(rec, nonAdmin)
	if rec.Code != http.StatusForbidden {
		t.Errorf("socket non-admin status = %d, want 403", rec.Code)
	}

	// The gateway socket with a valid administrator passes.
	rec = httptest.NewRecorder()
	server.SocketHandler().ServeHTTP(rec, request())
	if rec.Code != http.StatusOK {
		t.Errorf("socket admin status = %d, want 200", rec.Code)
	}
}

func TestPrintModeIDCardAndInvoice(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	// Build two tiny PNGs for the ID card.
	pngBytes := func(c color.RGBA) []byte {
		img := image.NewRGBA(image.Rect(0, 0, 120, 76))
		for y := 0; y < 76; y++ {
			for x := 0; x < 120; x++ {
				img.Set(x, y, c)
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("front", "front.png")
	_, _ = fw.Write(pngBytes(color.RGBA{200, 220, 255, 255}))
	bw, _ := w.CreateFormFile("back", "back.png")
	_, _ = bw.Write(pngBytes(color.RGBA{255, 235, 210, 255}))
	_ = w.WriteField("layout", "vertical")
	_ = w.WriteField("border", "1")
	_ = w.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/printmode/idcard", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Admin-Token", "secret")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("idcard status = %d", res.StatusCode)
	}
	var idcard struct {
		PreviewID string `json:"preview_id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&idcard)
	if idcard.PreviewID == "" {
		t.Fatal("missing idcard preview_id")
	}
	preview, err := http.Get(ts.URL + "/api/printmode/preview/" + idcard.PreviewID)
	if err != nil {
		t.Fatal(err)
	}
	preview.Body.Close()
	if preview.StatusCode != http.StatusOK {
		t.Fatalf("idcard preview status = %d", preview.StatusCode)
	}

	// Invoice endpoint with two image invoices.
	var invoiceBody bytes.Buffer
	iw := multipart.NewWriter(&invoiceBody)
	f1, _ := iw.CreateFormFile("files", "a.png")
	_, _ = f1.Write(pngBytes(color.RGBA{255, 255, 255, 255}))
	f2, _ := iw.CreateFormFile("files", "b.png")
	_, _ = f2.Write(pngBytes(color.RGBA{255, 255, 255, 255}))
	_ = iw.WriteField("image_sizes", `[{"width_mm":210,"height_mm":140},{"width_mm":210,"height_mm":140}]`)
	_ = iw.WriteField("per_sheet", "2")
	_ = iw.WriteField("trim", "1")
	_ = iw.Close()
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/printmode/invoice", &invoiceBody)
	req2.Header.Set("Content-Type", iw.FormDataContentType())
	req2.Header.Set("X-Admin-Token", "secret")
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("invoice status = %d", res2.StatusCode)
	}
	var invoice struct {
		PreviewID string `json:"preview_id"`
		PageCount int    `json:"page_count"`
	}
	_ = json.NewDecoder(res2.Body).Decode(&invoice)
	if invoice.PreviewID == "" || invoice.PageCount != 1 {
		t.Fatalf("invoice response = %+v", invoice)
	}
}

func TestFnOSStaticIndexUsesPrefixBaseAndAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `<html><head></head><body><script type="module" src="./assets/app.js"></script></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{StaticDir: dir, AuthMode: "fnos", FnOSPrefix: "/app/print-portal"}, logger: log.New(io.Discard, "", 0)}

	req := httptest.NewRequest(http.MethodGet, "/app/print-portal/", nil)
	req.Header.Set("X-Trim-Userid", "1")
	rec := httptest.NewRecorder()
	s.staticHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `<base href="/app/print-portal/">`) {
		t.Errorf("index missing prefix base: %s", rec.Body.String())
	}

	assetReq := httptest.NewRequest(http.MethodGet, "/app/print-portal/assets/app.js", nil)
	assetReq.Header.Set("X-Trim-Userid", "1")
	assetRec := httptest.NewRecorder()
	s.staticHandler().ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Errorf("asset status = %d", assetRec.Code)
	}
}
