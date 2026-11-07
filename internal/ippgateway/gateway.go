// Package ippgateway exposes only explicitly shared CUPS queues to the LAN.
package ippgateway

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/storage"
)

type clientHostKey struct{}

type Gateway struct {
	addr       string
	target     string
	publicHost string
	publicPort string
	store      *storage.Store
	logger     *log.Logger
}

func New(addr, cupsServer string, store *storage.Store, logger *log.Logger) *Gateway {
	return &Gateway{addr: addr, target: cupsServer, store: store, logger: logger, publicPort: "8631"}
}

// SetPublicAddress sets the host/port clients use to reach the gateway. CUPS
// reports loopback URIs in IPP responses; they are rewritten to this address
// because iOS AirPrint rejects printers whose reported URI does not match the
// discovered endpoint.
func (g *Gateway) SetPublicAddress(host, port string) {
	g.publicHost = strings.TrimSpace(host)
	if strings.TrimSpace(port) != "" {
		g.publicPort = strings.TrimSpace(port)
	}
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
	proxy.ModifyResponse = func(resp *http.Response) error {
		if err := g.captureJobResponse(resp); err != nil {
			return err
		}
		return g.rewriteResponse(resp)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !isPrivateRemote(r.RemoteAddr) {
			http.Error(w, "IPP gateway is limited to the local network", http.StatusForbidden)
			return
		}
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
		if queue == p.RAWQueue {
			const maxRawBody = 500 << 20
			if r.ContentLength > maxRawBody {
				http.Error(w, "raw print job too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxRawBody)
		}
		// Remember the address the client used before the reverse proxy
		// rewrites Host to the CUPS loopback target.
		r = r.WithContext(context.WithValue(r.Context(), clientHostKey{}, r.Host))
		r = g.captureJobName(r, queue)
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

func isPrivateRemote(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
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
