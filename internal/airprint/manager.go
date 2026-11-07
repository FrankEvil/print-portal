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
	"github.com/liuzhaojun/print-portal/internal/storage"
)

type Manager struct {
	store  *storage.Store
	cfg    config.Config
	logger *log.Logger

	mu       sync.Mutex
	services map[int64][]*zeroconf.Server
}

func New(store *storage.Store, cfg config.Config, logger *log.Logger) *Manager {
	return &Manager{store: store, cfg: cfg, logger: logger, services: map[int64][]*zeroconf.Server{}}
}

func (m *Manager) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	m.sync()
	for {
		select {
		case <-ctx.Done():
			m.stopAll()
			return
		case <-ticker.C:
			m.sync()
		}
	}
}

func (m *Manager) sync() {
	printers, err := m.store.ListPrinters()
	if err != nil {
		m.logger.Printf("airprint: list printers: %v", err)
		return
	}
	desired := map[int64]storage.Printer{}
	for _, p := range printers {
		if p.IsShared && p.AirPrint && strings.TrimSpace(p.PPDQueue) != "" {
			desired[p.ID] = p
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, servers := range m.services {
		if _, ok := desired[id]; !ok {
			stopServers(servers)
			delete(m.services, id)
			m.logger.Printf("airprint: stopped printer %d", id)
		}
	}
	for id, p := range desired {
		if _, ok := m.services[id]; ok {
			continue
		}
		servers, err := m.publish(p)
		if err != nil {
			m.logger.Printf("airprint: publish %s: %v", p.Name, err)
			continue
		}
		m.services[id] = servers
		m.logger.Printf("airprint: published %s", p.Name)
	}
}

func (m *Manager) publish(p storage.Printer) ([]*zeroconf.Server, error) {
	port := ippPort(m.cfg.IPPAddr)
	host := os.Getenv("HOSTNAME")
	if host == "" {
		host, _ = os.Hostname()
	}
	if host == "" {
		host = "print-portal"
	}
	host = strings.TrimSuffix(strings.TrimSuffix(host, "."), ".local")
	ip := m.cfg.PublicHost
	if ip == "" {
		ip = detectLANIP()
	}
	if ip == "" {
		return nil, fmt.Errorf("cannot determine LAN IP")
	}
	ifaces := interfacesForIP(ip)
	instance := fmt.Sprintf("AirPrint %s @ %s", p.Name, host)
	text := []string{
		"txtvers=1",
		"qtotal=1",
		"rp=printers/" + p.PPDQueue,
		"pdl=application/pdf,image/urf,image/jpeg,image/png",
		"URF=none",
		"product=(Print Portal)",
		"note=" + p.Name,
		"printer-state=3",
	}
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

func (m *Manager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, servers := range m.services {
		stopServers(servers)
		delete(m.services, id)
	}
}
