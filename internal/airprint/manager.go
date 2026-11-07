// Package airprint publishes shared Print Portal PPD queues as AirPrint /
// Mopria discoverable IPP services. The responder is embedded in printd and
// uses multicast DNS directly; it does not depend on host Avahi or D-Bus.
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

	"github.com/grandcat/zeroconf"
	"github.com/liuzhaojun/print-portal/internal/config"
	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

const (
	syncInterval  = 5 * time.Second
	unhealthyWait = 30 * time.Second
)

type Manager struct {
	store  *storage.Store
	cfg    config.Config
	cups   *cups.Client
	logger *log.Logger

	mu             sync.Mutex
	services       map[int64]*published
	unhealthySince map[int64]time.Time
	lanIP          string
	host           string
}

type published struct {
	servers     []*zeroconf.Server
	fingerprint string
	text        []string
}

func New(store *storage.Store, cfg config.Config, cupsClient *cups.Client, logger *log.Logger) *Manager {
	host := strings.TrimSpace(os.Getenv("HOSTNAME"))
	if got, err := os.Hostname(); err == nil && strings.TrimSpace(got) != "" {
		host = strings.TrimSpace(got)
	}
	if host == "" {
		host = "print-portal"
	}
	host = strings.TrimSuffix(strings.TrimSuffix(host, "."), ".local")
	return &Manager{
		store: store, cfg: cfg, cups: cupsClient, logger: logger,
		services: map[int64]*published{}, unhealthySince: map[int64]time.Time{}, host: host,
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
	ip := m.cfg.PublicHost
	if strings.TrimSpace(ip) == "" {
		ip = detectLANIP()
	}
	if ip == "" {
		m.logger.Printf("airprint: cannot determine LAN IP")
		return
	}

	// A network change invalidates every existing mDNS registration.
	m.mu.Lock()
	if m.lanIP != "" && m.lanIP != ip {
		m.stopAllLocked()
		m.logger.Printf("airprint: LAN IP changed %s -> %s; rebroadcasting", m.lanIP, ip)
	}
	m.lanIP = ip
	m.mu.Unlock()

	printers, err := m.store.ListPrinters()
	if err != nil {
		m.logger.Printf("airprint: list printers: %v", err)
		return
	}
	statuses, err := m.cups.ListPrinters(ctx)
	if err != nil {
		// Keep the last known services through a transient CUPS restart.
		m.logger.Printf("airprint: CUPS status unavailable: %v", err)
		return
	}
	byQueue := map[string]cups.PrinterStatus{}
	for _, st := range statuses {
		byQueue[st.Name] = st
	}

	now := time.Now()
	wanted := map[int64]storage.Printer{}
	for _, p := range printers {
		if !p.IsShared || !p.AirPrint || strings.TrimSpace(p.PPDQueue) == "" {
			continue
		}
		st, ok := byQueue[p.PPDQueue]
		healthy := ok && st.State != "stopped" && st.Accepting
		if healthy {
			delete(m.unhealthySince, p.ID)
			wanted[p.ID] = p
			continue
		}
		if _, exists := m.unhealthySince[p.ID]; !exists {
			m.unhealthySince[p.ID] = now
		}
		if now.Sub(m.unhealthySince[p.ID]) < unhealthyWait {
			// Keep advertising during short printer/CUPS hiccups.
			wanted[p.ID] = p
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for id, svc := range m.services {
		if _, ok := wanted[id]; !ok {
			stopServers(svc.servers)
			delete(m.services, id)
			m.logger.Printf("airprint: stopped printer %d", id)
		}
	}
	for id, p := range wanted {
		st := byQueue[p.PPDQueue]
		text := txtRecords(p, st)
		fp := fingerprint(p, m.lanIP, m.host, ippPort(m.cfg.IPPAddr), text)
		if svc, ok := m.services[id]; ok {
			if svc.fingerprint == fp {
				continue
			}
			stopServers(svc.servers)
			delete(m.services, id)
			m.logger.Printf("airprint: configuration changed for %s; re-registering", p.Name)
		}
		servers, err := m.publish(p, m.lanIP, m.host, text)
		if err != nil {
			m.logger.Printf("airprint: publish %s: %v", p.Name, err)
			continue
		}
		m.services[id] = &published{servers: servers, fingerprint: fp, text: text}
		m.logger.Printf("airprint: published %s (state=%s)", p.Name, st.State)
	}
}

func (m *Manager) publish(p storage.Printer, ip, host string, text []string) ([]*zeroconf.Server, error) {
	port := ippPort(m.cfg.IPPAddr)
	ifaces := interfacesForIP(ip)
	instance := fmt.Sprintf("AirPrint %s @ %s", p.Name, host)
	var servers []*zeroconf.Server
	for _, typ := range []string{"_ipp._tcp", "_universal._sub._ipp._tcp"} {
		s, err := zeroconf.RegisterProxy(instance, typ, "local", port, host, []string{ip}, text, ifaces)
		if err != nil {
			stopServers(servers)
			return nil, fmt.Errorf("%s: %w", typ, err)
		}
		s.TTL(120)
		servers = append(servers, s)
	}
	return servers, nil
}

func txtRecords(p storage.Printer, st cups.PrinterStatus) []string {
	state := "3"
	if st.State == "stopped" {
		state = "5"
	}
	return []string{
		"txtvers=1",
		"qtotal=1",
		"rp=printers/" + p.PPDQueue,
		"pdl=application/pdf,image/urf,image/jpeg,image/png",
		"URF=none",
		"product=(Print Portal)",
		"note=" + p.Name,
		"printer-state=" + state,
	}
}

func fingerprint(p storage.Printer, ip, host string, port int, text []string) string {
	return fmt.Sprintf("%d|%s|%s|%s|%d|%s", p.ID, p.Name, p.PPDQueue, ip, port, strings.Join(text, "\x1f"))
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

func stopServers(servers []*zeroconf.Server) {
	for _, s := range servers {
		s.Shutdown()
	}
}

func (m *Manager) stopAllLocked() {
	for id, svc := range m.services {
		stopServers(svc.servers)
		delete(m.services, id)
	}
}

func (m *Manager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopAllLocked()
}
