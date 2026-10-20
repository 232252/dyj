// template.go implements RFC 8011 §4.2.1.2 (ipp-attribute-fidelity):
// when a client submits a job template attribute we cannot honor AND set
// fidelity true, the job MUST be rejected with
// client-error-attributes-or-values-not-supported (0x040B) carrying an
// unsupported-attributes group. When fidelity is false (the default) the
// attribute is dropped and the job proceeds with what we can do.
package ipp

import (
	"fmt"
	"slices"

	"github.com/232252/dyj/pkg/attributes"
)

// unsupportedAttr is one offender for the unsupported-attributes group.
type unsupportedAttr struct {
	Name  string
	Value string // reason text for the server log; wire value is out-of-band
}

// templateAttributeNames are the job-template attributes dyj understands
// (mirrors job-creation-attributes-supported in pkg/attributes).
var templateAttributeNames = []string{
	"copies", // handled as integer, kept out of the string map
	"media",
	"multiple-document-handling",
	"orientation-requested", // enum, see below
	"output-bin",
	"print-color-mode",
	"print-quality", // enum
	"print-scaling",
	"printer-resolution", // resolution syntax
	"sides",
}

// validateTemplate splits offenders into two classes:
//
//   - fatal: the request CANNOT be honored as stated — an unrenderable
//     document-format or an undecodable compression. Continuing would
//     accept bytes we must drop or corrupt, so these reject regardless
//     of ipp-attribute-fidelity (with 0x040B + the unsupported group).
//   - soft: job-template values we could degrade on (media, copies...).
//     With fidelity true they reject the job; with fidelity false they
//     are dropped and the job proceeds (RFC 8011 §4.2.1.2).
func (s *Server) validateTemplate(msg *attributes.Message) (fatal, soft []unsupportedAttr) {
	job := msg.Job()
	op := msg.Operation()

	for _, name := range []string{"compression", "document-format"} {
		vals := attributes.GroupStrings(op, name)
		if len(vals) == 0 {
			continue
		}
		v := vals[0]
		switch name {
		case "compression":
			if v != "" && v != "none" && v != "gzip" {
				fatal = append(fatal, unsupportedAttr{name, v})
			}
		case "document-format":
			if !s.formatSupported(v) {
				fatal = append(fatal, unsupportedAttr{name, v})
			}
		}
	}
	if job == nil {
		return fatal, soft
	}

	// copies: integer within copies-supported (1..99, mirrored from the
	// attribute table).
	if copies, ok := attributes.GroupInt(job, "copies"); ok {
		if copies < 1 || copies > 99 {
			soft = append(soft, unsupportedAttr{"copies", fmt.Sprint(copies)})
		}
	}

	// media / sides / color mode / etc.: keyword membership in the
	// advertised supported sets.
	media := s.printer.Media
	if len(media) == 0 {
		media = attributes.DefaultMedia()
	}
	sets := map[string][]string{
		"media":                      media,
		"sides":                      {"one-sided", "two-sided-long-edge", "two-sided-short-edge"},
		"print-color-mode":           colorModes(s.printer.ColorSupported),
		"output-bin":                 {"face-down"},
		"print-scaling":              {"auto", "auto-fit", "fill", "fit", "none"},
		"multiple-document-handling": {"separate-documents-uncollated-copies", "separate-documents-collated-copies"},
	}
	for name, supported := range sets {
		vals := attributes.GroupStrings(job, name)
		if len(vals) == 0 {
			continue
		}
		v := vals[0]
		if !slices.Contains(supported, v) {
			soft = append(soft, unsupportedAttr{name, v})
		}
	}

	// Enum-valued templates.
	if q, ok := attributes.GroupInt(job, "print-quality"); ok && (q < 3 || q > 5) {
		soft = append(soft, unsupportedAttr{"print-quality", fmt.Sprint(q)})
	}
	if o, ok := attributes.GroupInt(job, "orientation-requested"); ok && (o < 3 || o > 6) {
		soft = append(soft, unsupportedAttr{"orientation-requested", fmt.Sprint(o)})
	}

	return fatal, soft
}

// fidelity reports the ipp-attribute-fidelity request flag (RFC 8011
// §4.2.1.2; false when absent).
func fidelity(msg *attributes.Message) bool {
	v, ok := attributes.GroupBool(msg.Operation(), "ipp-attribute-fidelity")
	return ok && v
}

// rejectTemplate enforces the fatal/soft split and returns non-nil when
// the request must be rejected.
func (s *Server) rejectTemplate(msg *attributes.Message) *Response {
	fatal, soft := s.validateTemplate(msg)
	if len(fatal) > 0 {
		return s.unsupportedResponse(msg.RequestID, fatal)
	}
	if len(soft) > 0 && fidelity(msg) {
		return s.unsupportedResponse(msg.RequestID, soft)
	}
	if len(soft) > 0 {
		names := make([]string, 0, len(soft))
		for _, b := range soft {
			names = append(names, b.Name+"="+b.Value)
		}
		s.log.Printf("ipp: dropping unsupported job-template attributes (fidelity=false): %v", names)
	}
	return nil
}

// unsupportedResponse renders 0x040B with the unsupported-attributes
// group (RFC 8011 §4.1.7: each offender as an out-of-band
// "unsupported" value, tag 0x10).
func (s *Server) unsupportedResponse(requestID int32, bad []unsupportedAttr) *Response {
	names := make([]string, 0, len(bad))
	for _, b := range bad {
		names = append(names, b.Name)
	}
	s.log.Printf("ipp: rejecting job with fidelity: unsupported attributes %v", names)

	unsup := make([]Attr, 0, len(bad))
	for _, b := range bad {
		unsup = append(unsup, attributes.Unsupported(b.Name))
	}

	resp := NewResponse(StatusErrorAttributesNotSupported, requestID)
	resp.Groups = []Group{
		{Tag: GroupOperation, Attrs: append(BuildOperationAttrs(),
			Attr{Tag: TagText, Name: "status-message",
				Values: [][]byte{Text("dyj: unsupported job attributes (ipp-attribute-fidelity)")}})},
		{Tag: GroupUnsupported, Attrs: unsup},
	}
	return resp
}

// formatSupported checks the declared format against the printer profile.
func (s *Server) formatSupported(format string) bool {
	switch format {
	case "", "application/octet-stream":
		return true // RFC 8011 §4.2.1.4: octet-stream means "as-is"
	}
	formats := s.printer.DocumentFormats
	if len(formats) == 0 {
		formats = attributes.DefaultFormats()
	}
	return slices.Contains(formats, format)
}

func colorModes(color bool) []string {
	if color {
		return []string{"auto", "color", "monochrome"}
	}
	return []string{"auto", "monochrome"}
}

// ExpandRequested normalises requested-attributes collection names
// (RFC 8011 §4.2.1.2 + PWG media-col-database): "all" means everything
// (nil), and the "printer-description" / "job-template" groups expand to
// their members. Unknown names are returned untouched so filtering
// simply misses them.
func ExpandRequested(want []string) []string {
	if len(want) == 0 {
		return nil
	}
	var out []string
	for _, w := range want {
		switch w {
		case "all":
			return nil
		case "printer-description":
			out = append(out, descriptionAttributes...)
		case "job-template":
			out = append(out, templateAttributeNames...)
		default:
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// descriptionAttributes is the printer-description subset we advertise,
// for requested-attributes=printer-description.
var descriptionAttributes = []string{
	"charset-configured",
	"charset-supported",
	"color-supported",
	"compression-supported",
	"document-format-default",
	"document-format-supported",
	"generated-natural-language-supported",
	"ipp-get-printer-attributes-supported",
	"ipp-versions-supported",
	"job-creation-attributes-supported",
	"media-default",
	"media-supported",
	"media-ready",
	"multiple-document-jobs-supported",
	"multiple-operation-time-out",
	"natural-language-configured",
	"operations-supported",
	"pages-per-minute",
	"printer-uuid",
	"printer-current-time",
	"printer-up-time",
	"printer-info",
	"printer-is-accepting-jobs",
	"printer-location",
	"printer-make-and-model",
	"printer-name",
	"printer-state",
	"printer-state-reasons",
	"printer-uri-supported",
	"pwg-raster-document-resolution-supported",
	"pwg-raster-document-sheet-back",
	"pwg-raster-document-type-supported",
	"queued-job-count",
	"uri-authentication-supported",
	"uri-security-supported",
}
