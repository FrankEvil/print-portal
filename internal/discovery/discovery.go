package discovery

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

// Service discovers printers and scanners and caches the result.
type Service struct {
	store *storage.Store
	cups  *cups.Client
}

func NewService(store *storage.Store, cupsClient *cups.Client) *Service {
	return &Service{store: store, cups: cupsClient}
}

// DiscoverPrinters scans USB/network print devices via CUPS.
func (s *Service) DiscoverPrinters(ctx context.Context) ([]storage.Device, error) {
	devices, err := s.cups.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
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
	if err := s.store.ReplaceDevices("printer", out); err != nil {
		return nil, err
	}
	return out, nil
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
