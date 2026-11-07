package discovery

import (
	"context"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/mdns"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

const mdnsBrowseTimeout = 2500 * time.Millisecond

// discoverMDNSPrinters browses _ipp._tcp/_ipps._tcp with the in-house mDNS
// browser. It returns direct IPP URIs (ipp://IP:port/printers/...) instead of
// dnssd:// URIs so CUPS does not need Avahi to resolve them at add/print time.
func discoverMDNSPrinters(ctx context.Context) []storage.Device {
	services, err := mdns.Browse(ctx, []string{"_ipp._tcp.local.", "_ipps._tcp.local."}, mdnsBrowseTimeout)
	if err != nil {
		return nil
	}
	localIPs := localIPv4Set()
	var out []storage.Device
	for _, svc := range services {
		if isSelfMDNSService(svc, localIPs) {
			continue
		}
		if dev, ok := deviceFromMDNSService(svc); ok {
			out = append(out, dev)
		}
	}
	return dedupeMDNSDevices(out)
}

func deviceFromMDNSService(svc mdns.Service) (storage.Device, bool) {
	if svc.IP == nil || svc.Port <= 0 || svc.Port > 65535 {
		return storage.Device{}, false
	}
	name := strings.TrimSpace(svc.Text["ty"])
	if name == "" {
		name = mdnsInstanceName(svc.Instance)
	}
	model := strings.Trim(strings.TrimSpace(svc.Text["product"]), "()")
	if model == "" {
		model = name
	}
	brand := firstWord(model)
	if brand == "" {
		brand = firstWord(name)
	}
	scheme := "ipp"
	if strings.EqualFold(svc.Service, "_ipps._tcp") {
		scheme = "ipps"
	}
	path := "/"
	if rp := strings.TrimSpace(svc.Text["rp"]); rp != "" {
		path = "/" + strings.TrimPrefix(rp, "/")
	}
	u := url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(svc.IP.String(), strconv.Itoa(svc.Port)),
		Path:   path,
	}
	return storage.Device{
		Kind:      "printer",
		Name:      name,
		Brand:     brand,
		Model:     model,
		DeviceURI: u.String(),
		Source:    "network",
		Metadata:  `{"transport":"network","protocol":"mdns"}`,
	}, true
}

func mdnsInstanceName(instance string) string {
	if i := strings.LastIndex(instance, " @ "); i > 0 {
		return strings.TrimSpace(instance[:i])
	}
	return strings.TrimSpace(instance)
}

func firstWord(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func localIPv4Set() map[string]bool {
	out := map[string]bool{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip4 := ip.To4(); ip4 != nil {
				out[ip4.String()] = true
			}
		}
	}
	return out
}

func isSelfMDNSService(svc mdns.Service, localIPs map[string]bool) bool {
	// Any Print Portal instance advertises the same synthetic product. Hide
	// the whole family so one portal never lists another portal's queue as a
	// physical printer.
	if strings.Contains(strings.ToLower(svc.Text["product"]), "print portal") {
		return true
	}
	if svc.IP != nil && localIPs[svc.IP.String()] {
		return svc.Port == 8631 || strings.EqualFold(strings.TrimSuffix(svc.Host, "."), "print-portal.local")
	}
	return false
}

// dedupeMDNSDevices keeps one entry per physical printer, preferring plain IPP
// over IPPS when a printer advertises both service types.
func dedupeMDNSDevices(in []storage.Device) []storage.Device {
	byKey := map[string]storage.Device{}
	var order []string
	for _, dev := range in {
		key := strings.ToLower(dev.Name) + "|" + uriHost(dev.DeviceURI)
		if existing, ok := byKey[key]; ok {
			if strings.HasPrefix(dev.DeviceURI, "ipp://") && strings.HasPrefix(existing.DeviceURI, "ipps://") {
				byKey[key] = dev
			}
			continue
		}
		byKey[key] = dev
		order = append(order, key)
	}
	out := make([]storage.Device, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func uriHost(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}
