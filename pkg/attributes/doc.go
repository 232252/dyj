// Package attributes implements RFC 8010 attribute serialization and the
// IPP Everywhere printer capability table.
//
// # This is the repository's ONLY RFC 8010 encoding layer
//
// pkg/ipp MUST consume this package rather than re-implement tag
// constants or an attribute encoder. Two independent encoders in one
// repository is how a real defect survived here: the same tag 0x23 was
// being written with two different widths in two places, and neither copy
// noticed the other. ADR-0002 D4 assigns this responsibility to
// pkg/attributes; keep it single-sourced.
//
// This package deliberately depends only on the standard library. It does
// NOT import go-ipp, so pkg/ipp remains free to use go-ipp for request
// decoding while using this package for response encoding.
//
// # Why this package exists
//
// go-ipp v1.7.0 resolves an attribute's wire tag through its
// AttributeTagMapping table, which contains only 70 attribute names and is
// missing almost every IPP Everywhere / AirPrint "-supported" and
// "-default" attribute (document-format-supported, operations-supported,
// printer-uri-supported, urf-supported, ...). AttributeEncoder.Encode
// returns "cannot get tag of attribute <name>" for anything unmapped and
// offers no way to supply a tag explicitly, and it additionally rejects
// values whose Go type does not match the mapped tag. See
// docs/research/07-go-ipp-server-capability.md appendix E.
//
// Here the tag is always supplied by the caller, so no mapping table and no
// type-cross-check stand between the program and the wire.
//
// # Public API
//
// Types:
//
//	Attribute       one attribute with an explicit tag and one or more values
//	AttributeGroup  a group delimiter plus its attributes
//	PrinterInfo     plain-data description of one printer
//
// Encoding entry points:
//
//	(Attribute).Write(io.Writer) error   stream one attribute
//	(Attribute).Marshal() ([]byte, err)  one attribute as bytes
//	(AttributeGroup).WriteGroup(io.Writer) error
//	(AttributeGroup).Marshal() ([]byte, error)
//	WriteAttributes(io.Writer, []Attribute) error
//	MarshalAttributes([]Attribute) ([]byte, error)
//
// Constructors, one per attribute syntax:
//
//	Text(tag, name, value)   Keyword / Name / URI / MimeType
//	Strings(tag, name, ...)  Keywords / MimeTypes
//	Enum(name, v)            Enums(name, ...)     Integer / Integers
//	Bool                     Resolution / Resolutions
//	RangeOfInteger           DateTime
//	NoValue / Unsupported
//
// Tag constants: TagInteger, TagBoolean, TagEnum, TagDateTime,
// TagResolution, TagRangeOfInt, TagText, TagName, TagKeyword, TagURI,
// TagURIScheme, TagCharset, TagNaturalLang, TagMimeMediaType, TagMemberAttr,
// TagUnsupported, TagUnknown, TagNoValue; group tags TagOperationAttributes,
// TagJobAttributes, TagPrinterAttributes, TagUnsupportedAttributes,
// TagEndOfAttributes.
//
// IPP Everywhere:
//
//	BuildPrinterAttributes(PrinterInfo) []Attribute
//	ValidatePrinterAttributes([]Attribute) error
//	RequiredPrinterAttributes  (the authoritative required-name list)
//	ValidateURI(string) error
//
// # Minimal integration example for pkg/ipp
//
//	// 1. Build the printer description (pkg/attributes).
//	printerAttrs := attributes.BuildPrinterAttributes(info)
//
//	// 2. Write the message header yourself: version(2) | status-code(2)
//	//    | request-id(4), then the operation-attributes group.
//	if err := (attributes.AttributeGroup{
//	        Delimiter:  attributes.TagOperationAttributes,
//	        Attributes: opAttrs,
//	    }).WriteGroup(w); err != nil {
//	        return err
//	}
//
//	// 3. Wrap the printer description in its own group.
//	if err := (attributes.AttributeGroup{
//	        Delimiter:  attributes.TagPrinterAttributes,
//	        Attributes: printerAttrs,
//	    }).WriteGroup(w); err != nil {
//	        return err
//	}
//
//	// 4. The end-of-attributes tag is written once, last.
//	_, err := w.Write([]byte{attributes.TagEndOfAttributes})
//
// For a raw wire struct, use attributes.Attribute directly — it is already
// tag + name + [][]byte of encoded values, with no interpretation layered
// on top, which is what an HTTP response writer wants.
//
// # Encoding rules this package guarantees
//
//   - attribute-with-one-value: tag(1) | name-length(2, BE) | name |
//     value-length(2, BE) | value — RFC 8010 §3.1.4 figure 4.
//   - additional-value (2nd and later 1setOf element): tag(1) |
//     name-length = 0x0000 | value-length(2, BE) | value — §3.1.5 figure 5.
//     Note the name is absent entirely, so value-length follows immediately.
//   - integer and enum values are exactly 4 octets, big-endian two's
//     complement — §3 "SIGNED-INTEGER is used for value fields", §3.8
//     "the sender MUST encode the value in exactly four octets", §3.9
//     Table 7 "integer and enum | a SIGNED-INTEGER". Appendix A.2 shows
//     an enum on the wire with value-length 0x0004.
//   - boolean is exactly 1 octet, 0x00 false / 0x01 true — §3.8 and
//     §3.9 Table 7; appendix A.1.
//   - out-of-band values (unsupported, no-value) have value-length 0 and
//     an empty value — §3.8, §3.5.2 Table 3, appendix A.3.
//   - names match §3.7: LALPHA *( LALPHA / DIGIT / "-" / "_" / "." ).
//
// # Verified against the RFC, not against itself
//
// The golden vectors in the tests are hand-computed from RFC 8010's own
// field tables. Two of them are literal transcriptions of Appendix A.1
// (227 bytes) and Appendix A.2, and crossvalidate_test.go feeds those same
// bytes to go-ipp's independent RequestDecoder, requiring it to recover the
// RFC's symbolic values. Self-consistency tests alone would not have caught
// the field-ordering defect this package shipped once.
package attributes
