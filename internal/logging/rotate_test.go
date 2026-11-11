package logging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotatingWriterRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	w, err := NewRotatingWriter(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("12345678")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "abcdef" {
		t.Errorf("current log = %q", got)
	}
	if got := readFile(t, path+".1"); got != "12345678" {
		t.Errorf("rotated log = %q", got)
	}

	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "0123456789" {
		t.Errorf("current log = %q", got)
	}
	if got := readFile(t, path+".1"); got != "abcdef" {
		t.Errorf(".1 log = %q", got)
	}
	if got := readFile(t, path+".2"); got != "12345678" {
		t.Errorf(".2 log = %q", got)
	}
}

func TestRotatingWriterKeepsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := NewRotatingWriter(path, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "oldnew" {
		t.Errorf("log = %q, want oldnew", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
