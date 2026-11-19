#!/usr/bin/env bash
set -Eeuo pipefail
# Standalone entry point: file, pipe, or bash <(curl/wget ...).
REPOSITORY=https://github.com/sck03/sing-box-vpn

installer_error() { echo "$*" >&2; return 1; }
installer_help() {
    cat <<'HELP'
sing-box-vpn 一键安装与维护
  bash install.sh                  打开菜单
  bash install.sh install          安装；已有当前平台时升级
  bash install.sh upgrade          升级已有安装
  bash install.sh uninstall        卸载（确认后删除程序和数据）
  bash install.sh uninstall --yes  无交互卸载
  bash install.sh --local          从解压后的完整发行包安装/升级
  bash install.sh --local --upgrade

支持 Debian/Ubuntu + systemd，Linux amd64/arm64，需要 root。
自动下载正式 Release 并校验 SHA256；VPS 无需 Go、Node.js 或 Python。
SB_RELEASE_VERSION=vMAJOR.MINOR.PATCH 可指定正式版本，例如 v5.0.0。
HELP
}
installer_installed() {
    # This is a persistent platform-format marker, not a release major version.
    [[ -f "${SB_HOME:-/opt/sing-box-vpn}/.managed" &&
       -f "${SB_ROOT:-/etc/s-box}/.platform-v5" ]]
}
installer_arch() {
    case $(uname -m) in
        x86_64) echo amd64;;
        aarch64|arm64) echo arm64;;
        *) installer_error '仅支持 Linux amd64 / arm64'; return 1;;
    esac
}
installer_require_system() {
    [[ $(uname -s) == Linux ]] || { installer_error '需要 Linux Debian/Ubuntu + systemd'; return 1; }
    [[ $(id -u) == 0 ]] || { installer_error '请切换到 root 后运行（sudo -i）'; return 1; }
    # shellcheck disable=SC1091
    source /etc/os-release
    [[ "$ID" == debian || "$ID" == ubuntu ]] || { installer_error '当前仅支持 Debian / Ubuntu'; return 1; }
    command -v systemctl >/dev/null || { installer_error '需要 systemd'; return 1; }
    installer_arch >/dev/null
}
installer_menu_text() {
    echo 'sing-box-vpn 安装与维护'
    if installer_installed; then
        echo '当前状态：已安装'
        local manager="${SB_MANAGER:-/usr/local/bin/yg-vpngate}" installed_version
        if [[ -x "$manager" ]] && installed_version=$(timeout 5 "$manager" --version 2>/dev/null) && [[ -n "$installed_version" ]]; then
            printf '当前版本：%s\n' "$installed_version"
        else
            echo '当前版本：未知（管理程序不可用）'
        fi
    else
        echo '当前状态：未安装'
    fi
    cat <<'MENU'
  1) 安装 / 更新到最新正式版
  2) 升级已有安装
  3) 面板访问方式
  4) 运行状态
  5) 诊断
  6) 重置管理员
  7) 恢复配置与服务
  8) 卸载（删除程序和数据）
  0) 退出
MENU
}
installer_menu() {
    installer_menu_text
    local choice
    if ! read -r -p '请选择 [1]: ' choice </dev/tty; then
        installer_error '无交互终端；请指定 install、upgrade 或 uninstall --yes'
        return 1
    fi
    case "${choice:-1}" in
        1) ACTION=install;; 2) ACTION=upgrade;; 3) ACTION=panel;;
        4) ACTION=status;; 5) ACTION=diagnose;; 6) ACTION=reset-admin;;
        7) ACTION=recover;; 8) ACTION=uninstall;; 0) ACTION='exit';;
        *) installer_error '无效选择'; return 1;;
    esac
}
installer_validate_package() {
    local source=$1 arch member
    arch=$(installer_arch)
    # Check the package contract instead of guessing its format from its tag.
    for member in sb.sh install.sh versions.json lib/update.sh lib/ownership.sh lib/restart-panel.sh \
        ".bin/linux-$arch/yg-vpngate" ".bin/linux-$arch/managed-core"; do
        [[ -f "$source/$member" && ! -L "$source/$member" ]] || {
            installer_error "发行包不完整或不支持当前平台（缺少 $member）"; return 1;
        }
    done
    [[ -x "$source/.bin/linux-$arch/yg-vpngate" && -x "$source/.bin/linux-$arch/managed-core" ]] || {
        installer_error '发行包中的管理程序或受管核心不可执行'; return 1;
    }
}
installer_write_entrypoint() {
    local destination=$1 temporary
    temporary=$(mktemp "${destination}.XXXXXXXX")
    {
        printf '%s\n' '#!/usr/bin/env bash' '# Generated from the running sing-box-vpn installer (GPL-3.0).' 'set -Eeuo pipefail'
        printf 'REPOSITORY=%q\n' "$REPOSITORY"
        declare -f installer_error installer_help installer_installed installer_arch installer_require_system \
            installer_menu_text installer_menu installer_validate_package installer_write_entrypoint \
            installer_download_tools installer_download installer_main
        printf '%s\n' 'if [[ -z "${BASH_SOURCE[0]:-}" || "${BASH_SOURCE[0]}" == "$0" ]]; then installer_main "$@"; fi'
    } > "$temporary"
    chmod 0755 "$temporary"
    mv -f -- "$temporary" "$destination"
}
installer_download_tools() {
    local -a missing=()
    command -v curl >/dev/null || missing+=(curl)
    command -v tar >/dev/null || missing+=(tar)
    [[ -s /etc/ssl/certs/ca-certificates.crt ]] || missing+=(ca-certificates)
    if ((${#missing[@]})); then
        apt-get update -qq
        DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends "${missing[@]}"
    fi
}
installer_download() (
    local action=$1 version="${SB_RELEASE_VERSION:-}" url archive scratch sum name extra digest='' count=0 member
    installer_download_tools
    if [[ -z "$version" ]]; then
        if ! url=$(curl -fLsSI --proto '=https' --proto-redir '=https' --retry 2 --connect-timeout 15 --max-time 60 \
            -o /dev/null -w '%{url_effective}' "$REPOSITORY/releases/latest"); then
            installer_error '无法获取正式 Release；请检查网络或是否已发布完整发行包'
            return 1
        fi
        [[ "$url" == "$REPOSITORY/releases/tag/"* ]] || { installer_error '无法确认正式发行地址'; return 1; }
        version="${url##*/}"
    fi
    [[ "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
        installer_error "发行版本号无效：$version；需要正式版本 vMAJOR.MINOR.PATCH"
        return 1
    }
    archive="sing-box-vpn-$version.tar.gz"
    scratch=$(mktemp -d /tmp/sing-box-vpn.XXXXXXXX)
    cleanup() { case "$scratch" in /tmp/sing-box-vpn.*) rm -rf -- "$scratch";; esac; }
    trap cleanup EXIT
    echo "下载 $version 并校验 SHA256..."
    curl -fLsS --proto '=https' --proto-redir '=https' --retry 2 --connect-timeout 15 --max-time 180 \
        "$REPOSITORY/releases/download/$version/$archive" -o "$scratch/$archive"
    curl -fLsS --proto '=https' --proto-redir '=https' --retry 2 --connect-timeout 15 --max-time 60 \
        "$REPOSITORY/releases/download/$version/SHA256SUMS" -o "$scratch/SHA256SUMS"
    while read -r sum name extra; do
        [[ "$name" == "$archive" ]] || continue
        [[ "$sum" =~ ^[[:xdigit:]]{64}$ && -z "$extra" ]] || { installer_error 'SHA256 清单格式无效'; return 1; }
        digest=$sum
        count=$((count + 1))
    done < "$scratch/SHA256SUMS"
    ((count == 1)) || { installer_error 'SHA256 清单缺少唯一的发行包校验值'; return 1; }
    (cd -- "$scratch" && printf '%s  %s\n' "$digest" "$archive" | sha256sum -c -)
    # Only regular files/directories under the release root may be extracted.
    tar -tzf "$scratch/$archive" > "$scratch/members"
    while IFS= read -r member; do
        [[ "$member" == sing-box-vpn/* && "$member" != *'/../'* && "$member" != */.. &&
           "$member" != *'/./'* && "$member" != */. && "$member" != *'//'* ]] || {
            installer_error '发行包包含不安全的路径'; return 1;
        }
    done < "$scratch/members"
    tar -tvzf "$scratch/$archive" > "$scratch/types"
    while IFS= read -r member; do
        [[ "$member" == -* || "$member" == d* ]] || { installer_error '发行包包含链接或特殊文件'; return 1; }
    done < "$scratch/types"
    tar -xzf "$scratch/$archive" -C "$scratch" --no-same-owner --no-same-permissions --keep-old-files
    installer_validate_package "$scratch/sing-box-vpn"
    # A published archive may contain an older bootstrap. Preserve this entry
    # for the installed menu, even when invoked from a pipe or /dev/fd.
    installer_write_entrypoint "$scratch/sing-box-vpn/install.sh"
    bash "$scratch/sing-box-vpn/sb.sh" "$action"
)
installer_main() {
    local option local_install=0 yes=0 source_root answer
    ACTION=''
    for option in "$@"; do
        case "$option" in
            help|--help|-h) installer_help; return 0;;
            --local) local_install=1;;
            --yes|-y) yes=1;;
            --upgrade) option=upgrade;;
            --uninstall) option=uninstall;;
            menu|install|upgrade|uninstall|panel|status|diagnose|reset-admin|recover) ;;
            *) installer_error "未知选项: $option"; return 1;;
        esac
        case "$option" in --local|--yes|-y) continue;; esac
        [[ -z "$ACTION" ]] || { installer_error '一次只能指定一个操作'; return 1; }
        ACTION=$option
    done
    if [[ -z "$ACTION" ]]; then
        if ((local_install)); then ACTION=install; else ACTION=menu; fi
    fi
    if ((local_install)) && [[ "$ACTION" != install && "$ACTION" != upgrade ]]; then
        installer_error '--local 仅用于完整发行包的安装或升级'; return 1
    fi
    if ((yes)) && [[ "$ACTION" != uninstall && "$ACTION" != menu ]]; then
        installer_error '--yes 仅用于卸载'; return 1
    fi
    installer_require_system
    if [[ "$ACTION" == menu ]]; then installer_menu; fi
    [[ "$ACTION" != exit ]] || return 0
    if [[ "$ACTION" == install ]] && installer_installed; then ACTION=upgrade; fi
    if [[ "$ACTION" != install ]] && ! installer_installed; then
        installer_error '未找到当前平台的完整安装；请先选择安装'; return 1
    fi
    case "$ACTION" in
        install|upgrade)
            if ((local_install)); then
                [[ -f "${BASH_SOURCE[0]}" ]] || { installer_error '--local 需要解压后的发行包'; return 1; }
                source_root="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd)"
                installer_validate_package "$source_root"
                bash "$source_root/sb.sh" "$ACTION"
            else
                installer_download "$ACTION"
            fi
            ;;
        *)
            local script="${SB_HOME:-/opt/sing-box-vpn}/sb.sh"
            [[ -f "$script" ]] || { installer_error '本机维护脚本缺失，请从完整发行包修复'; return 1; }
            if [[ "$ACTION" == uninstall ]]; then
                if ((!yes)); then
                    echo '将删除本项目的程序、配置、账户、证书和用量记录。'
                    if ! read -r -p '输入 yes 确认卸载: ' answer </dev/tty; then
                        installer_error '无交互终端；无交互卸载需明确指定 uninstall --yes'; return 1
                    fi
                    [[ "$answer" == yes ]] || { echo '已取消卸载。'; return 0; }
                fi
                bash "$script" uninstall --yes
            else
                bash "$script" "$ACTION"
            fi
            ;;
    esac
}
if [[ -z "${BASH_SOURCE[0]:-}" || "${BASH_SOURCE[0]}" == "$0" ]]; then installer_main "$@"; fi
