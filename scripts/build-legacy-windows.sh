#!/usr/bin/env bash
# Win7-SP1 legacy channel: Go 1.20.14 (the last toolchain that runs on
# Windows 7/8/8.1 — Go 1.21 requires Windows 10+) × windows/386 + amd64,
# packaged as portable zips with install/uninstall bat helpers
# (packaging/windows/legacy). The main tree stays on the modern
# toolchain — go.mod keeps `go 1.27.3` — because Go 1.21+ binaries
# cannot start on Win7 at all. This script does its work in a throwaway
# git worktree and rewrites go.mod only THERE.
#
# Dependency pins are the newest set that compiles AND passes
# go test ./... under go1.20.14 (verified 2026-11). Notably mdns stays
# on v1.0.6: the v1.0.7 go.mod served by the module proxy drags
# x/net v0.48, which is go1.24-only. browse.go needs nothing newer —
# mdns.QueryParam.Logger exists since v1.0.6.
#
# Artifacts into dist/: dyj-legacy-win7-{386,amd64}.exe,
# dyj-tray-legacy-win7-{386,amd64}.exe, dyj-legacy-win7-{386,amd64}.zip
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$(pwd)
OUT="$ROOT/dist"

VERSION=${VERSION:-dev}
COMMIT=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}
LDFLAGS="-s -w"
export GOTOOLCHAIN=go1.20.14

echo "== legacy build (VERSION=$VERSION COMMIT=$COMMIT, $(go version))"

WT=$(mktemp -d)/dyj-legacy
cleanup() { git worktree remove --force "$WT" >/dev/null 2>&1 || true; }
trap cleanup EXIT
git worktree add --detach "$WT" HEAD >/dev/null
cd "$WT"

# --- go1.20 downgrade: language version + dependency pins ------------
# sed, not `go mod edit`: go1.20.14 cannot even PARSE a `go 1.27.3`
# directive (patch-qualified versions are a go1.21 feature), so the go
# line must be rewritten before any go1.20 toolchain touches the file.
sed -i -E 's/^go [0-9]+\.[0-9]+(\.[0-9]+)?$/go 1.20/' go.mod
grep -q '^go 1\.20$' go.mod
go get fyne.io/systray@v1.11.0 \
    github.com/hashicorp/mdns@v1.0.6 \
    github.com/miekg/dns@v1.1.62 \
    golang.org/x/image@v0.18.0 \
    golang.org/x/mod@v0.18.0 \
    golang.org/x/net@v0.34.0 \
    golang.org/x/sync@v0.10.0 \
    golang.org/x/sys@v0.29.0 \
    golang.org/x/tools@v0.22.0
go mod tidy

echo "== go1.20.14 tests"
go test ./...

# --- exe resources (icon/version), mirroring build-all.sh ------------
if command -v go-winres >/dev/null 2>&1 && [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "stamping legacy Windows resources (go-winres, VERSION=$VERSION)"
    for exe in dyj dyj-tray; do
        pkg="cmd/$exe"
        case "$exe" in
            dyj) desc="dyj - LAN printer sharing service"; manifest=cli ;;
            *)   desc="dyj system tray";                   manifest=gui ;;
        esac
        go-winres simply --arch 386,amd64 --out "$pkg/rsrc" \
            --product-version "$VERSION.0" --file-version "$VERSION.0" \
            --product-name "dyj" --file-description "$desc (Win7 legacy)" \
            --copyright "Copyright dyj maintainers, Apache-2.0" \
            --original-filename "$exe.exe" \
            --icon assets/brand/dyj.ico --manifest "$manifest"
    done
else
    echo "keeping committed rsrc_*.syso (set VERSION=x.y.z + go-winres to restamp)"
fi

# --- build ------------------------------------------------------------
echo "== building legacy exes"
mkdir -p "$OUT" dist
for arch in 386 amd64; do
    CGO_ENABLED=0 GOOS=windows GOARCH=$arch \
        go build -trimpath -ldflags "$LDFLAGS" -o "dist/dyj-legacy-win7-$arch.exe" ./cmd/dyj
    CGO_ENABLED=0 GOOS=windows GOARCH=$arch \
        go build -trimpath -ldflags "$LDFLAGS -H=windowsgui" -o "dist/dyj-tray-legacy-win7-$arch.exe" ./cmd/dyj-tray
done

# --- portable zips ----------------------------------------------------
ziproot() { # ziproot <out.zip> <dir> — zip <dir>'s CONTENTS at the zip root
    local out=$1 dir=$2
    if command -v zip >/dev/null 2>&1; then
        (cd "$dir" && zip -q -r "$out" .)
    elif command -v 7z >/dev/null 2>&1; then
        (cd "$dir" && 7z a -tzip -bso0 "$out" . >/dev/null)
    elif command -v powershell.exe >/dev/null 2>&1; then
        powershell.exe -NoProfile -Command \
            "Compress-Archive -Path '$(cygpath -w "$dir")\*' -DestinationPath '$(cygpath -w "$out")' -Force" \
            >/dev/null
    else
        echo "error: need zip, 7z or powershell.exe to build the legacy zips" >&2
        return 1
    fi
}

STAGE=$(mktemp -d)
for arch in 386 amd64; do
    stage="$STAGE/dyj-legacy-win7-$arch"
    mkdir -p "$stage"
    cp "dist/dyj-legacy-win7-$arch.exe" "$stage/dyj.exe"
    cp "dist/dyj-tray-legacy-win7-$arch.exe" "$stage/dyj-tray.exe"
    cp "$ROOT"/packaging/windows/legacy/install-legacy.bat \
       "$ROOT"/packaging/windows/legacy/uninstall-legacy.bat \
       "$ROOT"/packaging/windows/legacy/README-Win7.txt "$stage/"
    echo "packing $(basename "$stage").zip"
    ziproot "$OUT/dyj-legacy-win7-$arch.zip" "$stage"
done
rm -rf "$STAGE"

cd "$ROOT"
rm -f "$OUT"/dyj-legacy-win7-*.exe "$OUT"/dyj-tray-legacy-win7-*.exe
cp "$WT"/dist/dyj-legacy-win7-*.exe "$WT"/dist/dyj-tray-legacy-win7-*.exe "$OUT/"

echo ""
echo "== legacy artifacts =="
ls -lh "$OUT"/dyj-legacy-win7-*.zip "$OUT"/dyj-legacy-win7-*.exe "$OUT"/dyj-tray-legacy-win7-*.exe \
    | awk '{printf "  %-34s %s\n", $NF, $5}'
echo ""
echo "sha256:"
(cd "$OUT" && sha256sum dyj-legacy-win7-386.zip dyj-legacy-win7-amd64.zip)
echo ""
echo "legacy build done (go1.20.14, Win7 SP1+ floor)"
