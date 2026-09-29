package main

import (
	"strings"
	"testing"
)

func TestEncodeForSubmissionPDF(t *testing.T) {
	pdf := []byte("%PDF-1.4\n%test bytes")
	format, body, err := encodeForSubmission("doc.pdf", pdf)
	if err != nil {
		t.Fatal(err)
	}
	if format != "application/pdf" {
		t.Errorf("format = %q, want application/pdf", format)
	}
	if &body[0] != &pdf[0] {
		// Passthrough must not re-encode: rasterizing a PDF client-side
		// would defeat the whole feature.
		t.Error("pdf body was copied instead of passed through")
	}
}

func TestEncodeForSubmissionFakePDFRejected(t *testing.T) {
	if _, _, err := encodeForSubmission("fake.pdf", []byte("just some text")); err == nil {
		t.Fatal("renamed text file must be rejected, not submitted as PDF")
	}
}

func TestEncodeForSubmissionPassthroughs(t *testing.T) {
	if format, _, err := encodeForSubmission("a.pwg", []byte("PWGDATA")); err != nil || format != "image/pwg-raster" {
		t.Errorf("pwg passthrough = (%q, %v)", format, err)
	}
	if _, _, err := encodeForSubmission("a.docx", []byte("x")); err == nil || !strings.Contains(err.Error(), ".docx") {
		t.Errorf("unknown extension error = %v", err)
	}
}
