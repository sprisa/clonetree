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

## UI layout tests

Live progress is tested the way a person sees it. `TestLiveLayoutIsStable` (in
the e2e suites above) runs `new`, `info`, and `ls` in a pseudo-terminal, replays
the output through a terminal emulator
([`charmbracelet/x/vt`](https://github.com/charmbracelet/x/tree/main/vt)), and
snapshots the screen whenever output pauses. Across every frame it checks that:

- each progress bar keeps its column and width, and never disappears while its
  row is still running;
- `DISK USED`, `SAVED`, and `FULL COPY` values stay right-aligned under their headers, whether a
  row is waiting, running, or done;
- running timers always show tenths (`19.9s`, `20.0s`);
- live frames never reach the last terminal column, where terminals wrap.

The sample project comes from [`internal/demotree`](internal/demotree), with
tiny files so it fits the 1 GiB Linux test images.

The README recording is checked with the same rules. `docs/demo/record.sh`
records text frames alongside the GIF (VHS `Output *.txt`, sampled every 100ms
during live views) and rejects the recording if `TestRecordedFramesAreStable`
finds a jump:

```sh
brew install vhs
bash docs/demo/record.sh
```

## Coverage

- Built CLI subprocesses: `init`, `new`, `info`, and `delete`; `list`/`ls` in-process.
- Concurrent size measurement matches a sequential walk for any reader count, and
  errors are attributed to the right snapshot. `ls` never reads snapshot contents.
- Live `info` rows resolve independently (finished, estimated, counting, waiting),
  and entry-count estimates round-trip through the cache.
- Live frames fit narrow terminals; the stopwatch always shows tenths.
- `clonetree`, `help`, and `--help` show the overview with plain ASCII art when
  output has no color; the art sits beside the text, shrinks to an icon, or is
  hidden as the terminal narrows, and never pushes the text down.
- Default/custom destinations, current-directory semantics, and multiple read batches.
- Hidden/ignored files, VCS metadata, Unicode names, permissions, times, and symlinks.
- APFS native-directory eligibility, set-ID/BSD-flag fallback, timestamp restoration,
  hardlink independence, late exclusions, and cancellation after a native clone.
- `info --size-only` (and the old `--skip-private-size`) skips disk-usage accounting while keeping live logical sizes.
- Immediate interrupt acknowledgement, second-interrupt exit, cleanup status, and
  native-batch progress without a per-file ETA.
- Shared extents on XFS/Btrfs (`filefrag`), and low initial disk usage on APFS.
- Independent edits in both directions; APFS disk used grows when the original changes.
- Existing IDs and symlink protection, excluded snapshots, and failed-clone cleanup.
- Cross-filesystem cloning fails instead of falling back to a full copy.
