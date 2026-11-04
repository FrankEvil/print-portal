package scan

import (
	"context"
	"strings"
	"testing"

	"github.com/liuzhaojun/print-portal/internal/storage"
)

type fakeRunner struct {
	args []string
	err  error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	f.args = append([]string{name}, args...)
	return "", "", f.err
}

func newStore(t *testing.T) *storage.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestScanValidation(t *testing.T) {
	store := newStore(t)
	svc := NewService(store, t.TempDir(), &fakeRunner{})
	if _, err := svc.Scan(context.Background(), Params{}); err == nil {
		t.Error("expected error for empty scanner")
	}
	if _, err := svc.Scan(context.Background(), Params{Scanner: "dev", Resolution: 123}); err == nil {
		t.Error("expected error for bad resolution")
	}
}

func TestScanSuccess(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	svc := NewService(store, t.TempDir(), runner)
	job, err := svc.Scan(context.Background(), Params{Scanner: "brother4:bus2;dev1", Format: "pdf", Resolution: 300, ColorMode: "color"})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.Format != "pdf" || job.Resolution != 300 {
		t.Errorf("job = %+v", job)
	}
	joined := strings.Join(runner.args, " ")
	for _, want := range []string{"scanimage", "--format=pdf", "--resolution=300", "--mode=Color", "--output-file="} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}
