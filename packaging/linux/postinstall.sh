#!/bin/sh
# Runs for deb (configure) and rpm (1=install, 2=upgrade). Enabling and
# starting go through preset semantics — never a bare `systemctl
# enable` — so distro/admin presets are respected and non-systemd
# systems degrade to a no-op.
set -e

enable_and_start() {
    if command -v deb-systemd-helper >/dev/null 2>&1; then
        deb-systemd-helper enable dyj.service >/dev/null 2>&1 || true
        deb-systemd-invoke start dyj.service >/dev/null 2>&1 || true
    elif command -v systemctl >/dev/null 2>&1; then
        systemctl preset dyj.service >/dev/null 2>&1 || true
        systemctl start dyj.service >/dev/null 2>&1 || true
    fi
}

# Dedicated system user for the service (ADR-0016). A static UID is
# required for avahi D-Bus coexistence: dbus authenticates peers via
# SO_PEERCRED and rejects transient DynamicUser UIDs.
create_user() {
    if ! getent passwd dyj >/dev/null 2>&1; then
        if command -v useradd >/dev/null 2>&1; then
            useradd --system --home-dir /var/lib/dyj --no-create-home \
                --shell /usr/sbin/nologin dyj >/dev/null 2>&1 || true
        elif command -v adduser >/dev/null 2>&1; then
            adduser --system --home /var/lib/dyj --no-create-home \
                --shell /usr/sbin/nologin dyj >/dev/null 2>&1 || true
        fi
    fi
}

case "$1" in
    configure|1|2)
        create_user
        enable_and_start
        echo "dyj: 管理页 http://localhost:6631/admin （局域网访问需管理令牌，见 /var/lib/dyj/admin-token）"
        echo "dyj: 防火墙提示：firewalld 系统需放行 TCP 6631（firewall-cmd --permanent --add-port=6631/tcp && firewall-cmd --reload）"
        ;;
esac
exit 0
