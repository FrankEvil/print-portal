package discovery

import (
	"context"
	"fmt"
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
		name := d.Name
		if name == "" {
			name = d.URI
		}
		out = append(out, storage.Device{
			Kind:      "printer",
			Name:      name,
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

// GuessModel heuristically extracts a model name from a device URI.
func GuessModel(uri string) string {
	if strings.HasPrefix(uri, "usb://") {
		rest := strings.TrimPrefix(uri, "usb://")
		if idx := strings.Index(rest, "?"); idx > 0 {
			rest = rest[:idx]
		}
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 {
			return parts[0] + " " + parts[1]
		}
		return rest
	}
	return ""
}
