package ipp

import (
	"github.com/232252/dyj/pkg/attributes"
)

// This file is the adapter between the transport layer and the single
// attribute-encoding layer. It contains no tag constants, no encoding loop and
// no attribute table of its own: pkg/attributes owns all of that
// (ADR-0002 D4). It exists only to merge the printer group returned by the
// attribute table with the dynamic bits this server knows at request time.

// BuildPrinterAttrs assembles the printer-attributes group for a
// Get-Printer-Attributes response.
//
// The attribute table comes from pkg/attributes.BuildPrinterAttributes. The
// per-process dynamic values (queued-job-count, printer-state-reasons,
// accepting-jobs) come from the JobSink's status hook.
func (s *Server) BuildPrinterAttrs(p Printer, want []string) []Attr {
	attrs := attributes.BuildPrinterAttributes(p.info(statusHook(s.sink)))
	out := FilterAttrs(attrs, want)
	if len(out) == 0 {
		// RFC 8011 lets the server return a minimal set for an unknown
		// requested-attributes list, but a completely empty printer group
		// makes clients give up. Keep the identity attributes.
		out = FilterAttrs(attrs, []string{
			"printer-name",
			"printer-state",
			"printer-state-reasons",
			"printer-is-accepting-jobs",
			"printer-uri-supported",
		})
	}
	return out
}

// BuildPrinterAttrNames lists the attribute names the table can emit. Useful
// for tests and diagnostics.
func BuildPrinterAttrNames(p Printer) []string {
	attrs := attributes.BuildPrinterAttributes(p.info(nil))
	out := make([]string, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, a.Name)
	}
	return out
}

// ValidatePrinter checks the table against the IPP Everywhere required set, so
// a missing mandatory attribute fails loudly at startup instead of silently
// producing a printer no client will accept.
func ValidatePrinter(p Printer) error {
	return attributes.ValidatePrinterAttributes(attributes.BuildPrinterAttributes(p.info(nil)))
}
