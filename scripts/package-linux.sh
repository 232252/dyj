#!/usr/bin/env bash
# Builds dyj deb+rpm for amd64 and arm64 into dist/packages/.
# Requires: nfpm (go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest)
# and dist/dyj-linux-{amd64,arm64} from scripts/build-all.sh.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=${VERSION:?VERSION env var required, e.g. 0.2.0}
NFPM=${NFPM:-nfpm}
command -v "$NFPM" >/dev/null || { echo "nfpm not found; install: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest" >&2; exit 1; }

mkdir -p dist/packages
for arch in amd64 arm64; do
    bin="dist/dyj-linux-$arch"
    [ -f "$bin" ] || { echo "missing $bin (run scripts/build-all.sh first)" >&2; exit 1; }
    # nfpm does not expand env vars inside contents[].src on all
    # versions, so stamp the arch into a throwaway config.
    cfg="dist/nfpm-$arch.yaml"
    sed "s/\${TARGETARCH}/$arch/g" packaging/nfpm.yaml > "$cfg"
    for fmt in deb rpm; do
        out="dist/packages/dyj-$VERSION-linux-$arch.$fmt"
        echo "packaging $out"
        "$NFPM" package -f "$cfg" -p "$fmt" -t "$out" >/dev/null
    done
    rm -f "$cfg"
done
ls -lh dist/packages/
