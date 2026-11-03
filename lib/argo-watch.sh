#!/usr/bin/env bash
set -Eeuo pipefail
exec /usr/local/bin/yg-vpngate config --root "$1" argo-watch
