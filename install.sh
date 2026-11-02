#!/usr/bin/env bash
set -Eeuo pipefail
VERSION=v1.0.0
REPOSITORY=https://github.com/sck03/sing-box-vpn
if [[ "${1:-}" == --local ]];then
    SOURCE="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd)"
    exec bash "$SOURCE/sb.sh" install
fi
[[ $(id -u) == 0 && $(uname -s) == Linux ]] || { echo '安装需要 Linux root' >&2;exit 1; }
command -v curl >/dev/null || { echo '请先安装 curl' >&2;exit 1; }
SCRATCH=$(mktemp -d /tmp/sing-box-vpn.XXXXXXXX)
cleanup() { case "$SCRATCH" in /tmp/sing-box-vpn.*) rm -rf -- "$SCRATCH";;esac; }
trap cleanup EXIT
curl -fLsS --retry 2 --connect-timeout 15 --max-time 180 "$REPOSITORY/releases/download/$VERSION/sing-box-vpn-$VERSION.tar.gz" -o "$SCRATCH/sing-box-vpn-$VERSION.tar.gz"
curl -fLsS --retry 2 "$REPOSITORY/releases/download/$VERSION/SHA256SUMS" -o "$SCRATCH/SHA256SUMS"
(cd "$SCRATCH" && grep "  sing-box-vpn-$VERSION.tar.gz$" SHA256SUMS | sha256sum -c -)
tar -xzf "$SCRATCH/sing-box-vpn-$VERSION.tar.gz" -C "$SCRATCH"
bash "$SCRATCH/sing-box-vpn/sb.sh" install
