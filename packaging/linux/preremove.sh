#!/bin/sh
# deb remove/upgrade (remove|upgrade) and rpm erase/upgrade (0|1).
set -e
case "$1" in
    remove|upgrade|0|1)
        if command -v deb-systemd-invoke >/dev/null 2>&1; then
            deb-systemd-invoke stop dyj.service >/dev/null 2>&1 || true
        elif command -v systemctl >/dev/null 2>&1; then
            systemctl stop dyj.service >/dev/null 2>&1 || true
        fi
        ;;
esac
exit 0
