package discovery

import (
	"net"
	"testing"

	"github.com/liuzhaojun/print-portal/internal/mdns"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

func TestDeviceFromMDNSService(t *testing.T) {
	svc := mdns.Service{
		Instance: "EPSON L1110 Series @ 刘兆军的Mac mini",
		Service:  "_ipp._tcp",
		Host:     "liuzhaojundeMac-mini.local",
		IP:       net.ParseIP("192.168.2.80"),
		Port:     631,
		Text: map[string]string{
			"rp":      "printers/EPSON_L1110_Series",
			"ty":      "EPSON L1110 Series",
			"product": "(EPSON ET-1110 Series)",
			"TLS":     "1.2",
		},
	}
	dev, ok := deviceFromMDNSService(svc)
	if !ok {
		t.Fatal("expected device")
	}
	if dev.DeviceURI != "ipp://192.168.2.80:631/printers/EPSON_L1110_Series" {
		t.Fatalf("unexpected URI: %s", dev.DeviceURI)
	}
	if dev.Name != "EPSON L1110 Series" || dev.Brand != "EPSON" || dev.Model != "EPSON ET-1110 Series" {
		t.Fatalf("unexpected identity: %+v", dev)
	}
	if dev.Source != "network" {
		t.Fatalf("unexpected source: %s", dev.Source)
	}
}

func TestDeviceFromMDNSServiceRequiresIP(t *testing.T) {
	if _, ok := deviceFromMDNSService(mdns.Service{Service: "_ipp._tcp", Port: 631}); ok {
		t.Fatal("expected no device without an IPv4 address")
	}
}

func TestIsSelfMDNSService(t *testing.T) {
	local := map[string]bool{"192.168.2.30": true}
	cases := []struct {
		name string
		svc  mdns.Service
		want bool
	}{
		{"print portal product", mdns.Service{IP: net.ParseIP("192.168.2.31"), Port: 8632, Text: map[string]string{"product": "(Print Portal)"}}, true},
		{"own host", mdns.Service{Host: "print-portal.local", IP: net.ParseIP("192.168.2.30"), Port: 8631}, true},
		{"remote print portal", mdns.Service{Host: "print-portal.local", IP: net.ParseIP("192.168.2.31"), Port: 8631, Text: map[string]string{"product": "(Print Portal)"}}, true},
		{"external", mdns.Service{Host: "mac-mini.local", IP: net.ParseIP("192.168.2.80"), Port: 631, Text: map[string]string{"product": "(EPSON ET-1110 Series)"}}, false},
	}
	for _, tc := range cases {
		if got := isSelfMDNSService(tc.svc, local); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestDedupeMDNSDevicesPrefersIPP(t *testing.T) {
	in := []storage.Device{
		{Name: "EPSON L1110 Series", DeviceURI: "ipps://192.168.2.80:631/printers/EPSON_L1110_Series"},
		{Name: "EPSON L1110 Series", DeviceURI: "ipp://192.168.2.80:631/printers/EPSON_L1110_Series"},
	}
	out := dedupeMDNSDevices(in)
	if len(out) != 1 {
		t.Fatalf("expected one device, got %d", len(out))
	}
	if out[0].DeviceURI != "ipp://192.168.2.80:631/printers/EPSON_L1110_Series" {
		t.Fatalf("expected plain IPP to win, got %s", out[0].DeviceURI)
	}
}
