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
	pc, err := lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:5353")
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
