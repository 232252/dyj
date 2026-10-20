// Package ipp implements the server side of the IPP wire protocol
// (RFC 8010 framing, RFC 8011 semantics).
//
// Layering (ADR-0002 D4):
//
//	pkg/attributes — the ONLY RFC 8010 encoding AND decoding layer: tags,
//	                 the Attribute type, serialization, streaming parsing,
//	                 and the IPP Everywhere attribute tables.
//	pkg/ipp        — HTTP(S) transport, operation-code routing, operation
//	                 semantics, and the JobSink seam towards the spooler.
//
// Neither direction goes through phin1x/go-ipp, for two empirically
// established reasons:
//
//   - Encoding: go-ipp resolves an attribute's tag through
//     AttributeTagMapping, which holds only 70 names and is missing
//     almost every IPP Everywhere "-supported"/"-default" attribute, and
//     Encode hard-errors on anything unmapped
//     (docs/research/07-go-ipp-server-capability.md, appendix E).
//   - Decoding: go-ipp's request decoder overwrites repeated attribute
//     names (the literal TODO "handle attributes with array values"),
//     silently dropping every value of a 1setOf keyword such as
//     requested-attributes (docs/adr/0006-own-request-decoder.md).
//
// go-ipp remains a test-only cross-validation tool.
package ipp

import (
	"bytes"
	"io"
	"os"
	"strconv"

	"github.com/232252/dyj/pkg/attributes"
)

// Re-exported tags. The values live in pkg/attributes; these aliases exist so
// pkg/ipp's own callers (and the tests) have a single import.
const (
	GroupOperation   = attributes.TagOperationAttributes
	GroupJob         = attributes.TagJobAttributes
	GroupEnd         = attributes.TagEndOfAttributes
	GroupPrinter     = attributes.TagPrinterAttributes
	GroupUnsupported = attributes.TagUnsupportedAttributes

	TagUnsupported = attributes.TagUnsupported
	TagNoValue     = attributes.TagNoValue
	TagInteger     = attributes.TagInteger
	TagBoolean     = attributes.TagBoolean
	TagEnum        = attributes.TagEnum
	TagDateTime    = attributes.TagDateTime
	TagResolution  = attributes.TagResolution
	TagRangeOfInt  = attributes.TagRangeOfInt
	TagText        = attributes.TagText
	TagName        = attributes.TagName
	TagKeyword     = attributes.TagKeyword
	TagURI         = attributes.TagURI
	TagURIScheme   = attributes.TagURIScheme
	TagCharset     = attributes.TagCharset
	TagNaturalLang = attributes.TagNaturalLang
	TagLanguage    = attributes.TagNaturalLanguage
	TagMimeType    = attributes.TagMimeMediaType
	TagMemberName  = attributes.TagMemberAttr
	TagTextLang    = attributes.TagTextLang
)

// Attr is an alias for the single attribute type of this repository.
type Attr = attributes.Attribute

// Group is one attribute group in an IPP message.
type Group struct {
	Tag   byte
	Attrs []Attr
}

// Value constructors, re-exported from pkg/attributes.

// Int encodes a 4-byte signed integer value.
func Int(v int32) []byte { return attributes.Integer("x", v).Values[0] }

// Enum encodes an enum value (4 octets, RFC 8010 §3.8).
func Enum(v int32) []byte { return attributes.Enum("x", v).Values[0] }

// Bool encodes a boolean value.
func Bool(v bool) []byte { return attributes.Bool("x", v).Values[0] }

// Text encodes a raw character-string value.
func Text(s string) []byte { return []byte(s) }

// EncodeAttrs writes a full attribute group (delimiter + attributes) to w.
func EncodeAttrs(w io.Writer, g Group) error {
	if _, err := w.Write([]byte{g.Tag}); err != nil {
		return err
	}
	return attributes.WriteAttributes(w, g.Attrs)
}

// ---------------------------------------------------------------------------
// Response
// ---------------------------------------------------------------------------

// Response is an IPP response ready to be encoded.
type Response struct {
	VersionMajor byte
	VersionMinor byte
	Status       int16
	RequestID    int32
	Groups       []Group
}

// NewResponse builds a response for the given request id.
func NewResponse(status int16, requestID int32) *Response {
	return &Response{VersionMajor: 2, VersionMinor: 0, Status: status, RequestID: requestID}
}

// Encode serialises the response. It never fails on an "unknown" attribute
// name, which is the reason this package does not delegate to go-ipp here.
func (r *Response) Encode() ([]byte, error) {
	var w bytes.Buffer
	w.Write([]byte{
		r.VersionMajor, r.VersionMinor,
		byte(uint16(r.Status) >> 8), byte(uint16(r.Status)),
		byte(uint32(r.RequestID) >> 24), byte(uint32(r.RequestID) >> 16),
		byte(uint32(r.RequestID) >> 8), byte(uint32(r.RequestID)),
	})
	for _, g := range r.Groups {
		if err := EncodeAttrs(&w, g); err != nil {
			return nil, err
		}
	}
	w.WriteByte(GroupEnd)
	return w.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// DecodeRequestStream parses an IPP request from r, streaming. The
// document bytes stay unread in msg.DataReader.
func DecodeRequestStream(r io.Reader) (*attributes.Message, error) {
	return attributes.DecodeMessageStream(r)
}

// DecodeRequest parses a fully buffered IPP request. Test convenience.
func DecodeRequest(body []byte) (*attributes.Message, error) {
	return attributes.DecodeMessage(body)
}

// RequestedAttributes returns the requested-attributes list from the
// message's operation group. A missing or empty list means "everything"
// (RFC 8011 §4.2.1.2). Every value of the 1setOf keyword is returned —
// decoding with go-ipp here would have kept only the last one.
func RequestedAttributes(msg *attributes.Message) []string {
	return attributes.GroupStrings(msg.Operation(), "requested-attributes")
}

// FilterAttrs keeps only the attributes named in want; an empty want keeps
// everything.
func FilterAttrs(attrs []Attr, want []string) []Attr {
	if len(want) == 0 {
		return attrs
	}
	set := make(map[string]struct{}, len(want))
	for _, w := range want {
		set[w] = struct{}{}
	}
	out := make([]Attr, 0, len(attrs))
	for _, a := range attrs {
		if _, ok := set[a.Name]; ok {
			out = append(out, a)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Printer profile passed to pkg/attributes
// ---------------------------------------------------------------------------

// Printer is the capability description served to clients. It maps onto
// attributes.PrinterInfo, keeping this package free of attribute-table
// construction (which pkg/attributes owns).
type Printer struct {
	Name         string
	Info         string
	Location     string
	MakeAndModel string
	UUID         string
	URIs         []string
	Port         int

	// Key is the slug identifying this printer inside a Router ("" for the
	// classic single-printer setup): /ipp/print/<key> routes here.
	Key string

	DocumentFormats []string
	Media           []string
	ColorSupported  bool
	AcceptingJobs   bool
}

// DefaultPrinter is a conservative IPP Everywhere capable printer profile.
func DefaultPrinter() Printer {
	host, _ := os.Hostname()
	return Printer{
		Name:           "dyj",
		Info:           "dyj shared printer",
		Location:       host,
		MakeAndModel:   "dyj IPP Everywhere Printer",
		UUID:           "urn:uuid:00000000-0000-4000-8000-000000000001",
		URIs:           []string{"ipp://127.0.0.1:6631/ipp/print"},
		Port:           6631,
		ColorSupported: false,
		AcceptingJobs:  true,
	}
}

func (p Printer) primaryURI() string {
	if len(p.URIs) > 0 {
		return p.URIs[0]
	}
	return "ipp://localhost:" + strconv.Itoa(p.Port) + "/ipp/print"
}

// info converts the transport-facing profile into the attribute package's
// input type. This is the seam between the two packages, and it is one-way.
// Dynamic bits (queued-job-count, state reasons, accepting-jobs) come from
// the status hook the Server was built with, if any.
func (p Printer) info(status printerStatus) attributes.PrinterInfo {
	info := attributes.PrinterInfo{
		URI:           p.primaryURI(),
		UUID:          p.UUID,
		Name:          p.Name,
		Info:          p.Info,
		Location:      p.Location,
		MakeAndModel:  p.MakeAndModel,
		Color:         p.ColorSupported,
		State:         attributes.PrinterStateIdle,
		AcceptingJobs: p.AcceptingJobs,
	}
	if len(p.DocumentFormats) > 0 {
		info.FormatsOverride = p.DocumentFormats
	}
	if len(p.Media) > 0 {
		info.MediaSupported = p.Media
	}
	if status != nil {
		info.AcceptingJobs, info.QueuedJobCount, info.StateReasons = status()
		if !info.AcceptingJobs {
			info.State = attributes.PrinterStateStopped
		}
	}
	return info
}

// printerStatus is the optional dynamic-state hook wired from the spooler.
type printerStatus = func() (accepting bool, queued int32, reasons []string)

// DescribeOperation renders an operation-id for logs.
func DescribeOperation(op uint16) string {
	switch op {
	case 0x0002:
		return "Print-Job"
	case 0x0003:
		return "Print-URI"
	case 0x0004:
		return "Validate-Job"
	case 0x0005:
		return "Create-Job"
	case 0x0006:
		return "Send-Document"
	case 0x0008:
		return "Cancel-Job"
	case 0x0009:
		return "Get-Job-Attributes"
	case 0x000A:
		return "Get-Jobs"
	case 0x000B:
		return "Get-Printer-Attributes"
	case 0x003C:
		return "Identify-Printer"
	case 0x4001:
		return "CUPS-Get-Default"
	case 0x4002:
		return "CUPS-Get-Printers"
	default:
		return "0x" + hex4(op)
	}
}

func hex4(v uint16) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[v>>12&0xf], digits[v>>8&0xf], digits[v>>4&0xf], digits[v&0xf]})
}
