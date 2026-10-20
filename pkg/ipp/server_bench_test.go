package ipp

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkHandleGetPrinterAttributes drives the full unfiltered
// Get-Printer-Attributes path: HTTP → decode → attribute-table build →
// encode. This is the request Windows issues when adding the printer, so
// its latency is directly visible in the add-printer flow. The logger is
// silenced so the numbers measure protocol work, not stderr writes.
func BenchmarkHandleGetPrinterAttributes(b *testing.B) {
	s := NewServer(Options{Printer: DefaultPrinter(), Logger: log.New(io.Discard, "", 0)})
	body := encodeGetPrinterAttrs(b, 1) // no requested-attributes = full table
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRequest(http.MethodPost, "/ipp/print", bytes.NewReader(body))
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		s.handle(w, r)
		if w.Code != http.StatusOK {
			b.Fatalf("status %d", w.Code)
		}
	}
}
