package attributes

import (
	"bytes"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
)

// testPrinter is a realistic colour-printer configuration matching this
// project's target machine (an Epson colour inkjet), per ADR-0002 D3.
func testPrinter() PrinterInfo {
	return PrinterInfo{
		URI:            "ipp://192.168.1.10:631/ipp/print",
		Name:           "dyj Epson L3250",
		Info:           "Shared via dyj",
		Location:       "Home LAN",
		MakeAndModel:   "Epson L3250 (dyj IPP Everywhere)",
		Color:          true,
		AcceptingJobs:  true,
		QueuedJobCount: 0,
	}
}

func attrMap(attrs []Attribute) map[string]Attribute {
	m := make(map[string]Attribute, len(attrs))
	for _, a := range attrs {
		m[a.Name] = a
	}
	return m
}

// TestEverywhereTableIsComplete is the main acceptance test: every
// attribute in the IPP Everywhere / RFC 8011 REQUIRED set must be
// present, and ValidatePrinterAttributes must agree.
func TestEverywhereTableIsComplete(t *testing.T) {
	attrs := BuildPrinterAttributes(testPrinter())

	if err := ValidatePrinterAttributes(attrs); err != nil {
		t.Fatalf("ValidatePrinterAttributes reported problems:\n%v", err)
	}

	got := attrMap(attrs)
	for _, want := range RequiredPrinterAttributes {
		a, ok := got[want]
		if !ok {
			t.Errorf("missing required attribute %q", want)
			continue
		}
		if len(a.Values) == 0 {
			t.Errorf("attribute %q has no values", want)
		}
	}
}

// TestAttributeNamesAreUnique guards the encoder's structural
// precondition: RFC 8010 §3.1.3 gives one attribute-with-one-value per
// name, so emitting the same name twice in a group is a bug.
func TestAttributeNamesAreUnique(t *testing.T) {
	attrs := BuildPrinterAttributes(testPrinter())
	seen := map[string]int{}
	for _, a := range attrs {
		seen[a.Name]++
	}
	var dupes []string
	for name, n := range seen {
		if n > 1 {
			dupes = append(dupes, name)
		}
	}
	if len(dupes) > 0 {
		sort.Strings(dupes)
		t.Fatalf("duplicate attribute names: %v", dupes)
	}
}

// TestNamesFollowRFC8010Grammar asserts RFC 8010 §3.7:
// name = LALPHA *( LALPHA / DIGIT / "-" / "_" / "." )
func TestNamesFollowRFC8010Grammar(t *testing.T) {
	for _, a := range BuildPrinterAttributes(testPrinter()) {
		if a.Name == "" {
			t.Error("empty attribute name")
			continue
		}
		for i := 0; i < len(a.Name); i++ {
			c := a.Name[i]
			ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
			if !ok {
				t.Errorf("attribute name %q contains invalid byte 0x%02x at %d", a.Name, c, i)
				break
			}
			if i == 0 && !(c >= 'a' && c <= 'z') {
				t.Errorf("attribute name %q must start with LALPHA", a.Name)
				break
			}
		}
	}
}

// TestColorPrinterAdvertisesJPEG is the PWG rule that matters for this
// project's target machine: "PWG Raster and JPEG JFIF file formats
// (JPEG only required for color printers)".
// image/urf must never replace PWG Raster.
func TestColorPrinterAdvertisesJPEG(t *testing.T) {
	attrs := attrMap(BuildPrinterAttributes(testPrinter()))

	df, ok := attrs["document-format-supported"]
	if !ok {
		t.Fatal("document-format-supported missing")
	}
	var formats []string
	for _, v := range df.Values {
		formats = append(formats, string(v))
	}

	hasPWG, hasJPEG, hasPDF := false, false, false
	for _, f := range formats {
		switch f {
		case "image/pwg-raster":
			hasPWG = true
		case "image/jpeg":
			hasJPEG = true
		case "application/pdf":
			hasPDF = true
		}
	}
	if !hasPWG {
		t.Error("colour printer must advertise image/pwg-raster (PWG required)")
	}
	if !hasJPEG {
		t.Error("colour printer must advertise image/jpeg (PWG requires JPEG JFIF for colour printers)")
	}
	if hasPDF {
		t.Error("printer must NOT advertise application/pdf: dyj has no PDF renderer (ADR-0007); declaring it would accept jobs and corrupt them")
	}

	// document-format-default must be one of the supported values.
	def := attrs["document-format-default"]
	if len(def.Values) != 1 {
		t.Fatalf("document-format-default should be single-valued, got %d values", len(def.Values))
	}
	defVal := string(def.Values[0])
	if !contains(formats, defVal) {
		t.Errorf("document-format-default %q is not in document-format-supported %v", defVal, formats)
	}
}

// TestDefaultsAreWithinSupportedSets checks the general IPP invariant
// that every *-default value appears in the corresponding *-supported
// list. Clients reject a printer that violates this.
func TestDefaultsAreWithinSupportedSets(t *testing.T) {
	attrs := attrMap(BuildPrinterAttributes(testPrinter()))

	pairs := [][2]string{
		{"document-format-default", "document-format-supported"},
		{"media-default", "media-supported"},
		{"sides-default", "sides-supported"},
		{"print-quality-default", "print-quality-supported"},
		{"orientation-requested-default", "orientation-requested-supported"},
		{"output-bin-default", "output-bin-supported"},
		{"finishings-default", "finishings-supported"},
		{"print-color-mode-default", "print-color-mode-supported"},
		{"print-scaling-default", "print-scaling-supported"},
		{"copies-default", "copies-supported"},
	}
	for _, p := range pairs {
		def, ok := attrs[p[0]]
		if !ok {
			t.Errorf("missing %s", p[0])
			continue
		}
		sup, ok := attrs[p[1]]
		if !ok {
			t.Errorf("missing %s", p[1])
			continue
		}
		if len(def.Values) != 1 {
			t.Errorf("%s should be single-valued, got %d", p[0], len(def.Values))
			continue
		}
		raw := def.Values[0]
		found := false
		for _, sv := range sup.Values {
			if bytes.Equal(raw, sv) {
				found = true
				break
			}
		}
		// copies-default is an int and copies-supported is a range, so
		// compare numerically in that one case.
		if !found && p[0] == "copies-default" {
			found = rangeContains(sup.Values[0], raw)
		}
		if !found {
			t.Errorf("%s = %s is not present in %s", p[0],
				hex.EncodeToString(raw), p[1])
		}
	}
}

// rangeContains reports whether the 4-octet int in v falls inside the
// 8-octet lower/upper bounds encoded in r.
func rangeContains(r, v []byte) bool {
	if len(r) != 8 || len(v) != 4 {
		return false
	}
	lo := int32(uint32(r[0])<<24 | uint32(r[1])<<16 | uint32(r[2])<<8 | uint32(r[3]))
	hi := int32(uint32(r[4])<<24 | uint32(r[5])<<16 | uint32(r[6])<<8 | uint32(r[7]))
	x := int32(uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3]))
	return x >= lo && x <= hi
}

// TestOperationsSupportedUsesFourOctetEnums is the focused check that the
// IPP Everywhere table does not reintroduce the 2-octet enum bug.
func TestOperationsSupportedUsesFourOctetEnums(t *testing.T) {
	attrs := attrMap(BuildPrinterAttributes(testPrinter()))
	op, ok := attrs["operations-supported"]
	if !ok {
		t.Fatal("operations-supported missing")
	}
	if op.Tag != TagEnum {
		t.Fatalf("operations-supported tag = 0x%02x, want 0x23 (enum)", op.Tag)
	}
	for i, v := range op.Values {
		if len(v) != 4 {
			t.Errorf("operations-supported value %d is %d octets, RFC 8010 §3.8 requires 4", i, len(v))
		}
	}
	// Must include the operations the server actually implements.
	wantOps := map[int32]string{
		OperationPrintJob:        "Print-Job",
		OperationValidateJob:     "Validate-Job",
		OperationCreateJob:       "Create-Job",
		OperationSendDocument:    "Send-Document",
		OperationCancelJob:       "Cancel-Job",
		OperationGetJobAttrs:     "Get-Job-Attributes",
		OperationGetJobs:         "Get-Jobs",
		OperationGetPrinterAttrs: "Get-Printer-Attributes",
	}
	have := map[int32]bool{}
	for _, v := range op.Values {
		n := int32(uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3]))
		have[n] = true
	}
	for opID, name := range wantOps {
		if !have[opID] {
			t.Errorf("operations-supported missing 0x%04x (%s)", opID, name)
		}
	}

	// And the golden bytes for the real table.
	got, err := op.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := "23" + "0014" + "6f7065726174696f6e732d737570706f72746564" +
		"0004" + "00000002" + // Print-Job
		"230000" + "0004" + "00000004" + // Validate-Job
		"230000" + "0004" + "00000005" + // Create-Job
		"230000" + "0004" + "00000006" + // Send-Document
		"230000" + "0004" + "00000008" + // Cancel-Job
		"230000" + "0004" + "00000009" + // Get-Job-Attributes
		"230000" + "0004" + "0000000a" + // Get-Jobs
		"230000" + "0004" + "0000000b" + // Get-Printer-Attributes
		"230000" + "0004" + "0000003c" // Identify-Printer
	if hex.EncodeToString(got) != want {
		t.Fatalf("operations-supported golden mismatch\ngot  %s\nwant %s", hex.EncodeToString(got), want)
	}
}

// TestPrinterURISupportedGolden pins the exact bytes of the attribute
// that tells a client where to send jobs.
func TestPrinterURISupportedGolden(t *testing.T) {
	attrs := attrMap(BuildPrinterAttributes(testPrinter()))
	a := attrs["printer-uri-supported"]
	got, err := a.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	uri := "ipp://192.168.1.10:631/ipp/print"
	want := "45" + "0015" + "7072696e7465722d7572692d737570706f72746564" +
		"0020" + hex.EncodeToString([]byte(uri))
	if hex.EncodeToString(got) != want {
		t.Fatalf("printer-uri-supported golden mismatch\ngot  %s\nwant %s", hex.EncodeToString(got), want)
	}
}

// TestDocumentFormatSupportedGolden pins the bytes of the attribute
// go-ipp literally cannot emit (no AttributeTagMapping entry).
func TestDocumentFormatSupportedGolden(t *testing.T) {
	attrs := attrMap(BuildPrinterAttributes(testPrinter()))
	got, err := attrs["document-format-supported"].Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := "49" + "0019" + "646f63756d656e742d666f726d61742d737570706f72746564" +
		"0010" + "696d6167652f7077672d726173746572" +
		"490000" + "000a" + "696d6167652f6a706567" +
		"490000" + "0009" + "696d6167652f706e67" +
		"490000" + "000a" + "746578742f706c61696e"
	if hex.EncodeToString(got) != want {
		t.Fatalf("document-format-supported golden mismatch\ngot  %s\nwant %s", hex.EncodeToString(got), want)
	}
}

// TestWholePrinterGroupIsEncodable proves the entire table can actually
// be serialized and re-walked, i.e. no attribute is structurally broken.
func TestWholePrinterGroupIsEncodable(t *testing.T) {
	g := AttributeGroup{
		Delimiter:  TagPrinterAttributes,
		Attributes: BuildPrinterAttributes(testPrinter()),
	}
	raw, err := g.Marshal()
	if err != nil {
		t.Fatalf("Marshal whole printer group: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("encoded group is empty")
	}
	if raw[0] != TagPrinterAttributes {
		t.Fatalf("group does not start with 0x04, got 0x%02x", raw[0])
	}
	// Every attribute after the first byte must be walkable.
	if err := walkAttributes(raw[1:]); err != nil {
		t.Fatalf("encoded printer group is not well-formed: %v", err)
	}
}

// TestMonoPrinterOmitsJPEGRequirement documents the colour-vs-mono rule
// so it cannot silently invert.
func TestMonoPrinterAdvertisesPWGAndPDF(t *testing.T) {
	info := testPrinter()
	info.Color = false
	attrs := attrMap(BuildPrinterAttributes(info))

	if got := attrs["color-supported"]; len(got.Values) != 1 || got.Values[0][0] != 0x00 {
		t.Errorf("color-supported should encode false for a mono printer")
	}
	var formats []string
	for _, v := range attrs["document-format-supported"].Values {
		formats = append(formats, string(v))
	}
	if !contains(formats, "image/pwg-raster") {
		t.Error("mono printer must still advertise image/pwg-raster")
	}
	// dyj accepts JPEG for mono printers too: a grayscale JPEG is
	// perfectly printable, and the render pipeline converts colour JPEGs
	// to gray. PWG only makes JPEG a REQUIREMENT for colour printers.
	if contains(formats, "application/pdf") || contains(formats, "image/urf") {
		t.Error("formats dyj cannot render (pdf/urf) must not be advertised")
	}
}

// TestValidateRejectsIncompleteTable proves the validator is not a no-op.
func TestValidateRejectsIncompleteTable(t *testing.T) {
	// A table missing everything.
	err := ValidatePrinterAttributes([]Attribute{Keyword("printer-name", "x")})
	if err == nil {
		t.Fatal("want error for an incomplete table")
	}
	msg := err.Error()
	for _, want := range []string{"document-format-supported", "operations-supported", "printer-uuid"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should name missing attribute %q, got:\n%s", want, msg)
		}
	}

	// A duplicate is also a structural error.
	good := BuildPrinterAttributes(testPrinter())
	dup := append(append([]Attribute{}, good...), Keyword("printer-name", "again"))
	if err := ValidatePrinterAttributes(dup); err == nil {
		t.Fatal("want error for duplicate attribute name")
	}
}

// TestValidateURIAcceptsAndRejects covers the URI shape check.
func TestValidateURI(t *testing.T) {
	valid := []string{
		"ipp://192.168.1.10:631/ipp/print",
		"ipps://printer.local:631/ipp/print",
		"ipp://[fe80::1]:631/ipp/print",
	}
	for _, u := range valid {
		if err := ValidateURI(u); err != nil {
			t.Errorf("ValidateURI(%q) = %v, want nil", u, err)
		}
	}
	invalid := []string{
		"192.168.1.10:631/ipp/print", // no scheme
		"http://192.168.1.10/ipp/print",
		"ipp:///ipp/print",   // no host
		"ipp://192.168.1.10", // no path
		"",
	}
	for _, u := range invalid {
		if err := ValidateURI(u); err == nil {
			t.Errorf("ValidateURI(%q) = nil, want error", u)
		}
	}
}

// TestBuildNeverPanicsOnEmptyOverrides covers the edge case where a caller
// sets an override field to an explicitly EMPTY (non-nil) slice. Every
// slice the builder indexes into must still be non-empty, otherwise
// FormatsOverride[0] / MediaSupported[0] / ResolutionsDPI[0] would panic.
func TestBuildNeverPanicsOnEmptyOverrides(t *testing.T) {
	cases := map[string]PrinterInfo{
		"empty formats":     {FormatsOverride: []string{}},
		"empty media":       {MediaSupported: []string{}},
		"empty sides":       {SidesSupported: []string{}},
		"empty resolutions": {ResolutionsDPI: [][2]int32{}},
		"empty security":    {URISecurity: []string{}},
		"empty auth":        {URIAuthentication: []string{}},
		"empty reasons":     {StateReasons: []string{}},
		"all empty": {
			FormatsOverride:   []string{},
			MediaSupported:    []string{},
			SidesSupported:    []string{},
			ResolutionsDPI:    [][2]int32{},
			URISecurity:       []string{},
			URIAuthentication: []string{},
			StateReasons:      []string{},
		},
		"zero value struct": {},
	}
	for name, info := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("BuildPrinterAttributes panicked: %v", r)
				}
			}()
			attrs := BuildPrinterAttributes(info)
			if len(attrs) == 0 {
				t.Fatal("no attributes produced")
			}
			// The table must still be complete and encodable.
			if err := ValidatePrinterAttributes(attrs); err != nil {
				t.Fatalf("table incomplete for zero-ish PrinterInfo: %v", err)
			}
			if _, err := MarshalAttributes(attrs); err != nil {
				t.Fatalf("table not encodable: %v", err)
			}
		})
	}
}

// TestBuildUsesProvidedURIWhenSet checks the printer-uri-supported value
// actually reflects the caller's URI rather than a placeholder.
func TestBuildUsesProvidedURIWhenSet(t *testing.T) {
	info := testPrinter()
	attrs := attrMap(BuildPrinterAttributes(info))
	got := string(attrs["printer-uri-supported"].Values[0])
	if got != info.URI {
		t.Fatalf("printer-uri-supported = %q, want %q", got, info.URI)
	}
}

// TestBuildIsDeterministic guards against map iteration sneaking into the
// table, which would make golden-byte tests flaky and responses
// unstable.
func TestBuildIsDeterministic(t *testing.T) {
	a := BuildPrinterAttributes(testPrinter())
	var first, second []byte
	var err error
	first, err = MarshalAttributes(a)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for i := 0; i < 20; i++ {
		second, err = MarshalAttributes(BuildPrinterAttributes(testPrinter()))
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		// The dateTime attributes use time.Now, so compare names and
		// value lengths/structure rather than bytes for those.
		if got, want := attrNames(second), attrNames(first); !equalStrings(got, want) {
			t.Fatalf("attribute order changed between builds:\n got %v\nwant %v", got, want)
		}
	}
}

func attrNames(b []byte) []string {
	var names []string
	i := 0
	for i < len(b) {
		if i+5 > len(b) {
			break
		}
		nameLen := int(b[i+1])<<8 | int(b[i+2])
		if i+3+nameLen+2 > len(b) {
			break
		}
		if nameLen > 0 {
			names = append(names, string(b[i+3:i+3+nameLen]))
		}
		valLen := int(b[i+3+nameLen])<<8 | int(b[i+3+nameLen+1])
		i += 3 + nameLen + 2 + valLen
	}
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
