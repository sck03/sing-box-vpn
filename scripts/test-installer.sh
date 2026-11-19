#!/usr/bin/env bash
# No host installation: all downloads and installer hand-offs use fixtures.
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SCRATCH=$(mktemp -d /tmp/sb-installer-test.XXXXXXXX)
cleanup() { case "$SCRATCH" in /tmp/sb-installer-test.*) rm -rf -- "$SCRATCH";; esac; }
trap cleanup EXIT
export TEST_HOME="$SCRATCH" SB_HOME="$SCRATCH/app" SB_ROOT="$SCRATCH/config"
export TEST_VERSION=v5.0.0 TEST_INSTALLED_VERSION=v5.0.0
mkdir -p "$SCRATCH/source/sing-box-vpn/lib" "$SCRATCH/artifacts"
cat > "$SCRATCH/source/sing-box-vpn/sb.sh" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" > "$TEST_HOME/invoked"
entry="$(dirname -- "${BASH_SOURCE[0]}")/install.sh"
if [[ -f "$entry" ]]; then cp "$entry" "$TEST_HOME/retained-installer.sh"; fi
MOCK
touch "$SCRATCH/source/sing-box-vpn/lib/"{update,ownership,restart-panel}.sh
for arch in amd64 arm64; do
    mkdir -p "$SCRATCH/source/sing-box-vpn/.bin/linux-$arch"
    cat > "$SCRATCH/source/sing-box-vpn/.bin/linux-$arch/yg-vpngate" <<'MOCK'
#!/usr/bin/env bash
printf 'yg-vpngate %s\n' "$TEST_INSTALLED_VERSION"
MOCK
    cp "$SCRATCH/source/sing-box-vpn/.bin/linux-$arch/yg-vpngate" "$SCRATCH/source/sing-box-vpn/.bin/linux-$arch/managed-core"
    chmod +x "$SCRATCH/source/sing-box-vpn/.bin/linux-$arch/"*
done
export SB_MANAGER="$SCRATCH/source/sing-box-vpn/.bin/linux-amd64/yg-vpngate"
# Simulate an already-published archive containing the old, broken entry point.
printf '#!/usr/bin/env bash\necho "old v4-only entry"\nexit 42\n' > "$SCRATCH/source/sing-box-vpn/install.sh"
archive=''
pack() {
    archive="sing-box-vpn-$TEST_VERSION.tar.gz"
    printf '{"release":"%s"}\n' "$TEST_VERSION" > "$SCRATCH/source/sing-box-vpn/versions.json"
    tar -czf "$SCRATCH/artifacts/$archive" -C "$SCRATCH/source" sing-box-vpn
    (cd "$SCRATCH/artifacts" && sha256sum "$archive" > SHA256SUMS)
}
pack
id() { echo 0; }
uname() { if [[ "$1" == -s ]]; then echo Linux; else echo "${TEST_ARCH:-x86_64}"; fi; }
systemctl() { :; }
apt-get() { echo 'Unexpected package installation during download fixture' >&2; return 1; }
curl() {
    local output='' url=''
    while (($#)); do
        case "$1" in
            -o) output=$2; shift 2;;
            https://*) url=$1; shift;;
            *) shift;;
        esac
    done
    printf '%s\n' "$url" >> "$TEST_HOME/downloads"
    [[ "${TEST_CURL_FAIL:-0}" == 0 ]] || return 22
    case "$url" in
        */releases/latest) echo "https://github.com/sck03/sing-box-vpn/releases/tag/$TEST_VERSION";;
        */sing-box-vpn-*.tar.gz) cp "$TEST_HOME/artifacts/${url##*/}" "$output";;
        */SHA256SUMS) cp "$TEST_HOME/artifacts/SHA256SUMS" "$output";;
        *) echo "Unexpected download: $url" >&2; return 1;;
    esac
}
export -f id uname systemctl apt-get curl
reset_calls() { rm -f "$SCRATCH/invoked"; : > "$SCRATCH/downloads"; }
must_fail() {
    reset_calls
    if "$@" > "$SCRATCH/output" 2>&1; then
        echo "Unexpected success: $*" >&2; exit 1
    fi
    [[ ! -e "$SCRATCH/invoked" ]] || { echo 'Installer executed after failure' >&2; exit 1; }
}

# Stable tag major numbers do not select the platform/data format.
for version in v4.0.0 v5.0.0 v6.2.3 v12.10.20; do
    TEST_VERSION=$version
    pack
    reset_calls
    bash <(cat "$ROOT/install.sh") install > "$SCRATCH/output"
    [[ $(<"$SCRATCH/invoked") == install ]]
    grep -q "/$TEST_VERSION/$archive$" "$SCRATCH/downloads"
    bash "$SCRATCH/retained-installer.sh" --help > "$SCRATCH/help"
    grep -q 'vMAJOR.MINOR.PATCH' "$SCRATCH/help"
done
TEST_VERSION=v5.0.0
pack
reset_calls
cat "$ROOT/install.sh" | bash -s -- install > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == install ]]
reset_calls
SB_RELEASE_VERSION=v5.0.0 bash "$ROOT/install.sh" install > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == install ]]
[[ $(wc -l < "$SCRATCH/downloads") == 2 ]]
reset_calls
TEST_ARCH=arm64 bash "$ROOT/install.sh" install > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == install ]]
reset_calls
bash -c 'source "$1"; installer_menu_text' _ "$ROOT/install.sh" > "$SCRATCH/menu"
grep -q '当前状态：未安装' "$SCRATCH/menu"
if grep -q 'v4' "$SCRATCH/menu"; then echo 'Uninstalled menu hardcodes v4' >&2; exit 1; fi

mkdir -p "$SB_HOME" "$SB_ROOT"
touch "$SB_HOME/.managed" "$SB_ROOT/.platform-v5"
cp "$SCRATCH/source/sing-box-vpn/sb.sh" "$SB_HOME/sb.sh"
reset_calls
# The entry retained from the streamed install must handle future upgrades too.
TEST_VERSION=v12.10.20
pack
bash "$SCRATCH/retained-installer.sh" install > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == upgrade ]]
for version in v5.0.0 v12.10.20; do
    TEST_INSTALLED_VERSION=$version
    bash -c 'source "$1"; installer_menu_text' _ "$SCRATCH/retained-installer.sh" > "$SCRATCH/menu"
    grep -q "当前版本：yg-vpngate $version" "$SCRATCH/menu"
    if grep -q 'v4' "$SCRATCH/menu"; then echo 'Installed menu hardcodes v4' >&2; exit 1; fi
done
TEST_VERSION=v5.0.0
pack
reset_calls
bash "$ROOT/install.sh" uninstall --yes > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == 'uninstall --yes' && ! -s "$SCRATCH/downloads" ]]
reset_calls
cp "$ROOT/install.sh" "$SCRATCH/source/sing-box-vpn/install.sh"
bash "$SCRATCH/source/sing-box-vpn/install.sh" --local --upgrade > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == upgrade && ! -s "$SCRATCH/downloads" ]]

must_fail env TEST_ARCH=ppc64le bash "$ROOT/install.sh" install
[[ ! -s "$SCRATCH/downloads" ]]
for version in v5.0.0-rc.1 v05.0.0 v5.01.0 v5.0.00 v5.0 v5.0.0/extra; do
    must_fail env TEST_VERSION="$version" bash "$ROOT/install.sh" install
    grep -q '发行版本号无效' "$SCRATCH/output"
done
# Reject an incomplete legacy package because of its contents, not its major.
mv "$SCRATCH/source/sing-box-vpn/.bin/linux-amd64/managed-core" "$SCRATCH/runtime.saved"
TEST_VERSION=v2.1.0
pack
must_fail bash "$ROOT/install.sh" install
grep -q '缺少 .bin/linux-amd64/managed-core' "$SCRATCH/output"
must_fail bash "$SCRATCH/source/sing-box-vpn/install.sh" --local
mv "$SCRATCH/runtime.saved" "$SCRATCH/source/sing-box-vpn/.bin/linux-amd64/managed-core"
TEST_VERSION=v5.0.0
pack
must_fail env TEST_CURL_FAIL=1 bash "$ROOT/install.sh" install
must_fail bash "$ROOT/install.sh" --unknown
must_fail bash "$ROOT/install.sh" install uninstall
must_fail bash "$ROOT/install.sh" --local uninstall

printf '%064d  %s\n' 0 "$archive" > "$SCRATCH/artifacts/SHA256SUMS"
must_fail bash "$ROOT/install.sh" install
pack
cp "$SCRATCH/artifacts/SHA256SUMS" "$SCRATCH/extra-sum"
cat "$SCRATCH/extra-sum" >> "$SCRATCH/artifacts/SHA256SUMS"
must_fail bash "$ROOT/install.sh" install
ln -s "$SCRATCH/elsewhere" "$SCRATCH/source/sing-box-vpn/unsafe-link"
pack
must_fail bash "$ROOT/install.sh" install
grep -q '链接或特殊文件' "$SCRATCH/output"
echo 'Installer: stable versions across majors, dynamic menus, retained entry, local install, upgrade, offline uninstall and unsafe-download rejection passed.'
