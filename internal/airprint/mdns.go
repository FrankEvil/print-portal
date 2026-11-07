package airprint

import (
	"errors"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/liuzhaojun/print-portal/internal/mdns"
	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
)

const (
	mdnsGroupAddr    = "224.0.0.251:5353"
	mdnsTTL          = 120
	announceInterval = 60 * time.Second
)

// responder is a small multicast DNS responder that publishes DNS-SD service
// records. It is written in-house because generic mDNS libraries cannot emit
// DNS-SD subtype PTR records (_universal._sub._ipp._tcp), which Apple devices
// require to list a printer as AirPrint capable.
type responder struct {
	logger *log.Logger
	iface  *net.Interface
	conn   *ipv4.PacketConn
	udp    *net.UDPConn

	mu    sync.RWMutex
	index map[string][]dns.RR
	all   []dns.RR

	done      chan struct{}
	closeOnce sync.Once
}

// serviceInstance is one DNS-SD service published by the responder.
type serviceInstance struct {
	Instance string   // e.g. "AirPrint Brother DCP-7060D @ fnos"
	Service  string   // e.g. "_ipp._tcp"
	Subtypes []string // e.g. "_universal._sub._ipp._tcp"
	Host     string   // e.g. "print-portal.local"
	IP       net.IP
	Port     int
	Text     []string
}

func openMulticast(iface *net.Interface) (*ipv4.PacketConn, *net.UDPConn, error) {
	return mdns.OpenMulticast(iface)
}

func newResponder(iface *net.Interface, logger *log.Logger) (*responder, error) {
	conn, udp, err := openMulticast(iface)
	if err != nil {
		return nil, err
	}
	if logger != nil {
		name := "<default>"
		if iface != nil {
			name = iface.Name
		}
		logger.Printf("airprint: mDNS responder bound on iface=%s", name)
	}
	r := &responder{
		logger: logger,
		iface:  iface,
		conn:   conn,
		udp:    udp,
		index:  map[string][]dns.RR{},
		done:   make(chan struct{}),
	}
	go r.readLoop()
	go r.announceLoop()
	return r, nil
}

func (r *responder) readLoop() {
	buf := make([]byte, 9000)
	for {
		n, cm, _, err := r.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		msg := new(dns.Msg)
		if err := msg.Unpack(buf[:n]); err != nil {
			continue
		}
		if msg.Response || len(msg.Question) == 0 {
			continue
		}
		ifIndex := 0
		if cm != nil {
			ifIndex = cm.IfIndex
		}
		r.answer(msg, ifIndex)
	}
}

func (r *responder) answer(query *dns.Msg, ifIndex int) {
	r.mu.RLock()
	var answers, additionals []dns.RR
	seen := map[string]bool{}
	add := func(dst *[]dns.RR, rr dns.RR) {
		key := rr.String()
		if seen[key] {
			return
		}
		seen[key] = true
		*dst = append(*dst, rr)
	}
	for _, q := range query.Question {
		name := strings.ToLower(dns.Fqdn(q.Name))
		for _, rr := range r.index[name] {
			if q.Qtype != dns.TypeANY && rr.Header().Rrtype != q.Qtype {
				continue
			}
			add(&answers, rr)
			// Add SRV/TXT/A targets so clients resolve in one round trip.
			switch v := rr.(type) {
			case *dns.PTR:
				for _, extra := range r.index[strings.ToLower(v.Ptr)] {
					add(&additionals, extra)
				}
			case *dns.SRV:
				for _, extra := range r.index[strings.ToLower(v.Target)] {
					add(&additionals, extra)
				}
			}
		}
	}
	r.mu.RUnlock()
	if len(answers) == 0 {
		return
	}
	resp := new(dns.Msg)
	resp.SetReply(query)
	resp.Authoritative = true
	resp.RecursionAvailable = false
	resp.Answer = answers
	resp.Extra = additionals
	r.send(resp, ifIndex, 0)
}

func (r *responder) send(msg *dns.Msg, ifIndex int, ttl uint32) {
	r.sendTTL(msg, ifIndex, ttl, false)
}

// sendTTL multicasts msg with an explicit record TTL. Unlike send it honours
// ttl=0, which is required for goodbye packets that expire stale records.
func (r *responder) sendTTL(msg *dns.Msg, ifIndex int, ttl uint32, allowZero bool) {
	if ttl == 0 && !allowZero {
		ttl = mdnsTTL
	}
	out := msg.Copy()
	for _, section := range [][]dns.RR{out.Answer, out.Ns, out.Extra} {
		for _, rr := range section {
			rr.Header().Ttl = ttl
		}
	}
	buf, err := out.Pack()
	if err != nil {
		return
	}
	dst := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	cm := &ipv4.ControlMessage{}
	if ifIndex > 0 {
		cm.IfIndex = ifIndex
	} else if r.iface != nil {
		cm.IfIndex = r.iface.Index
	}
	if _, err := r.conn.WriteTo(buf, cm, dst); err != nil {
		if r.logger != nil {
			r.logger.Printf("airprint: mDNS send failed: %v", err)
		}
	}
}

func (r *responder) announceLoop() {
	ticker := time.NewTicker(announceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.announce(false)
		}
	}
}

func (r *responder) announce(goodbye bool) {
	r.mu.RLock()
	all := append([]dns.RR(nil), r.all...)
	r.mu.RUnlock()
	if len(all) == 0 {
		return
	}
	msg := new(dns.Msg)
	msg.Response = true
	msg.Authoritative = true
	msg.Answer = all
	ttl := uint32(mdnsTTL)
	if goodbye {
		ttl = 0
	}
	r.send(msg, 0, ttl)
}

// SendGoodbye multicasts zero-TTL records so clients drop stale service names
// immediately. It repeats the packet because a single multicast goodbye is
// easily lost on Wi-Fi.
func (r *responder) SendGoodbye(records []dns.RR) {
	if len(records) == 0 {
		return
	}
	for i := 0; i < 3; i++ {
		msg := new(dns.Msg)
		msg.Response = true
		msg.Authoritative = true
		msg.Answer = records
		r.sendTTL(msg, 0, 0, true)
		time.Sleep(150 * time.Millisecond)
	}
}

// SetRecords replaces the published record set and announces it.
func (r *responder) SetRecords(records []dns.RR) {
	index := map[string][]dns.RR{}
	for _, rr := range records {
		key := strings.ToLower(rr.Header().Name)
		index[key] = append(index[key], rr)
	}
	r.mu.Lock()
	r.all = records
	r.index = index
	r.mu.Unlock()
	// Announce twice, as recommended for reliability on lossy Wi-Fi.
	r.announce(false)
	time.AfterFunc(time.Second, func() {
		select {
		case <-r.done:
		default:
			r.announce(false)
		}
	})
}

func (r *responder) Close() {
	r.closeOnce.Do(func() {
		close(r.done)
		r.announce(true) // goodbye
		time.Sleep(50 * time.Millisecond)
		r.udp.Close()
	})
}

// goodbyeRecords builds zero-TTL records that remove an instance from client
// caches. RFC 6762 section 10.1 requires every record of the service to be
// re-sent with TTL 0, not only the PTR: Apple clients keep the entry in the
// printer picker as long as SRV/TXT are still cached.
//
// legacySubtype also expires the malformed form produced by early builds,
// where the DNS-SD subtype was registered as an ordinary service type and the
// instance therefore lived under "<instance>._universal._sub._ipp._tcp"
// instead of "<instance>._ipp._tcp". iOS shows that as a second printer.
func goodbyeRecords(instance, service string, subtypes []string, legacySubtype bool) []dns.RR {
	zero := func(name string, rrtype uint16) dns.RR_Header {
		return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: rrtype, Class: dns.ClassINET, Ttl: 0}
	}
	serviceName := dns.Fqdn(service)
	instanceName := dns.Fqdn(instance + "." + serviceName)
	out := []dns.RR{
		&dns.PTR{Hdr: zero(serviceName, dns.TypePTR), Ptr: instanceName},
		&dns.SRV{Hdr: zero(instanceName, dns.TypeSRV), Port: 0, Target: "."},
		&dns.TXT{Hdr: zero(instanceName, dns.TypeTXT), Txt: []string{}},
	}
	for _, sub := range subtypes {
		subName := dns.Fqdn(sub)
		out = append(out, &dns.PTR{Hdr: zero(subName, dns.TypePTR), Ptr: instanceName})
		if legacySubtype {
			legacyName := dns.Fqdn(instance + "." + subName)
			out = append(out,
				&dns.PTR{Hdr: zero(subName, dns.TypePTR), Ptr: legacyName},
				&dns.SRV{Hdr: zero(legacyName, dns.TypeSRV), Port: 0, Target: "."},
				&dns.TXT{Hdr: zero(legacyName, dns.TypeTXT), Txt: []string{}},
			)
		}
	}
	return out
}

// buildRecords renders DNS-SD records for the given service instances.
func buildRecords(instances []serviceInstance, ttl uint32) []dns.RR {
	if ttl == 0 {
		ttl = mdnsTTL
	}
	var out []dns.RR
	// Shared PTR records must not use the cache-flush bit, while SRV/TXT/A
	// records are unique to this responder and should flush stale copies.
	hdr := func(name string, rrtype uint16) dns.RR_Header {
		class := uint16(dns.ClassINET)
		if rrtype != dns.TypePTR {
			class |= 0x8000
		}
		return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: rrtype, Class: class, Ttl: ttl}
	}
	hosts := map[string]net.IP{}
	services := map[string]bool{}
	for _, in := range instances {
		serviceName := dns.Fqdn(in.Service)
		instanceName := dns.Fqdn(in.Instance + "." + strings.TrimSuffix(serviceName, "."))
		hostName := dns.Fqdn(in.Host)

		// PTR for the service type and each DNS-SD subtype.
		out = append(out, &dns.PTR{Hdr: hdr(serviceName, dns.TypePTR), Ptr: instanceName})
		for _, sub := range in.Subtypes {
			out = append(out, &dns.PTR{Hdr: hdr(dns.Fqdn(sub), dns.TypePTR), Ptr: instanceName})
		}
		services[serviceName] = true
		out = append(out, &dns.SRV{
			Hdr:    hdr(instanceName, dns.TypeSRV),
			Port:   uint16(in.Port),
			Target: hostName,
		})
		out = append(out, &dns.TXT{Hdr: hdr(instanceName, dns.TypeTXT), Txt: in.Text})
		if in.IP != nil {
			hosts[hostName] = in.IP
		}
	}
	// Service type enumeration, used by some browsers.
	for svc := range services {
		out = append(out, &dns.PTR{
			Hdr: hdr("_services._dns-sd._udp.local.", dns.TypePTR),
			Ptr: svc,
		})
	}
	for host, ip := range hosts {
		if ip4 := ip.To4(); ip4 != nil {
			out = append(out, &dns.A{Hdr: hdr(host, dns.TypeA), A: ip4})
		}
	}
	return out
}
