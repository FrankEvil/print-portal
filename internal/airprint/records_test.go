package airprint

import (
	"net"
	"testing"
)

func TestBuildRecords(t *testing.T) {
	instances := []serviceInstance{{
		Instance: "AirPrint Brother DCP-7060D @ fnos",
		Service:  "_ipp._tcp.local",
		Subtypes: []string{"_universal._sub._ipp._tcp.local"},
		Host:     "fnos.local",
		IP:       net.ParseIP("192.168.2.30"),
		Port:     8631,
		Text:     []string{"txtvers=1", "rp=printers/x-ppd"},
	}}
	for _, rr := range buildRecords(instances, 120) {
		t.Log(rr.String())
	}
}
