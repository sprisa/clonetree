# Performance

Clonetree shares existing file data through native filesystem clones. Runtime
depends primarily on the number of entries and the filesystem's metadata work.
Large file contents stay shared until either copy changes.

## Benchmarks

Measured October 26, 2026, on an Apple M4 Max with 128 GiB RAM, macOS 27.0.1,
APFS, and Go 1.27.3. Commands ran non-interactively with default Go garbage
collection.

### APFS cloning

The workload is a fixed snapshot of a local monorepo, including installed
dependencies, build output, hidden files, and repository metadata.

| Workload | Value |
| --- | ---: |
| Logical file bytes | 61,280,422,870 (**57.1 GiB**) |
| Regular files | 320,652 |
| Directories | 59,778 |
| Symlinks | 16,844 |

Results for `clonetree --no-progress new`, averaged over two runs:

| Measurement | Mean |
| --- | ---: |
| Wall time | **9.867s** |
| User CPU time | 0.260s |
| System CPU time | 8.100s |
| Total process CPU time | **8.360s** |
| Peak resident memory (RSS) | **14.031 MiB** |

| Run | Wall time | User CPU | System CPU | Peak RSS |
| --- | ---: | ---: | ---: | ---: |
| 1 | 9.963s | 0.260s | 8.190s | 14.203 MiB |
| 2 | 9.771s | 0.260s | 8.010s | 13.859 MiB |

CPU time is user plus system time for the process. It measures CPU work across
the command's lifetime, not peak utilization; cloning can occupy a core while
running. Wall time also includes time waiting for filesystem operations.

### Memory scaling

The following fixed-depth sources have one small file per leaf directory, grouped
into directories of at most 256 leaves. `runtime.MemStats.HeapAlloc` was sampled
every 20 ms during clone creation, with fixture setup and deletion excluded.

| Leaf directories | Sampled peak Go heap |
| --- | ---: |
| 4,000 | 3.71 MiB |
| 16,000 | 3.74 MiB |
| 64,000 | 3.73 MiB |

On the monorepo workload, separately sampled peak Go heap was **4.9 MiB**.

Go heap and RSS are different measurements. RSS covers the resident memory of the
whole process. The live Go heap depends on traversal depth and fixed cache limits;
it does not retain a record for every file or directory. Cumulative allocations
increase as entries are processed, while garbage collection reclaims temporary
metadata.

### Size listing

`info` walks every snapshot to calculate live sizes. It measures all snapshots
concurrently with a shared pool of six directory readers. Readers prefer the first
unfinished snapshot and move to later ones only when it has no directories ready,
so rows finish in order while the total work stays the same. Child directories
are opened relative to their parent's descriptor.

Measured November 24, 2026, on an Apple M4 Max with 128 GiB RAM and macOS 27.2.1:
10 snapshots of a local monorepo, **4.83 million entries** and 527 GiB of logical
file data in total. Commands ran with `--no-progress`, with warm caches.

| Command | Before (sequential) | After (concurrent) | Process CPU after | Peak RSS after |
| --- | ---: | ---: | ---: | ---: |
| `info` | 120–126s | **17.8–18.7s** | 58.5–65.5s | 21.6 MiB |
| `info --size-only` | 69.2s | **13.5–13.7s** | 41.2–42.5s | 22.7 MiB |

The concurrent walk uses more total CPU: most of `getattrlistbulk`'s work on APFS
runs under kernel locks, and contention grows with the number of readers. Sweeping
the reader count on the same workload (disk usage included):

| Readers | Wall time | System CPU |
| ---: | ---: | ---: |
| 1 (previous sequential walk) | 120–126s | 36–37s |
| 4 | 22.7s | 47s |
| 6 | 17.8–18.4s | 58–59s |
| 8 | 17.3–18.3s | 90–93s |
| 12 | 28.5s | 273s |
| 16 | 34.7–36.0s | 429–445s |

Six readers is the knee: eight is no faster and costs half again as much CPU.
Beyond that, wall time gets worse as well. Opening child directories relative to
their parents did not change these numbers; the contention is inside the
metadata reads themselves.

`--size-only` omits the APFS private-size queries behind `DISK USED` and `SAVED`.
It still scans the files to calculate logical size.

`list` (`ls`) reads only the snapshot directory itself and returns immediately,
regardless of snapshot size.

### Deletion

`delete` removes a snapshot with four parallel workers. Workers take
directories, unlink their files relative to the open directory, and remove each
directory once everything inside it is gone. Cleanup after a cancelled or
failed `new` uses the same path. If parallel removal hits anything unexpected,
the serial `os.RemoveAll` path finishes the job.

Measured November 25, 2026, on the same machine: a fresh APFS clone of a sample
project with 254,000 files (`go run ./docs/demo/fixture`, 900 packages), one
fresh clone per run, two runs each.

| Removal | Wall time |
| --- | ---: |
| `os.RemoveAll` (previous) | 13.1–13.2s |
| 1 worker | 13.2–13.3s |
| 2 workers | 8.9–9.0s |
| 3 workers | 6.7–6.8s |
| **4 workers** | **6.2–6.8s** |
| 5 workers | 6.4s |
| 6 workers | 6.8–7.1s |
| 8 workers | 7.7–8.1s |
| 16 workers | 12.2–12.7s |

As with reads, APFS serializes much of the work, and contention outweighs the
gain past four workers. End to end, `clonetree --no-progress delete` on that
clone takes 6.1–6.2s, down from 12.4–12.5s.

### Live progress estimates

Interactive `info` runs record each snapshot's entry count in the user cache
directory (`~/Library/Caches/clonetree/entries.json` on macOS,
`$XDG_CACHE_HOME/clonetree/entries.json` on Linux). The next interactive run uses
those counts as the denominator for per-snapshot progress bars, capped at 99%
until a snapshot actually finishes. Snapshots without a previous count show a
live entry count instead. Sizes are always measured live; the cache only drives
the progress display, and an unreadable cache just means no percentages.

## Filesystem operations

### APFS directory cloning

A source scan identifies subtrees that require per-entry handling. Excluded
directories, set-ID permissions, BSD flags, and unavailable flag information
prevent the containing subtree from using native directory cloning. Eligible
children can use the native path even when their parent requires per-entry work.

Eligible subtrees with at most 65,536 planned descendants are cloned with native
APFS directory operations. Larger subtrees use smaller eligible children or the
per-file path, allowing cancellation checks between operations. The completed
subtree is scanned to calculate its logical size, restore directory timestamps,
and remove excluded directories that appeared during cloning. Unsupported
directory-clone operations use native per-file cloning. There is no full-copy
fallback.

Progress identifies native cloning separately from metadata checking. Checked
entries are reported during the final scan; native batches show no per-file rate
or ETA. Cancellation is checked before and after each operation and during scans.
The first interrupt acknowledges cancellation and starts cleanup; a second exits
without waiting for cleanup. Graceful cancellation removes the partial clone.

### Metadata traversal and memory bounds

On macOS, `getattrlistbulk` reads metadata into reusable 64 KiB buffers. Names are
owned independently of those buffers. Standard metadata reads handle unsupported
bulk operations, mount points, firmlinks, and missing required attributes.
Linux uses the standard metadata reader and per-file XFS/Btrfs reflinks.

Native-clone eligibility tracks unsafe directory paths and their ancestors, with
a limit of 4,096 paths. Reaching that limit selects the per-entry path. Pending
late-exclusion records are capped too. Directory metadata is read when needed
during restoration. Traversal and cleanup enumerate entries in bounded batches.

Writable trees are removed directly in one streaming pass. If copied directory
permissions prevent deletion, remaining directories are made writable in bounded
batches and removal is retried.

Working memory therefore scales with traversal depth and fixed limits, rather
than total entry count. Runtime still scales with the entries being visited.

## Measurement method

- Binaries were built with `go build -trimpath -ldflags='-s -w'`. `GOGC`,
  `GOMEMLIMIT`, and `GODEBUG` were unset.
- Commands used a prepared input snapshot and wrote outside it on the same APFS
  filesystem. Existing `.clonetree` directories were excluded from that input.
- Wall time surrounds `/usr/bin/time -l <command>`. CPU time and peak RSS come
  from the timing tool. RSS bytes are converted to MiB.
- Clone timings include eligibility scanning, native cloning, output inspection,
  and timestamp restoration. Independent output verification, fixture creation,
  and deletion are outside the timings. Filesystem caches were not cleared.
- Outputs were checked for matching paths, entry types, logical sizes,
  permissions, modification times, and symlink targets. The head and tail of 64
  sampled files were checked for matching contents.
- Heap measurements use a separate sampling harness with default garbage
  collection. They are not total-allocation measurements.

## Run benchmarks

From this module, run the filesystem fixtures and build a local binary:

```sh
GOWORK=off go test -run '^$' -bench . -benchmem
GOWORK=off go build -trimpath -ldflags='-s -w' -o /tmp/clonetree .
```

From the directory you want to clone, choose a temporary parent on the same
filesystem and measure each command:

```sh
BENCH=$(mktemp -d "${TMPDIR:-/tmp}/clonetree-bench.XXXXXX")
/usr/bin/time -l /tmp/clonetree --dir "$BENCH" --no-progress new sample
/usr/bin/time -l /tmp/clonetree --dir "$BENCH" --no-progress info
/usr/bin/time -l /tmp/clonetree --dir "$BENCH" --no-progress info --size-only
/usr/bin/time -l /tmp/clonetree --dir "$BENCH" list
/tmp/clonetree --dir "$BENCH" --no-progress delete sample
rmdir "$BENCH"
```
