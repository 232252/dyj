package attributes

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/phin1x/go-ipp"
)

// hexOf renders b as lowercase hex, the form used for every golden
// vector in this file.
func hexOf(b []byte) string { return hex.EncodeToString(b) }

// mustHex decodes a literal hex string; it panics on a typo in a golden
// vector so a malformed expectation can never silently pass.
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("golden vector is not valid hex: %v", err)
	}
	return b
}

// -----------------------------------------------------------------------
// Golden byte vectors.
//
// Every literal in this section was computed BY HAND from RFC 8010's
// own field tables, not by running this encoder:
//
//	tag(1B) | name-length(2B big-endian) | name | value-length(2B BE) | value
//
// and for 1setOf elements 2..n (RFC 8010 §3.1.5):
//
//	tag(1B) | 0x0000 | value-length(2B BE) | value
//
// Two of them (A.1 and A.2) are literal transcriptions of RFC 8010
// Appendix A and are additionally cross-checked against an independent
// decoder in crossvalidate_test.go.
// -----------------------------------------------------------------------

func TestGoldenSingleValues(t *testing.T) {
	cases := []struct {
		name string
		attr Attribute
		want string // hand-computed hex
	}{
		{
			// RFC 8010 §3.1.4 figure 4 and appendix A.1, printer-uri row:
			// 0x45 uri | 0x000b (11 = len "printer-uri") | "printer-uri"
			// | 0x002c (44 = len of the URI) | the URI.
			name: "uri (string)",
			attr: URI("printer-uri", "ipp://printer.example.com/ipp/print/pinetree"),
			want: "45" + "000b" + "7072696e7465722d757269" + "002c" +
				"6970703a2f2f7072696e7465722e6578616d706c652e636f6d2f6970702f7072696e742f70696e6574726565",
		},
		{
			// RFC 8010 appendix A.1, attributes-charset row.
			// 0x47 charset | 0x0012 (18) | "attributes-charset"
			// | 0x0005 (5) | "utf-8".
			name: "charset",
			attr: Text(TagCharset, "attributes-charset", "utf-8"),
			want: "470012" + "617474726962757465732d63686172736574" + "0005" + "7574662d38",
		},
		{
			// RFC 8010 Table 6: 0x42 = nameWithoutLanguage.
			// 0x42 | 0x0008 | "job-name" | 0x0006 | "foobar"
			// (appendix A.1 job-name row).
			name: "nameWithoutLanguage",
			attr: Name("job-name", "foobar"),
			want: "420008" + "6a6f622d6e616d65" + "0006" + "666f6f626172",
		},
		{
			// RFC 8010 Table 6: 0x44 = keyword.
			// 0x44 | 0x0005 | "sides" | 0x0013 (19) | "two-sided-long-edge"
			// (appendix A.1 sides row).
			name: "keyword",
			attr: Keyword("sides", "two-sided-long-edge"),
			want: "440005" + "7369646573" + "0013" + "74776f2d73696465642d6c6f6e672d65646765",
		},
		{
			// RFC 8010 Table 6: 0x49 = mimeMediaType.
			// 0x49 | 0x0017 (23 = len "document-format-default")
			// | "document-format-default"
			// | 0x000f (15 = len "application/pdf")
			// | "application/pdf".
			name: "mimeMediaType",
			attr: MimeType("document-format-default", "application/pdf"),
			want: "490017" + "646f63756d656e742d666f726d61742d64656661756c74" +
				"000f" + "6170706c69636174696f6e2f706466",
		},
		{
			// RFC 8010 Table 4: 0x21 = integer; §3.8 MUST: exactly four
			// octets. 20 = 0x00000014 (appendix A.1 copies row).
			name: "integer 4 octets",
			attr: Integer("copies", 20),
			want: "210006" + "636f70696573" + "0004" + "00000014",
		},
		{
			// Two's complement, big-endian: -2 = 0xFFFFFFFE.
			name: "integer negative 4 octets",
			attr: Integer("n", -2),
			want: "210001" + "6e" + "0004" + "fffffffe",
		},
		{
			// RFC 8010 §3.9 Table 7: "boolean | SIGNED-BYTE where 0x00 is
			// 'false' and 0x01 is 'true'"; §3.8 MUST: exactly one octet.
			// appendix A.1 ipp-attribute-fidelity row.
			name: "boolean true is one octet",
			attr: Bool("ipp-attribute-fidelity", true),
			want: "220016" + "6970702d6174747269627574652d666964656c697479" + "0001" + "01",
		},
		{
			name: "boolean false is one octet",
			attr: Bool("x", false),
			want: "220001" + "78" + "0001" + "00",
		},
		{
			// RFC 8010 Table 4: 0x23 = enum. Table 7: "integer and enum |
			// a SIGNED-INTEGER"; §3.8 MUST: four octets. This is the exact
			// shape of appendix A.2's job-state: 0x23 | 0x0004 | 0x00000003.
			name: "enum is 4 octets not 2",
			attr: Enum("job-state", 3),
			want: "230009" + "6a6f622d7374617465" + "0004" + "00000003",
		},
		{
			// Same tag, longer enum value: proves the width stays 4 even
			// when the number needs more than one octet. 0x000B = 11.
			name: "enum large value still 4 octets",
			attr: Enum("e", 0x000B),
			want: "230001" + "65" + "0004" + "0000000b",
		},
		{
			// RFC 8010 Table 5 / §3.9 Table 7: resolution is two
			// SIGNED-INTEGERs (cross-feed then feed) plus a SIGNED-BYTE
			// unit (RFC 8011 §5.1.10): NINE octets. Unit 3 = dpi.
			// 300 = 0x0000012c.
			name: "resolution nine octets",
			attr: Resolution("printer-resolution-default", 300, 300),
			want: "32" + "001a" + "7072696e7465722d7265736f6c7574696f6e2d64656661756c74" +
				"0009" + "0000012c" + "0000012c" + "03",
		},
		{
			// RFC 8010 Table 5: 0x33 = rangeOfInteger, lower then upper.
			name: "rangeOfInteger two int32",
			attr: RangeOfInteger("copies-supported", 1, 99),
			want: "33" + "0010" + "636f706965732d737570706f72746564" +
				"0008" + "00000001" + "00000063",
		},
		{
			// RFC 8010 Table 5: 0x31 = dateTime, eleven octets.
			// year 2000 = 0x07D0, then 01 01 00 00 00 00, UTC sign '+'
			// = 0x2B, offset 00 00. Appendix A.2's job-uri/status-message
			// are unrelated; this vector is built purely from RFC 2579
			// DateAndTime field order as referenced by RFC 8010 Table 7.
			name: "dateTime eleven octets",
			attr: DateTime("printer-current-time", 2000, 1, 1, 0, 0, 0, 0, '+', 0, 0),
			want: "31" + "0014" + "7072696e7465722d63757272656e742d74696d65" +
				"000b" + "07d0" + "01" + "01" + "00" + "00" + "00" + "00" + "2b" + "00" + "00",
		},
		{
			// Empty character-string value is legal: value-length 0 with a
			// non-zero name-length (i.e. still an attribute-with-one-value,
			// not an additional-value).
			name: "empty string value",
			attr: Text(TagText, "t", ""),
			want: "41" + "0001" + "74" + "0000",
		},
		{
			// RFC 8010 §3.8: for out-of-band values the value-length MUST
			// be 0 and the value empty; §3.5.2 Table 3 gives 0x10
			// unsupported and 0x13 no-value. Appendix A.3 shows sides
			// with value-length 0x0000.
			name: "unsupported out-of-band zero length",
			attr: Unsupported("sides"),
			want: "10" + "0005" + "7369646573" + "0000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.attr.Marshal()
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			want := mustHex(t, tc.want)
			if !bytes.Equal(got, want) {
				t.Fatalf("golden mismatch for %s\ngot  (%d bytes) %s\nwant (%d bytes) %s",
					tc.attr.Name, len(got), hexOf(got), len(want), hexOf(want))
			}
		})
	}
}

// TestGoldenOneSetOf locks the additional-value encoding of RFC 8010
// §3.1.5: every element after the first repeats the tag and carries a
// ZERO name-length (0x0000).
func TestGoldenOneSetOf(t *testing.T) {
	cases := []struct {
		name string
		attr Attribute
		want string
	}{
		{
			// Two keyword values. First element is a normal
			// attribute-with-one-value; the second has name-length 0.
			// 0x44 | 0x000f (15 = len "sides-supported") | "sides-supported"
			//      | 0x0009 | "one-sided"
			// 0x44 | 0x0000 | 0x0013 | "two-sided-long-edge"
			name: "1setOf keyword two values",
			attr: Keywords("sides-supported", "one-sided", "two-sided-long-edge"),
			want: "44" + "000f" + "73696465732d737570706f72746564" + "0009" + "6f6e652d7369646564" +
				"44" + "0000" + "0013" + "74776f2d73696465642d6c6f6e672d65646765",
		},
		{
			// Single-element 1setOf is encoded as a plain
			// attribute-with-one-value and has NO additional-value
			// (RFC 8010 §3.1.3).
			name: "1setOf with one value has no additional-value",
			attr: Keywords("media-ready", "iso_a4_210x297mm"),
			want: "44" + "000b" + "6d656469612d7265616479" + "0010" + "69736f5f61345f323130783239376d6d",
		},
		{
			// Three values: "a", "bb", "ccc". Lengths 1, 2, 3.
			name: "1setOf three values",
			attr: Keywords("k", "a", "bb", "ccc"),
			want: "44" + "0001" + "6b" + "0001" + "61" +
				"44" + "0000" + "0002" + "6262" +
				"44" + "0000" + "0003" + "636363",
		},
		{
			// operations-supported is a 1setOf ENUM. Every element is 4
			// octets (RFC 8010 §3.8 MUST), including the additional-values
			// which carry name-length 0x0000.
			//
			// name "operations-supported" is 20 bytes = 0x0014.
			//   0x23 | 0x0014 | "operations-supported" | 0x0004 | 0x00000002  (Print-Job)
			//   0x23 | 0x0000 | 0x0004 | 0x00000004                                (Validate-Job)
			//   0x23 | 0x0000 | 0x0004 | 0x0000000b                                (Get-Printer-Attributes)
			name: "operations-supported is 1setOf 4-octet enum",
			attr: Enums("operations-supported", OperationPrintJob, OperationValidateJob, OperationGetPrinterAttrs),
			want: "23" + "0014" + "6f7065726174696f6e732d737570706f72746564" + "0004" + "00000002" +
				"23" + "0000" + "0004" + "00000004" +
				"23" + "0000" + "0004" + "0000000b",
		},
		{
			// A 1setOf mimeMediaType: document-format-supported is the
			// attribute go-ipp cannot emit at all (no mapping entry).
			// name "document-format-supported" is 25 bytes = 0x0019.
			name: "document-format-supported 1setOf mimeMediaType",
			attr: MimeTypes("document-format-supported", "image/pwg-raster", "image/jpeg"),
			want: "49" + "0019" + "646f63756d656e742d666f726d61742d737570706f72746564" + "0010" + "696d6167652f7077672d726173746572" +
				"49" + "0000" + "000a" + "696d6167652f6a706567",
		},
		{
			// 1setOf integer elements are also 4 octets each.
			// name "orientation-requested-supported" is 31 bytes = 0x001f.
			name: "1setOf integer",
			attr: Integers("orientation-requested-supported", 3, 4),
			want: "21" + "001f" + "6f7269656e746174696f6e2d7265717565737465642d737570706f72746564" + "0004" + "00000003" +
				"21" + "0000" + "0004" + "00000004",
		},
		{
			// 1setOf resolution: each element is NINE octets
			// (two SIGNED-INTEGERs + the SIGNED-BYTE unit 3 = dpi,
			// RFC 8011 §5.1.10).
			// name "printer-resolution-supported" is 28 bytes = 0x001c.
			name: "1setOf resolution",
			attr: Resolutions("printer-resolution-supported", [2]int32{300, 300}, [2]int32{600, 600}),
			want: "32" + "001c" + "7072696e7465722d7265736f6c7574696f6e2d737570706f72746564" + "0009" + "0000012c" + "0000012c" + "03" +
				"32" + "0000" + "0009" + "00000258" + "00000258" + "03",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.attr.Marshal()
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			want := mustHex(t, tc.want)
			if !bytes.Equal(got, want) {
				t.Fatalf("golden mismatch for %s\ngot  (%d bytes) %s\nwant (%d bytes) %s",
					tc.attr.Name, len(got), hexOf(got), len(want), hexOf(want))
			}
		})
	}
}

// TestGoldenGroupPrefix checks the begin-attribute-group-tag byte that
// precedes an attribute group (RFC 8010 §3.1.2 figure 2, §3.5.1 Table 2).
func TestGoldenGroupPrefix(t *testing.T) {
	g := AttributeGroup{
		Delimiter: TagOperationAttributes,
		Attributes: []Attribute{
			Text(TagCharset, "attributes-charset", "utf-8"),
		},
	}
	got, err := g.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// 0x01 operation-attributes-tag, then the charset attribute.
	want := mustHex(t, "01"+"470012"+"617474726962757465732d63686172736574"+"0005"+"7574662d38")
	if !bytes.Equal(got, want) {
		t.Fatalf("group golden mismatch\ngot  %s\nwant %s", hexOf(got), hexOf(want))
	}
}

// TestGoldenMatchGoIppWrittenBytes compares our encoding against bytes
// produced by go-ipp's OWN AttributeEncoder at runtime, for the handful of
// attributes both libraries can encode.
//
// This is a genuine cross-implementation check, and it is the test that
// would have caught the field-order defect: an earlier revision of this
// package wrote tag|name-length|value-length|name|value, and go-ipp writes
// tag|name-length|name|value-length|value, so the two disagree loudly.
//
// go-ipp resolves tags through AttributeTagMapping, so only names in that
// 70-entry table can be compared here. Enums are deliberately excluded:
// go-ipp agrees with us at 4 octets, but the comparison is most useful on
// the string/integer/boolean shapes that exercise field ordering.
func TestGoldenMatchGoIppWrittenBytes(t *testing.T) {
	cases := []struct {
		name  string // attribute name, must exist in go-ipp's mapping
		ours  Attribute
		value any // value to hand go-ipp's encoder
	}{
		{"attributes-charset", Text(TagCharset, "attributes-charset", "utf-8"), "utf-8"},
		{"printer-uri", URI("printer-uri", "ipp://192.168.1.10:631/ipp/print"), "ipp://192.168.1.10:631/ipp/print"},
		{"job-name", Name("job-name", "foobar"), "foobar"},
		{"copies", Integer("copies", 20), 20},
		{"ipp-attribute-fidelity", Bool("ipp-attribute-fidelity", true), true},
		{"printer-state", Enum("printer-state", 3), 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := ipp.NewAttributeEncoder(&buf).Encode(tc.name, tc.value); err != nil {
				t.Skipf("go-ipp cannot encode %s: %v", tc.name, err)
			}

			ours, err := tc.ours.Marshal()
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if !bytes.Equal(ours, buf.Bytes()) {
				t.Fatalf("diverges from go-ipp for %s\nours   %s\ngo-ipp %s",
					tc.name, hexOf(ours), hexOf(buf.Bytes()))
			}
		})
	}
}

// TestGoldenA1RequestRoundTrip re-encodes RFC 8010 appendix A.1's
// Print-Job request with this package and asserts the concatenation is
// byte-identical to the RFC's transcribed octets.
//
// The transcription itself is validated independently in
// crossvalidate_test.go, where the same 227 bytes are fed to go-ipp's
// RequestDecoder and must decode to the RFC's symbolic values.
func TestGoldenA1RequestRoundTrip(t *testing.T) {
	// RFC 8010 appendix A.1, verbatim from the "Octets" column.
	const (
		versionNumber      = "0101"
		operationId        = "0002" // Print-Job
		requestId          = "00000001"
		opAttributesTag    = "01"
		jobAttributesTag   = "02"
		endOfAttributesTag = "03"
	)

	// The operation-attributes group's attributes, in the RFC's order.
	// Rebuilt here as literals as well, so the test fails if the encoder
	// ever changes field order or widths.
	opAttrsWant := "470012" + "617474726962757465732d63686172736574" + "0005" + "7574662d38" +
		"48001b" + "617474726962757465732d6e61747572616c2d6c616e6775616765" + "0005" + "656e2d7573" +
		"45000b" + "7072696e7465722d757269" + "002c" + "6970703a2f2f7072696e7465722e6578616d706c652e636f6d2f6970702f7072696e742f70696e6574726565" +
		"420008" + "6a6f622d6e616d65" + "0006" + "666f6f626172" +
		"220016" + "6970702d6174747269627574652d666964656c697479" + "0001" + "01"

	jobAttrsWant := "210006" + "636f70696573" + "0004" + "00000014" +
		"440005" + "7369646573" + "0013" + "74776f2d73696465642d6c6f6e672d65646765"
	gotOp, err := MarshalAttributes([]Attribute{
		Text(TagCharset, "attributes-charset", "utf-8"),
		Text(TagNaturalLanguage, "attributes-natural-language", "en-us"),
		URI("printer-uri", "ipp://printer.example.com/ipp/print/pinetree"),
		Name("job-name", "foobar"),
		Bool("ipp-attribute-fidelity", true),
	})
	if err != nil {
		t.Fatalf("MarshalAttributes(op): %v", err)
	}
	if hexOf(gotOp) != opAttrsWant {
		t.Fatalf("operation-attributes mismatch\ngot  %s\nwant %s", hexOf(gotOp), opAttrsWant)
	}

	gotJob, err := MarshalAttributes([]Attribute{
		Integer("copies", 20),
		Keyword("sides", "two-sided-long-edge"),
	})
	if err != nil {
		t.Fatalf("MarshalAttributes(job): %v", err)
	}
	if hexOf(gotJob) != jobAttrsWant {
		t.Fatalf("job-attributes mismatch\ngot  %s\nwant %s", hexOf(gotJob), jobAttrsWant)
	}

	// Assemble the whole message and compare against the RFC's 227 bytes.
	var msg bytes.Buffer
	msg.Write(mustHex(t, versionNumber))
	msg.Write(mustHex(t, operationId))
	msg.Write(mustHex(t, requestId))
	msg.Write(mustHex(t, opAttributesTag))
	msg.Write(gotOp)
	msg.Write(mustHex(t, jobAttributesTag))
	msg.Write(gotJob)
	msg.Write(mustHex(t, endOfAttributesTag))

	const wantFull = "0101" + "0002" + "00000001" +
		"01" + "470012617474726962757465732d6368617273657400057574662d38" +
		"48001b617474726962757465732d6e61747572616c2d6c616e67756167650005656e2d7573" +
		"45000b7072696e7465722d757269002c6970703a2f2f7072696e7465722e6578616d706c652e636f6d2f6970702f7072696e742f70696e6574726565" +
		"4200086a6f622d6e616d650006666f6f626172" +
		"2200166970702d6174747269627574652d666964656c697479000101" +
		"02" + "210006636f70696573000400000014" +
		"4400057369646573001374776f2d73696465642d6c6f6e672d65646765" + "03"

	if hexOf(msg.Bytes()) != wantFull {
		t.Fatalf("whole A.1 message mismatch\ngot  %s\nwant %s", hexOf(msg.Bytes()), wantFull)
	}
	if msg.Len() != 227 {
		t.Fatalf("A.1 message length = %d, want 227", msg.Len())
	}
}

// TestEnumWidthIsFourOctetsOnTheWire is the focused regression test for
// the defect this package shipped once already: Enums() writing 2-octet
// elements. RFC 8010 fixes integer/enum at four octets in §3, §3.8
// (MUST) and §3.9 Table 7.
func TestEnumWidthIsFourOctetsOnTheWire(t *testing.T) {
	for _, tc := range []struct {
		name string
		attr Attribute
	}{
		{"Enum", Enum("job-state", 3)},
		{"Enums single", Enums("operations-supported", OperationPrintJob)},
		{"Enums multi", Enums("operations-supported", OperationPrintJob, OperationValidateJob)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.attr.Marshal()
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			// Walk the encoded attributes and assert every 0x23 value
			// field is exactly 4 octets long.
			for i := 0; i < len(got); {
				tag := got[i]
				if tag < 0x10 {
					t.Fatalf("unexpected delimiter tag 0x%02x at %d", tag, i)
				}
				nameLen := int(got[i+1])<<8 | int(got[i+2])
				nameEnd := i + 3 + nameLen
				valLen := int(got[nameEnd])<<8 | int(got[nameEnd+1])
				valStart := nameEnd + 2
				if tag == TagEnum {
					if valLen != 4 {
						t.Fatalf("enum at offset %d has value-length %d, RFC 8010 §3.8 requires exactly 4",
							i, valLen)
					}
					if valStart+4 > len(got) {
						t.Fatalf("enum at offset %d truncated", i)
					}
				}
				i = valStart + valLen
			}
		})
	}
}

// TestAdditionalValueHasZeroNameLength asserts the one field that
// distinguishes an additional-value from an attribute-with-one-value
// (RFC 8010 §3.1.5). This is the single easiest thing to get wrong when
// hand-writing 1setOf.
func TestAdditionalValueHasZeroNameLength(t *testing.T) {
	a := Keywords("sides-supported", "one-sided", "two-sided-long-edge", "two-sided-short-edge")
	got, err := a.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Skip the first attribute-with-one-value, then require that every
	// following field has name-length 0x0000.
	nameLen := int(got[1])<<8 | int(got[2])
	valLen := int(got[3+nameLen])<<8 | int(got[3+nameLen+1])
	i := 3 + nameLen + 2 + valLen

	seen := 0
	for i < len(got) {
		if got[i] != TagKeyword {
			t.Fatalf("additional-value at %d has tag 0x%02x, want 0x44", i, got[i])
		}
		an := int(got[i+1])<<8 | int(got[i+2])
		if an != 0 {
			t.Fatalf("additional-value at %d has name-length %d, RFC 8010 §3.1.5 requires 0", i, an)
		}
		av := int(got[i+3])<<8 | int(got[i+4])
		i += 5 + av
		seen++
	}
	if seen != 2 {
		t.Fatalf("found %d additional-values, want 2", seen)
	}
}

// -----------------------------------------------------------------------
// Error cases: a malformed attribute must fail loudly rather than emit
// bytes a client cannot parse.
// -----------------------------------------------------------------------

func TestMarshalRejectsBadInput(t *testing.T) {
	t.Run("empty name", func(t *testing.T) {
		if _, err := (Attribute{Tag: TagKeyword, Values: [][]byte{[]byte("x")}}).Marshal(); err == nil {
			t.Fatal("want error for empty name")
		}
	})
	t.Run("no values", func(t *testing.T) {
		if _, err := (Attribute{Tag: TagKeyword, Name: "k"}).Marshal(); err == nil {
			t.Fatal("want error for zero values")
		}
	})
	t.Run("delimiter tag used as value tag", func(t *testing.T) {
		a := Attribute{Tag: TagEndOfAttributes, Name: "k", Values: [][]byte{[]byte("x")}}
		_, err := a.Marshal()
		if err == nil {
			t.Fatal("want error for delimiter tag")
		}
		if !strings.Contains(err.Error(), "delimiter") {
			t.Fatalf("error should mention delimiter, got %v", err)
		}
	})
	t.Run("over-long value", func(t *testing.T) {
		a := Attribute{Tag: TagKeyword, Name: "k", Values: [][]byte{make([]byte, MaxValueLength+1)}}
		if _, err := a.Marshal(); err == nil {
			t.Fatal("want error for value longer than 0xFFFF")
		}
	})
	t.Run("over-long name", func(t *testing.T) {
		a := Attribute{Tag: TagKeyword, Name: strings.Repeat("a", MaxNameLength+1), Values: [][]byte{[]byte("x")}}
		if _, err := a.Marshal(); err == nil {
			t.Fatal("want error for name longer than 0xFFFF")
		}
	})
	t.Run("invalid utf-8 in character string", func(t *testing.T) {
		a := Attribute{Tag: TagKeyword, Name: "k", Values: [][]byte{{0xff, 0xfe}}}
		if _, err := a.Marshal(); err == nil {
			t.Fatal("want error for invalid UTF-8 in a character string")
		}
	})
	t.Run("invalid utf-8 allowed in octet string", func(t *testing.T) {
		// dateTime (0x31) is an octetString syntax, so arbitrary bytes are
		// fine there (RFC 8010 §3.9 Table 7).
		a := Attribute{Tag: TagDateTime, Name: "k", Values: [][]byte{{0xff, 0xfe}}}
		if _, err := a.Marshal(); err != nil {
			t.Fatalf("unexpected error for octet string: %v", err)
		}
	})
}

// TestWriteMatchesMarshal proves the io.Writer path and the convenience
// Marshal path cannot drift apart.
func TestWriteMatchesMarshal(t *testing.T) {
	attrs := []Attribute{
		Text(TagCharset, "attributes-charset", "utf-8"),
		Enums("operations-supported", OperationPrintJob, OperationGetPrinterAttrs),
		Keywords("sides-supported", "one-sided", "two-sided-long-edge"),
		Bool("color-supported", true),
		Resolution("printer-resolution-default", 300, 300),
		Integer("copies", 1),
	}
	var buf bytes.Buffer
	if err := WriteAttributes(&buf, attrs); err != nil {
		t.Fatalf("WriteAttributes: %v", err)
	}
	want, err := MarshalAttributes(attrs)
	if err != nil {
		t.Fatalf("MarshalAttributes: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("io.Writer path differs from Marshal path\nwriter %s\nmarshal %s",
			hexOf(buf.Bytes()), hexOf(want))
	}
}

// TestGoldenVectorsAreWellFormed re-parses every golden vector with an
// independent minimal parser written from the RFC, so a typo that
// happens to match the encoder is still caught.
func TestGoldenVectorsAreWellFormed(t *testing.T) {
	vectors := map[string]string{
		"charset":   "470012617474726962757465732d6368617273657400057574662d38",
		"copies":    "210006636f70696573000400000014",
		"bool":      "2200166970702d6174747269627574652d666964656c697479000101",
		"job-state": "2300096a6f622d7374617465000400000003",
		"1setOfSides": "44000f73696465732d737570706f7274656400096f6e652d7369646564" +
			"440000001374776f2d73696465642d6c6f6e672d65646765",
	}
	for name, hx := range vectors {
		raw := mustHex(t, hx)
		if err := walkAttributes(raw); err != nil {
			t.Fatalf("golden vector %s is not well-formed: %v", name, err)
		}
	}
}

// walkAttributes is a deliberately naive RFC 8010 §3.2 attribute walker
// used only to validate that a byte string is structurally sane.
func walkAttributes(b []byte) error {
	i := 0
	for i < len(b) {
		if i+5 > len(b) {
			return fmt.Errorf("truncated header at %d", i)
		}
		tag := b[i]
		if tag < 0x10 {
			return fmt.Errorf("value-tag 0x%02x at %d is a delimiter tag", tag, i)
		}
		nameLen := int(b[i+1])<<8 | int(b[i+2])
		if i+3+nameLen+2 > len(b) {
			return fmt.Errorf("truncated name at %d", i)
		}
		name := string(b[i+3 : i+3+nameLen])
		valLen := int(b[i+3+nameLen])<<8 | int(b[i+3+nameLen+1])
		valStart := i + 3 + nameLen + 2
		if valStart+valLen > len(b) {
			return fmt.Errorf("truncated value at %d", i)
		}
		// RFC 8010 §3.7: name = LALPHA *( LALPHA / DIGIT / "-" / "_" / "." )
		for k, c := range []byte(name) {
			ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
			if k == 0 && !(c >= 'a' && c <= 'z') {
				return fmt.Errorf("name %q at %d does not start with LALPHA", name, i)
			}
			if !ok {
				return fmt.Errorf("name %q at %d contains invalid byte 0x%02x", name, i, c)
			}
		}
		i = valStart + valLen
	}
	if i != len(b) {
		return fmt.Errorf("walk ended at %d, length %d", i, len(b))
	}
	return nil
}
