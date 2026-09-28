# Changelog

All notable changes to this project are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/); versioning: semver.

## [0.1.0] — 2026-09-29

First working delivery: full server + client, all platforms.

### Added
- **Server** (`dyj serve`): IPP 2.0 endpoint with the complete
  driverless operation set — Get-Printer-Attributes, Print-Job,
  Validate-Job, Create-Job + Send-Document (multi-document),
  Get-Jobs, Get-Job-Attributes, Cancel-Job, Identify-Printer.
  gzip document compression (IPP-level and HTTP-level),
  `ipp-attribute-fidelity` rejection with unsupported-attributes
  group, optional ipps:// (self-signed TLS), mDNS/DNS-SD
  advertisement with per-interface host names and conflict probing.
- **Spooler**: job state machine (held→pending→processing→completed/
  canceled/aborted), streaming to-disk spool, crash recovery
  (requeue pending jobs; docs are only recorded after their file is
  complete), bounded finished-job history.
- **Render pipeline**: byte-exact PWG Raster (PWG 5102.4) reader and
  writer against the CUPS implementation; text layout engine
  (bitmap font, word wrap, pagination); JPEG/PNG rasterization to
  page bitmaps (fit-scale, sRGB/sGray).
- **Drivers**: `wingdi` (Windows GDI print path through the installed
  vendor driver; DEVMODE paper/copies patching), `file` (page-per-PNG,
  debugging + E2E), `null`.
- **Client** (`dyj browse/status/print/jobs/cancel`): mDNS browsing,
  attribute inspection, document submission (txt/jpg/png/pwg → PWG
  raster), queue listing, job cancellation.
- **Service integration**: real Windows SCM service (install/uninstall/
  start/stop, recovery actions), systemd unit, launchd LaunchDaemon.
- **Build**: `scripts/build-all.sh` — CGO-free static binaries for
  windows/linux/darwin on amd64/arm64 (8–9 MB each); `scripts/e2e.sh`
  binary-level end-to-end test.
- **Attributes layer**: strict streaming RFC 8010 decoder that keeps
  every value of 1setOf attributes (replacing go-ipp's overwrite-bug
  decoder, ADR-0006); 9-octet resolution values (RFC 8011 §5.1.10);
  `printer-uuid`; honest document-format table (ADR-0007).

### Fixed
- operations-supported / all enums: 4-octet wire values (RFC 8010
  §3.8), fixing a desynchronizing 2-octet encoding.
- Attribute value-pair field order (tag|name-length|name|value-length|
  value), fixing responses no client could parse.
- resolution values carried the missing units octet (8→9 bytes).
- printer-current-time now carries the real UTC offset instead of
  local time mislabeled as +00:00.

### Security
- Private key material (TLS) is generated at runtime, persisted
  user-read-only, never committed; service install reads options from
  disk, never from command lines visible to other users; process
  spawning is limited to fixed allowListed helpers with constant
  arguments.

### Notes
- go-ipp remains a dependency for test-side cross-validation only
  (ADR-0006); production encode/decode is entirely in pkg/attributes.
