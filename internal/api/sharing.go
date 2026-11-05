package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/liuzhaojun/print-portal/internal/storage"
)

type sharingPrinter struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	State     string `json:"state"`
	IsDefault bool   `json:"is_default"`
	IsShared  bool   `json:"is_shared"`
	DeviceURI string `json:"device_uri"`
	PPDQueue  string `json:"ppd_queue"`
	RAWQueue  string `json:"raw_queue"`
	PPDURI    string `json:"ppd_uri"`
	RAWURI    string `json:"raw_uri"`
	WebURL    string `json:"web_url"`
}

// handleSharing returns LAN connection information for every printer.
func (s *Server) handleSharing(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil || host == "" {
		host = r.Host
	}
	if configured, ok, _ := s.store.GetSetting("public_host"); ok && strings.TrimSpace(configured) != "" {
		host = strings.TrimSpace(configured)
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
			IsDefault: p.IsDefault, IsShared: p.IsShared, DeviceURI: p.DeviceURI,
			PPDQueue: p.PPDQueue, RAWQueue: p.RAWQueue,
			PPDURI: ppdURI, RAWURI: rawURI,
			WebURL: fmt.Sprintf("http://%s:%s/", host, webPort),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"public_host": host,
		"cups_port":   cupsPort,
		"web_port":    webPort,
		"printers":    out,
	})
}

func settingOr(store *storage.Store, key, fallback string) string {
	if v, ok, _ := store.GetSetting(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}
