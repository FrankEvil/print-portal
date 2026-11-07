package mdns

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// Service is one DNS-SD service discovered by Browse.
type Service struct {
	Instance string            // e.g. "EPSON L1110 Series @ 刘兆军的Mac mini"
	Service  string            // e.g. "_ipp._tcp"
	Host     string            // SRV target, without trailing dot
	IP       net.IP            // first IPv4 A record for Host
	Port     int               // SRV port
	Text     map[string]string // parsed TXT key/value pairs
}

// Browse sends DNS-SD PTR queries for serviceTypes and collects responses
// until timeout. It returns whatever was discovered; a socket-level error is
// only returned when nothing could be read at all.
func Browse(ctx context.Context, serviceTypes []string, timeout time.Duration) ([]Service, error) {
	conn, ifaces, err := OpenMulticastAll()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return browseWithConn(ctx, conn, ifaces, serviceTypes, timeout)
}

func browseWithConn(ctx context.Context, conn *PacketConn, ifaces []*net.Interface, serviceTypes []string, timeout time.Duration) ([]Service, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	collector := newCollector(serviceTypes)

	msgs := make(chan *dns.Msg, 64)
	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, MaxPacketSize)
		for {
			n, _, _, err := conn.Packet.ReadFrom(buf)
			if err != nil {
				select {
				case readErr <- err:
				default:
				}
				return
			}
			msg := new(dns.Msg)
			if err := msg.Unpack(buf[:n]); err != nil {
				continue
			}
			if !msg.Response {
				continue
			}
			select {
			case msgs <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	send := func(name string, qtype uint16) {
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(name), qtype)
		msg.RecursionDesired = false
		for _, iface := range ifaces {
			_ = conn.Send(msg, iface.Index)
		}
	}

	for _, svc := range serviceTypes {
		send(svc, dns.TypePTR)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return collector.results(), nil
		case <-timer.C:
			return collector.results(), nil
		case <-ticker.C:
			// PTR queries are cheap and mDNS is lossy; repeat them while
			// collecting, then chase missing SRV/TXT/A records.
			for _, svc := range serviceTypes {
				send(svc, dns.TypePTR)
			}
			for _, q := range collector.pendingQueries() {
				send(q.name, q.qtype)
			}
		case msg := <-msgs:
			collector.add(msg)
		case err := <-readErr:
			if len(collector.results()) > 0 {
				return collector.results(), nil
			}
			return nil, err
		}
	}
}

type pendingQuery struct {
	name  string
	qtype uint16
}

type collector struct {
	types    map[string]bool
	records  map[string][]dns.RR
	services map[string]*Service
	order    []string
}

func newCollector(serviceTypes []string) *collector {
	types := make(map[string]bool, len(serviceTypes))
	for _, svc := range serviceTypes {
		types[normalizeServiceType(svc)] = true
	}
	return &collector{
		types:    types,
		records:  map[string][]dns.RR{},
		services: map[string]*Service{},
	}
}

func normalizeServiceType(name string) string {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	return strings.TrimSuffix(name, ".local")
}

func (c *collector) add(msg *dns.Msg) {
	if msg == nil || !msg.Response {
		return
	}
	for _, section := range [][]dns.RR{msg.Answer, msg.Ns, msg.Extra} {
		for _, rr := range section {
			key := strings.ToLower(unescapeDNS(rr.Header().Name))
			c.records[key] = append(c.records[key], rr)
		}
	}
	c.rebuild()
}

func (c *collector) rebuild() {
	for _, rrs := range c.records {
		for _, rr := range rrs {
			ptr, ok := rr.(*dns.PTR)
			if !ok {
				continue
			}
			service := normalizeServiceType(unescapeDNS(ptr.Hdr.Name))
			if !c.types[service] {
				continue
			}
			target := strings.TrimSuffix(unescapeDNS(ptr.Ptr), ".")
			suffix := "." + service + ".local"
			if len(target) <= len(suffix) || !strings.HasSuffix(strings.ToLower(target), strings.ToLower(suffix)) {
				continue
			}
			instance := target[:len(target)-len(suffix)]
			key := strings.ToLower(instance) + "|" + service
			svc, ok := c.services[key]
			if !ok {
				svc = &Service{Instance: instance, Service: service}
				c.services[key] = svc
				c.order = append(c.order, key)
			}
			c.fill(svc)
		}
	}
}

func (c *collector) fill(svc *Service) {
	instanceName := dns.Fqdn(svc.Instance + "." + svc.Service + ".local")
	for _, rr := range c.records[strings.ToLower(instanceName)] {
		switch v := rr.(type) {
		case *dns.SRV:
			svc.Host = strings.TrimSuffix(unescapeDNS(v.Target), ".")
			svc.Port = int(v.Port)
		case *dns.TXT:
			svc.Text = parseTXT(v.Txt)
		}
	}
	if svc.Host == "" || svc.IP != nil {
		return
	}
	for _, rr := range c.records[strings.ToLower(dns.Fqdn(svc.Host))] {
		a, ok := rr.(*dns.A)
		if !ok || a.A == nil {
			continue
		}
		if ip4 := a.A.To4(); ip4 != nil {
			svc.IP = ip4
			break
		}
	}
}

// unescapeDNS converts the presentation-format escaping used by miekg/dns
// (e.g. "EPSON\ L1110" or "\229\136\152") back to raw UTF-8. DNS labels and
// TXT strings are not UTF-8 by definition; the wire format is raw bytes.
func unescapeDNS(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		if i+3 < len(s) && isDigit(s[i+1]) && isDigit(s[i+2]) && isDigit(s[i+3]) {
			v := int(s[i+1]-'0')*100 + int(s[i+2]-'0')*10 + int(s[i+3]-'0')
			if v <= 255 {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		i++
		b.WriteByte(s[i])
	}
	return b.String()
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func parseTXT(items []string) map[string]string {
	out := make(map[string]string, len(items))
	for _, raw := range items {
		item := unescapeDNS(raw)
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			out[item] = ""
			continue
		}
		out[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return out
}

func (c *collector) pendingQueries() []pendingQuery {
	var out []pendingQuery
	for _, key := range c.order {
		svc := c.services[key]
		if svc.Host == "" {
			out = append(out, pendingQuery{dns.Fqdn(svc.Instance + "." + svc.Service + ".local"), dns.TypeSRV})
		}
		if svc.Text == nil {
			out = append(out, pendingQuery{dns.Fqdn(svc.Instance + "." + svc.Service + ".local"), dns.TypeTXT})
		}
		if svc.Host != "" && svc.IP == nil {
			out = append(out, pendingQuery{dns.Fqdn(svc.Host), dns.TypeA})
		}
	}
	return out
}

func (c *collector) results() []Service {
	out := make([]Service, 0, len(c.order))
	for _, key := range c.order {
		svc := c.services[key]
		copySvc := *svc
		if svc.Text != nil {
			copySvc.Text = make(map[string]string, len(svc.Text))
			for k, v := range svc.Text {
				copySvc.Text[k] = v
			}
		}
		if svc.IP != nil {
			copySvc.IP = append(net.IP(nil), svc.IP...)
		}
		out = append(out, copySvc)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Instance == out[j].Instance {
			return out[i].Service < out[j].Service
		}
		return out[i].Instance < out[j].Instance
	})
	return out
}
