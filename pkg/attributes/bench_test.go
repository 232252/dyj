package attributes

import (
	"bytes"
	"testing"
)

func benchPrinterInfo() PrinterInfo {
	return PrinterInfo{
		URI:           "ipp://127.0.0.1:6631/ipp/print",
		UUID:          "urn:uuid:00000000-0000-4000-8000-000000000001",
		Name:          "dyj on bench",
		Info:          "dyj shared printer",
		Location:      "bench",
		MakeAndModel:  "dyj IPP Everywhere Printer",
		Color:         true,
		State:         PrinterStateIdle,
		AcceptingJobs: true,
	}
}

// BuildPrinterAttributes assembles the whole IPP Everywhere table — this is
// the cost of every Get-Printer-Attributes without requested-attributes
// filtering (the Windows add-printer probe).
func BenchmarkBuildPrinterAttributes(b *testing.B) {
	info := benchPrinterInfo()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildPrinterAttributes(info)
	}
}

func BenchmarkEncodePrinterAttributes(b *testing.B) {
	attrs := BuildPrinterAttributes(benchPrinterInfo())
	var sizing bytes.Buffer
	if err := WriteAttributes(&sizing, attrs); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(sizing.Len()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		if err := WriteAttributes(&buf, attrs); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodePrinterAttributes(b *testing.B) {
	attrs := BuildPrinterAttributes(benchPrinterInfo())
	var buf bytes.Buffer
	// Frame a full message: 8-byte header (version 2.0, code 0, request-id 1),
	// one printer-attributes group delimiter, then the attribute stream.
	buf.Write([]byte{2, 0, 0, 0, 0, 0, 0, 1})
	buf.WriteByte(TagPrinterAttributes)
	if err := WriteAttributes(&buf, attrs); err != nil {
		b.Fatal(err)
	}
	buf.WriteByte(TagEndOfAttributes)
	stream := buf.Bytes()
	b.SetBytes(int64(len(stream)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeMessageStream(bytes.NewReader(stream)); err != nil {
			b.Fatal(err)
		}
	}
}
