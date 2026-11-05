package ippgateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/OpenPrinting/goipp"
)

type jobCaptureKey struct{}

type jobCapture struct {
	name  string
	queue string
}

const maxCaptureBody = 64 << 20

// captureJobName reads an IPP request before it is proxied and extracts the
// document name. The request body is restored so the reverse proxy can forward
// the original message unchanged.
func (g *Gateway) captureJobName(r *http.Request, queue string) *http.Request {
	if r.Method != http.MethodPost || !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), goipp.ContentType) {
		return r
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return r
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) == 0 || len(body) > maxCaptureBody {
		return r
	}
	var m goipp.Message
	if err := m.Decode(bytes.NewReader(body)); err != nil {
		g.logger.Printf("IPP capture decode failed: op=%#x body=%d err=%v", uint16(m.Code), len(body), err)
		return r
	}
	name := firstStringAttr(&m, "job-name", "document-name", "document-name-supplied")
	if name == "" {
		g.logger.Printf("IPP capture no name: op=%#x body=%d", uint16(m.Code), len(body))
		return r
	}
	g.logger.Printf("IPP capture: op=%#x name=%q body=%d", uint16(m.Code), name, len(body))
	capture := &jobCapture{name: name, queue: queue}
	if goipp.Op(m.Code) == goipp.OpSendDocument {
		if id := firstIntAttr(&m, "job-id"); id > 0 {
			_ = g.store.SaveLocalJobName(fmt.Sprintf("%s-%d", queue, id), name)
		} else if uri := firstStringAttr(&m, "job-uri"); uri != "" {
			if id := jobIDFromURI(uri); id != "" {
				_ = g.store.SaveLocalJobName(queue+"-"+id, name)
			}
		}
	}
	return r.WithContext(context.WithValue(r.Context(), jobCaptureKey{}, capture))
}

// captureJobResponse records the CUPS job-id returned for Create-Job/Print-Job.
func (g *Gateway) captureJobResponse(resp *http.Response) error {
	cap, _ := resp.Request.Context().Value(jobCaptureKey{}).(*jobCapture)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if cap == nil || cap.name == "" {
		return nil
	}
	var m goipp.Message
	if err := m.Decode(bytes.NewReader(body)); err != nil {
		return nil
	}
	id := firstIntAttr(&m, "job-id")
	if id <= 0 {
		if uri := firstStringAttr(&m, "job-uri"); uri != "" {
			if s := jobIDFromURI(uri); s != "" {
				_ = g.store.SaveLocalJobName(cap.queue+"-"+s, cap.name)
			}
		}
		return nil
	}
	_ = g.store.SaveLocalJobName(fmt.Sprintf("%s-%d", cap.queue, id), cap.name)
	return nil
}

func firstStringAttr(m *goipp.Message, names ...string) string {
	for _, group := range m.AttrGroups() {
		for _, attr := range group.Attrs {
			for _, want := range names {
				if attr.Name != want || len(attr.Values) == 0 {
					continue
				}
				switch v := attr.Values[0].V.(type) {
				case goipp.String:
					return string(v)
				case goipp.TextWithLang:
					return v.Text
				}
			}
		}
	}
	return ""
}

func firstIntAttr(m *goipp.Message, name string) int32 {
	for _, group := range m.AttrGroups() {
		for _, attr := range group.Attrs {
			if attr.Name == name && len(attr.Values) > 0 {
				if v, ok := attr.Values[0].V.(goipp.Integer); ok {
					return int32(v)
				}
			}
		}
	}
	return 0
}

func jobIDFromURI(uri string) string {
	uri = strings.TrimRight(uri, "/")
	idx := strings.LastIndex(uri, "/")
	if idx < 0 {
		return ""
	}
	id := uri[idx+1:]
	if _, err := strconv.Atoi(id); err != nil {
		return ""
	}
	return id
}
