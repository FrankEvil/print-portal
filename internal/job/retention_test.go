package job

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

func newTestManager(t *testing.T) (*Manager, *storage.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	printer := &storage.Printer{Name: "DCP", PPDQueue: "dcp-ppd", RAWQueue: "dcp-raw", DeviceURI: "usb://x"}
	if err := store.CreatePrinter(printer); err != nil {
		t.Fatal(err)
	}
	m := New(store, cups.NewClient(fakeRunner{}, ""), fakeConverter{}, dir, log.New(io.Discard, "", 0))
	return m, store, dir
}

func TestCleanupRetention(t *testing.T) {
	m, store, dir := newTestManager(t)
	uploads := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	jobDir := func(id int64) string { return filepath.Join(dir, "jobs", itoa(id)) }

	// Old finished job: record, job directory and upload must all go.
	oldFile := filepath.Join(uploads, "old.pdf")
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldJob := &storage.Job{PrinterID: 1, Source: "web", Filename: "old.pdf", Status: "completed", StoredPath: oldFile, Copies: 1}
	if err := store.CreateJob(oldJob); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobDir(oldJob.ID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDir(oldJob.ID), "converted.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := store.DB().Exec(`UPDATE jobs SET updated_at=? WHERE id=?`, past, oldJob.ID); err != nil {
		t.Fatal(err)
	}

	// Recent finished job must survive.
	recentFile := filepath.Join(uploads, "recent.pdf")
	_ = os.WriteFile(recentFile, []byte("recent"), 0o644)
	recentJob := &storage.Job{PrinterID: 1, Source: "web", Filename: "recent.pdf", Status: "completed", StoredPath: recentFile, Copies: 1}
	if err := store.CreateJob(recentJob); err != nil {
		t.Fatal(err)
	}

	// Active job must survive even when old.
	activeFile := filepath.Join(uploads, "active.pdf")
	_ = os.WriteFile(activeFile, []byte("active"), 0o644)
	activeJob := &storage.Job{PrinterID: 1, Source: "web", Filename: "active.pdf", Status: "pending", StoredPath: activeFile, Copies: 1}
	if err := store.CreateJob(activeJob); err != nil {
		t.Fatal(err)
	}
	_, _ = store.DB().Exec(`UPDATE jobs SET updated_at=? WHERE id=?`, past, activeJob.ID)

	// Orphan upload older than the retention window.
	orphan := filepath.Join(uploads, "orphan.pdf")
	_ = os.WriteFile(orphan, []byte("orphan"), 0o644)
	stale := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(orphan, stale, stale)

	// Orphan job directory with no database row.
	orphanDir := jobDir(99999)
	_ = os.MkdirAll(orphanDir, 0o755)
	_ = os.WriteFile(filepath.Join(orphanDir, "converted.pdf"), []byte("x"), 0o644)

	stats, err := m.CleanupRetention(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Jobs != 1 {
		t.Errorf("jobs removed = %d, want 1", stats.Jobs)
	}
	if stats.Uploads != 1 {
		t.Errorf("uploads removed = %d, want 1", stats.Uploads)
	}
	if stats.JobDirs != 1 {
		t.Errorf("job dirs removed = %d, want 1", stats.JobDirs)
	}

	if job, _ := store.GetJob(oldJob.ID); job != nil {
		t.Error("old job record still present")
	}
	if _, err := os.Stat(jobDir(oldJob.ID)); !os.IsNotExist(err) {
		t.Error("old job directory still present")
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("old upload still present")
	}
	if job, _ := store.GetJob(recentJob.ID); job == nil {
		t.Error("recent job was removed")
	}
	if job, _ := store.GetJob(activeJob.ID); job == nil {
		t.Error("active job was removed")
	}
	if _, err := os.Stat(activeFile); err != nil {
		t.Error("active job upload was removed")
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("orphan upload still present")
	}
	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Error("orphan job dir still present")
	}
}

func TestPurgeFinishedRemovesFiles(t *testing.T) {
	m, store, dir := newTestManager(t)
	uploads := filepath.Join(dir, "uploads")
	_ = os.MkdirAll(uploads, 0o755)

	var finishedIDs []int64
	for _, name := range []string{"a.pdf", "b.pdf"} {
		path := filepath.Join(uploads, name)
		_ = os.WriteFile(path, []byte(name), 0o644)
		j := &storage.Job{PrinterID: 1, Source: "web", Filename: name, Status: "failed", StoredPath: path, Copies: 1}
		if err := store.CreateJob(j); err != nil {
			t.Fatal(err)
		}
		finishedIDs = append(finishedIDs, j.ID)
		jdir := filepath.Join(dir, "jobs", itoa(j.ID))
		_ = os.MkdirAll(jdir, 0o755)
		_ = os.WriteFile(filepath.Join(jdir, "converted.pdf"), []byte("pdf"), 0o644)
	}
	active := &storage.Job{PrinterID: 1, Source: "web", Filename: "live.pdf", Status: "pending", Copies: 1}
	if err := store.CreateJob(active); err != nil {
		t.Fatal(err)
	}

	cleared, err := m.PurgeFinished(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cleared != 2 {
		t.Fatalf("cleared = %d, want 2", cleared)
	}
	for _, id := range finishedIDs {
		if job, _ := store.GetJob(id); job != nil {
			t.Errorf("job %d still present", id)
		}
		if _, err := os.Stat(filepath.Join(dir, "jobs", itoa(id))); !os.IsNotExist(err) {
			t.Errorf("job dir %d still present", id)
		}
	}
	if job, _ := store.GetJob(active.ID); job == nil {
		t.Error("active job was removed")
	}
}

func TestRetryFallsBackToConvertedPDF(t *testing.T) {
	m, store, dir := newTestManager(t)
	j := &storage.Job{PrinterID: 1, Source: "web", Filename: "a.pdf", Status: "failed", Copies: 1}
	if err := store.CreateJob(j); err != nil {
		t.Fatal(err)
	}
	jdir := filepath.Join(dir, "jobs", itoa(j.ID))
	_ = os.MkdirAll(jdir, 0o755)
	converted := filepath.Join(jdir, "converted.pdf")
	_ = os.WriteFile(converted, []byte("pdf"), 0o644)

	clone, err := m.Retry(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if clone.StoredPath != converted {
		t.Errorf("retry source = %q, want %q", clone.StoredPath, converted)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func TestCleanupKeepsSharedUpload(t *testing.T) {
	m, store, dir := newTestManager(t)
	uploads := filepath.Join(dir, "uploads")
	_ = os.MkdirAll(uploads, 0o755)
	shared := filepath.Join(uploads, "shared.pdf")
	_ = os.WriteFile(shared, []byte("shared"), 0o644)

	old := &storage.Job{PrinterID: 1, Source: "web", Filename: "shared.pdf", Status: "completed", StoredPath: shared, Copies: 1}
	if err := store.CreateJob(old); err != nil {
		t.Fatal(err)
	}
	active := &storage.Job{PrinterID: 1, Source: "web", Filename: "shared.pdf", Status: "pending", StoredPath: shared, Copies: 1}
	if err := store.CreateJob(active); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	_, _ = store.DB().Exec(`UPDATE jobs SET updated_at=? WHERE id=?`, past, old.ID)

	if _, err := m.CleanupRetention(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatal("shared upload was removed while an active job still references it")
	}
}

func TestPurgeKeepsSharedUpload(t *testing.T) {
	m, store, dir := newTestManager(t)
	uploads := filepath.Join(dir, "uploads")
	_ = os.MkdirAll(uploads, 0o755)
	shared := filepath.Join(uploads, "shared.pdf")
	_ = os.WriteFile(shared, []byte("shared"), 0o644)

	finished := &storage.Job{PrinterID: 1, Source: "web", Filename: "shared.pdf", Status: "failed", StoredPath: shared, Copies: 1}
	if err := store.CreateJob(finished); err != nil {
		t.Fatal(err)
	}
	active := &storage.Job{PrinterID: 1, Source: "web", Filename: "shared.pdf", Status: "pending", StoredPath: shared, Copies: 1}
	if err := store.CreateJob(active); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PurgeFinished(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatal("shared upload was removed while an active job still references it")
	}
}
