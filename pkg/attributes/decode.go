// decode.go adds the request-side mirror of the writer: a strict RFC 8010
// message decoder.
//
// Why this package carries its own decoder when go-ipp is already a
// dependency (empirically established, see docs/adr/0006-own-request-decoder.md):
//
//	go-ipp v1.7.0's request decoder overwrites repeated attribute names —
//	request_decoder_state_machine.go contains the literal TODO
//	"handle attributes with array values" and assigns
//	currentAttributes[name] = value in a loop. A 1setOf keyword such as
//	requested-attributes therefore arrives with only its LAST value
//	intact, and any server built on it mis-filters Get-Printer-Attributes
//	(CUPS sends "requested-attributes" as a multi-valued keyword in every
//	driverless exchange).
//
// This decoder keeps every value. Because it fills the same Attribute /
// AttributeGroup types the writer serialises, decode→re-encode is
// lossless, which the tests lock in.
//
// The decoder is streaming: attribute groups are parsed incrementally
// from any io.Reader and the document bytes (everything after the
// end-of-attributes tag) are left unread in Message.DataReader, so a
// 100 MB PWG raster job never has to be buffered whole.
package attributes

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// maxHeaderBytes bounds the attribute-section of a decoded message.
// The IPP Everywhere attribute table is a few KiB; anything beyond this
// in the attribute area is either a hostile peer or a broken client.
// Document data is NOT bounded by this — it streams.
const maxHeaderBytes = 1 << 20 // 1 MiB

// Message is one decoded IPP message (RFC 8010 §3.1 figure 1).
//
// For a REQUEST, Code is the operation-id; for a RESPONSE it is the
// status-code. The decoder does not distinguish the two: the framing is
// identical, and callers know which side they are on.
type Message struct {
	VersionMajor byte
	VersionMinor byte
	Code         uint16
	RequestID    int32
	Groups       []AttributeGroup

	// DataReader holds the document bytes — everything after the
	// end-of-attributes tag. Always non-nil after DecodeMessageStream
	// (reading it yields io.EOF immediately when there is no document).
	// Callers must read it exactly once.
	DataReader io.Reader
	// Data is set ONLY by the convenience DecodeMessage: the document
	// fully buffered. Nil when the message carried no document.
	Data []byte
}

// Group returns the first group with the given delimiter tag, or nil.
func (m *Message) Group(tag byte) *AttributeGroup {
	for i := range m.Groups {
		if m.Groups[i].Delimiter == tag {
			return &m.Groups[i]
		}
	}
	return nil
}

// Operation returns the operation-attributes group, or nil.
func (m *Message) Operation() *AttributeGroup { return m.Group(TagOperationAttributes) }

// Job returns the job-attributes group, or nil.
func (m *Message) Job() *AttributeGroup { return m.Group(TagJobAttributes) }

// GroupStrings returns every value of the named attribute in group,
// provided the attribute uses a character-string syntax (text/name/
// keyword/uri/uriScheme/charset/naturalLanguage/mimeMediaType, RFC 8010
// §3.5.2 Table 6). Binary-syntax attributes return nil rather than
// garbage.
func GroupStrings(group *AttributeGroup, name string) []string {
	if group == nil {
		return nil
	}
	var out []string
	for _, a := range group.Attributes {
		if a.Name != name {
			continue
		}
		if !isCharacterStringTag(a.Tag) {
			return nil
		}
		for _, v := range a.Values {
			out = append(out, string(v))
		}
	}
	return out
}

// GroupFirstString is GroupStrings limited to the first value (""
// when absent or not a character-string attribute).
func GroupFirstString(group *AttributeGroup, name string) string {
	if v := GroupStrings(group, name); len(v) > 0 {
		return v[0]
	}
	return ""
}

// GroupInt returns the first value of the named attribute as an int32,
// when the attribute uses integer or enum syntax (4-octet big-endian
// values, RFC 8010 §3.5.2 Table 4). ok is false when the attribute is
// absent or has another syntax.
func GroupInt(group *AttributeGroup, name string) (int32, bool) {
	if group == nil {
		return 0, false
	}
	for _, a := range group.Attributes {
		if a.Name != name || (a.Tag != TagInteger && a.Tag != TagEnum) {
			continue
		}
		if len(a.Values) == 0 || len(a.Values[0]) != 4 {
			return 0, false
		}
		return int32(binary.BigEndian.Uint32(a.Values[0])), true
	}
	return 0, false
}

// GroupBool returns the first value of the named attribute as a bool,
// when the attribute uses boolean syntax (1 octet, RFC 8010 §3.9).
func GroupBool(group *AttributeGroup, name string) (bool, bool) {
	if group == nil {
		return false, false
	}
	for _, a := range group.Attributes {
		if a.Name != name || a.Tag != TagBoolean {
			continue
		}
		if len(a.Values) == 0 || len(a.Values[0]) != 1 {
			return false, false
		}
		return a.Values[0][0] != 0, true
	}
	return false, false
}

// DecodeMessageStream parses the framing and attribute groups of one IPP
// message from r and leaves the document unconsumed in DataReader.
//
// It is strict: a value length that overruns the stream, an unknown
// delimiter, or an operation-attributes group whose first attribute is
// not attributes-charset is an error (RFC 8011 §4.1.4: "The
// attributes-charset attribute MUST be the first attribute in the
// operation-attributes group").
func DecodeMessageStream(r io.Reader) (*Message, error) {
	rd := &countingReader{r: bufio.NewReaderSize(r, 16<<10)}
	m := &Message{}

	head := make([]byte, 8)
	if _, err := io.ReadFull(rd, head); err != nil {
		return nil, fmt.Errorf("attributes: reading message header: %w", err)
	}
	m.VersionMajor = head[0]
	m.VersionMinor = head[1]
	m.Code = binary.BigEndian.Uint16(head[2:4])
	m.RequestID = int32(binary.BigEndian.Uint32(head[4:8]))

	var cur *AttributeGroup
	firstAttrOfGroup := true

	for {
		tag, err := rd.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("attributes: message ends without an end-of-attributes tag")
		}

		if tag == TagEndOfAttributes {
			m.DataReader = rd
			return m, nil
		}

		if tag < 0x10 { // delimiter tags (RFC 8010 §3.5.1)
			switch tag {
			case TagOperationAttributes, TagJobAttributes, TagPrinterAttributes, TagUnsupportedAttributes:
				m.Groups = append(m.Groups, AttributeGroup{Delimiter: tag})
				cur = &m.Groups[len(m.Groups)-1]
				firstAttrOfGroup = true
				continue
			default:
				return nil, fmt.Errorf("attributes: unknown delimiter tag 0x%02x at message offset %d", tag, rd.n)
			}
		}

		if cur == nil {
			return nil, fmt.Errorf("attributes: value tag 0x%02x before any attribute group at message offset %d", tag, rd.n)
		}

		// attribute-with-one-value / additional-value (RFC 8010 §3.1.4/§3.1.5):
		// tag | name-length(2) | name | value-length(2) | value
		nameLen, err := readU16(rd)
		if err != nil {
			return nil, fmt.Errorf("attributes: truncated name-length: %w", err)
		}
		var name string
		if nameLen > 0 {
			b := make([]byte, nameLen)
			if _, err := io.ReadFull(rd, b); err != nil {
				return nil, fmt.Errorf("attributes: truncated name: %w", err)
			}
			name = string(b)
		}
		valLen, err := readU16(rd)
		if err != nil {
			return nil, fmt.Errorf("attributes: truncated value-length on %q: %w", name, err)
		}
		var value []byte
		if valLen > 0 {
			value = make([]byte, valLen)
			if _, err := io.ReadFull(rd, value); err != nil {
				return nil, fmt.Errorf("attributes: truncated value on %q: %w", name, err)
			}
		}

		if firstAttrOfGroup {
			if nameLen == 0 {
				return nil, fmt.Errorf("attributes: group 0x%02x begins with an additional-value (empty name)", cur.Delimiter)
			}
			if cur.Delimiter == TagOperationAttributes && name != "attributes-charset" {
				return nil, fmt.Errorf("attributes: first operation attribute is %q, must be attributes-charset (RFC 8011 §4.1.4)", name)
			}
			firstAttrOfGroup = false
		}

		if nameLen == 0 {
			// Additional-value: same name as the previous attribute (§3.6).
			if len(cur.Attributes) == 0 {
				return nil, fmt.Errorf("attributes: additional-value before any named attribute")
			}
			last := &cur.Attributes[len(cur.Attributes)-1]
			if last.Tag != tag {
				return nil, fmt.Errorf("attributes: additional-value tag 0x%02x differs from 0x%02x on %q", tag, last.Tag, last.Name)
			}
			last.Values = append(last.Values, value)
			continue
		}

		cur.Attributes = append(cur.Attributes, Attribute{Tag: tag, Name: name, Values: [][]byte{value}})

		if rd.n > maxHeaderBytes {
			return nil, fmt.Errorf("attributes: attribute section exceeds %d bytes", maxHeaderBytes)
		}
	}
}

// DecodeMessage parses one complete IPP message, buffering the document
// into Data. Convenience for tests and small responses; servers use
// DecodeMessageStream so document bytes never buffer.
func DecodeMessage(data []byte) (*Message, error) {
	m, err := DecodeMessageStream(newSliceReader(data))
	if err != nil {
		return nil, err
	}
	if m.DataReader != nil {
		m.Data, err = io.ReadAll(m.DataReader)
		if err != nil {
			return nil, err
		}
		if len(m.Data) == 0 {
			m.Data = nil
		}
	}
	return m, nil
}

// countingReader counts bytes pulled through, for offset reporting and
// the attribute-section bound.
type countingReader struct {
	r *bufio.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}

func readU16(c *countingReader) (int, error) {
	b := make([]byte, 2)
	if _, err := io.ReadFull(c, b); err != nil {
		return 0, err
	}
	return int(binary.BigEndian.Uint16(b)), nil
}

// sliceReader is a minimal io.Reader over a byte slice.
type sliceReader struct {
	b []byte
	i int
}

func newSliceReader(b []byte) *sliceReader { return &sliceReader{b: b} }

func (s *sliceReader) Read(p []byte) (int, error) {
	if s.i >= len(s.b) {
		return 0, io.EOF
	}
	n := copy(p, s.b[s.i:])
	s.i += n
	return n, nil
}
