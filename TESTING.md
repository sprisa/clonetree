# Testing

## Unit tests

```sh
GOWORK=off go test -race ./...
```

The black-box `e2e` package is opt-in. Configured runs fail if the fixture directory
is on the wrong filesystem or native cloning is unavailable.

## Ubuntu Docker: XFS and Btrfs

```sh
docker build -f e2e/Dockerfile -t clonetree-e2e .
docker run --rm --privileged clonetree-e2e
# Or test one filesystem:
docker run --rm --privileged clonetree-e2e xfs
docker run --rm --privileged clonetree-e2e btrfs
```

The Ubuntu container needs mount privileges and a Linux kernel with loop, XFS,
and Btrfs support. It creates fresh 1 GiB sparse image files, enables XFS reflinks,
mounts the images, and removes the mounts/images on exit. Both images are mounted
so cross-filesystem cloning failures can be tested. Formatting targets are always
new image files, not existing disks.

## Ubuntu VM or Linux host

```sh
sudo apt-get update
sudo apt-get install -y xfsprogs btrfs-progs util-linux e2fsprogs
GOWORK=off go build -o /tmp/clonetree-e2e .
GOWORK=off go test -c -o /tmp/clonetree-e2e.test ./e2e
sudo env CLONETREE_E2E_BINARY=/tmp/clonetree-e2e \
  CLONETREE_E2E_TEST_BINARY=/tmp/clonetree-e2e.test bash e2e/linux.sh
```

To use an already-mounted filesystem, run without mount privileges:

```sh
CLONETREE_E2E_ROOT=/mnt/test-volume CLONETREE_E2E_FS=xfs \
  GOWORK=off go test -count=1 -v ./e2e
```

Use `btrfs` for Btrfs, and optionally set `CLONETREE_E2E_OTHER_ROOT` to a writable
directory on the other filesystem to include the cross-filesystem failure test.

## macOS host: APFS

```sh
bash e2e/macos.sh
```

This uses a temporary directory on the host and verifies it is APFS. APFS tests
run directly on macOS; there is no Linux-container APFS job.

## Coverage

- Built CLI subprocesses: `init`, `new`, `info`, and `delete`.
- Default/custom destinations, current-directory semantics, and multiple read batches.
- Hidden/ignored files, VCS metadata, Unicode names, permissions, times, and symlinks.
- APFS native-directory eligibility, set-ID/BSD-flag fallback, timestamp restoration,
  hardlink independence, late exclusions, and cancellation after a native clone.
- `info --skip-private-size` omits private accounting while keeping live logical sizes.
- Immediate interrupt acknowledgement, second-interrupt exit, cleanup status, and
  native-batch progress without a per-file ETA.
- Shared extents on XFS/Btrfs (`filefrag`), and low initial private usage on APFS.
- Independent edits in both directions; APFS private storage grows when the original changes.
- Existing IDs and symlink protection, excluded snapshots, and failed-clone cleanup.
- Cross-filesystem cloning fails instead of falling back to a full copy.
