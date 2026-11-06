package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/liuzhaojun/print-portal/internal/storage"
)

type sharingPrinter struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Model          string `json:"model"`
	State          string `json:"state"`
	IsDefault      bool   `json:"is_default"`
	IsShared       bool   `json:"is_shared"`
	AllowDiscovery bool   `json:"allow_discovery"`
	DeviceURI      string `json:"device_uri"`
	PPDQueue       string `json:"ppd_queue"`
	RAWQueue       string `json:"raw_queue"`
	PPDURI         string `json:"ppd_uri"`
	RAWURI         string `json:"raw_uri"`
	WebURL         string `json:"web_url"`
}

// handleSharing returns LAN connection information for every printer.
func (s *Server) handleSharing(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil || host == "" {
		host = r.Host
	}
	if configuredHost := strings.TrimSpace(s.cfg.PublicHost); configuredHost != "" {
		host = configuredHost
	} else if configured, ok, _ := s.store.GetSetting("public_host"); ok && strings.TrimSpace(configured) != "" {
		configured = strings.TrimSpace(configured)
		if !isLoopbackHost(configured) {
			host = configured
		}
	}
	// Requests arriving through the fnOS unified gateway use a Unix socket,
	// so r.Host is often localhost. Show the NAS LAN address instead.
	if isLoopbackHost(host) {
		if lan := detectLANIP(); lan != "" {
			host = lan
		}
	}
	cupsPort := settingOr(s.store, "cups_port", "8631")
	webPort := settingOr(s.store, "web_port", "8088")

	printers, err := s.store.ListPrinters()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	statuses, _ := s.cups.ListPrinters(r.Context())
	stateByQueue := map[string]string{}
	for _, st := range statuses {
		stateByQueue[st.Name] = st.State
	}
	out := make([]sharingPrinter, 0, len(printers))
	for _, p := range printers {
		ppdURI := fmt.Sprintf("ipp://%s:%s/printers/%s", host, cupsPort, p.PPDQueue)
		rawURI := fmt.Sprintf("ipp://%s:%s/printers/%s", host, cupsPort, p.RAWQueue)
		state := stateByQueue[p.PPDQueue]
		if state == "" {
			state = "unknown"
		}
		out = append(out, sharingPrinter{
			ID: p.ID, Name: p.Name, Model: p.Model, State: state,
			IsDefault: p.IsDefault, IsShared: p.IsShared, AllowDiscovery: p.AllowDiscovery, DeviceURI: p.DeviceURI,
			PPDQueue: p.PPDQueue, RAWQueue: p.RAWQueue,
			PPDURI: ppdURI, RAWURI: rawURI,
			WebURL: fmt.Sprintf("http://%s:%s/", host, webPort),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_mode":   s.cfg.AuthMode,
		"public_host": host,
		"cups_port":   cupsPort,
		"web_port":    webPort,
		"printers":    out,
	})
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func detectLANIP() string {
	// Prefer the address selected by the host's default route. This avoids
	// returning a Docker bridge address when the app runs with host network.
	if conn, err := net.Dial("udp", "223.5.5.5:80"); err == nil {
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			conn.Close()
			if ip4 := addr.IP.To4(); ip4 != nil && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
				return ip4.String()
			}
		} else {
			conn.Close()
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var fallback string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "virbr") || strings.HasPrefix(name, "cni") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			if ip4.IsPrivate() {
				return ip4.String()
			}
			if fallback == "" {
				fallback = ip4.String()
			}
		}
	}
	return fallback
}

func settingOr(store *storage.Store, key, fallback string) string {
	if v, ok, _ := store.GetSetting(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}
