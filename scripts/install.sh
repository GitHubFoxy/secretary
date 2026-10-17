#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

command -v git >/dev/null 2>&1 || fail "Git is required; install git and retry."
command -v zsh >/dev/null 2>&1 || fail "zsh is required (macOS includes it; on Debian/Ubuntu: sudo apt install zsh)."
if ! command -v mise >/dev/null 2>&1 && ! command -v go >/dev/null 2>&1; then
  fail "Go 1.23+ or mise is required to build Secretary."
fi
command -v openssl >/dev/null 2>&1 || fail "OpenSSL is required to create private bootstrap credentials."
case "$(uname -s)" in
  Darwin|Linux) ;;
  *) fail "Secretary server supports macOS and Linux only." ;;
esac

target="${SECRETARY_INSTALL_DIR:-$HOME/.local/src/secretary}"
mkdir -p "$(dirname "$target")"
if [[ -e "$target" ]]; then
  [[ -d "$target/.git" ]] || fail "Installation destination exists but is not a Git checkout: $target"
  origin="$(git -C "$target" remote get-url origin)"
  case "$origin" in
    https://github.com/GitHubFoxy/secretary|https://github.com/GitHubFoxy/secretary.git|git@github.com:GitHubFoxy/secretary.git) ;;
    *) fail "Installation destination is a different repository: $target" ;;
  esac
  [[ -z "$(git -C "$target" status --porcelain)" ]] || fail "Existing checkout has local changes; not overwriting it."
  git -C "$target" pull --ff-only
else
  git clone https://github.com/GitHubFoxy/secretary.git "$target"
fi

export PATH="$HOME/.local/bin:$PATH"
if [[ -t 1 && -r /dev/tty ]]; then
  "$target/secretary" setup --interactive
else
  "$target/secretary" setup --non-interactive
fi
printf 'Secretary is installed in %s\nAdd %s to PATH if necessary.\n' "$target" "$HOME/.local/bin"
