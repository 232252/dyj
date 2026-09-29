#!/bin/sh
# dyj installer for Linux/macOS headless and desktop hosts.
#   curl -fsSL https://raw.githubusercontent.com/sq199/dyj/main/packaging/install.sh | sh
# or pin a version:
#   curl -fsSL .../install.sh | sh -s -- v0.2.0
#
# Installs the single static binary to /usr/local/bin (override with
# DYJ_PREFIX) and prints the follow-up commands (systemd, firewall).
# It never touches the firewall itself — see the printed hints.
set -eu

REPO="sq199/dyj"
VERSION="${1:-latest}"
PREFIX="${DYJ_PREFIX:-/usr/local}"

os=$(uname -s)
arch=$(uname -m)
case "$os" in
    Linux) os_name="linux" ;;
    Darwin) os_name="darwin" ;;
    *) echo "dyj install: unsupported OS $os" >&2; exit 1 ;;
esac
case "$arch" in
    x86_64|amd64) arch_name="amd64" ;;
    aarch64|arm64) arch_name="arm64" ;;
    *) echo "dyj install: unsupported arch $arch" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
        | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
    [ -n "$VERSION" ] || { echo "dyj install: cannot resolve latest release" >&2; exit 1; }
fi

asset="dyj-${os_name}-${arch_name}"
url="https://github.com/$REPO/releases/download/$VERSION/$asset"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "dyj install: $url"
curl -fsSL "$url" -o "$tmp/dyj"
chmod 0755 "$tmp/dyj"

if [ -w "$PREFIX/bin" ] 2>/dev/null || [ -w "$PREFIX" ]; then
    install -m 0755 "$tmp/dyj" "$PREFIX/bin/dyj"
else
    echo "dyj install: need root for $PREFIX/bin (rerun with sudo)"
    sudo install -m 0755 "$tmp/dyj" "$PREFIX/bin/dyj"
fi

echo ""
echo "installed: $PREFIX/bin/dyj"
case "$os_name" in
    linux)
        echo "next steps (as root):"
        echo "  $PREFIX/bin/dyj serve -name '办公室打印机' -printer <队列>   # 先试跑"
        echo "  $PREFIX/bin/dyj service install -- -name '办公室打印机' -printer <队列>"
        echo "  systemctl start dyj   # 若服务包/preset 未自动启动"
        echo "  管理页: http://localhost:6631/admin"
        echo "  firewalld 用户: firewall-cmd --permanent --add-port=6631/tcp && firewall-cmd --reload"
        ;;
    darwin)
        echo "next steps:"
        echo "  sudo $PREFIX/bin/dyj service install   # 写入 /Library/LaunchDaemons 并 bootstrap"
        echo "  管理页: http://localhost:6631/admin"
        ;;
esac
