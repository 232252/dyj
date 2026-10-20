#!/usr/bin/env bash
# Binary-level end-to-end test: builds dyj, starts a real server
# process, submits a document with the real client, and checks the
# printed output. Run from the repo root: scripts/e2e.sh
set -euo pipefail

cd "$(dirname "$0")/.."
WORK=$(mktemp -d)
# Cleanup must never turn a PASS into a FAIL: wait for the server to
# actually exit (kill is asynchronous and Windows keeps the log handle
# locked until then), and tolerate rm failures.
trap 'if [ -n "${SRV_PID:-}" ]; then kill $SRV_PID 2>/dev/null || true; wait $SRV_PID 2>/dev/null || true; fi; rm -rf "$WORK" 2>/dev/null || true' EXIT

echo "== build =="
go build -o "$WORK/dyj" ./cmd/dyj
SIZE=$(du -h "$WORK/dyj" | cut -f1)
echo "binary: $SIZE"

echo "== start server (file backend, no mDNS) =="
OUT="$WORK/out"; SPOOL="$WORK/spool"
"$WORK/dyj" serve --no-mdns --port 16731 --backend file --out "$OUT" \
    --spool "$SPOOL" --name "e2e-printer" >"$WORK/server.log" 2>&1 &
SRV_PID=$!
for i in $(seq 1 50); do
    if grep -q "IPP endpoint" "$WORK/server.log" 2>/dev/null; then break; fi
    sleep 0.1
done
grep -q "IPP endpoint" "$WORK/server.log" || { cat "$WORK/server.log"; exit 1; }

echo "== status =="
"$WORK/dyj" status 127.0.0.1:16731

echo "== print a text document =="
printf 'dyj end-to-end test\ngenerated %s\n' "$(date)" > "$WORK/note.txt"
"$WORK/dyj" print 127.0.0.1:16731 "$WORK/note.txt"

echo "== wait for completion =="
STATE=""
for i in $(seq 1 100); do
    STATE=$("$WORK/dyj" jobs --which all 127.0.0.1:16731 2>/dev/null | awk '{print $3}' | grep -E '^(completed|aborted|canceled)' | head -1 || true)
    [ -n "$STATE" ] && break
    sleep 0.1
done
[ "$STATE" = "completed" ] || { echo "job did not complete:"; tail -5 "$WORK/server.log"; exit 1; }

echo "== verify output =="
PNG=$(ls "$OUT"/job-*.png 2>/dev/null | head -1)
[ -n "$PNG" ] || { echo "no PNG produced"; exit 1; }
# PNG magic + plausible size
head -c 8 "$PNG" | od -An -tx1 | grep -q "89 50 4e 47 0d 0a 1a 0a" || { echo "not a PNG"; exit 1; }
echo "output: $PNG ($(du -h "$PNG" | cut -f1))"

echo "== jobs + cleanup =="
"$WORK/dyj" jobs --which all 127.0.0.1:16731

echo ""
echo "E2E PASS  (binary: $SIZE)"
