#!/usr/bin/env bash
# GPL-3.0. Installation and local recovery only; daily operations use the panel.
set -Eeuo pipefail
SCRIPT_ROOT="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd)"
APP_HOME="${SB_HOME:-/opt/sing-box-vpn}"
SB_ROOT="${SB_ROOT:-/etc/s-box}"
CORE="$SB_ROOT/sing-box"
RUNTIME="$SB_ROOT/managed-core"
MANAGER="${SB_MANAGER:-/usr/local/bin/yg-vpngate}"
source "$SCRIPT_ROOT/lib/update.sh"
require_root() { [[ $(id -u) == 0 ]] || { echo '需要 root 权限' >&2; return 1; }; }
require_system() {
    require_root
    [[ $(uname -s) == Linux ]] || { echo '需要 Linux systemd' >&2; return 1; }
    source /etc/os-release
    [[ "$ID" == debian || "$ID" == ubuntu ]] || { echo '支持 Debian / Ubuntu' >&2; return 1; }
    command -v systemctl >/dev/null
}
arch_name() { case $(uname -m) in x86_64) echo amd64;; aarch64|arm64) echo arm64;; *) return 1;; esac; }
panel() { "$MANAGER" panel --root "$SB_ROOT" --core "$CORE" --runtime "$RUNTIME" "$@"; }
administrator_input() {
    local username password
    read -r -p '管理员用户名 [admin]: ' username </dev/tty
    read -r -s -p '管理员密码（12–72 字节）: ' password </dev/tty
    printf '\n' >/dev/tty
    printf '%s\n%s' "${username:-admin}" "$password" | python3 -c 'import json,sys; u,p=sys.stdin.read().split("\n",1); print(json.dumps({"username":u,"password":p}))'
    unset password
}
install_main() (
    require_system
    local arch address
    arch=$(arch_name)
    [[ ! -e "$APP_HOME" ]] || { echo '程序目录已存在；v4 不导入旧版本，请使用空目录' >&2; return 1; }
    if [[ -d "$SB_ROOT" && -n $(find "$SB_ROOT" -mindepth 1 -maxdepth 1 -print -quit) ]]; then
        echo '首次安装要求空配置目录；不兼容旧数据与接口' >&2; return 1
    fi
    [[ -x "$SCRIPT_ROOT/.bin/linux-$arch/yg-vpngate" && -x "$SCRIPT_ROOT/.bin/linux-$arch/managed-core" ]] || {
        echo '需要完整 v4 发行包；源码请先 go run ./cmd/release' >&2; return 1;
    }
    address=${SB_ADDRESS:-}
    if [[ -z "$address" ]]; then read -r -p '发布地址（VPS IP 或域名）: ' address </dev/tty; fi
    [[ -n "$address" ]] || return 1
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ca-certificates curl iproute2 iptables procps tar python3 openvpn certbot
    local created=0 units=0 failure_stamp
    failure_stamp=$(date +%s)
    install_failed() {
        local code=$?
        trap - EXIT
        if ((code != 0 && created)); then
            if ((units)); then systemctl disable --now sing-box sb-panel 2>/dev/null || true; fi
            [[ ! -d "$APP_HOME" ]] || mv -- "$APP_HOME" "${APP_HOME}.install-failed.$failure_stamp"
            [[ ! -d "$SB_ROOT" ]] || mv -- "$SB_ROOT" "${SB_ROOT}.install-failed.$failure_stamp"
            echo "安装失败，诊断材料保留在 .install-failed.$failure_stamp 目录；可重新运行安装。" >&2
        fi
        exit "$code"
    }
    trap install_failed EXIT
    created=1
    install -d -m 0755 "$APP_HOME"
    install -d -m 0700 "$SB_ROOT"
    cp -a -- "$SCRIPT_ROOT/." "$APP_HOME/"
    printf '%s\n' 'sck03/sing-box-vpn v4' > "$APP_HOME/.managed"
    install -m 0755 "$APP_HOME/.bin/linux-$arch/yg-vpngate" "$MANAGER.new"
    mv -f -- "$MANAGER.new" "$MANAGER"
    install -m 0755 "$APP_HOME/.bin/linux-$arch/managed-core" "$RUNTIME"
    # The official binary validates generated configurations only.
    "$MANAGER" core --root "$SB_ROOT" prepare --version 1.14.4 --output "$CORE.candidate" >/dev/null
    mv -- "$CORE.candidate" "$CORE"
    if [[ -n "${SB_ADMIN_JSON_FILE:-}" ]]; then
        panel --address "$address" init < "$SB_ADMIN_JSON_FILE"
    else
        administrator_input | panel --address "$address" init
    fi
    panel recover
    panel validate
    panel install-services
    units=1
    ln -sfn "$APP_HOME/sb.sh" /usr/bin/sb
    systemctl enable --now sing-box sb-panel
    systemctl is-active --quiet sing-box sb-panel
    echo 'v4 已安装。面板默认仅监听 127.0.0.1:2096。'
    echo '本机执行 SSH 端口转发：ssh -L 2096:127.0.0.1:2096 root@VPS，然后打开 http://127.0.0.1:2096'
    echo '在面板创建权益并绑定入站后才允许代理认证。公开访问需在 panel.json 配置 TLS 或可信 HTTPS 反向代理。'
    trap - EXIT
)
uninstall_main() {
    require_system
    [[ "$APP_HOME" == /opt/sing-box-vpn && -f "$APP_HOME/.managed" && "$SB_ROOT" == /etc/s-box && -f "$SB_ROOT/.platform-v4" ]] || {
        echo '卸载只接受带 v4 标记的默认目录；自定义目录请手动清理' >&2; return 1;
    }
    systemctl disable --now sb-panel sb-subscription sb-argo sing-box-vpngate yg-vpngate sing-box 2>/dev/null || true
    "$MANAGER" cleanup --config "$SB_ROOT/vpngate/gateway.json" 2>/dev/null || true
    "$MANAGER" firewall --root "$SB_ROOT" --remove
    local service
    for service in sb-panel sb-subscription sb-argo sing-box-vpngate yg-vpngate sing-box; do
        rm -f -- "/etc/systemd/system/$service.service"
    done
    rm -f -- /usr/bin/sb "$MANAGER" /etc/sysctl.d/90-sing-box-vpn.conf
    rm -rf -- "$SB_ROOT" "$APP_HOME"
    systemctl daemon-reload
}
diagnose() {
    panel status
    systemctl --no-pager --full status sb-panel sing-box sing-box-vpngate yg-vpngate sb-subscription sb-argo || true
    journalctl -u sb-panel -u sing-box -u sing-box-vpngate -n 100 --no-pager
}
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then return 0; fi
case "${1:-panel}" in
    install) install_main;;
    upgrade) upgrade_program;;
    panel) echo '面板：http://127.0.0.1:2096；远程访问使用 SSH 转发或配置 HTTPS。';;
    reset-admin) require_system; administrator_input | panel reset-admin;;
    recover) require_system; panel recover; panel validate; panel install-services;;
    status) panel status;;
    diagnose) diagnose;;
    uninstall) uninstall_main;;
    help|--help) echo 'sb [panel|install|status|diagnose|reset-admin|recover|upgrade|uninstall]';;
    *) echo '未知维护命令；日常配置请使用面板' >&2; exit 1;;
esac
