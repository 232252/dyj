# Changelog

All notable changes to this project are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/); versioning: semver.

## [Unreleased]

### Added
- **Brand identity** (docs/research/11): self-drawn vector mark (printer +
  broadcast waves, `assets/brand/dyj-mark.svg`) and full-color app tile
  (`assets/brand/dyj-logo.svg`); favicon (SVG + 4 KB multi-size ICO) served
  from the embedded admin UI. `dyj-icon` now vendors 23 icons from Lucide
  v1.48.0 (ISC) with a provenance header — nav semantics fixed (status →
  activity, network → topology graph, jobs → files).
- **Branded Windows tray icon**: the 32×32 tray icon is now the blue brand
  tile with a white printer glyph plus a status badge (green/gray/red),
  rasterized at runtime from signed-distance fields — still zero asset
  files, with a theme-aware badge rim for light/dark taskbars
  (`AppsUseLightTheme` polled by the Windows glue).
- **Windows exe resources**: dyj.exe / dyj-tray.exe embed a multi-size ICO
  (16–256), version info (product name/version/copyright) and an app
  manifest via committed `rsrc_windows_*.syso` (go-winres) — Explorer, the
  taskbar and the uninstaller no longer show the default Go icon.
  `scripts/build-all.sh` restamps resources when go-winres + VERSION are
  available; the release workflow installs go-winres.
- **Linux desktop icon**: dyj hicolor icons (16–256 + scalable SVG) ship in
  the deb/rpm; the `.desktop` entry uses `Icon=dyj` instead of the generic
  theme printer icon. `scripts/genicons` regenerates every raster
  derivative from the 512 px master render.

### Fixed
- **Admin UI white-screen at `/admin` without a trailing slash**: the
  handler served index.html in place, so browsers resolved `app.js` against
  the server root and rejected its text/plain MIME; `/admin` now 301s to
  `/admin/` (regression test included).
- **Wizard and diagnostic components never rendered**: `dyj-stepper` and
  `dyj-diagnostic` used `this.shadowRoot` without attaching a shadow root,
  so the first `steps=` / `check=` assignment threw and left both empty;
  both attach in the constructor now (caught by live UI verification while
  shipping the icon work).

## [0.2.0] — 2026-09-29

Complete client + server delivery: human-facing UI, real-printer support on
Linux/macOS, optional PDF, per-platform installers (ADR-0008, ADR-0009;
research: docs/research/10).

### Added
- **Web admin UI** (`http://<host>:6631/admin`, embedded, zero build chain):
  status dashboard, printer/network settings (restart-to-apply), job list
  with cancel, live SSE log view, first-run wizard with per-platform
  diagnostics. Vanilla Web Components — one control per file
  (`pkg/admin/web/components/`), one page per file, zh-CN default + en.
- **Admin API** under `/admin/api/*`: status, jobs, config
  (validated, persisted, restart-required), pause/resume, cancel, log SSE,
  diagnostics. Auth: loopback has full access; LAN gets read-only
  status/jobs and needs the `X-Dyj-Token` header for everything else
  (256-bit token in `<stateDir>/admin-token`, printed once at startup).
- **cups backend** (`-backend cups`, auto-picked when CUPS queues exist):
  Linux/macOS servers can now share REAL printers — spooled raster pages
  are wrapped into an image-only PDF (new `render.PagesToPDF`, golden
  tested) and submitted to a local CUPS queue via lp. Hardened process
  discipline: pinned absolute executable, argv-array only, bare-token
  validation for externally-sourced values.
- **Optional PDF support**: when poppler's `pdftoppm` is installed the
  server declares `application/pdf` (and syncs the mDNS `pdl=` promise);
  decoding runs pdftoppm as an external rasterizer. Without it, behavior
  is byte-identical to 0.1. Client: `dyj print *.pdf` passes PDF through
  sniffed by `%PDF-` magic.
- **`dyj doctor`**: per-machine diagnostics (state dir, port, 5353
  occupancy, TLS cert, PDF capability, firewall guidance, backend probe)
  in human or `--json` form; the same checks power the admin page.
- **Windows tray** (`dyj tray` + shipping `dyj-tray.exe`, windowsgui
  subsystem): status dot icon (green/gray/red), open admin page,
  pause/resume sharing, HKCU Run autostart toggle, quit-without-touching-
  the-service. `scripts/build-all.sh` now emits both Windows binaries.
- **Installers & packaging**:
  - Linux: `packaging/nfpm.yaml` → deb/rpm (amd64+arm64) shipping the
    hardened unit into `/usr/lib/systemd/system`, an avahi static service
    file (`/etc/avahi/services/dyj.service`, `_ipp._tcp` + admin page
    `_http._tcp`), and an admin-page `.desktop` entry; enable/start via
    preset semantics; the package never touches the running firewall.
    `scripts/package-linux.sh` is the one entry point (locally + CI).
  - Windows: `packaging/windows/dyj.iss` (Inno Setup) — per-machine
    install, firewall rules pre-created on ALL three profiles, HKLM Run
    tray autostart, ordered uninstall cleanup with keep-my-data prompt.
  - macOS: `packaging/macos/dyj.rb` Homebrew formula (with `brew services`
    support; no notarization needed via the formula path) and
    `org.dyj.printer.plist` LaunchDaemon template.
  - `packaging/install.sh`: curl-sh installer for headless hosts.
  - `.github/workflows/release.yml`: tag → binaries + packages + ISCC
    installer on the GitHub Release (signing steps pre-wired).
- **Homebrew-style service support**: `brew services start dyj` maps to
  `dyj service run` (documented in the formula).

### Changed
- Linux default backend (no `-backend`): **cups** when local CUPS queues
  exist, else file. The explicit flag always wins.
- mDNS TXT `adminurl` now points at `/admin` (previously the bare IPP
  GET endpoint).
- `dyj serve` banner prints the admin URL and admin token.

### Fixed
- **systemd service (`dyj service install`) hardened**: DynamicUser +
  StateDirectory (no more root), NoNewPrivileges, ProtectSystem=strict,
  empty capability bounding set (6631/5353 need none), AF_NETLINK allowed
  for interface enumeration; unit renderer shared by CLI install and the
  packaged unit (ADR-0009 D3).
- **launchd service fixed on three counts**: plists are now chowned
  root:wheel and chmod 644 (launchd otherwise refuses with "Dubious
  permissions"), load/unload replaced by bootstrap/bootout (10.10+
  domain syntax, legacy fallback), StandardOutPath added.


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
