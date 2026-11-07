package ippgateway

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/OpenPrinting/goipp"
)

// uriAttrs lists IPP attributes whose values are URIs pointing at the CUPS
// instance. CUPS reports them with its loopback address, which makes strict
// clients (notably iOS AirPrint) treat the discovered printer as unreachable
// because the URI does not match the address they connected to.
var uriAttrs = map[string]bool{
	"printer-uri-supported":             true,
	"printer-more-info":                 true,
	"printer-icons":                     true,
	"printer-strings-uri":               true,
	"printer-supply-info-uri":           true,
	"printer-up-time":                   false,
	"job-uri":                           true,
	"job-printer-uri":                   true,
	"printer-icc-profiles":              true,
	"printer-device-id":                 false,
	"printer-organization":              false,
	"printer-organizational-unit":       false,
	"printer-static-resource-directory": true,
}

// rewriteResponse replaces CUPS loopback URIs with the address the client
// actually used, keeping the IPP body valid.
func (g *Gateway) rewriteResponse(resp *http.Response) error {
	cap, _ := resp.Request.Context().Value(jobCaptureKey{}).(*jobCapture)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(ct, "application/ipp") {
		return nil
	}
	if len(body) == 0 {
		return nil
	}
	var msg goipp.Message
	if err := msg.Decode(bytes.NewReader(body)); err != nil {
		return nil
	}
	repl := g.publicBase(resp.Request)
	if repl == "" {
		return nil
	}
	changed := false
	for _, group := range msg.AttrGroups() {
		for i := range group.Attrs {
			attr := &group.Attrs[i]
			if !uriAttrs[attr.Name] {
				continue
			}
			for j := range attr.Values {
				s, ok := attr.Values[j].V.(goipp.String)
				if !ok {
					continue
				}
				next := replaceLoopback(string(s), repl)
				if next != string(s) {
					attr.Values[j].V = goipp.String(next)
					changed = true
				}
			}
		}
	}
	if !changed {
		return nil
	}
	var buf bytes.Buffer
	if err := msg.Encode(&buf); err != nil {
		return nil
	}
	resp.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
	resp.ContentLength = int64(buf.Len())
	resp.Header.Set("Content-Length", itoa(buf.Len()))
	_ = cap
	return nil
}

// replaceLoopback rewrites URIs that point at the CUPS loopback address so
// clients see the address they actually connected to.
func replaceLoopback(value, base string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return value
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return value
	}
	parsed.Host = base
	return parsed.String()
}

// publicBase returns "<host>:<port>" as seen by the client. The client Host
// captured before proxying is authoritative because the proxy rewrites Host to
// the CUPS loopback target.
func (g *Gateway) publicBase(r *http.Request) string {
	host := ""
	if v, ok := r.Context().Value(clientHostKey{}).(string); ok {
		host = v
	}
	if host == "" {
		host = r.Host
	}
	h, p, err := splitHostPort(host)
	if err == nil && p != "" {
		host = h
	}
	if host == "" {
		return ""
	}
	if g.publicHost != "" {
		host = g.publicHost
	}
	return host + ":" + g.publicPort
}

func splitHostPort(hostport string) (string, string, error) {
	if i := strings.LastIndex(hostport, ":"); i > 0 && !strings.Contains(hostport[i:], "]") {
		return hostport[:i], hostport[i+1:], nil
	}
	return hostport, "", nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
