package mdns

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestCollectorParsesPTRSRVAndTXT(t *testing.T) {
	c := newCollector([]string{"_ipp._tcp.local."})
	instance := "EPSON L1110 Series @ Mac mini"
	service := "_ipp._tcp.local."
	instanceFQDN := instance + "." + service

	msg := &dns.Msg{MsgHdr: dns.MsgHdr{Response: true}}
	msg.Answer = []dns.RR{
		&dns.PTR{
			Hdr: dns.RR_Header{Name: service, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 120},
			Ptr: instanceFQDN,
		},
	}
	msg.Extra = []dns.RR{
		&dns.SRV{
			Hdr:    dns.RR_Header{Name: instanceFQDN, Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120},
			Port:   631,
			Target: "mac-mini.local.",
		},
		&dns.TXT{
			Hdr: dns.RR_Header{Name: instanceFQDN, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 120},
			Txt: []string{
				"rp=printers/EPSON_L1110_Series",
				"ty=EPSON L1110 Series",
				"product=(EPSON ET-1110 Series)",
				"TLS=1.2",
			},
		},
		&dns.A{
			Hdr: dns.RR_Header{Name: "mac-mini.local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
			A:   net.ParseIP("192.168.2.80").To4(),
		},
	}
	c.add(msg)
	got := c.results()
	if len(got) != 1 {
		t.Fatalf("expected 1 service, got %d: %+v", len(got), got)
	}
	svc := got[0]
	if svc.Instance != instance || svc.Service != "_ipp._tcp" {
		t.Fatalf("unexpected service identity: %+v", svc)
	}
	if svc.Host != "mac-mini.local" || svc.Port != 631 {
		t.Fatalf("unexpected SRV: %+v", svc)
	}
	if svc.IP == nil || svc.IP.String() != "192.168.2.80" {
		t.Fatalf("unexpected A record: %v", svc.IP)
	}
	if svc.Text["rp"] != "printers/EPSON_L1110_Series" || svc.Text["ty"] != "EPSON L1110 Series" {
		t.Fatalf("unexpected TXT: %+v", svc.Text)
	}
}

func TestCollectorDeduplicatesPTR(t *testing.T) {
	c := newCollector([]string{"_ipp._tcp.local."})
	instance := "Printer @ host"
	service := "_ipp._tcp.local."
	instanceFQDN := instance + "." + service
	for _, msg := range []*dns.Msg{
		{MsgHdr: dns.MsgHdr{Response: true}, Answer: []dns.RR{&dns.PTR{Hdr: dns.RR_Header{Name: service, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 120}, Ptr: instanceFQDN}}},
		{MsgHdr: dns.MsgHdr{Response: true}, Answer: []dns.RR{&dns.PTR{Hdr: dns.RR_Header{Name: service, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 120}, Ptr: instanceFQDN}}},
	} {
		c.add(msg)
	}
	if got := c.results(); len(got) != 1 {
		t.Fatalf("expected deduped service, got %d", len(got))
	}
}

func TestCollectorIgnoresUnrelatedPTR(t *testing.T) {
	c := newCollector([]string{"_ipp._tcp.local."})
	c.add(&dns.Msg{MsgHdr: dns.MsgHdr{Response: true}, Answer: []dns.RR{
		&dns.PTR{Hdr: dns.RR_Header{Name: "_printer._tcp.local.", Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 120}, Ptr: "LPD Printer._printer._tcp.local."},
	}})
	if got := c.results(); len(got) != 0 {
		t.Fatalf("expected no services, got %+v", got)
	}
}

func TestUnescapeDNS(t *testing.T) {
	cases := map[string]string{
		`EPSON\ L1110\ Series\ \@\ Mac\ mini`:                       "EPSON L1110 Series @ Mac mini",
		`\229\136\152\229\133\134\229\134\155\231\154\132Mac\ mini`: "刘兆军的Mac mini",
		`plain`: "plain",
	}
	for in, want := range cases {
		if got := unescapeDNS(in); got != want {
			t.Fatalf("unescapeDNS(%q) = %q, want %q", in, got, want)
		}
	}
}
