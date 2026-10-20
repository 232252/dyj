# Performance baselines

Benchmarks for the hot paths of the print pipeline. Re-run before/after any
performance-relevant change:

```sh
go test -run XXX -bench . -benchtime 2s ./pkg/render/ ./pkg/attributes/ ./pkg/ipp/
```

Baseline recorded 2026-10-20 — go1.27.3, windows/amd64, AMD Ryzen 5 5600G
(12 hardware threads). Encode figures are after the inline pixel-compare
fix (pixEqual) that followed the first profile; the pre-fix gray-encode
baseline was 220.7 MB/s.

| Benchmark | ns/op | Throughput | Notes |
|---|---|---|---|
| `render/PWGEncodeA4Gray` | 20.9–24.2 ms | 359–415 MB/s | A4@300dpi sgray, mixed run/literal page |
| `render/PWGDecodeA4Gray` | 24.3 ms | 358.3 MB/s | decode of the same stream |
| `render/PWGEncodeLetterColor` | 36.7–41.8 ms | 604–688 MB/s | Letter@300dpi sRGB |
| `attributes/BuildPrinterAttributes` | 9.3 µs | — | full IPP Everywhere table assembly |
| `attributes/EncodePrinterAttributes` | 10.2 µs | 282.2 MB/s | wire encoding of that table |
| `attributes/DecodePrinterAttributes` | 26.8 µs | 107.6 MB/s | strict streaming decode of the same |
| `ipp/HandleGetPrinterAttributes` | 40.8 µs | — | full HTTP→decode→build→encode round trip (logger silenced) |

Reading the table:

- A 100 MB photo spool compresses in roughly 0.5 s per 40 MB page set on this
  class of machine — the PWG codec, not the network, is the share-side bound
  for big jobs.
- The Windows add-printer probe (`Get-Printer-Attributes` unfiltered) costs
  ~41 µs of protocol work per request; table build + encode is a quarter of
  that, the rest is HTTP recorder overhead and response framing.
- The `ipp` benchmark silences the server logger (`log.New(io.Discard, ...)`)
  so numbers measure protocol work; the production path also writes one log
  line per request, which is invisible at print rates.

When a change regresses any number by more than run-to-run noise (~±10 % on
this machine), treat it as a negative optimization and revert.
