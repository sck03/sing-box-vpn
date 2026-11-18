#!/usr/bin/env bash
# No host installation: all downloads and installer hand-offs use fixtures.
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
SCRATCH=$(mktemp -d /tmp/sb-installer-test.XXXXXXXX)
cleanup() { case "$SCRATCH" in /tmp/sb-installer-test.*) rm -rf -- "$SCRATCH";; esac; }
trap cleanup EXIT
export TEST_HOME="$SCRATCH" SB_HOME="$SCRATCH/app" SB_ROOT="$SCRATCH/config"
mkdir -p "$SCRATCH/source/sing-box-vpn/lib" "$SCRATCH/artifacts"
cat > "$SCRATCH/source/sing-box-vpn/sb.sh" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" > "$TEST_HOME/invoked"
MOCK
touch "$SCRATCH/source/sing-box-vpn/lib/update.sh"
cp "$ROOT/install.sh" "$SCRATCH/source/sing-box-vpn/install.sh"
archive=sing-box-vpn-v4.0.0.tar.gz
pack() {
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
        */releases/latest) echo "https://github.com/sck03/sing-box-vpn/releases/tag/${TEST_VERSION:-v4.0.0}";;
        */sing-box-vpn-v4.0.0.tar.gz) cp "$TEST_HOME/artifacts/sing-box-vpn-v4.0.0.tar.gz" "$output";;
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

# The exact remote-entry invocation forms do not depend on a sibling lib/.
bash <(cat "$ROOT/install.sh") install > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == install ]]
reset_calls
cat "$ROOT/install.sh" | bash -s -- install > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == install ]]
reset_calls
SB_RELEASE_VERSION=v4.0.0 bash "$ROOT/install.sh" install > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == install ]]
[[ $(wc -l < "$SCRATCH/downloads") == 2 ]]

mkdir -p "$SB_HOME" "$SB_ROOT"
touch "$SB_HOME/.managed" "$SB_ROOT/.platform-v4"
cp "$SCRATCH/source/sing-box-vpn/sb.sh" "$SB_HOME/sb.sh"
reset_calls
bash "$ROOT/install.sh" install > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == upgrade ]]
reset_calls
bash "$ROOT/install.sh" uninstall --yes > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == 'uninstall --yes' && ! -s "$SCRATCH/downloads" ]]
reset_calls
bash "$SCRATCH/source/sing-box-vpn/install.sh" --local --upgrade > "$SCRATCH/output"
[[ $(<"$SCRATCH/invoked") == upgrade && ! -s "$SCRATCH/downloads" ]]

must_fail env TEST_ARCH=ppc64le bash "$ROOT/install.sh" install
[[ ! -s "$SCRATCH/downloads" ]]
must_fail env TEST_VERSION=v2.1.0 bash "$ROOT/install.sh" install
grep -q 'v4.*Release' "$SCRATCH/output"
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
echo 'Installer: streamed entry, local install, upgrade, offline uninstall and unsafe-download rejection passed.'
