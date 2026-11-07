package mdns

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestBrowseLive is an opt-in integration test against the real LAN. Run with:
//
//	MDNS_LIVE=1 go test ./internal/mdns -run TestBrowseLive -v
func TestBrowseLive(t *testing.T) {
	if os.Getenv("MDNS_LIVE") == "" {
		t.Skip("set MDNS_LIVE=1 to browse the real LAN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	services, err := Browse(ctx, []string{"_ipp._tcp.local.", "_ipps._tcp.local."}, 3*time.Second)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if len(services) == 0 {
		t.Fatal("no mDNS printers discovered")
	}
	for _, svc := range services {
		t.Logf("instance=%q service=%q host=%q ip=%v port=%d txt=%v", svc.Instance, svc.Service, svc.Host, svc.IP, svc.Port, svc.Text)
	}
}
