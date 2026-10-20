package attributes

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

// This file builds the IPP Everywhere Printer Description attribute set
// that Get-Printer-Attributes must return.
//
// References:
//   - RFC 8011 (IPP/1.1 Model and Semantics) §5.4 "Printer Description
//     Attributes" — the MUST/REQUIRED sets per conformance level.
//   - PWG 5100.14 "IPP Everywhere" §6-§7 — the extended required set.
//   - https://www.pwg.org/ipp/everywhere.html — "Required: IPP/2.0,
//     DNS-SD, PWG Raster and JPEG JFIF file formats (JPEG only required
//     for color printers). Recommended: PDF, IPP-USB."
//   - Apple AirPrint: urf-supported / mopria-certified, additional to
//     (never a replacement for) the PWG set.

// PrinterInfo is everything the attribute table needs to describe one
// printer. It is deliberately plain data: no methods, no I/O, no
// dependency on the spooler or the driver layer, so the table stays
// trivially testable and the "无巨石" rule holds.
type PrinterInfo struct {
	// URI is the canonical ipp:// URI clients must use. e.g.
	// "ipp://192.168.1.10:631/ipp/print".
	URI string
	// UUID is used for the DNS-SD "uuid" TXT key and the ipp UUID
	// attribute; optional.
	UUID string

	Name         string // printer-name (DNS-SD service instance name)
	Info         string // printer-info (free-form description)
	Location     string // printer-location (free-form location)
	MakeAndModel string // printer-make-and-model

	// Color controls color-supported and, per PWG, whether
	// image/jpeg is required in document-format-supported.
	Color bool

	// FormatsOverride replaces the default document-format-supported
	// list. Empty means "derive from Color".
	FormatsOverride []string

	// ResolutionsDPI lists the supported resolutions as
	// {cross-feed, feed} pairs in dpi. Empty means {300,300},{600,600}.
	ResolutionsDPI [][2]int32

	// MediaSupported lists PWG media size names. Empty means the
	// common A4/Letter set.
	MediaSupported []string

	// SidesSupported overrides the sides-supported list.
	SidesSupported []string

	// URISecurity and URIAuthentication parallel URI in
	// uri-security-supported / uri-authentication-supported.
	URISecurity       []string
	URIAuthentication []string

	// State is the RFC 8011 §5.4.11 printer-state value: 3 idle,
	// 4 processing, 5 stopped. Zero means idle.
	State int32

	// StateReasons is RFC 8011 §5.4.12 printer-state-reasons. Nil
	// means {"none"}.
	StateReasons []string

	// AcceptingJobs maps to printer-is-accepting-jobs.
	AcceptingJobs bool

	// QueuedJobCount maps to queued-job-count.
	QueuedJobCount int32
}

// Constants used by the default table. Values are from the IANA IPP
// registry and RFC 8011 §5.4.
const (
	// PrinterState* are RFC 8011 §5.4.11 printer-state values.
	PrinterStateIdle       int32 = 3
	PrinterStateProcessing int32 = 4
	PrinterStateStopped    int32 = 5

	// Operation* are RFC 8011 §5.4.15 operation-id values. Although the
	// numbers are described as type2 enums and their values fit in a short,
	// operations-supported is encoded with FOUR octets per element:
	// RFC 8010 §3.8 requires every binary signed integer value to be
	// exactly four octets, and §3.9 Table 7 maps "integer and enum" to a
	// SIGNED-INTEGER. See Enums.
	OperationPrintJob        int32 = 0x0002
	OperationPrintURI        int32 = 0x0003
	OperationValidateJob     int32 = 0x0004
	OperationCreateJob       int32 = 0x0005
	OperationSendDocument    int32 = 0x0006
	OperationSendURI         int32 = 0x0007
	OperationCancelJob       int32 = 0x0008
	OperationGetJobAttrs     int32 = 0x0009
	OperationGetJobs         int32 = 0x000A
	OperationGetPrinterAttrs int32 = 0x000B
	OperationIdentifyPrinter int32 = 0x003C

	// OrientationRequested* are RFC 8011 §5.4.14 / PWG 5100.14.
	OrientationPortrait    int32 = 3
	OrientationLandscape   int32 = 4
	OrientationReverseLand int32 = 5
	OrientationReversePort int32 = 6

	// PrintQuality* are RFC 8011 §5.4.10.
	PrintQualityDraft  int32 = 3
	PrintQualityNormal int32 = 4
	PrintQualityHigh   int32 = 5
)

// Default document formats are the ones dyj can actually DECODE with
// the standard library alone (see pkg/render and ADR-0007):
//
//   - image/pwg-raster — required by PWG 5100.14 for every printer.
//   - image/jpeg       — required for color printers (PWG 5100.14).
//   - image/png        — common from phones/web clients.
//   - text/plain       — rendered by dyj's own layout engine.
//
// application/pdf and image/urf are deliberately NOT declared: we have
// no renderer for either (Ghostscript is AGPL-contaminated and huge;
// Apple's URF spec is not available to us as an authoritative
// source). Declaring a format we cannot render would accept jobs and
// then drop or corrupt them — the attribute table must match reality.
var defaultFormats = []string{
	"image/pwg-raster",
	"image/jpeg",
	"image/png",
	"text/plain",
}

// DefaultFormats returns a copy of the default document-format-supported
// list (the formats pkg/render can decode; see ADR-0007).
func DefaultFormats() []string { return append([]string(nil), defaultFormats...) }

// DefaultMedia returns a copy of the default media-supported list.
func DefaultMedia() []string { return defaultMediaSupported() }

func defaultMediaSupported() []string {
	return []string{
		"iso_a4_210x297mm",
		"na_letter_8.5x11in",
		"na_legal_8.5x14in",
		"iso_a5_148x210mm",
		"na_index-4x6_4x6in",
	}
}

func defaultSidesSupported() []string {
	return []string{"one-sided", "two-sided-long-edge", "two-sided-short-edge"}
}

// BuildPrinterAttributes returns the full IPP Everywhere printer
// attribute table for info, in a deterministic order suitable for
// golden-byte testing.
//
// The returned slice is meant to be written after the
// operation-attributes group by the server. It does NOT contain
// attributes-charset / attributes-natural-language: those belong to the
// operation-attributes group and RFC 8010's ordering rule (charset
// first, natural-language second) is enforced there by go-ipp's
// Response.Encode. Re-declaring them here would be wrong.
//
// BuildPrinterAttributes never panics: every slice it indexes into is
// normalised by withDefaults, and a caller-supplied override that is
// present but empty falls back to the default rather than tripping an
// index-out-of-range.
func BuildPrinterAttributes(info PrinterInfo) []Attribute {
	info = info.withDefaults()

	attrs := make([]Attribute, 0, 64)

	// ---- charset / language: RFC 8011 §5.4.1-§5.4.4 ----
	attrs = append(attrs,
		Keyword("charset-configured", "utf-8"),
		Keywords("charset-supported", "utf-8"),
		Keyword("natural-language-configured", "en"),
		Keywords("generated-natural-language-supported", "en"),
	)

	// ---- URI attributes: RFC 8011 §5.4.5-§5.4.7 ----
	attrs = append(attrs,
		URI("printer-uri-supported", info.URI),
		Keywords("uri-security-supported", info.URISecurity...),
		Keywords("uri-authentication-supported", info.URIAuthentication...),
	)

	// ---- identification: RFC 8011 §5.4.8-§5.4.12, §5.4.18 ----
	attrs = append(attrs,
		Name("printer-name", info.Name),
		Text(TagText, "printer-info", info.Info),
		Text(TagText, "printer-location", info.Location),
		Text(TagText, "printer-make-and-model", info.MakeAndModel),
		Enum("printer-state", info.State),
		Keywords("printer-state-reasons", info.StateReasons...),
		Bool("printer-is-accepting-jobs", info.AcceptingJobs),
	)

	// ---- operations & versions: RFC 8011 §5.4.15, PWG 5100.14 §6 ----
	// operations-supported is 1setOf enum. Every element is FOUR octets per
	// RFC 8010 §3.8 / §3.9 Table 7 — not two, despite the operation-id
	// numbers themselves being shorts.
	attrs = append(attrs,
		Enums("operations-supported",
			OperationPrintJob,
			OperationValidateJob,
			OperationCreateJob,
			OperationSendDocument,
			OperationCancelJob,
			OperationGetJobAttrs,
			OperationGetJobs,
			OperationGetPrinterAttrs,
			OperationIdentifyPrinter,
		),
		// ipp-versions-supported: keyword '1.1' and '2.0'. RFC 8011
		// operates at 1.1; IPP Everywhere requires 2.0 in the list.
		Keywords("ipp-versions-supported", "1.1", "2.0"),
		// ipp-get-printer-attributes-supported: PWG 5100.14 §6.2.
		// 'false' would also be legal; we always return the full set.
		Keyword("ipp-get-printer-attributes-supported", "true"),
		// multiple-operation-time-out is an integer in seconds
		// (RFC 8011 §5.4.13 / PWG 5100.14).
		Integer("multiple-operation-time-out", 120),
		Bool("multiple-document-jobs-supported", true),
		// printer-uuid: PWG 5100.14 §7.1. A stable urn:uuid identity is
		// how clients detect that the printer they already know is still
		// the same one across restarts and mDNS renames.
		URI("printer-uuid", info.UUID),
	)

	// ---- document formats: PWG 5100.14 §6, ADR-0002 D3 ----
	attrs = append(attrs,
		MimeTypes("document-format-supported", info.FormatsOverride...),
		MimeType("document-format-default", info.FormatsOverride[0]),
	)

	// ---- color: RFC 8011 §5.4.19 ----
	attrs = append(attrs,
		Bool("color-supported", info.Color),
	)

	// ---- media: PWG 5100.14 §6, PWG 5101.1 media names ----
	attrs = append(attrs,
		Keywords("media-supported", info.MediaSupported...),
		Keyword("media-default", info.MediaSupported[0]),
		Keywords("media-ready", info.MediaSupported[0]),
		Keywords("media-type-supported", "stationery"),
	)

	// ---- sides / quality / resolution ----
	sides := info.SidesSupported
	attrs = append(attrs,
		Keywords("sides-supported", sides...),
		Keyword("sides-default", "one-sided"),
		Integers("print-quality-supported", PrintQualityDraft, PrintQualityNormal, PrintQualityHigh),
		Enum("print-quality-default", PrintQualityNormal),
		Resolutions("printer-resolution-supported", info.ResolutionsDPI...),
		Resolution("printer-resolution-default", info.ResolutionsDPI[0][0], info.ResolutionsDPI[0][1]),
	)

	// ---- PWG Raster: IPP Everywhere core ----
	// urf-supported / mopria-certified are deliberately absent: they
	// advertise Apple AirPrint capabilities we do not implement (URF
	// decoding). See the document-format comment above and ADR-0007.
	attrs = append(attrs,
		Keywords("pwg-raster-document-type-supported", "srgb_8", "sgray_8"),
		Resolutions("pwg-raster-document-resolution-supported", info.ResolutionsDPI...),
		Keyword("pwg-raster-document-sheet-back", "normal"),
	)

	// ---- job creation: PWG 5100.14 §6 ----
	attrs = append(attrs,
		Keywords("job-creation-attributes-supported",
			"copies",
			"compression",
			"document-format",
			"ipp-attribute-fidelity",
			"job-name",
			"media",
			"multiple-document-handling",
			"orientation-requested",
			"output-bin",
			"print-color-mode",
			"print-quality",
			"printer-resolution",
			"sides",
		),
		// compression-supported: RFC 8011 §5.4.33. We can decompress gzip
		// with the standard library, so we advertise it.
		Keywords("compression-supported", "none", "gzip"),
		// copies-supported is a rangeOfInteger (RFC 8011 §5.4.34).
		RangeOfInteger("copies-supported", 1, 99),
		Integer("copies-default", 1),
		// finishings-supported: RFC 8011 §5.4.35. 3 = none.
		Integers("finishings-supported", 3),
		Enum("finishings-default", 3),
		Integers("orientation-requested-supported",
			OrientationPortrait, OrientationLandscape, OrientationReverseLand, OrientationReversePort),
		Enum("orientation-requested-default", OrientationPortrait),
		Keywords("output-bin-supported", "face-down"),
		Keyword("output-bin-default", "face-down"),
		// multiple-document-handling-default per RFC 8011 §5.4.36.
		Keyword("multiple-document-handling-default", "separate-documents-uncollated-copies"),
		Keywords("multiple-document-handling-supported",
			"separate-documents-uncollated-copies",
			"separate-documents-collated-copies"),
		Keyword("print-color-mode-default", printColorModeDefault(info.Color)),
		Keywords("print-color-mode-supported", printColorModeSupported(info.Color)...),
		Keyword("print-scaling-default", "auto"),
		Keywords("print-scaling-supported", "auto", "auto-fit", "fill", "fit", "none"),
	)

	// ---- status / timestamps: RFC 8011 §5.4.30-§5.4.32 ----
	now := time.Now()
	up := int32(uptimeSeconds())
	tzDir, tzHours, tzMinutes := utcOffset(now)
	dateTime := func(name string) Attribute {
		return DateTime(name,
			int16(now.Year()), byte(now.Month()), byte(now.Day()),
			byte(now.Hour()), byte(now.Minute()), byte(now.Second()),
			byte(now.Nanosecond()/100_000_000),
			tzDir, tzHours, tzMinutes)
	}
	attrs = append(attrs,
		// printer-up-time: seconds since the printer was started.
		Integer("printer-up-time", up),
		dateTime("printer-current-time"),
		dateTime("printer-config-change-date-time"),
		dateTime("printer-state-change-date-time"),
		Integer("printer-config-change-time", up),
		Integer("printer-state-change-time", up),
		Integer("queued-job-count", info.QueuedJobCount),
	)

	return attrs
}

// utcOffset renders the zone offset of t as the dateTime fields the IPP
// dateTime syntax expects (RFC 8010 §3.5.2 Table 5): a direction byte
// ('+' or '-'), the offset hours and the offset minutes. The RFC's
// dateTime carries LOCAL time fields plus this offset, so emitting local
// wall-clock time with a computed offset (rather than claiming UTC) is
// the correct form.
func utcOffset(t time.Time) (dir byte, hours, minutes byte) {
	_, off := t.Zone()
	if off < 0 {
		dir = '-'
		off = -off
	} else {
		dir = '+'
	}
	return dir, byte(off / 3600), byte((off % 3600) / 60)
}

func printColorModeDefault(color bool) string {
	if color {
		return "auto"
	}
	return "monochrome"
}

func printColorModeSupported(color bool) []string {
	if color {
		return []string{"auto", "color", "monochrome"}
	}
	return []string{"auto", "monochrome"}
}

// RequiredPrinterAttributes is the IPP Everywhere / RFC 8011 REQUIRED
// printer-description set this project must always emit. It exists so
// tests (and a future runtime self-check) can assert completeness
// against one authoritative list rather than a hand-copied one.
//
// Sources: RFC 8011 §5.4 (REQUIRED column) plus PWG 5100.14 §6-§7.
// attributes-charset / attributes-natural-language are intentionally
// absent: they belong to the operation-attributes group.
var RequiredPrinterAttributes = []string{
	// RFC 8011 §5.4 required, IPP Everywhere required
	"charset-configured",
	"charset-supported",
	"natural-language-configured",
	"generated-natural-language-supported",
	"printer-uri-supported",
	"uri-security-supported",
	"uri-authentication-supported",
	"printer-name",
	"printer-state",
	"printer-state-reasons",
	"printer-is-accepting-jobs",
	"operations-supported",
	"ipp-versions-supported",
	"document-format-supported",
	"color-supported",
	"media-supported",
	"media-default",
	"sides-supported",
	"print-quality-supported",
	"printer-resolution-supported",
	"printer-resolution-default",
	"compression-supported",
	"copies-supported",

	// PWG 5100.14 IPP Everywhere required
	"printer-info",
	"printer-location",
	"printer-make-and-model",
	"document-format-default",
	"ipp-get-printer-attributes-supported",
	"job-creation-attributes-supported",
	"media-ready",
	"multiple-document-jobs-supported",
	"multiple-operation-time-out",
	"natural-language-configured",
	"printer-uuid",
	"pwg-raster-document-type-supported",
	"pwg-raster-document-resolution-supported",
	"pwg-raster-document-sheet-back",
	"sides-default",
	"print-quality-default",
	"copies-default",
	"finishings-supported",
	"orientation-requested-supported",
	"output-bin-supported",
	"printer-up-time",
	"printer-config-change-date-time",
	"printer-state-change-date-time",
	"printer-state-change-time",
	"printer-current-time",
	"queued-job-count",
}

// ValidatePrinterAttributes reports every name in
// RequiredPrinterAttributes that is absent from attrs, plus structural
// problems that would produce an unparseable response.
//
// It is used by tests today and is intended to be called once at printer
// startup so a missing mandatory attribute is a loud failure rather than
// a client that silently refuses to print.
func ValidatePrinterAttributes(attrs []Attribute) error {
	seen := make(map[string]bool, len(attrs))
	var problems []string

	for _, a := range attrs {
		if a.Name == "" {
			problems = append(problems, "attribute with empty name")
			continue
		}
		if seen[a.Name] {
			problems = append(problems, fmt.Sprintf("attribute %q appears more than once", a.Name))
		}
		seen[a.Name] = true

		if len(a.Values) == 0 {
			problems = append(problems, fmt.Sprintf("attribute %q has no values", a.Name))
		}
		if a.Tag < 0x10 {
			problems = append(problems, fmt.Sprintf("attribute %q uses delimiter tag 0x%02x", a.Name, a.Tag))
		}
	}

	var missing []string
	for _, want := range RequiredPrinterAttributes {
		if !seen[want] {
			missing = append(missing, want)
		}
	}
	sort.Strings(missing)
	sort.Strings(problems)

	for _, m := range missing {
		problems = append(problems, fmt.Sprintf("missing required attribute %q", m))
	}
	if len(problems) > 0 {
		return fmt.Errorf("attributes: invalid printer attribute set:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

func (info PrinterInfo) withDefaults() PrinterInfo {
	if info.Name == "" {
		info.Name = "dyj printer"
	}
	if info.UUID == "" {
		// Stable identity derived from the printer name. It changes only
		// when the operator renames the printer, which is exactly when a
		// client SHOULD treat it as a new printer. Deployment (cmd/dyj)
		// normally overrides this with a persisted random UUID.
		info.UUID = "urn:uuid:" + DeriveUUID("dyj-printer:"+info.Name)
	}
	if info.MakeAndModel == "" {
		info.MakeAndModel = "dyj IPP Everywhere Printer"
	}
	if info.State == 0 {
		info.State = PrinterStateIdle
	}
	if len(info.StateReasons) == 0 {
		info.StateReasons = []string{"none"}
	}
	if len(info.FormatsOverride) == 0 {
		info.FormatsOverride = append([]string(nil), defaultFormats...)
	}
	if len(info.MediaSupported) == 0 {
		info.MediaSupported = defaultMediaSupported()
	}
	if len(info.SidesSupported) == 0 {
		info.SidesSupported = defaultSidesSupported()
	}
	if len(info.ResolutionsDPI) == 0 {
		info.ResolutionsDPI = [][2]int32{{300, 300}, {600, 600}}
	}
	if len(info.URISecurity) == 0 {
		info.URISecurity = []string{"none"}
	}
	if len(info.URIAuthentication) == 0 {
		info.URIAuthentication = []string{"requesting-user-name"}
	}
	return info
}

// ValidateURI checks that URI is an ipp:// or ipps:// URI with a host
// and a path. Returned error is safe to show to an operator.
func ValidateURI(uri string) error {
	scheme, rest, found := strings.Cut(uri, "://")
	if !found {
		return fmt.Errorf("attributes: printer URI %q has no scheme", uri)
	}
	if scheme != "ipp" && scheme != "ipps" {
		return fmt.Errorf("attributes: printer URI scheme %q must be ipp or ipps", scheme)
	}
	hostport, path, _ := strings.Cut(rest, "/")
	if hostport == "" {
		return fmt.Errorf("attributes: printer URI %q has no host", uri)
	}
	if path == "" {
		return fmt.Errorf("attributes: printer URI %q has no path (RFC 8011 requires e.g. /ipp/print)", uri)
	}
	if host, _, err := net.SplitHostPort(hostport); err == nil && host == "" {
		return fmt.Errorf("attributes: printer URI %q has an empty host", uri)
	}
	return nil
}

// uptimeSeconds is a package-level hook so tests can freeze it.
var uptimeSeconds = func() int64 {
	return int64(time.Since(startTime) / time.Second)
}

var startTime = time.Now()
