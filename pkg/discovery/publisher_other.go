//go:build !linux

package discovery

// NewPublisher returns dyj's own mDNS advertiser on non-Linux platforms.
// avahi is a Linux daemon; Windows/macOS have no equivalent port-5353
// owner that would starve dyj's responder, so the raw-socket path is used
// directly. The bool reports whether avahi/D-Bus is in use (always false
// here).
func NewPublisher(cfg Config) (Publisher, bool, error) {
	a, err := New(cfg)
	if err != nil {
		return nil, false, err
	}
	return a, false, nil
}