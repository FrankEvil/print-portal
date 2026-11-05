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

func TestIsPrivateRemote(t *testing.T) {
	cases := map[string]bool{
		"192.168.2.10:1234":    true,
		"10.0.0.5:1234":        true,
		"172.16.1.2:1234":      true,
		"127.0.0.1:1234":       true,
		"8.8.8.8:1234":         false,
		"2001:4860:4860::8888": false,
	}
	for addr, want := range cases {
		if got := isPrivateRemote(addr); got != want {
			t.Errorf("isPrivateRemote(%q) = %v, want %v", addr, got, want)
		}
	}
}
