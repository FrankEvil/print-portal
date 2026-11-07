package discovery

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

// Service discovers printers and scanners and caches the result.
type Service struct {
	store *storage.Store
	cups  *cups.Client

	mu    sync.Mutex
	scans map[string]*ScanState
}

// ScanState is a snapshot of an asynchronous device scan.
type ScanState struct {
	ID        string           `json:"scan_id"`
	Scope     string           `json:"scope"`  // network / usb
	Status    string           `json:"status"` // running / done / failed
	Devices   []storage.Device `json:"devices"`
	Error     string           `json:"error,omitempty"`
	StartedAt time.Time        `json:"started_at"`
}

func NewService(store *storage.Store, cupsClient *cups.Client) *Service {
	return &Service{store: store, cups: cupsClient, scans: map[string]*ScanState{}}
}

// DiscoverPrinters scans USB/network print devices. CUPS supplies USB and
// manual network schemes; Bonjour printers are browsed by the in-house mDNS
// browser so the appliance does not depend on Avahi/D-Bus.
func (s *Service) DiscoverPrinters(ctx context.Context) ([]storage.Device, error) {
	devices, err := s.cups.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]cups.Device, 0, len(devices))
	for _, d := range devices {
		// dnssd:// URIs are resolved through the system DNS-SD stack. Print
		// Portal browses them itself and stores direct ipp(s):// addresses.
		if strings.HasPrefix(d.URI, "dnssd://") {
			continue
		}
		filtered = append(filtered, d)
	}
	out := buildDevices(filtered)
	out = mergeDeviceLists(out, discoverMDNSPrinters(ctx))
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if err := s.store.ReplaceDevices("printer", out); err != nil {
		return nil, err
	}
	return out, nil
}

// DiscoverUSBPrinters scans only the USB backend (fast, no mDNS wait).
func (s *Service) DiscoverUSBPrinters(ctx context.Context) ([]storage.Device, error) {
	devices, err := s.cups.ListUSBDevices(ctx)
	if err != nil {
		return nil, err
	}
	out := buildDevices(devices)
	// Merge into the cached device list instead of replacing it, so network
	// devices from a previous scan are not lost.
	existing, _ := s.store.ListDevices("printer")
	merged := mergeDeviceLists(existing, out)
	if err := s.store.ReplaceDevices("printer", merged); err != nil {
		return nil, err
	}
	return out, nil
}

func buildDevices(devices []cups.Device) []storage.Device {
	out := make([]storage.Device, 0, len(devices))
	for _, d := range devices {
		name, brand, model := DeviceIdentity(d.URI)
		if name == "" {
			name = d.URI
		}
		out = append(out, storage.Device{
			Kind:      "printer",
			Name:      name,
			Brand:     brand,
			Model:     model,
			DeviceURI: d.URI,
			Source:    d.Transport,
			Metadata:  fmt.Sprintf(`{"transport":%q}`, d.Transport),
		})
	}
	return out
}

func mergeDeviceLists(base, extra []storage.Device) []storage.Device {
	byURI := map[string]storage.Device{}
	for _, d := range base {
		byURI[d.DeviceURI] = d
	}
	for _, d := range extra {
		byURI[d.DeviceURI] = d
	}
	out := make([]storage.Device, 0, len(byURI))
	for _, d := range byURI {
		out = append(out, d)
	}
	return out
}

// StartScan starts (or reuses) an asynchronous discovery scan for a scope.
// scope = "network", "usb" or "all".
func (s *Service) StartScan(scope string) string {
	switch scope {
	case "usb", "all":
	default:
		scope = "network"
	}
	s.mu.Lock()
	if st, ok := s.scans[scope]; ok && st.Status == "running" {
		id := st.ID
		s.mu.Unlock()
		return id
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	st := &ScanState{ID: id, Scope: scope, Status: "running", StartedAt: time.Now(), Devices: []storage.Device{}}
	// Seed with previously cached devices so the UI can render immediately.
	if cached, err := s.store.ListDevices("printer"); err == nil {
		for _, d := range cached {
			if scanMatches(scope, d.DeviceURI) {
				st.Devices = append(st.Devices, d)
			}
		}
	}
	s.scans[scope] = st
	s.mu.Unlock()

	go s.runScan(scope, id)
	return id
}

func (s *Service) runScan(scope, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var devices []storage.Device
	var err error
	if scope == "usb" {
		devices, err = s.DiscoverUSBPrinters(ctx)
	} else {
		devices, err = s.DiscoverPrinters(ctx)
	}
	var filtered []storage.Device
	for _, d := range devices {
		if scanMatches(scope, d.DeviceURI) {
			filtered = append(filtered, d)
		}
	}
	if filtered == nil {
		filtered = []storage.Device{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.scans[scope]
	if !ok || st.ID != id {
		return
	}
	if err != nil {
		st.Status = "failed"
		st.Error = err.Error()
		return
	}
	st.Devices = filtered
	st.Status = "done"
}

func scanMatches(scope, uri string) bool {
	if scope == "all" {
		return true
	}
	if scope == "usb" {
		return strings.HasPrefix(uri, "usb://")
	}
	return !strings.HasPrefix(uri, "usb://")
}

// ScanStatus returns a snapshot of a scan by id.
func (s *Service) ScanStatus(id string) (*ScanState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.scans {
		if st.ID == id {
			copyState := *st
			copyState.Devices = append([]storage.Device(nil), st.Devices...)
			return &copyState, true
		}
	}
	return nil, false
}

// DiscoverScanners scans SANE devices.
func (s *Service) DiscoverScanners(ctx context.Context) ([]storage.Device, error) {
	scanners, err := s.cups.ListScanners(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]storage.Device, 0, len(scanners))
	for _, sc := range scanners {
		out = append(out, storage.Device{
			Kind:      "scanner",
			Name:      sc.Name,
			Model:     sc.Name,
			DeviceURI: sc.URI,
			Source:    "sane",
		})
	}
	if err := s.store.ReplaceDevices("scanner", out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeviceIdentity derives a display name, brand and model from a CUPS device URI.
func DeviceIdentity(uri string) (name, brand, model string) {
	switch {
	case strings.HasPrefix(uri, "usb://"):
		rest := strings.TrimPrefix(uri, "usb://")
		if idx := strings.Index(rest, "?"); idx >= 0 {
			rest = rest[:idx]
		}
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 {
			brand = strings.TrimSpace(parts[0])
			model = strings.TrimSpace(parts[0] + " " + parts[1])
			name = model
			return
		}
		return rest, "", rest
	case strings.HasPrefix(uri, "dnssd://"):
		raw := strings.TrimPrefix(uri, "dnssd://")
		decoded, err := url.PathUnescape(raw)
		if err != nil {
			decoded = raw
		}
		if idx := strings.Index(decoded, "/"); idx >= 0 {
			decoded = decoded[:idx]
		}
		// Drop the mDNS service suffix for a cleaner display name.
		if idx := strings.Index(decoded, "._"); idx >= 0 {
			decoded = decoded[:idx]
		}
		// CUPS/macOS appends hardware IDs and location suffixes like
		// " (d1:a8:71) (2)"; keep the human-readable model part.
		if idx := strings.Index(decoded, " ("); idx > 0 {
			decoded = decoded[:idx]
		}
		if at := strings.Index(decoded, " @"); at > 0 {
			decoded = decoded[:at]
		}
		name = decoded
		if idx := strings.Index(decoded, "AirPrint "); idx >= 0 {
			rest := decoded[idx+len("AirPrint "):]
			if at := strings.Index(rest, " @"); at >= 0 {
				rest = rest[:at]
			}
			rest = strings.ReplaceAll(rest, "-", " ")
			model = strings.TrimSpace(rest)
			if fields := strings.Fields(model); len(fields) > 0 {
				brand = fields[0]
			}
		}
		if brand == "" {
			if fields := strings.Fields(name); len(fields) > 0 {
				brand = fields[0]
			}
		}
		return name, brand, model
	case strings.HasPrefix(uri, "ipp://"), strings.HasPrefix(uri, "ipps://"):
		if parsed, err := url.Parse(uri); err == nil {
			return parsed.Host, "", ""
		}
	}
	return "", "", ""
}

// GuessModel heuristically extracts a model name from a device URI.
func GuessModel(uri string) string {
	_, _, model := DeviceIdentity(uri)
	return model
}
