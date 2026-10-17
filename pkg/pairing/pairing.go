// Package pairing implements the 4-digit PIN handshake of the v0.3 data
// plane (ADR-0010 D3): a rolling PIN a human can read aloud exchanges for a
// long-lived device token that then rides inside the IPP URI path.
//
// Threat model (research/13 §8-Q4): stop casual LAN freeloading, not a
// determined attacker. A 4-digit PIN has 10⁴ combinations, so the store
// rate-limits PIN attempts per source IP (sliding window of failures +
// lockout) before the PIN is even compared. The PIN itself lives in memory
// only; only SHA-256 hashes of issued tokens are persisted.
package pairing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store owns the PIN, the per-IP attempt limiter and the paired-device
// registry. All methods are safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	path string // paired-devices.json; "" = memory only (tests)

	pin       string
	pinExpiry time.Time
	ttl       time.Duration

	failWindow   time.Duration
	maxFails     int
	lockDuration time.Duration

	fails    map[string][]time.Time // source ip → recent failure times
	lockedAt map[string]time.Time   // source ip → lock start

	devices   []Device
	byToken   map[string]int // sha256(token) hex → index into devices
	dirtySeen bool           // LastSeen changed since last persist

	events   Eventer
	now      func() time.Time
	randRead func(b []byte) (int, error)
}

// Eventer is the subset of *logx.Logger the store emits through.
type Eventer interface {
	Info(evt string, kv ...any)
	Warn(evt string, kv ...any)
}

// Device is one paired receiver.
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenHash string    `json:"token_hash"` // sha256 hex
	AddedAt   time.Time `json:"added_at"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
	SourceIP  string    `json:"source_ip,omitempty"`
}

// Limits of the abuse countermeasures; fields left zero take the defaults.
type Limits struct {
	PINTTL       time.Duration // rolling PIN lifetime (default 10m)
	FailWindow   time.Duration // failure window per IP (default 10m)
	MaxFails     int           // failures inside the window before lockout (default 10)
	LockDuration time.Duration // lockout length (default 30m)
}

const (
	defaultPINTTL       = 10 * time.Minute
	defaultFailWindow   = 10 * time.Minute
	defaultMaxFails     = 10
	defaultLockDuration = 30 * time.Minute
)

// Open loads (or creates) the device registry at path. path may be "" for a
// memory-only store. The PIN is generated immediately.
func Open(path string, lim Limits, events Eventer) (*Store, error) {
	if events == nil {
		events = nopEvents{}
	}
	s := &Store{
		path:     path,
		ttl:      orDefault(lim.PINTTL, defaultPINTTL),
		fails:    map[string][]time.Time{},
		lockedAt: map[string]time.Time{},
		byToken:  map[string]int{},
		events:   events,
		now:      time.Now,
		randRead: rand.Read,
	}
	s.failWindow = orDefault(lim.FailWindow, defaultFailWindow)
	s.maxFails = lim.MaxFails
	if s.maxFails <= 0 {
		s.maxFails = defaultMaxFails
	}
	s.lockDuration = orDefault(lim.LockDuration, defaultLockDuration)
	if s.path != "" {
		if err := s.load(); err != nil {
			return nil, err
		}
	}
	s.rotateLocked("start")
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("pairing: reading %s: %w", s.path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &s.devices); err != nil {
		return fmt.Errorf("pairing: %s is corrupt: %w", s.path, err)
	}
	for i := range s.devices {
		s.byToken[s.devices[i].TokenHash] = i
	}
	return nil
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.devices, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ---------------------------------------------------------------------------
// PIN
// ---------------------------------------------------------------------------

// Current returns the active PIN and its remaining seconds. An expired PIN
// is rotated transparently.
func (s *Store) Current() (pin string, remainSec int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rotateIfDueLocked()
	// Remain follows the same clock as rotation. time.Until would ignore
	// an injected clock and, once the wall clock passes a test fixture,
	// report a negative TTL so the PIN looks expired when it is not.
	remain := s.pinExpiry.Sub(s.now())
	if remain < 0 {
		remain = 0
	}
	return s.pin, int(remain.Seconds() + 0.999)
}

// Rotate invalidates the current PIN immediately.
func (s *Store) Rotate() (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rotateLocked("manual")
	return s.pin, int(s.ttl.Seconds())
}

func (s *Store) rotateIfDueLocked() {
	if s.now().After(s.pinExpiry) {
		s.rotateLocked("expired")
	}
}

func (s *Store) rotateLocked(reason string) {
	s.pin = randomPin(s.randRead)
	s.pinExpiry = s.now().Add(s.ttl)
	s.events.Info("pair.pin.rotate", "reason", reason, "ttl_sec", int(s.ttl.Seconds()))
}

// ---------------------------------------------------------------------------
// Pairing
// ---------------------------------------------------------------------------

// Errors surfaced to the HTTP layer.
var (
	ErrBadPin = errors.New("pairing: wrong pin")
	ErrLocked = errors.New("pairing: too many attempts, locked")
	ErrNoPin  = errors.New("pairing: pin required")
)

// LockedError wraps ErrLocked with the remaining wait; handlers surface it
// as retry_after_sec.
type LockedError struct{ RetryAfter time.Duration }

func (e *LockedError) Error() string {
	return ErrLocked.Error() + " (" + e.RetryAfter.Round(time.Second).String() + ")"
}

func (e *LockedError) Unwrap() error { return ErrLocked }

// Pair verifies the PIN (under the per-IP limiter) and issues a device
// token. Re-pairing an existing device id rotates its token. The caller
// never logs the returned token.
func (s *Store) Pair(pin, deviceID, name, srcIP string) (token string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pin == "" {
		return "", ErrNoPin
	}
	if wait, locked := s.lockRemainingLocked(srcIP); locked {
		s.events.Warn("pair.attempt", "src", srcIP, "locked", true)
		return "", &LockedError{RetryAfter: wait}
	}
	s.rotateIfDueLocked()
	if !constantTimeEqual(pin, s.pin) {
		remaining := s.recordFailLocked(srcIP)
		s.events.Warn("pair.attempt", "src", srcIP, "ok", false, "remaining", remaining)
		if remaining == 0 {
			s.events.Warn("pair.lock", "src", srcIP, "dur", s.lockDuration.String())
		}
		return "", ErrBadPin
	}
	delete(s.fails, srcIP)
	token = randomToken(s.randRead)
	hash := hashToken(token)
	dev := Device{
		ID:        deviceID,
		Name:      name,
		TokenHash: hash,
		AddedAt:   s.now(),
		LastSeen:  s.now(),
		SourceIP:  srcIP,
	}
	replaced := false
	for i := range s.devices {
		if s.devices[i].ID == deviceID {
			s.devices[i] = dev
			replaced = true
			break
		}
	}
	if !replaced {
		s.devices = append(s.devices, dev)
	}
	s.reindexLocked()
	if err := s.persistLocked(); err != nil {
		s.events.Warn("pair.ok", "device", devicePrefix(deviceID), "persist_err", err)
	}
	s.events.Info("pair.ok", "src", srcIP, "device", devicePrefix(deviceID), "name", name, "rotated", replaced)
	return token, nil
}

// Verify maps a presented token to its device id; ok is false for unknown,
// revoked or rotated-away tokens. Touch updates LastSeen (memory only;
// persisted on Close to keep the print path write-free).
func (s *Store) Verify(token string) (deviceID string, ok bool) {
	if token == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.byToken[hashToken(token)]
	if !ok {
		return "", false
	}
	s.devices[i].LastSeen = s.now()
	s.dirtySeen = true
	return s.devices[i].ID, true
}

// Revoke removes a device; its token stops working immediately.
func (s *Store) Revoke(deviceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices {
		if s.devices[i].ID == deviceID {
			s.devices = append(s.devices[:i], s.devices[i+1:]...)
			s.reindexLocked()
			_ = s.persistLocked()
			s.events.Info("pair.revoke", "device", devicePrefix(deviceID))
			return true
		}
	}
	return false
}

// List returns a copy of the paired devices, oldest first.
func (s *Store) List() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Device, len(s.devices))
	copy(out, s.devices)
	sort.SliceStable(out, func(i, j int) bool { return out[i].AddedAt.Before(out[j].AddedAt) })
	return out
}

// Close persists any LastSeen updates.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirtySeen {
		_ = s.persistLocked()
		s.dirtySeen = false
	}
}

func (s *Store) reindexLocked() {
	s.byToken = make(map[string]int, len(s.devices))
	for i := range s.devices {
		s.byToken[s.devices[i].TokenHash] = i
	}
}

// ---------------------------------------------------------------------------
// Limiter
// ---------------------------------------------------------------------------

// lockRemainingLocked reports how long the IP must still wait, if locked.
func (s *Store) lockRemainingLocked(ip string) (time.Duration, bool) {
	until, ok := s.lockedAt[ip]
	if !ok {
		return 0, false
	}
	remain := until.Sub(s.now())
	if remain <= 0 {
		delete(s.lockedAt, ip)
		s.events.Info("pair.unlock", "src", ip)
		return 0, false
	}
	return remain, true
}

// recordFailLocked stores one failure and returns the attempts left before
// lockout (0 = just locked).
func (s *Store) recordFailLocked(ip string) int {
	now := s.now()
	recent := s.fails[ip][:0]
	for _, t := range s.fails[ip] {
		if now.Sub(t) <= s.failWindow {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	s.fails[ip] = recent
	if len(recent) >= s.maxFails {
		s.lockedAt[ip] = now.Add(s.lockDuration)
		delete(s.fails, ip)
		return 0
	}
	return s.maxFails - len(recent)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func randomPin(read func([]byte) (int, error)) string {
	b := make([]byte, 2)
	if _, err := read(b); err != nil {
		// Never guessed in practice; fail toward a nonzero pin.
		return "0000"
	}
	n := int(b[0])<<8 | int(b[1])
	return fmt.Sprintf("%04d", n%10000)
}

func randomToken(read func([]byte) (int, error)) string {
	b := make([]byte, 16)
	if _, err := read(b); err != nil {
		b[0] = byte(time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// constantTimeEqual keeps per-attempt timing from leaking the PIN one char
// at a time; both sides are always 4 chars here.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func devicePrefix(id string) string {
	if len(id) > 4 {
		return id[:4]
	}
	return id
}

func orDefault(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

type nopEvents struct{}

func (nopEvents) Info(string, ...any) {}
func (nopEvents) Warn(string, ...any) {}
