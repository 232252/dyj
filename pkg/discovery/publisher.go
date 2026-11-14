package discovery

// Publisher advertises one DNS-SD service. Two implementations exist:
//
//   - *Advertiser: dyj's own mDNS responder (raw sockets, per-interface
//     ${IFACE} naming). Used on Windows/macOS and on Linux without avahi.
//   - *avahiPublisher (Linux only): publishes through the system avahi
//     daemon over D-Bus. Required when avahi owns port 5353 — the Linux
//     SO_REUSEPORT multicast semantics deliver incoming queries to only
//     one socket, so dyj's own responder never sees them (research/16 §3).
//
// NewPublisher picks the implementation; node.go only talks to this
// interface so the open-printing TXT sync (SetTXT) works on both paths.
type Publisher interface {
	// Start begins advertising. Safe to call once.
	Start() error
	// Close stops advertising and releases resources (goodbye/Free).
	Close() error
	// Instance reports the instance name actually in use.
	Instance() string
	// SetTXT replaces the TXT record content and re-announces.
	SetTXT(txt []string)
	// TXT returns a copy of the current TXT content.
	TXT() []string
}