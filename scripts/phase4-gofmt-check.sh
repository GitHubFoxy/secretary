#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GOFMT=(mise exec go@1.27.1 -- gofmt)
base_ref="${PHASE4_GOFMT_BASE_REF:-}"

if [[ -z "$base_ref" && "${GITHUB_EVENT_NAME:-}" == "pull_request" ]]; then
  [[ -n "${GITHUB_BASE_REF:-}" ]] || {
    printf 'Missing GITHUB_BASE_REF for pull_request gofmt verification\n' >&2
    exit 1
  }
  base_ref="refs/remotes/origin/${GITHUB_BASE_REF}"
fi

files=()
if [[ -n "$base_ref" ]]; then
  git rev-parse --verify -q "${base_ref}^{commit}" >/dev/null || {
    printf 'Cannot resolve gofmt PR base ref: %s\n' "$base_ref" >&2
    exit 1
  }
  merge_base="$(git merge-base "$base_ref" HEAD)" || {
    printf 'Cannot find a merge base for gofmt comparison: %s\n' "$base_ref" >&2
    exit 1
  }
  mapfile -d '' -t changed < <(git diff --name-only --diff-filter=ACMR -z "$merge_base" HEAD)
  for file in "${changed[@]}"; do
    if [[ "$file" == *.go && -f "$file" ]]; then
      files+=("$file")
    fi
  done
  printf 'gofmt PR scope: %d changed Go file(s), base %s\n' "${#files[@]}" "$base_ref"
else
  mapfile -d '' -t files < <(find internal cmd web -type f -name '*.go' -print0)
  printf 'gofmt standalone scope: %d Go file(s) under internal, cmd, web\n' "${#files[@]}"
fi

if (( ${#files[@]} == 0 )); then
  exit 0
fi

unformatted="$("${GOFMT[@]}" -l "${files[@]}")"
if [[ -n "$unformatted" ]]; then
  printf 'Unformatted Go files:\n%s\n' "$unformatted" >&2
  exit 1
fi
