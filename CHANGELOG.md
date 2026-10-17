# Changelog

All notable changes to this project are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/); versioning: semver.

## [0.4.2] — 2026-10-17

### Added
- **Known-printer rediscovery (research/15 P2)**: opening the client page
  probes every previously added share address with IPP Get-Printer-Attributes
  (not a TCP dial — mesh/VPN overlays complete a handshake on ports that have
  no listener) and pins the ones that still answer at the top of the scan
  list, marked "以前添加过". A revoked pairing stays visible and asks for a
  new code; a live token path can be reinstalled directly. Logged as
  `scan.known`.
- Installer finish page offers a checked-by-default "立即打开 dyj" (`dyj open`).
  `dyj open` retries for a few seconds so it still finds a service that has
  only just started.

### Fixed
- **Pairing code boxes rendered empty**: `dyj-pin-input` called `t()` in its
  constructor without importing it. The custom element upgrade threw, the
  shadow tree stayed empty, and the dialog looked blank. Same failure class
  as the old `dyj-diagnostic` constructor throw; it was not specific to an
  embedded browser.
- Client scan no longer answers 502 with the text "mDNS scan failed" when
  browsing errors. That string hid the manual-address form and leaked a
  banned word onto the client page. An empty list (plus the address form)
  is the mesh/VPN path; the error stays in the `scan.result` log.
- A scan error in the page still shows the address form and a rescan button.
- Receiver status (`added-printers`) and `dyj doctor --client` judge the
  share side by an IPP exchange. A TCP-open/application-silent peer is
  offline, not "ok" / "可达".
- Recent-prints "copies" no longer shows 0 when the job omitted the
  attribute. The IPP default is 1 (research/12 §3).
- PIN remaining time uses the same clock as PIN rotation, so an injected
  clock cannot report a negative TTL.

### Changed
- Go module path and packaging homepage/download URLs are
  `github.com/232252/dyj`, matching the repository. Historical research
  notes that record the original Windows profile path are unchanged.

## [0.4.1] — 2026-09-30

### Added
- **Add a printer by address (research/15 P1)**: the client page has a manual
  entry block below the scan list (also in the empty state) — type the other
  computer's address (`192.168.1.20:6631`, bare host defaults to 6631, `[IPv6]`
  accepted) plus the optional 4-digit pairing code. This is the only way to add
  a printer when the two machines are connected only via a mesh/VPN overlay:
  mDNS is link-local and can never cross subnets, but pairing and printing are
  plain unicast. The queue URI echoes the address you entered (pair proxy
  already did; now reachable from the UI).
- Wrong-PIN attempts during pairing no longer trigger the admin-token banner
  (the share side's 401 passthrough was indistinguishable from a stale
  admin token; `pair` is now excluded from the 401 broadcast).

### Fixed
- **Scan listed one printer per network interface (research/15 D-1)**: the raw
  and reference mDNS browsing paths report the same service under different
  per-interface SRV targets, and the dedupe key included that host — one
  physical printer showed up as 2+ rows. The merge key is now instance label +
  port, and both views' addresses are unioned before the reachability probe.
- **Pairing dialog auto-connect no longer depends on the PIN component's
  value setter** (`connect(code)` takes the code directly; the `dyj-complete`
  event no longer passes the event object into the connect handler).

## [0.4.0] — 2026-09-30

### Changed
- **Minimal two-mode UI (v0.4, ADR-0011 / research/14)**: the admin console is
  gone — the whole product is now 4 routes with no sidebar (`#/` role cards,
  `#/server`, `#/client`, `#/help`); a configured machine lands directly on its
  role's home page (server: PIN + per-printer share switches + connected
  devices + an 8-row recent-prints block; client: scan → 4-digit code modal →
  install → installed list with live per-printer status). Old v0.3 hashes
  redirect. The network page (port/TLS/mDNS/hostname/ifaces), the printer
  management page, the URI copy fields, the per-platform manual setup guides
  and the metrics cards are all REMOVED from the UI — ports, drivers, queue
  names and connection hosts are automated behavior now (research/14 §4);
  CLI flags and service.json remain the only "advanced" surface.
- **Copy rules enforced by a test**: a banned-words guard scans both i18n
  tables; engineering jargon may only appear under `help_`/`about_`/`token_`
  keys (the help page is the single read-only home for technical info: doctor
  self-check, log tail, about line).
- **Tray menu follows the role**: pairing code and pause/resume only appear
  on machines configured as share/both; receiver machines see open/autostart/quit.

### Added
- **`GET /admin/api/diag/export`** (help page button): one zip with summary,
  live config (no secrets), doctor checks (share + receiver sides) and the
  256 KB log tail — send one file, get a diagnosis.
- **`GET /admin/api/added-printers` now carries a per-printer `status`**
  (`ok` / `offline` / `missing` / `revoked`), a read-only receiver snapshot
  for the client home list.
- **`GET /admin/api/jobs?limit=N`** returns the newest N jobs (newest first)
  for the server home "recent prints" block.
- **4-digit PIN input component** (auto-advance, backspace, whole-code paste,
  one-time-code autocomplete) behind the client pairing dialog.

## [0.3.0] — 2026-09-30

### Added
- **Zero-config first run (ADR-0010 D1/D2, research/13)**: `dyj` with no
  arguments opens a browser wizard with two roles — "share local printers"
  (auto-enumerated via EnumPrintersW/lpstat, virtual writers hidden, one
  click shares the selection) and "add a network printer" (LAN scan, 4-digit
  pairing, automatic system-queue install, one-click test page). The node
  ALWAYS boots — an unconfigured machine serves the wizard instead of dying
  on a missing -printer (the v0.2 first-boot deadlock), ports probe upward
  when taken, and `dyj open` (the installer desktop shortcut) finds the
  running node.
- **4-digit PIN pairing with device tokens (D3)**: rolling 10-minute PIN
  (admin dashboard, tray menu, console banner), per-IP rate limiting with
  lockout, constant-time compare, 128-bit tokens persisted only as SHA-256
  hashes, per-device revoke. Data plane: loopback exempt, LAN default-deny;
  tokens ride in the IPP URI path (`/p/<token>/ipp/print/<key>`) because the
  Windows IPP Class Driver cannot send custom headers. mDNS TXT gains
  `dyj=1`/`pair=1`/`dyjver=3`.
- **Receiver-side automatic queue install (D4)**: `Add-Printer -IppUrl` on
  Windows with automatic name dedup and a one-prompt UAC path (the wizard
  relaunches this binary via runas and picks up the result file); `lpadmin
  -m everywhere` on Linux/macOS. An added-printers registry
  (`added-printers.json`) drives the manage/remove UI and client-side
  doctor.
- **Multi-printer sharing (D6)**: service.json grows `role` + `printers[]`
  (v0.2 configs migrate on load); each printer gets its own IPP profile,
  spooler and mDNS instance behind one listener; legacy single-printer
  paths keep working.
- **In-process config hot-reload (D5)**: saving config in the admin UI
  rebuilds the share stack (mDNS goodbye → spool drain → new backends)
  without closing the listener — `restart_required` is gone.
- **Observability contract (D7)**: `pkg/logx` structured events
  (logfmt `lvl/evt/kv`), rotating file logs under `<stateDir>/logs/` with
  token/PIN redaction enforced in tests, the 24-checkpoint event set from
  research/13 §5.2, doctor additions — mDNS-egress self-test (share side)
  and `dyj doctor --client` (queue exists / server reachable / token
  validity) — plus `docs/troubleshooting.md`, the symptom → event → fix
  fault tree.
- **Pairing management UI**: dashboard pairing-code card with countdown and
  rotate, paired-device list with revoke; share wizard and add-printer
  wizard pages; first-run redirect keyed off the server-side role.
- **Branded tray gains the pairing code** as a live menu item.
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

### Changed
- **LAN data plane requires pairing (breaking)**: IPP requests from
  non-loopback peers without a valid device token receive 401 (v0.2 served
  anyone). Loopback keeps full access; the wizard is the supported path for
  LAN clients.
- **`service install` without flags persists a bootstrap config** (role "")
  instead of options that fail to boot; the installer's argument-less
  install now lands in the reachable wizard state.
- **Admin config saves apply live**; the UI no longer claims a restart is
  required.
- **Windows installer** adds a desktop "dyj" shortcut (`dyj open`) and a
  start-menu setup entry alongside the tray.

### Fixed
- **serve crashed at startup with a non-ASCII machine name** when
  `-tls-port` was set: the self-signed certificate's DNS SAN folded the
  host name to `生命.local`, which x509 rejects (DNS names are IA5String);
  host names are now folded to `[a-z0-9-]` with a `dyj` fallback, and the
  `-hostname` override feeds the certificate too (regression test with a
  CJK host name included; found in Windows physical-machine acceptance).
- **mDNS announcements never left the wire on multi-homed Windows** (and
  could mis-route on multi-homed Linux): per-interface sockets bound the
  wildcard address, where Windows ignores `IP_MULTICAST_IF` for egress, so
  every announcement followed the default route's NIC and LAN peers heard
  nothing. Sockets now bind the interface address, pinning both send and
  receive (measured: Debian peer's Avahi saw nothing before, both services
  plus CUPS auto-created queues after).
- **`dyj status` printed only the first declared format**: the table
  carries four, the client used a first-value getter; it now joins the
  full `document-format-supported` list.
- **Admin UI white-screen at `/admin` without a trailing slash**: the
  handler served index.html in place, so browsers resolved `app.js` against
  the server root and rejected its text/plain MIME; `/admin` now 301s to
  `/admin/` (regression test included).
- **Wizard and diagnostic components never rendered**: `dyj-stepper` and
  `dyj-diagnostic` used `this.shadowRoot` without attaching a shadow root,
  so the first `steps=` / `check=` assignment threw and left both empty;
  both attach in the constructor now (caught by live UI verification while
  shipping the icon work).

### Notes
- The items in **Added** above are what shipped in 0.3.0 (ADR-0010). An
  earlier draft of this note said the redesign was "design only"; that
  sentence was already false when 0.3.0 was tagged. The design record
  remains `docs/research/13-zero-config-redesign.md`. LAN clients without
  a device token receive 401.

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
