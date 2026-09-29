#!/bin/sh
# deb postrm (remove) / rpm %postun (0=erase, 1=upgrade).
set -e
case "$1" in
    purge)
        if command -v deb-systemd-helper >/dev/null 2>&1; then
            deb-systemd-helper purge dyj.service >/dev/null 2>&1 || true
        fi
        ;;
esac
# /var/lib/dyj (spool, UUID, TLS key, admin token) is deliberately kept
# across upgrades AND removals — reinstalling without re-setup is the
# common case; administrators can remove it by hand.
exit 0
