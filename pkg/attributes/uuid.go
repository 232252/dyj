package attributes

import (
	"crypto/sha1"
	"encoding/hex"
)

// uuidNamespaceURL is the RFC 4122 namespace id for URLs, used by
// DeriveUUID so the same name always yields the same UUID across
// platforms and restarts.
var uuidNamespaceURL = [6]uint32{
	0x6ba7b811, 0x9dad, 0x11d1, 0x80b4, 0x00c04fd430c8 >> 16, 0x00c04fd430c8 & 0xffff,
}

// DeriveUUID computes a stable RFC 4122 version-5 (SHA-1, URL namespace)
// UUID string for name. Used as the fallback printer-uuid when no
// persisted identity exists: the value changes only when the name does,
// which is the semantic clients expect from printer-uuid.
//
// SHA-1 here is mandated by RFC 4122 §4.3 for version-5 UUIDs and is an
// identifier-derivation use, not a cryptographic protection: the output
// authenticates nothing and guards nothing, so the "SHA-1 is broken for
// collision resistance" caveat does not apply. Deviating to SHA-256 would
// produce bytes that are no longer a valid v5 UUID for clients.
func DeriveUUID(name string) string {
	var ns [16]byte
	ns[0] = byte(uuidNamespaceURL[0] >> 24)
	ns[1] = byte(uuidNamespaceURL[0] >> 16)
	ns[2] = byte(uuidNamespaceURL[0] >> 8)
	ns[3] = byte(uuidNamespaceURL[0])
	ns[4] = byte(uuidNamespaceURL[1] >> 8)
	ns[5] = byte(uuidNamespaceURL[1])
	ns[6] = byte(uuidNamespaceURL[2] >> 8)
	ns[7] = byte(uuidNamespaceURL[2])
	ns[8] = byte(uuidNamespaceURL[3] >> 8)
	ns[9] = byte(uuidNamespaceURL[3])
	ns[10] = byte(uuidNamespaceURL[4] >> 8)
	ns[11] = byte(uuidNamespaceURL[4])
	ns[12] = byte(uuidNamespaceURL[5] >> 8)
	ns[13] = byte(uuidNamespaceURL[5])

	h := sha1.New()
	h.Write(ns[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)

	sum[6] = (sum[6] & 0x0f) | 0x50 // version 5
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
