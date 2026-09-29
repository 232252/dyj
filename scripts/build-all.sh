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
