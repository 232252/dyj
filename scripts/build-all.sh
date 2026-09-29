#!/usr/bin/env bash
# Cross-compiles dyj for every supported platform into dist/.
# Pure Go (CGO_ENABLED=0), no runtime dependencies.
#
# Windows additionally gets dyj-tray.exe — the windowsgui-subsystem tray
# binary (no console flash at login autostart, docs/research/10 §3.2).
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${VERSION:-dev}
COMMIT=${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}
LDFLAGS="-s -w"

# Windows exe resources (brand icon + version info + manifest,
# docs/research/11 §4.3). Committed rsrc_*.syso files are the fallback;
# when go-winres is available and VERSION is a real semver, restamp so
# the file properties carry the release version.
if command -v go-winres >/dev/null 2>&1 && [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "stamping Windows resources (go-winres, VERSION=$VERSION)"
    for exe in dyj dyj-tray; do
        pkg="cmd/$exe"
        case "$exe" in
            dyj) desc="dyj - LAN printer sharing service"; manifest=cli ;;
            *)   desc="dyj system tray";                   manifest=gui ;;
        esac
        go-winres simply --arch amd64,arm64 --out "$pkg/rsrc" \
            --product-version "$VERSION.0" --file-version "$VERSION.0" \
            --product-name "dyj" --file-description "$desc" \
            --copyright "Copyright dyj maintainers, Apache-2.0" \
            --original-filename "$exe.exe" \
            --icon assets/brand/dyj.ico --manifest "$manifest"
    done
else
    echo "keeping committed rsrc_*.syso (install go-winres + set VERSION=x.y.z to restamp)"
fi

mkdir -p dist
rm -f dist/dyj-*

for platform in \
    windows/amd64 windows/arm64 \
    linux/amd64 linux/arm64 \
    darwin/amd64 darwin/arm64; do
    GOOS=${platform%/*}; GOARCH=${platform#*/}
    EXT=""; [ "$GOOS" = "windows" ] && EXT=".exe"
    OUT="dist/dyj-$GOOS-$GOARCH$EXT"
    echo "building $OUT"
    CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH \
        go build -trimpath -ldflags "$LDFLAGS" -o "$OUT" ./cmd/dyj
    if [ "$GOOS" = "windows" ]; then
        TRAY="dist/dyj-tray-$GOOS-$GOARCH$EXT"
        echo "building $TRAY"
        # -H=windowsgui: GUI subsystem so the Run-key launch at login
        # never flashes a console window.
        CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH \
            go build -trimpath -ldflags "$LDFLAGS -H=windowsgui" -o "$TRAY" ./cmd/dyj-tray
    fi
done

echo ""
echo "== artifacts =="
ls -lh dist/ | awk 'NR>1 {printf "  %-28s %s\n", $9, $5}'
echo ""
echo "all platforms built (VERSION=$VERSION COMMIT=$COMMIT)"
