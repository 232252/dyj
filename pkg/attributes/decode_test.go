package attributes

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// rfcA1Request is the RFC 8010 Appendix A.1 Print-Job request, byte for
// byte as published by the RFC and independently re-derived during the
// adversarial verification round (docs/research/09, §3.2). It exercises
// both syntaxes the decoder must survive: character-string attributes,
// an integer, a boolean, a job-attributes group and end-of-attributes.
const rfcA1Request = "010100020000000101470012617474726962757465732d6368617273657400057574662d38" +
	"48001b617474726962757465732d6e61747572616c2d6c616e67756167650005656e2d7573" +
	"45000b7072696e7465722d757269002c6970703a2f2f7072696e7465722e6578616d706c652e" +
	"636f6d2f6970702f7072696e742f70696e6574726565" +
	"4200086a6f622d6e616d650006666f6f626172" +
	"2200166970702d6174747269627574652d666964656c697479000101" +
	"02" +
	"210006636f70696573000400000014" +
	"4400057369646573001374776f2d73696465642d6c6f6e672d65646765" +
	"03"

func hexFixture(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex fixture: %v", err)
	}
	return b
}

// TestDecodeRFC_A1 decodes the RFC's own sample request and asserts every
// field comes back intact.
func TestDecodeRFC_A1(t *testing.T) {
	msg, err := DecodeMessage(hexFixture(t, rfcA1Request))
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if msg.VersionMajor != 1 || msg.VersionMinor != 1 {
		t.Errorf("version = %d.%d, want 1.1", msg.VersionMajor, msg.VersionMinor)
	}
	if msg.Code != 0x0002 {
		t.Errorf("operation = 0x%04X, want 0x0002 (Print-Job)", msg.Code)
	}
	if msg.RequestID != 1 {
		t.Errorf("request-id = %d, want 1", msg.RequestID)
	}
	if msg.Data != nil {
		t.Errorf("data = %v, want nil (A.1 has no document)", msg.Data)
	}

	op := msg.Operation()
	if op == nil {
		t.Fatal("no operation-attributes group")
	}
	if got := GroupFirstString(op, "attributes-charset"); got != "utf-8" {
		t.Errorf("attributes-charset = %q", got)
	}
	if got := GroupFirstString(op, "printer-uri"); got != "ipp://printer.example.com/ipp/print/pinetree" {
		t.Errorf("printer-uri = %q", got)
	}
	if ok, _ := GroupBool(op, "ipp-attribute-fidelity"); !ok {
		t.Errorf("ipp-attribute-fidelity did not decode as true")
	}

	job := msg.Job()
	if job == nil {
		t.Fatal("no job-attributes group")
	}
	copies, ok := GroupInt(job, "copies")
	if !ok || copies != 20 {
		t.Errorf("copies = %d (ok=%v), want 20", copies, ok)
	}
	if got := GroupFirstString(job, "sides"); got != "two-sided-long-edge" {
		t.Errorf("sides = %q", got)
	}
}

// TestDecodeKeepsEveryValue is the regression test for the go-ipp decoder
// limitation documented at the top of decode.go: a 1setOf keyword must come
// back with ALL its values, not only the last one.
func TestDecodeKeepsEveryValue(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{2, 0, 0x00, 0x0B})
	b.Write([]byte{0, 0, 0, 1})
	b.WriteByte(TagOperationAttributes)
	writeKV(&b, TagCharset, "attributes-charset", "utf-8")
	writeKV(&b, TagNaturalLang, "attributes-natural-language", "en-us")
	writeKV(&b, TagKeyword, "requested-attributes", "printer-name")
	writeKV(&b, TagKeyword, "", "printer-state")
	writeKV(&b, TagKeyword, "", "media-default")
	b.WriteByte(TagEndOfAttributes)

	msg, err := DecodeMessage(b.Bytes())
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	got := GroupStrings(msg.Operation(), "requested-attributes")
	want := []string{"printer-name", "printer-state", "media-default"}
	if len(got) != len(want) {
		t.Fatalf("requested-attributes = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("requested-attributes[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestDecodeRoundTrip re-encodes a decoded message and asserts the bytes
// are identical — the decoder must not lose or reorder anything.
func TestDecodeRoundTrip(t *testing.T) {
	raw := hexFixture(t, rfcA1Request)
	msg, err := DecodeMessage(raw)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}

	var out bytes.Buffer
	out.Write([]byte{msg.VersionMajor, msg.VersionMinor,
		byte(msg.Code >> 8), byte(msg.Code),
		byte(uint32(msg.RequestID) >> 24), byte(uint32(msg.RequestID) >> 16),
		byte(uint32(msg.RequestID) >> 8), byte(uint32(msg.RequestID))})
	for _, g := range msg.Groups {
		if err := g.WriteGroup(&out); err != nil {
			t.Fatalf("re-encode group: %v", err)
		}
	}
	out.WriteByte(TagEndOfAttributes)

	if !bytes.Equal(out.Bytes(), raw) {
		t.Fatalf("round trip differs\ngot  %x\nwant %x", out.Bytes(), raw)
	}
}

// TestDecodeWithData covers Print-Job framing: bytes after the
// end-of-attributes tag are the document.
func TestDecodeWithData(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{2, 0, 0x00, 0x02})
	b.Write([]byte{0, 0, 0, 7})
	b.WriteByte(TagOperationAttributes)
	writeKV(&b, TagCharset, "attributes-charset", "utf-8")
	b.WriteByte(TagEndOfAttributes)
	b.WriteString("PWG-DOCUMENT-BYTES")

	msg, err := DecodeMessage(b.Bytes())
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if string(msg.Data) != "PWG-DOCUMENT-BYTES" {
		t.Errorf("data = %q", msg.Data)
	}
}

func TestDecodeErrors(t *testing.T) {
	cases := []struct {
		name    string
		build   func() []byte
		wantSub string
	}{
		{
			name: "too short",
			build: func() []byte {
				return []byte{2, 0, 0, 2, 0, 0, 0, 1}
			},
			wantSub: "ends without an end-of-attributes tag",
		},
		{
			name: "no end tag",
			build: func() []byte {
				return []byte{2, 0, 0, 2, 0, 0, 0, 1, TagOperationAttributes}
			},
			wantSub: "ends without an end-of-attributes tag",
		},
		{
			name: "charset not first",
			build: func() []byte {
				var b bytes.Buffer
				b.Write([]byte{2, 0, 0, 2, 0, 0, 0, 1})
				b.WriteByte(TagOperationAttributes)
				writeKV(&b, TagURI, "printer-uri", "ipp://x/")
				b.WriteByte(TagEndOfAttributes)
				return b.Bytes()
			},
			wantSub: "must be attributes-charset",
		},
		{
			name: "value overrun",
			build: func() []byte {
				var b bytes.Buffer
				b.Write([]byte{2, 0, 0, 2, 0, 0, 0, 1})
				b.WriteByte(TagOperationAttributes)
				writeKV(&b, TagCharset, "attributes-charset", "utf-8")
				// Lie about the value length.
				b.WriteByte(TagKeyword)
				b.Write([]byte{0, 4, 'n', 'a', 'm', 'e', 0xFF, 0xFF})
				return b.Bytes()
			},
			wantSub: "truncated value",
		},
		{
			name: "unknown delimiter",
			build: func() []byte {
				var b bytes.Buffer
				b.Write([]byte{2, 0, 0, 2, 0, 0, 0, 1})
				b.WriteByte(0x09)
				return b.Bytes()
			},
			wantSub: "unknown delimiter tag",
		},
		{
			name: "value tag before group",
			build: func() []byte {
				var b bytes.Buffer
				b.Write([]byte{2, 0, 0, 2, 0, 0, 0, 1})
				writeKV(&b, TagCharset, "attributes-charset", "utf-8")
				b.WriteByte(TagEndOfAttributes)
				return b.Bytes()
			},
			wantSub: "before any attribute group",
		},
		{
			name: "additional-value tag mismatch",
			build: func() []byte {
				var b bytes.Buffer
				b.Write([]byte{2, 0, 0, 2, 0, 0, 0, 1})
				b.WriteByte(TagOperationAttributes)
				writeKV(&b, TagCharset, "attributes-charset", "utf-8")
				writeKV(&b, TagKeyword, "k", "a")
				writeKV(&b, TagKeyword, "", "b")
				// Now a mismatched additional value on the same name.
				writeKV(&b, TagURI, "", "c")
				b.WriteByte(TagEndOfAttributes)
				return b.Bytes()
			},
			wantSub: "differs from",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeMessage(tc.build())
			if err == nil {
				t.Fatalf("expected an error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func writeKV(b *bytes.Buffer, tag byte, name, value string) {
	b.WriteByte(tag)
	b.WriteByte(byte(len(name) >> 8))
	b.WriteByte(byte(len(name)))
	b.WriteString(name)
	b.WriteByte(byte(len(value) >> 8))
	b.WriteByte(byte(len(value)))
	b.WriteString(value)
}
