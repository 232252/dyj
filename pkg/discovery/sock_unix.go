//go:build !windows

package discovery

import (
	"context"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// listenMulticast is the POSIX counterpart of sock_windows.go. It additionally
// sets SO_REUSEPORT, which is what lets Avahi (Linux) / mDNSResponder (macOS)
// and this responder share port 5353.
func listenMulticast(ifi *net.Interface, ip net.IP) (*ifaceConn, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
					serr = fmt.Errorf("SO_REUSEADDR: %w", err)
					return
				}
				if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
					serr = fmt.Errorf("SO_REUSEPORT: %w", err)
					return
				}
				if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_MULTICAST_TTL, 255); err != nil {
					serr = fmt.Errorf("IP_MULTICAST_TTL: %w", err)
					return
				}
				if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_MULTICAST_LOOP, 1); err != nil {
					serr = fmt.Errorf("IP_MULTICAST_LOOP: %w", err)
					return
				}
				// Pin multicast egress to this interface (imr_multiaddr
				// INADDR_ANY + imr_interface = local address): without it
				// every socket follows the multicast route instead of its
				// own NIC, so multi-homed hosts announce on the wrong wire.
				if ip4 := ip.To4(); ip4 != nil {
					mif := &unix.IPMreq{Interface: [4]byte{ip4[0], ip4[1], ip4[2], ip4[3]}}
					if err := unix.SetsockoptIPMreq(int(fd), unix.IPPROTO_IP, unix.IP_MULTICAST_IF, mif); err != nil {
						serr = fmt.Errorf("IP_MULTICAST_IF(%s): %w", ip4, err)
						return
					}
				}
				if ip4 := ip.To4(); ip4 != nil {
					mreq := &unix.IPMreq{Multiaddr: [4]byte{224, 0, 0, 251}}
					copy(mreq.Interface[:], ip4)
					if err := unix.SetsockoptIPMreq(int(fd), unix.IPPROTO_IP, unix.IP_ADD_MEMBERSHIP, mreq); err != nil {
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
	// Bind to the interface address when available: a wildcard bind
	// leaves multicast egress to the routing table, so on a multi-homed
	// host announcements can leave through the wrong NIC. A concrete
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
	_ = conn.SetReadBuffer(1 << 20)
	_ = conn.SetWriteBuffer(1 << 20)
	return &ifaceConn{ifi: ifi, conn: conn, ipv4: true, ip: ip.To4()}, nil
}
