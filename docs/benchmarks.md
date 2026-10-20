# Performance baselines

Benchmarks for the hot paths of the print pipeline. Re-run before/after any
performance-relevant change:

```sh
go test -run XXX -bench . -benchtime 2s ./pkg/render/ ./pkg/attributes/ ./pkg/ipp/
```

Baseline recorded 2026-10-20 — go1.27.3, windows/amd64, AMD Ryzen 5 5600G
(12 hardware threads), idle machine, three consecutive runs per benchmark.
Encode figures are after the inline pixel-compare fix (pixEqual) and decode
after the exponential run expansion; the original pre-fix baselines were
220.7 MB/s (encode gray) and 358.3 MB/s (decode gray).

| Benchmark | ns/op | Throughput | Notes |
|---|---|---|---|
| `render/PWGEncodeA4Gray` | 13.6–14.1 ms | 615–640 MB/s | A4@300dpi sgray, mixed run/literal page |
| `render/PWGDecodeA4Gray` | 4.1–4.3 ms | 2009–2144 MB/s | decode of the same stream |
| `render/PWGEncodeLetterColor` | 22.0–26.2 ms | 964–1146 MB/s | Letter@300dpi sRGB |
| `attributes/BuildPrinterAttributes` | 7.0–10.3 µs | — | full IPP Everywhere table assembly |
| `attributes/EncodePrinterAttributes` | 8.7–9.1 µs | 314–330 MB/s | wire encoding of that table |
| `attributes/DecodePrinterAttributes` | 23.6–33.4 µs | 86–122 MB/s | strict streaming decode of the same |
| `ipp/HandleGetPrinterAttributes` | 40.8 µs | — | full HTTP→decode→build→encode round trip (logger silenced) |

Reading the table:

- A 100 MB photo spool compresses in well under half a second per 40 MB
  page set on this class of machine — the PWG codec, not the network, is
  the share-side bound for big jobs.
- The Windows add-printer probe (`Get-Printer-Attributes` unfiltered) costs
  ~41 µs of protocol work per request; table build + encode is a quarter of
  that, the rest is HTTP recorder overhead and response framing.
- The `ipp` benchmark silences the server logger (`log.New(io.Discard, ...)`)
  so numbers measure protocol work; the production path also writes one log
  line per request, which is invisible at print rates.

Noise guidance, measured: on an idle machine consecutive runs stay within
~±5 %, but ordinary background load on the same box depressed absolute
numbers by 40 %+ across sessions (encode gray measured 359–415 MB/s under
load vs 615–640 idle, same binary). **Never compare absolute numbers across
sessions** — run the before/after benchmarks back to back in one session and
compare those. Profiles (relative hotspots) are load-robust where absolute
throughput is not.
