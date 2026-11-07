// Package mdns contains the multicast DNS plumbing used by Print Portal's
// built-in responder and browser. It deliberately has no dependency on Avahi
// or D-Bus: the appliance must work on hosts where no mDNS daemon exists.
package mdns

import (
	"context"
	"errors"
	"net"
	"syscall"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

const (
	// GroupAddr is the mDNS multicast group and port.
	GroupAddr = "224.0.0.251:5353"

	// MaxPacketSize is large enough for normal DNS-SD responses. mDNS
	// responders keep answers well below this; oversized packets are dropped
	// by Unpack and simply retried by the caller.
	MaxPacketSize = 9000
)

// PacketConn wraps an IPv4 UDP socket joined to the mDNS multicast group.
type PacketConn struct {
	Packet *ipv4.PacketConn
	UDP    *net.UDPConn
}

func openSocket() (*ipv4.PacketConn, *net.UDPConn, error) {
	lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
		var setErr error
		if err := c.Control(func(fd uintptr) {
			if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
				setErr = err
				return
			}
			// Allow coexisting with a host mDNS daemon on the same port.
			_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		}); err != nil {
			return err
		}
		return setErr
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:5353")
	if err != nil {
		return nil, nil, err
	}
	udp, ok := pc.(*net.UDPConn)
	if !ok {
		pc.Close()
		return nil, nil, errors.New("mdns: unexpected packet connection type")
	}
	return ipv4.NewPacketConn(udp), udp, nil
}

func configure(p *ipv4.PacketConn) {
	_ = p.SetMulticastTTL(255)
	_ = p.SetMulticastLoopback(true)
	_ = p.SetControlMessage(ipv4.FlagInterface, true)
}

// MulticastInterfaces returns interfaces that can carry IPv4 multicast. It
// excludes loopback and link-local-only interfaces: sending browse queries on
// awdl0/usbnet-style interfaces would either fail or pollute the result set.
func MulticastInterfaces() []*net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []*net.Interface
	for i := range ifaces {
		iface := ifaces[i]
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagMulticast == 0 {
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
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, &iface)
			break
		}
	}
	return out
}

// OpenMulticast opens an IPv4 mDNS socket and joins the multicast group on
// the given interface. A nil interface keeps the kernel's default behavior,
// which is what the AirPrint responder uses when no LAN interface was found.
func OpenMulticast(iface *net.Interface) (*ipv4.PacketConn, *net.UDPConn, error) {
	p, udp, err := openSocket()
	if err != nil {
		return nil, nil, err
	}
	if iface != nil {
		group := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251)}
		if err := p.JoinGroup(iface, group); err != nil {
			udp.Close()
			return nil, nil, err
		}
	}
	configure(p)
	return p, udp, nil
}

// OpenMulticastAll opens one IPv4 mDNS socket and joins the multicast group on
// every usable interface. Returning the interface list lets the browser send
// each query explicitly on each interface (mDNS is not routed by default).
func OpenMulticastAll() (*PacketConn, []*net.Interface, error) {
	p, udp, err := openSocket()
	if err != nil {
		return nil, nil, err
	}
	group := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251)}
	ifaces := MulticastInterfaces()
	if len(ifaces) == 0 {
		udp.Close()
		return nil, nil, errors.New("mdns: no multicast interface")
	}
	joined := 0
	for _, iface := range ifaces {
		if err := p.JoinGroup(iface, group); err != nil {
			continue
		}
		joined++
	}
	if joined == 0 {
		udp.Close()
		return nil, nil, errors.New("mdns: cannot join multicast group")
	}
	configure(p)
	return &PacketConn{Packet: p, UDP: udp}, ifaces, nil
}

// Send packs and multicasts msg on the given interface. ifIndex=0 uses the
// kernel's default interface.
func (c *PacketConn) Send(msg *dns.Msg, ifIndex int) error {
	buf, err := msg.Pack()
	if err != nil {
		return err
	}
	dst := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	cm := &ipv4.ControlMessage{}
	if ifIndex > 0 {
		cm.IfIndex = ifIndex
	}
	_, err = c.Packet.WriteTo(buf, cm, dst)
	return err
}

// Close releases the socket.
func (c *PacketConn) Close() error {
	return c.UDP.Close()
}
