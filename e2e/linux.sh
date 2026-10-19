#!/usr/bin/env bash
set -euo pipefail

selection="${1:-all}"
if [[ "$#" -gt 1 ]]; then
  printf 'Usage: bash e2e/linux.sh [all|xfs|btrfs]\n' >&2
  exit 2
fi
case "$selection" in
  all) filesystems=(xfs btrfs) ;;
  xfs|btrfs) filesystems=("$selection") ;;
  *) printf 'Usage: bash e2e/linux.sh [all|xfs|btrfs]\n' >&2; exit 2 ;;
esac

if [[ "$(uname -s)" != Linux || "$EUID" != 0 ]]; then
  printf 'Linux filesystem images require a Linux host and root mount privileges.\n' >&2
  exit 1
fi
for tool in mkfs.xfs mkfs.btrfs mount umount truncate losetup filefrag; do
  command -v "$tool" >/dev/null || { printf 'Missing tool: %s\n' "$tool" >&2; exit 1; }
done

export CLONETREE_E2E_BINARY="${CLONETREE_E2E_BINARY:-/usr/local/bin/clonetree}"
test_binary="${CLONETREE_E2E_TEST_BINARY:-/usr/local/bin/clonetree-e2e.test}"
[[ -x "$CLONETREE_E2E_BINARY" && -x "$test_binary" ]] || {
  printf 'Set CLONETREE_E2E_BINARY and CLONETREE_E2E_TEST_BINARY to built executables.\n' >&2
  exit 1
}

work="$(mktemp -d "${TMPDIR:-/tmp}/clonetree-fs-e2e.XXXXXX")"
mounts=()
cleanup() {
  status=$?
  trap - EXIT INT TERM
  set +e
  failed=0
  for ((i=${#mounts[@]}-1; i>=0; i--)); do
    if ! umount "${mounts[$i]}"; then
      failed=1
      status=1
    fi
  done
  if [[ "$failed" == 0 ]]; then
    rm -rf -- "$work"
  else
    printf 'Unmount failed; retained disposable images at %s\n' "$work" >&2
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Format fresh regular files only. mount's loop option uses auto-clearing loop
# devices, released when these private mounts are unmounted.
for filesystem in xfs btrfs; do
  image="$work/$filesystem.img"
  directory="$work/$filesystem"
  truncate -s 1G "$image"
  mkdir "$directory"
  if [[ "$filesystem" == xfs ]]; then
    mkfs.xfs -q -f -m reflink=1 "$image"
  else
    mkfs.btrfs -q -f --nodiscard "$image"
  fi
  mount -t "$filesystem" -o loop "$image" "$directory"
  mounts+=("$directory")
  mkdir "$directory/tests"
done

for filesystem in "${filesystems[@]}"; do
  other=xfs
  [[ "$filesystem" == xfs ]] && other=btrfs
  printf '\n=== Native %s end-to-end tests ===\n' "$filesystem"
  CLONETREE_E2E_ROOT="$work/$filesystem/tests" \
    CLONETREE_E2E_FS="$filesystem" \
    CLONETREE_E2E_OTHER_ROOT="$work/$other/tests" \
    "$test_binary" -test.v -test.count=1 -test.timeout=5m
done
