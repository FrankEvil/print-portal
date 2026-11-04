package convert

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		data []byte
		want Kind
	}{
		{"a.pdf", []byte("%PDF-1.4\n"), KindPDF},
		{"a.ps", []byte("%!PS-Adobe"), KindPostscript},
		{"a.png", []byte{0x89, 'P', 'N', 'G', 1, 2}, KindImage},
		{"a.jpg", []byte{0xFF, 0xD8, 0xFF, 1}, KindImage},
		{"a.docx", []byte("PK\x03\x04"), KindOffice},
		{"a.txt", []byte("hello"), KindUnknown},
	}
	for _, c := range cases {
		path := filepath.Join(dir, c.name)
		if err := os.WriteFile(path, c.data, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Detect(path); got != c.want {
			t.Errorf("Detect(%s) = %s, want %s", c.name, got, c.want)
		}
	}
}
