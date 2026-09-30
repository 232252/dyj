//go:build windows

package discovery

import (
	"context"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

// listenMulticast creates a UDP socket bound to 0.0.0.0:5353 (or [::]:5353) that
// can coexist with other 5353 listeners (Chrome on this host) and that joins
// the mDNS group on the requested interface only.
//
// This is the code path hashicorp/mdns does not give us. Measured behaviour of
// the alternatives on this host is recorded in
// docs/research/06-mdns-coexistence-verified.md.
func listenMulticast(ifi *net.Interface, ip net.IP) (*ifaceConn, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				h := windows.Handle(fd)
				if err := windows.SetsockoptInt(h, windows.SOL_SOCKET, windows.SO_REUSEADDR, 1); err != nil {
					serr = fmt.Errorf("SO_REUSEADDR: %w", err)
					return
				}
				// SO_REUSEPORT does not exist on Windows; SO_REUSEADDR is the
				// documented multi-listener mechanism there.
				if err := windows.SetsockoptInt(h, windows.IPPROTO_IP, windows.IP_MULTICAST_TTL, 255); err != nil {
					serr = fmt.Errorf("IP_MULTICAST_TTL: %w", err)
					return
				}
				if err := windows.SetsockoptInt(h, windows.IPPROTO_IP, windows.IP_MULTICAST_LOOP, 1); err != nil {
					serr = fmt.Errorf("IP_MULTICAST_LOOP: %w", err)
					return
				}
				// Pin multicast egress to this interface: without
				// IP_MULTICAST_IF every per-interface socket sends through
				// the default route's NIC, so on a multi-homed host LAN
				// peers never hear the announcements.
				if ip4 := ip.To4(); ip4 != nil {
					mif := &windows.IPMreq{Interface: [4]byte{ip4[0], ip4[1], ip4[2], ip4[3]}}
					if err := windows.SetsockoptIPMreq(h, windows.IPPROTO_IP, windows.IP_MULTICAST_IF, mif); err != nil {
						serr = fmt.Errorf("IP_MULTICAST_IF(%s): %w", ip4, err)
						return
					}
				}
				if ip4 := ip.To4(); ip4 != nil {
					mreq := &windows.IPMreq{Multiaddr: [4]byte{224, 0, 0, 251}}
					copy(mreq.Interface[:], ip4)
					if err := windows.SetsockoptIPMreq(h, windows.IPPROTO_IP, windows.IP_ADD_MEMBERSHIP, mreq); err != nil {
						serr = fmt.Errorf("IP_ADD_MEMBERSHIP(%s): %w", ip4, err)
						return
					}
				}
			}); err != nil {
				return err
			}
			return serr
		},
	}
	// Bind to the interface address, not the wildcard: on Windows a
	// wildcard-bound socket ignores IP_MULTICAST_IF for egress and every
	// announcement leaves through the default route's NIC (measured:
	// LAN peers receive nothing from a multi-homed host). A concrete
	// local address pins the interface for both send and receive.
	bind := "0.0.0.0:5353"
	if ip4 := ip.To4(); ip4 != nil {
		bind = ip4.String() + ":5353"
	}
	pc, err := lc.ListenPacket(context.Background(), "udp4", bind)
	if err != nil {
		return nil, err
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()
		return nil, fmt.Errorf("expected *net.UDPConn, got %T", pc)
	}
	// Read deadline is managed by the caller (probeName sets one temporarily).
	_ = conn.SetReadBuffer(1 << 20)
	_ = conn.SetWriteBuffer(1 << 20)
	return &ifaceConn{ifi: ifi, conn: conn, ipv4: true, ip: ip.To4()}, nil
}
