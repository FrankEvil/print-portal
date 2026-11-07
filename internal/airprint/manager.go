// Package airprint publishes shared Print Portal PPD queues as AirPrint /
// Mopria discoverable IPP services. The responder is embedded in printd and
// speaks multicast DNS directly; it does not depend on host Avahi or D-Bus.
package airprint

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/liuzhaojun/print-portal/internal/config"
	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
	"github.com/miekg/dns"
)

const (
	syncInterval  = 5 * time.Second
	unhealthyWait = 30 * time.Second
	// legacyGoodbyeWindow is how long after startup stale instance names keep
	// being expired. Phones asleep during a restart would otherwise keep a
	// duplicate entry in their cache until its TTL elapses.
	legacyGoodbyeWindow = 10 * time.Minute
)

type Manager struct {
	store  *storage.Store
	cfg    config.Config
	cups   *cups.Client
	logger *log.Logger

	mu             sync.Mutex
	responder      *responder
	expired        map[string]bool
	legacyDeadline time.Time
	records        []dns.RR
	unhealthySince map[int64]time.Time
	lanIP          string
	host           string
}

func New(store *storage.Store, cfg config.Config, cupsClient *cups.Client, logger *log.Logger) *Manager {
	host := strings.TrimSpace(os.Getenv("HOSTNAME"))
	if got, err := os.Hostname(); err == nil && strings.TrimSpace(got) != "" {
		host = strings.TrimSpace(got)
	}
	if host == "" {
		host = "print-portal"
	}
	// Advertise a dedicated mDNS hostname instead of reusing the container
	// hostname. On fnOS the host avahi-daemon already owns "<host>.local", and
	// two responders claiming the same name makes iOS drop the printer while
	// more tolerant clients still work.
	if v := strings.TrimSpace(os.Getenv("PRINT_PORTAL_AIRPRINT_HOST")); v != "" {
		host = v
	} else {
		host = "print-portal"
	}
	host = strings.TrimSuffix(strings.TrimSuffix(host, "."), ".local")
	if logger != nil {
		logger.Printf("airprint: advertising mDNS host %q.local", host)
	}
	return &Manager{
		store: store, cfg: cfg, cups: cupsClient, logger: logger,
		unhealthySince: map[int64]time.Time{}, expired: map[string]bool{}, host: host,
	}
}

func (m *Manager) Start(ctx context.Context) {
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	m.sync(ctx)
	for {
		select {
		case <-ctx.Done():
			m.stopAll()
			return
		case <-ticker.C:
			m.sync(ctx)
		}
	}
}

func (m *Manager) sync(ctx context.Context) {
	ip := strings.TrimSpace(m.cfg.PublicHost)
	if ip == "" {
		ip = detectLANIP()
	}
	if ip == "" {
		m.logger.Printf("airprint: cannot determine LAN IP")
		return
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		m.logger.Printf("airprint: invalid LAN IP %q", ip)
		return
	}

	m.mu.Lock()
	ipChanged := m.lanIP != "" && m.lanIP != ip
	m.lanIP = ip
	m.mu.Unlock()
	if ipChanged {
		m.logger.Printf("airprint: LAN IP changed to %s; rebroadcasting", ip)
	}

	printers, err := m.store.ListPrinters()
	if err != nil {
		m.logger.Printf("airprint: list printers: %v", err)
		return
	}
	statuses, err := m.cups.ListPrinters(ctx)
	if err != nil {
		// Keep the last known records through a transient CUPS restart.
		m.logger.Printf("airprint: CUPS status unavailable: %v", err)
		return
	}
	byQueue := map[string]cups.PrinterStatus{}
	for _, st := range statuses {
		byQueue[st.Name] = st
	}

	now := time.Now()
	var instances []serviceInstance
	for _, p := range printers {
		if !p.IsShared || !p.AirPrint || strings.TrimSpace(p.PPDQueue) == "" {
			continue
		}
		st, ok := byQueue[p.PPDQueue]
		healthy := ok && st.State != "stopped" && st.Accepting
		if healthy {
			delete(m.unhealthySince, p.ID)
		} else {
			since, seen := m.unhealthySince[p.ID]
			if !seen {
				since = now
				m.unhealthySince[p.ID] = now
			}
			if now.Sub(since) >= unhealthyWait {
				// Advertise offline instead of vanishing, so devices keep the
				// printer listed while it is temporarily unavailable.
				continue
			}
		}
		instances = append(instances, serviceInstance{
			// Keep the instance name derived from the container hostname, which
			// is stable for a given deployment. Clients cache DNS-SD entries by
			// instance name, so a changing name looks like an extra printer.
			Instance: fmt.Sprintf("AirPrint %s @ %s", p.Name, m.instanceHost()),
			Service:  "_ipp._tcp.local",
			Subtypes: []string{"_universal._sub._ipp._tcp.local"},
			Host:     m.host + ".local",
			IP:       parsed,
			Port:     ippPort(m.cfg.IPPAddr),
			Text:     txtRecords(p, st, m.host),
		})
	}

	m.expireLegacyInstances(printers, instances)

	records := buildRecords(instances, mdnsTTL)
	m.mu.Lock()
	changed := !sameRecords(m.records, records)
	m.records = records
	if changed {
		if m.responder == nil {
			iface := interfacesForIP(ip)
			var chosen *net.Interface
			if len(iface) > 0 {
				chosen = &iface[0]
			}
			r, err := newResponder(chosen, m.logger)
			if err != nil {
				m.logger.Printf("airprint: mDNS responder unavailable: %v", err)
			} else {
				m.responder = r
			}
		}
		if m.responder != nil {
			m.responder.SetRecords(records)
		}
	}
	m.mu.Unlock()

	if changed {
		m.logger.Printf("airprint: published %d service(s) on %s:%d", len(instances), ip, ippPort(m.cfg.IPPAddr))
	}
}

// expireLegacyInstances sends goodbye records for instance names produced by
// earlier builds. Those names embedded the mDNS hostname, so every hostname
// change left a duplicate printer in client caches. Goodbyes are repeated for
// a grace window because phones that were asleep when the container restarted
// would otherwise keep the stale entry until its TTL ran out.
func (m *Manager) expireLegacyInstances(printers []storage.Printer, current []serviceInstance) {
	m.mu.Lock()
	if m.responder == nil {
		m.mu.Unlock()
		return
	}
	r := m.responder
	if m.expired == nil {
		m.expired = map[string]bool{}
	}
	if m.legacyDeadline.IsZero() {
		m.legacyDeadline = time.Now().Add(legacyGoodbyeWindow)
	}
	if time.Now().After(m.legacyDeadline) {
		m.mu.Unlock()
		return
	}
	keep := map[string]bool{}
	for _, in := range current {
		keep[in.Instance] = true
	}
	var todo []string
	for _, p := range printers {
		if !p.IsShared || !p.AirPrint {
			continue
		}
		for _, legacy := range legacyInstanceNames(p.Name) {
			if keep[legacy] {
				continue
			}
			todo = append(todo, legacy)
		}
	}
	firstRun := len(m.expired) == 0
	m.mu.Unlock()

	for _, name := range todo {
		r.SendGoodbye(goodbyeRecords(name, "_ipp._tcp.local", []string{"_universal._sub._ipp._tcp.local"}, true))
		if firstRun {
			m.logger.Printf("airprint: expired legacy instance %q", name)
		}
	}
	m.mu.Lock()
	for _, name := range todo {
		m.expired[name] = true
	}
	m.mu.Unlock()
}

// legacyInstanceNames lists instance names emitted by earlier releases. Each
// rename left an extra entry in client DNS-SD caches, so every historical form
// has to be expired explicitly.
func legacyInstanceNames(printerName string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	// Historical "<printer> (Print Portal)" form.
	add(fmt.Sprintf("%s (Print Portal)", printerName))
	// Historical "AirPrint <printer> @ <hostname>" forms.
	hosts := []string{"print-portal", "fnos", "orbstack", "print-portal"}
	if got, err := os.Hostname(); err == nil {
		hosts = append(hosts, strings.TrimSuffix(strings.TrimSuffix(got, "."), ".local"))
	}
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		add(fmt.Sprintf("AirPrint %s @ %s", printerName, h))
	}
	return out
}

func sameRecords(a, b []dns.RR) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].String() != b[i].String() {
			return false
		}
	}
	return true
}

// instanceHost returns the hostname fragment used in DNS-SD instance names.
func (m *Manager) instanceHost() string {
	if v := strings.TrimSpace(os.Getenv("PRINT_PORTAL_AIRPRINT_NAME")); v != "" {
		return v
	}
	host := strings.TrimSpace(os.Getenv("HOSTNAME"))
	if got, err := os.Hostname(); err == nil && strings.TrimSpace(got) != "" {
		host = strings.TrimSpace(got)
	}
	host = strings.TrimSuffix(strings.TrimSuffix(host, "."), ".local")
	if host == "" {
		host = "print-portal"
	}
	return host
}

func txtRecords(p storage.Printer, st cups.PrinterStatus, host string) []string {
	state := "3"
	if st.State == "stopped" {
		state = "5"
	}
	// product must be a human readable model, not a driver URI such as
	// drv:///brlaser.drv/br7060d.ppd; iOS shows it in the picker.
	model := strings.TrimSpace(st.MakeModel)
	if model == "" {
		model = strings.TrimSpace(p.Model)
	}
	if strings.Contains(model, "://") || strings.HasSuffix(strings.ToLower(model), ".ppd") {
		model = ""
	}
	if model == "" {
		model = "Print Portal"
	}
	// Apple's Bonjour Printing Specification requires ty, product, pdl and
	// URF; iOS is stricter than macOS/Mopria and silently hides services that
	// omit ty or advertise capabilities the queue cannot honour.
	return []string{
		"txtvers=1",
		"qtotal=1",
		"rp=printers/" + p.PPDQueue,
		"ty=" + p.Name,
		"product=(" + model + ")",
		"pdl=application/pdf,image/urf,image/jpeg,image/png",
		"URF=none",
		"note=" + p.Name,
		"printer-state=" + state,
		"Color=F",
		"Duplex=F",
		"kind=document,envelope,photo",
		"PaperMax=legal-A4",
		"adminurl=http://" + host + ".local:8088/admin",
	}
}

func ippPort(addr string) int {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		var port int
		if _, err := fmt.Sscanf(addr[i+1:], "%d", &port); err == nil && port > 0 {
			return port
		}
	}
	return 8631
}

func detectLANIP() string {
	if conn, err := net.Dial("udp", "223.5.5.5:80"); err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			if ip4 := addr.IP.To4(); ip4 != nil && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
				return ip4.String()
			}
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip4 := ip.To4(); ip4 != nil && ip4.IsPrivate() {
				return ip4.String()
			}
		}
	}
	return ""
}

func interfacesForIP(ip string) []net.Interface {
	target := net.ParseIP(ip)
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var got net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				got = v.IP
			case *net.IPAddr:
				got = v.IP
			}
			if got != nil && got.Equal(target) {
				return []net.Interface{iface}
			}
		}
	}
	return nil
}

func (m *Manager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.responder != nil {
		m.responder.Close()
		m.responder = nil
	}
	m.records = nil
}
