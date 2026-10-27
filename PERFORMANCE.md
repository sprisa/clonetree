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
| Logical file bytes | 61,279,400,665 (**57.1 GiB**) |
| Regular files | 320,444 |
| Directories | 59,700 |
| Symlinks | 16,875 |

Results for `clonetree --no-progress new`, averaged over two runs:

| Measurement | Mean |
| --- | ---: |
| Wall time | **10.031s** |
| User CPU time | 0.305s |
| System CPU time | 8.705s |
| Total process CPU time | **9.010s** |
| Peak resident memory (RSS) | **14.555 MiB** |

| Run | Wall time | User CPU | System CPU | Peak RSS |
| --- | ---: | ---: | ---: | ---: |
| 1 | 9.932s | 0.300s | 8.680s | 14.250 MiB |
| 2 | 10.130s | 0.310s | 8.730s | 14.859 MiB |

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

This workload contains 57.1 GiB of file data, 320,399 regular files, 59,685
directories, and 16,875 symlinks. Results are means of two runs per command.

| Command | Wall time | Process CPU time | Peak RSS |
| --- | ---: | ---: | ---: |
| `info` | 2.447s | 2.430s | 13.320 MiB |
| `info --skip-private-size` | 1.927s | 1.900s | 13.164 MiB |

`info` calculates live logical sizes and APFS private storage for every entry.
Private storage can change when another clone or the original is modified or
deleted, even if this clone is untouched.

`--skip-private-size` omits the private-storage queries and the `PRIVATE` column.
It still scans the files to calculate `SIZE`. In this measurement it used about
22% less process CPU time.

## Filesystem operations

### APFS directory cloning

A source scan identifies subtrees that require per-entry handling. Excluded
directories, set-ID permissions, BSD flags, and unavailable flag information
prevent the containing subtree from using native directory cloning. Eligible
children can use the native path even when their parent requires per-entry work.

Eligible subtrees are cloned with native APFS directory operations. The completed
subtree is scanned to calculate its logical size, restore directory timestamps,
and remove excluded directories that appeared during cloning. Unsupported
directory-clone operations use native per-file cloning. There is no full-copy
fallback.

Progress updates in directory batches. Cancellation is checked before and after
a native batch and during its final scan. Failed or cancelled clones are removed.

### Metadata traversal and memory bounds

On macOS, `getattrlistbulk` reads metadata into reusable 64 KiB buffers. Names are
owned independently of those buffers. Standard metadata reads handle unsupported
bulk operations, mount points, firmlinks, and missing required attributes.
Linux uses the standard metadata reader and per-file XFS/Btrfs reflinks.

Native-clone eligibility tracks unsafe directory paths and their ancestors, with
a limit of 4,096 paths. Reaching that limit selects the per-entry path. Pending
late-exclusion records are capped too. Directory metadata is read when needed
during restoration. Traversal and cleanup enumerate entries in bounded batches.

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
/usr/bin/time -l /tmp/clonetree --dir "$BENCH" --no-progress info --skip-private-size
/tmp/clonetree --dir "$BENCH" --no-progress delete sample
rmdir "$BENCH"
```
