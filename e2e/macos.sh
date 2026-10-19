#!/usr/bin/env bash
set -euo pipefail

[[ "$(uname -s)" == Darwin ]] || { printf 'APFS e2e tests run on a macOS host.\n' >&2; exit 1; }
project="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/clonetree-apfs-e2e.XXXXXX")"
trap 'rm -rf -- "$work"' EXIT

CLONETREE_E2E_ROOT="$work" CLONETREE_E2E_FS=apfs GOWORK=off \
  go -C "$project" test -count=1 -v ./e2e
