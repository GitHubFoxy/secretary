#!/usr/bin/env bash
# TEST FIXTURE ONLY. Never use this to populate a published binary release.
set -euo pipefail
[[ $# -eq 1 ]] || { echo "Usage: $0 FIXTURE_PACKAGE" >&2; exit 2; }
out="$1/bin/native-acp"
mkdir -p "$out"
temp="$(mktemp -d)"
trap 'rm -rf "$temp"' EXIT
printf '#!/bin/sh\nif [ "$1" = "--version" ]; then echo "@agentclientprotocol/codex-acp 2.2.2"; else exit 96; fi\n' > "$temp/codex-acp-2.2.2"
chmod 700 "$temp/codex-acp-2.2.2"
tar -czf "$out/codex-acp.tar.gz" -C "$temp" codex-acp-2.2.2
if command -v sha256sum >/dev/null 2>&1; then
 (cd "$out" && sha256sum codex-acp.tar.gz > codex-acp.tar.gz.sha256)
else
 (cd "$out" && shasum -a 256 codex-acp.tar.gz > codex-acp.tar.gz.sha256)
fi
