package ippgateway

import "testing"

func TestQueueFromPath(t *testing.T) {
	cases := map[string]string{
		"/printers/virtual-ppd":  "virtual-ppd",
		"/printers/virtual-raw/": "virtual-raw",
		"/printers/my%20printer": "my printer",
		"/admin":                 "",
		"/printers":              "",
		"/printers/":             "",
		"/printers/a/b":          "a",
	}
	for path, want := range cases {
		got, ok := queueFromPath(path)
		if want == "" {
			if ok {
				t.Errorf("%s: expected invalid, got %q", path, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%s: got %q, %v; want %q", path, got, ok, want)
		}
	}
}
