#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

case "$(uname -s)/$(uname -m)" in
  Linux/x86_64) platform=linux; arch=amd64 ;;
  Linux/aarch64|Linux/arm64) platform=linux; arch=arm64 ;;
  Darwin/arm64) platform=darwin; arch=arm64 ;;
  Darwin/x86_64) platform=darwin; arch=amd64 ;;
  *) echo "Unsupported CI host" >&2; exit 1 ;;
esac

cat > "$stage/releases.json" <<EOF
[
  {"tag_name":"v99.0.0","draft":false,"published_at":"2026-10-20T20:00:00Z","assets":[]},
  {"tag_name":"v0.1.0-alpha.5","draft":false,"prerelease":true,"published_at":"2026-10-20T10:00:00Z","assets":[
    {"name":"secretary-v0.1.0-alpha.5-$platform-$arch.json"},
    {"name":"secretary-v0.1.0-alpha.5-$platform-$arch.tar.gz"}
  ]},
  {"tag_name":"v0.0.9","draft":false,"published_at":"2026-10-01T10:00:00Z","assets":[
    {"name":"secretary-v0.0.9-$platform-$arch.json"},
    {"name":"secretary-v0.0.9-$platform-$arch.tar.gz"}
  ]}
]
EOF
mkdir -p "$stage/mock" "$stage/home"
cat > "$stage/mock/curl" <<'CURL'
#!/usr/bin/env bash
set -euo pipefail
dest=""
url=""
while (($#)); do
  case "$1" in
    -o) dest="$2"; shift 2 ;;
    --retry|--retry-delay) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
[[ -n "$dest" && -n "$url" ]] || exit 63
printf '%s\n' "$url" >> "$TEST_URL_LOG"
case "$url" in
  *'/releases?per_page=30') cp "$TEST_RELEASE_FILE" "$dest" ;;
  *'/scripts/install-linux-binary.sh'|*'/scripts/install-macos-binary.sh')
    if [[ "${TEST_FAIL_CHILD:-}" == 1 ]]; then
      printf 'curl: (35) TLS connect error: unexpected eof while reading\n' >&2
      exit 35
    fi
    cat > "$dest" <<'SCRIPT'
#!/usr/bin/env bash
printf '%s\n' "$SECRETARY_VERSION" > "$TEST_RESULT"
SCRIPT
    ;;
  *) echo "Unexpected URL: $url" >&2; exit 64 ;;
esac
CURL
chmod +x "$stage/mock/curl"
export HOME="$stage/home" PATH="$stage/mock:$PATH" TEST_RELEASE_FILE="$stage/releases.json"
export TEST_URL_LOG="$stage/urls" TEST_RESULT="$stage/selected"
export SECRETARY_INSTALLER_REF="feat/unified-binary-installer-20261020"

unset SECRETARY_VERSION || true
bash "$root/scripts/install.sh"
[[ "$(cat "$TEST_RESULT")" == "0.1.0-alpha.5" ]]
[[ "$(wc -l < "$TEST_URL_LOG" | tr -d '[:space:]')" == 2 ]]
grep -Fq '/releases?per_page=30' "$TEST_URL_LOG"
grep -Fq "feat/unified-binary-installer-20261020/scripts/install-$platform-binary.sh" "$TEST_URL_LOG"

: > "$TEST_URL_LOG"
SECRETARY_VERSION=0.1.0-alpha.5 bash "$root/scripts/install.sh"
[[ "$(cat "$TEST_RESULT")" == "0.1.0-alpha.5" ]]
[[ "$(wc -l < "$TEST_URL_LOG" | tr -d '[:space:]')" == 1 ]]

: > "$TEST_URL_LOG"
if SECRETARY_INSTALLER_REF="../malicious" SECRETARY_VERSION=0.1.0-alpha.5 bash "$root/scripts/install.sh" >/dev/null 2>&1; then
  echo "Unsafe installer ref was accepted" >&2
  exit 1
fi
[[ ! -s "$TEST_URL_LOG" ]]

rm -f "$TEST_RESULT"
if TEST_FAIL_CHILD=1 SECRETARY_VERSION=0.1.0-alpha.5 bash "$root/scripts/install.sh" >/dev/null 2>&1; then
  echo "Failed installer download was accepted" >&2
  exit 1
fi
[[ ! -f "$TEST_RESULT" ]]

grep -Fq 'git clone https://github.com/GitHubFoxy/secretary.git' "$root/scripts/install-from-source.sh"
grep -Fq 'Go 1.27.1+ or mise is required' "$root/scripts/install-from-source.sh"
echo "Unified binary installer platform, prerelease, explicit pin, and fail-closed tests passed"
