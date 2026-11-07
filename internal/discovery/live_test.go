package discovery

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDiscoverMDNSPrintersLive(t *testing.T) {
	if os.Getenv("MDNS_LIVE") == "" {
		t.Skip("set MDNS_LIVE=1 to browse the real LAN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	devices := discoverMDNSPrinters(ctx)
	if len(devices) == 0 {
		t.Fatal("no mDNS printers discovered")
	}
	for _, d := range devices {
		t.Logf("name=%q brand=%q model=%q uri=%q", d.Name, d.Brand, d.Model, d.DeviceURI)
	}
}
