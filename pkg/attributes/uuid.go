package attributes

import (
	"crypto/sha256"
	"encoding/hex"
)

// uuidNamespaceURL is the RFC 9562 namespace id for URLs (inherited from
// RFC 4122), used by DeriveUUID so the same name always yields the same
// UUID across platforms and restarts.
var uuidNamespaceURL = [16]byte{
	0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1,
	0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8,
}

// DeriveUUID computes a stable name-derived UUID string for name: a
// SHA-256 digest of the URL namespace plus name, truncated and tagged as
// an RFC 9562 version-8 (custom) UUID. Used as the fallback printer-uuid
// when no persisted identity exists: the value changes only when the name
// does, which is the semantic clients expect from printer-uuid. Clients
// treat printer-uuid as an opaque string, so the custom version is safe;
// the digest guards nothing (identifier derivation, not cryptography).
func DeriveUUID(name string) string {
	h := sha256.New()
	h.Write(uuidNamespaceURL[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)[:16]

	sum[6] = (sum[6] & 0x0f) | 0x80 // version 8 (custom)
	sum[8] = (sum[8] & 0x3f) | 0x80 // variant 10xx

	buf := make([]byte, 36)
	hex.Encode(buf[0:8], sum[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], sum[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], sum[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], sum[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], sum[10:16])
	return string(buf)
}
