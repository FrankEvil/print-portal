// Package ippgateway exposes only explicitly shared CUPS queues to the LAN.
package ippgateway

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/storage"
)

type Gateway struct {
	addr   string
	target string
	store  *storage.Store
	logger *log.Logger
}

func New(addr, cupsServer string, store *storage.Store, logger *log.Logger) *Gateway {
	return &Gateway{addr: addr, target: cupsServer, store: store, logger: logger}
}

var printerPath = regexp.MustCompile(`^/printers/([^/]+)(?:/.*)?$`)

// Start runs the IPP gateway until ctx is cancelled. Only requests to a
// printer queue whose record has is_shared=true are forwarded to CUPS.
func (g *Gateway) Start(ctx context.Context) error {
	if g.addr == "" {
		return nil
	}
	targetURL, err := url.Parse("http://" + g.target)
	if err != nil {
		return fmt.Errorf("parse CUPS target: %w", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		// CUPS validates the Host header; forward as the loopback target
		// instead of the LAN address used by the client.
		req.Host = targetURL.Host
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		queue, ok := queueFromPath(r.URL.Path)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		p, err := g.store.GetPrinterByQueue(queue)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if p == nil || !p.IsShared {
			http.Error(w, "printer is not shared", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	})
	srv := &http.Server{Addr: g.addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	g.logger.Printf("IPP gateway listening on %s (cups=%s)", g.addr, g.target)
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func queueFromPath(path string) (string, bool) {
	m := printerPath.FindStringSubmatch(path)
	if m == nil {
		return "", false
	}
	queue, err := url.PathUnescape(m[1])
	if err != nil || strings.TrimSpace(queue) == "" {
		return "", false
	}
	return queue, true
}
