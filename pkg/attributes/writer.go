// Package attributes implements tag-explicit serialization of IPP attributes
// per RFC 8010 and the IPP Everywhere required-attribute table.
//
// Why this package exists (empirically established, see
// docs/research/07-go-ipp-server-capability.md appendix E):
//
//	phin1x/go-ipp v1.7.0 resolves an attribute's wire tag through the
//	AttributeTagMapping table, which holds only 70 attribute names and is
//	missing almost every IPP Everywhere / AirPrint "-supported" /
//	"-default" attribute. AttributeEncoder.Encode returns
//	"cannot get tag of attribute <name>" for anything unmapped, and there
//	is no WithTag-style escape hatch. It also cross-checks value type
//	against the mapped tag, so an untyped []any is rejected as well.
//
// Therefore this package encodes with an EXPLICIT tag supplied by the
// caller and never consults any mapping table. go-ipp remains in use for
// request decoding only.
//
// Wire format (RFC 8010 §3.1.3, §3.1.4, §3.1.5; ABNF in §3.2 Figure 9):
//
//	attribute                = attribute-with-one-value *additional-value
//	attribute-with-one-value = value-tag name-length name
//	                           value-length value
//	additional-value         = value-tag zero-name-length
//	                           value-length value
//
//	value-tag    = 1 octet
//	name-length  = SIGNED-SHORT  = 2 octets, big-endian
//	value-length = SIGNED-SHORT  = 2 octets, big-endian
//	zero-name-length = %x00.00   (this is what marks an additional-value)
//
// Every integer in the encoding MUST be two's-complement big-endian
// (RFC 8010 §3); the number of octets is 1, 2 or 4 depending on usage.
package attributes

import (
	"encoding/binary"
	"fmt"
	"io"
	"unicode/utf8"
)

// Value tags defined by RFC 8010 §3.5.2. Only the ones this project can
// emit are named; additional syntaxes come from extended RFCs (see the
// PWG tags below) and the IANA IPP registry.
//
// Table 3 (out-of-band, RFC 8010 §3.5.2):
//
//	0x10 unsupported  0x12 unknown  0x13 no-value
//
// Table 4 (integer, RFC 8010 §3.5.2):
//
//	0x21 integer  0x22 boolean  0x23 enum
//
// Table 5 (octetString, RFC 8010 §3.5.2):
//
//	0x31 dateTime  0x32 resolution  0x33 rangeOfInteger
//
// Table 6 (character-string, RFC 8010 §3.5.2):
//
//	0x41 textWithoutLanguage  0x42 nameWithoutLanguage  0x44 keyword
//	0x45 uri  0x46 uriScheme  0x47 charset  0x48 naturalLanguage
//	0x49 mimeMediaType  0x4a memberAttrName
const (
	TagUnsupported   byte = 0x10 // RFC 8010 Table 3
	TagUnknown       byte = 0x12 // RFC 8010 Table 3
	TagNoValue       byte = 0x13 // RFC 8010 Table 3
	TagInteger       byte = 0x21 // RFC 8010 Table 4
	TagBoolean       byte = 0x22 // RFC 8010 Table 4
	TagEnum          byte = 0x23 // RFC 8010 Table 4
	TagDateTime      byte = 0x31 // RFC 8010 Table 5
	TagResolution    byte = 0x32 // RFC 8010 Table 5
	TagRangeOfInt    byte = 0x33 // RFC 8010 Table 5
	TagText          byte = 0x41 // RFC 8010 Table 6
	TagName          byte = 0x42 // RFC 8010 Table 6
	TagKeyword       byte = 0x44 // RFC 8010 Table 6
	TagURI           byte = 0x45 // RFC 8010 Table 6
	TagURIScheme     byte = 0x46 // RFC 8010 Table 6
	TagCharset       byte = 0x47 // RFC 8010 Table 6
	TagNaturalLang   byte = 0x48 // RFC 8010 Table 6
	TagMimeMediaType byte = 0x49 // RFC 8010 Table 6
	TagMemberAttr    byte = 0x4a // RFC 8010 Table 6

	// TagNaturalLanguage is the naturalLanguage tag (RFC 8010 Table 6,
	// 0x48). It is the same wire value as TagNaturalLang; both names are
	// exported only because "natural-language" and "naturalLanguage"
	// both appear in the specs.
	TagNaturalLanguage byte = 0x48
	// TagTextLang is textWithLanguage (RFC 8010 §3.5.2 Table 5, 0x35):
	// an OCTET-STRING of two length-prefixed fields, see §3.9 Table 7.
	TagTextLang byte = 0x35
)

// Delimiter tags, RFC 8010 §3.5.1 Table 2.
const (
	TagOperationAttributes   byte = 0x01 // RFC 8010 §3.5.1
	TagJobAttributes         byte = 0x02 // RFC 8010 §3.5.1
	TagEndOfAttributes       byte = 0x03 // RFC 8010 §3.5.1
	TagPrinterAttributes     byte = 0x04 // RFC 8010 §3.5.1
	TagUnsupportedAttributes byte = 0x05 // RFC 8010 §3.5.1
)

// MaxValueLength is the largest value a SIGNED-SHORT value-length can
// describe: 0xFFFF (RFC 8010 §3.8). Values longer than this cannot be
// encoded by an attribute at all, which is why IPP Everywhere never puts
// more than 1KiB into a single attribute value.
const MaxValueLength = 0xFFFF

// MaxNameLength is the largest name a SIGNED-SHORT name-length can
// describe: 0xFFFF (RFC 8010 §3.6).
const MaxNameLength = 0xFFFF

// Attribute is one IPP attribute with an explicitly chosen wire tag.
//
// Tag is never inferred: RFC 8010 §3.5.2 note says an attribute value
// always has a type explicitly specified by its tag. Values holds one
// or more encodings of that same tag. A single-element Values slice is
// emitted as attribute-with-one-value; every further element is emitted
// as an additional-value with a zero name-length (§3.1.5).
type Attribute struct {
	Tag    byte
	Name   string
	Values [][]byte
}

// Text builds a single-valued character-string attribute.
// tag must be one of TagText, TagName, TagKeyword, TagURI, TagURIScheme,
// TagCharset, TagNaturalLang or TagMimeMediaType (RFC 8010 Table 6).
func Text(tag byte, name, value string) Attribute {
	return Attribute{Tag: tag, Name: name, Values: [][]byte{[]byte(value)}}
}

// Keyword builds a single-valued keyword attribute (tag 0x44).
func Keyword(name, value string) Attribute {
	return Text(TagKeyword, name, value)
}

// Name builds a single-valued nameWithoutLanguage attribute (tag 0x42).
func Name(name, value string) Attribute {
	return Text(TagName, name, value)
}

// URI builds a single-valued uri attribute (tag 0x45).
func URI(name, value string) Attribute {
	return Text(TagURI, name, value)
}

// MimeType builds a single-valued mimeMediaType attribute (tag 0x49).
//
// NOTE: mimeMediaType is not in RFC 8010's own tag tables; it comes from
// RFC 8011 §5.1.1 and is registered in the IANA IPP registry. RFC 8010
// Table 6 groups character-strings, and mimeMediaType is one, so the
// encoding is an ordinary UTF-8 character string.
func MimeType(name, value string) Attribute {
	return Text(TagMimeMediaType, name, value)
}

// Strings builds a 1setOf character-string attribute from values, in
// order. The first value is the attribute-with-one-value; the rest are
// additional-values, per RFC 8010 §3.1.3/§3.1.5.
func Strings(tag byte, name string, values ...string) Attribute {
	return Attribute{Tag: tag, Name: name, Values: stringValues(values)}
}

// Keywords builds a 1setOf keyword attribute (tag 0x44).
func Keywords(name string, values ...string) Attribute {
	return Strings(TagKeyword, name, values...)
}

// MimeTypes builds a 1setOf mimeMediaType attribute (tag 0x49), e.g.
// document-format-supported.
func MimeTypes(name string, values ...string) Attribute {
	return Strings(TagMimeMediaType, name, values...)
}

// Enum builds a single-valued enum attribute (tag 0x23), encoded as 4
// big-endian octets.
//
// RFC 8010 fixes the width in three independent places, all of which say
// four octets:
//
//   - §3: "A four-byte integer, henceforth called a SIGNED-INTEGER, is
//     used for value fields".
//   - §3.8 (normative MUST): "For any of the types represented by binary
//     signed integers, the sender MUST encode the value in exactly four
//     octets."
//   - §3.9 Table 7: "integer and enum | a SIGNED-INTEGER".
//
// Appendix A.2 shows it on the wire: job-state, tag 0x23, has
// value-length 0x0004 and value 0x00000003.
func Enum(name string, value int32) Attribute {
	return Attribute{Tag: TagEnum, Name: name, Values: [][]byte{int32Value(value)}}
}

// Enums builds a 1setOf enum attribute (tag 0x23), e.g.
// operations-supported.
//
// Each element is encoded as 4 big-endian octets, exactly like Enum.
// The IPP Model [RFC 8011 §5.4.15] describes operations-supported as a
// "1setOf type2 enum", but "type2" names the *semantic* type in the
// Model, not the wire width: RFC 8010 §3.8's MUST fixes every binary
// signed integer value at four octets regardless of which Model type it
// came from. The operation-id numbers themselves (0x0002 Print-Job,
// 0x0004 Validate-Job, 0x000B Get-Printer-Attributes, ...) are
// SIGNED-SHORTs, but that only bounds their numeric range; it does not
// shrink the encoded value field.
//
// Note for readers comparing against other implementations: a 1setOf
// enum with two-octet elements is a real and widespread divergence from
// RFC 8010 §3.8. It is tempting to emit because the numbers fit in two
// octets, but it is not what the RFC requires, so this package does not
// do it.
func Enums(name string, values ...int32) Attribute {
	out := make([][]byte, len(values))
	for i, v := range values {
		out[i] = int32Value(v)
	}
	return Attribute{Tag: TagEnum, Name: name, Values: out}
}

// Integer builds a single-valued integer attribute (tag 0x21), encoded
// as 4 big-endian octets (RFC 8010 §3, SIGNED-INTEGER).
func Integer(name string, value int32) Attribute {
	return Attribute{Tag: TagInteger, Name: name, Values: [][]byte{int32Value(value)}}
}

// Integers builds a 1setOf integer attribute (tag 0x21), each element
// 4 big-endian octets, e.g. orientation-requested-supported.
func Integers(name string, values ...int32) Attribute {
	out := make([][]byte, len(values))
	for i, v := range values {
		out[i] = int32Value(v)
	}
	return Attribute{Tag: TagInteger, Name: name, Values: out}
}

// Bool builds a single-valued boolean attribute (tag 0x22). RFC 8010
// §3.9 and appendix A.1 show the value as a single octet: 0x01 true,
// 0x00 false. This matches go-ipp and ippserver but contradicts RFC 8011
// §5.1.1, which describes boolean as a sign-and-magnitude integer; the
// 1-octet form is what interoperates.
func Bool(name string, value bool) Attribute {
	b := byte(0x00)
	if value {
		b = 0x01
	}
	return Attribute{Tag: TagBoolean, Name: name, Values: [][]byte{{b}}}
}

// ResolutionUnitDPI is the SIGNED-BYTE unit field of an IPP resolution
// value: dots per inch (RFC 8011 §5.1.10 resolution syntax; unit enum
// 3 = dots per inch, 4 = dots per centimeter). This project always
// speaks dpi.
const ResolutionUnitDPI byte = 3

// Resolution builds a single-valued resolution attribute (tag 0x32):
// two 4-octet big-endian integers, cross-feed then feed direction,
// followed by a 1-octet unit (RFC 8010 §3.5.2 Table 5; the resolution
// syntax is defined by RFC 8011 §5.1.10 as
// "SIGNED-INTEGER SIGNED-INTEGER SIGNED-BYTE", i.e. NINE octets total).
// The unit is ResolutionUnitDPI (dpi).
func Resolution(name string, crossFeed, feed int32) Attribute {
	v := make([]byte, 9)
	binary.BigEndian.PutUint32(v[0:4], uint32(crossFeed))
	binary.BigEndian.PutUint32(v[4:8], uint32(feed))
	v[8] = ResolutionUnitDPI
	return Attribute{Tag: TagResolution, Name: name, Values: [][]byte{v}}
}

// Resolutions builds a 1setOf resolution attribute (tag 0x32), e.g.
// printer-resolution-supported. Each element is the 9-octet resolution
// encoding (see Resolution).
func Resolutions(name string, pairs ...[2]int32) Attribute {
	out := make([][]byte, len(pairs))
	for i, p := range pairs {
		v := make([]byte, 9)
		binary.BigEndian.PutUint32(v[0:4], uint32(p[0]))
		binary.BigEndian.PutUint32(v[4:8], uint32(p[1]))
		v[8] = ResolutionUnitDPI
		out[i] = v
	}
	return Attribute{Tag: TagResolution, Name: name, Values: out}
}

// RangeOfInteger builds a single-valued rangeOfInteger attribute
// (tag 0x33): two 4-octet big-endian integers, lower then upper bound
// (RFC 8010 §3.5.2 Table 5).
func RangeOfInteger(name string, lower, upper int32) Attribute {
	v := make([]byte, 8)
	binary.BigEndian.PutUint32(v[0:4], uint32(lower))
	binary.BigEndian.PutUint32(v[4:8], uint32(upper))
	return Attribute{Tag: TagRangeOfInt, Name: name, Values: [][]byte{v}}
}

// DateTime builds a single-valued dateTime attribute (tag 0x31).
// RFC 8010 §3.5.2 Table 5 / IPP Model §5.1.1: 11 octets, a 2-octet
// big-endian year followed by 10 one-octet fields in the order
// month, day, hour, minutes, seconds, deci-seconds, and the UTC
// direction (+ / -) with its hours and minutes offsets.
// year is signed: 2000-01-01T00:00:00Z is 0x07D0 01 01 00 00 00 0 2B 00 00.
func DateTime(name string, year int16, month, day, hour, minute, second, deciSecond byte, utcDir byte, utcHours, utcMinutes byte) Attribute {
	v := make([]byte, 11)
	binary.BigEndian.PutUint16(v[0:2], uint16(year))
	v[2] = month
	v[3] = day
	v[4] = hour
	v[5] = minute
	v[6] = second
	v[7] = deciSecond
	v[8] = utcDir
	v[9] = utcHours
	v[10] = utcMinutes
	return Attribute{Tag: TagDateTime, Name: name, Values: [][]byte{v}}
}

// Unsupported builds an out-of-band "unsupported" attribute (tag 0x10)
// with a zero-length value, used in the unsupported-attributes group
// (RFC 8010 §3.5.2 Table 3, appendix A.3).
func Unsupported(name string) Attribute {
	return Attribute{Tag: TagUnsupported, Name: name, Values: [][]byte{{}}}
}

// AttributeGroup is one "attribute-group" field of an IPP message:
// a begin-attribute-group-tag followed by zero or more attributes
// (RFC 8010 §3.1.2 Figure 2, §3.2 ABNF).
type AttributeGroup struct {
	Delimiter  byte // 0x01 operation, 0x02 job, 0x04 printer, 0x05 unsupported
	Attributes []Attribute
}

// WriteGroup serializes the group delimiter and every attribute to w.
// The end-of-attributes-tag is NOT written; that belongs to the message
// frame (which go-ipp's Response.Encode can also supply).
func (g AttributeGroup) WriteGroup(w io.Writer) error {
	if _, err := w.Write([]byte{g.Delimiter}); err != nil {
		return err
	}
	return WriteAttributes(w, g.Attributes)
}

// WriteAttributes serializes attributes with no group delimiter in front.
func WriteAttributes(w io.Writer, attrs []Attribute) error {
	for _, a := range attrs {
		if err := a.Write(w); err != nil {
			return err
		}
	}
	return nil
}

// Write serializes one attribute as attribute-with-one-value followed by
// one additional-value per extra element (RFC 8010 §3.1.3–§3.1.5).
func (a Attribute) Write(w io.Writer) error {
	if err := a.validate(); err != nil {
		return err
	}

	for i, v := range a.Values {
		name := a.Name
		if i > 0 {
			// additional-value: RFC 8010 §3.1.5 requires name-length 0x0000.
			name = ""
		}
		if err := writeValuePair(w, a.Tag, name, v); err != nil {
			return err
		}
	}
	return nil
}

// Marshal serializes one attribute into a fresh byte slice. It is the
// building block used by the golden-byte tests.
func (a Attribute) Marshal() ([]byte, error) {
	var buf writeBuffer
	if err := a.Write(&buf); err != nil {
		return nil, err
	}
	return buf.b, nil
}

// Marshal serializes a whole group into a fresh byte slice.
func (g AttributeGroup) Marshal() ([]byte, error) {
	var buf writeBuffer
	if err := g.WriteGroup(&buf); err != nil {
		return nil, err
	}
	return buf.b, nil
}

// MarshalAttributes serializes a slice of attributes into a fresh slice.
func MarshalAttributes(attrs []Attribute) ([]byte, error) {
	var buf writeBuffer
	if err := WriteAttributes(&buf, attrs); err != nil {
		return nil, err
	}
	return buf.b, nil
}

func (a Attribute) validate() error {
	if a.Name == "" {
		return fmt.Errorf("attributes: attribute has empty name")
	}
	if len(a.Name) > MaxNameLength {
		return fmt.Errorf("attributes: attribute name %q is %d bytes, exceeds %d", a.Name, len(a.Name), MaxNameLength)
	}
	if a.Tag < 0x10 {
		return fmt.Errorf("attributes: attribute %q has delimiter tag 0x%02x; a value-tag must be >= 0x10 (RFC 8010 §3.5.2)", a.Name, a.Tag)
	}
	if len(a.Values) == 0 {
		return fmt.Errorf("attributes: attribute %q has no values", a.Name)
	}
	for i, v := range a.Values {
		if len(v) > MaxValueLength {
			return fmt.Errorf("attributes: attribute %q value %d is %d bytes, exceeds %d", a.Name, i, len(v), MaxValueLength)
		}
		if !utf8.Valid(v) && isCharacterStringTag(a.Tag) {
			return fmt.Errorf("attributes: attribute %q value %d is not valid UTF-8 but tag 0x%02x is a character string", a.Name, i, a.Tag)
		}
	}
	return nil
}

// isCharacterStringTag reports whether tag denotes a character-string
// syntax, which RFC 8010 §3 requires to be UTF-8 for the chosen charset.
func isCharacterStringTag(tag byte) bool {
	return tag >= TagText && tag <= TagMemberAttr
}

// writeValuePair emits one value field. The two RFC 8010 shapes differ in
// whether a name appears at all, so they are written as separate paths:
//
// attribute-with-one-value (RFC 8010 §3.1.4 figure 4):
//
//	value-tag(1) | name-length(2) | name(u) | value-length(2) | value(v)
//
// additional-value (RFC 8010 §3.1.5 figure 5) — used for the 2nd and
// later elements of a 1setOf, which carry no name:
//
//	value-tag(1) | name-length = 0x0000(2) | value-length(2) | value(v)
//
// Note that value-length comes AFTER the name in the first shape. Packing
// it into a fixed 5-byte header (as an earlier revision of this file did)
// silently produces tag|name-length|value-length|name|value, which makes
// every receiver desynchronise on the first attribute.
func writeValuePair(w io.Writer, tag byte, name string, value []byte) error {
	// value-tag and name-length are always adjacent at the start.
	var head [3]byte
	head[0] = tag
	binary.BigEndian.PutUint16(head[1:3], uint16(len(name)))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}

	// name, present only on attribute-with-one-value.
	if name != "" {
		if _, err := io.WriteString(w, name); err != nil {
			return err
		}
	}

	// value-length, then value.
	var vlen [2]byte
	binary.BigEndian.PutUint16(vlen[:], uint16(len(value)))
	if _, err := w.Write(vlen[:]); err != nil {
		return err
	}
	if len(value) > 0 {
		if _, err := w.Write(value); err != nil {
			return err
		}
	}
	return nil
}

func stringValues(values []string) [][]byte {
	out := make([][]byte, len(values))
	for i, s := range values {
		out[i] = []byte(s)
	}
	return out
}

func int32Value(v int32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(v))
	return b
}

// writeBuffer is a tiny in-memory io.Writer. bytes.Buffer would work
// too, but keeping the dependency to encoding/binary only makes the
// byte-level intent obvious in review.
type writeBuffer struct{ b []byte }

func (w *writeBuffer) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}
