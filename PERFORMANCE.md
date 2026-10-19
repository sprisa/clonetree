# Performance notes

Allocation/escape-analysis pass measured with Go 1.27.2 on an Apple M4 Max,
128 GiB RAM, macOS 27.0.1, and APFS.

## Allocation results

Filesystem benchmarks use 1,024 regular files (1 KiB each), 32 directories, and
32 symlinks. Fixture setup and snapshot deletion are excluded from clone metrics.

| Operation | Before | After |
| --- | ---: | ---: |
| Plain clone: allocated bytes | ~1.22 MB | ~0.46 MB (**62% less**) |
| Plain clone: allocations | 10,332 | 6,298 |
| Clone with entry counting: allocated bytes | ~1.46 MB | ~0.70 MB |
| Snapshot sizing: allocated bytes | ~0.90 MB | ~0.41 MB (**55% less**) |
| Snapshot sizing: allocations | 8,049 | 4,882 |
| Progress formatting per frame | 808 B / 20 allocations | **0 B / 0 allocations** |

The main changes:

- Batch metadata reads directly into `FileInfo` values, avoiding `DirEntry`
  wrappers and duplicate metadata/path work where metadata is required.
- Clone regular files relative to open directory handles, avoiding full-path
  construction and conversion for each file. Destination handles are closed
  before descending, keeping descriptor use bounded by traversal depth.
- Query APFS private sizes relative to the same directory handles.
- Reuse one progress-rendering buffer with append-based formatting.
- Validate IDs with an ASCII loop, removing the regexp dependency.
- Change directory permissions during deletion only when owner access is missing.
- Skip the progress-counting pass entirely for non-TTY or `--no-progress` runs.

Compiler reports confirm traversal callbacks do not escape. Most remaining
per-entry allocations are in Go's filesystem metadata and filename APIs.

## Collection-disabled experiment

Two alternating runs per version on the 39.2 GiB monorepo, with `GOGC=off`,
`--no-progress`, and GC tracing enabled. No GC cycles were observed.

| Metric | Before | After |
| --- | ---: | ---: |
| Mean peak resident memory | 513.7 MiB | **252.0 MiB** |
| Mean wall time | 51.4s | 48.8s |
| Individual wall times | 55.1s / 47.8s | 48.7s / 48.9s |

The memory reduction is repeatable; wall times overlap and remain dependent on
filesystem/cache activity. Memory use with collection disabled still grows with
entry count because the filesystem APIs allocate per entry.

Standard Go's `GOGC=off` disables automatic collection; it does **not** remove the
collector from the binary. A normal build decreased from 6.06 MB to 5.67 MB;
`-trimpath -ldflags='-s -w'` produced a 3.77 MB executable.

## Default macOS bulk-metadata reader

The macOS default uses `getattrlistbulk` and reusable 64 KiB buffers to
fetch names, types, permissions, modification times, sizes, and optionally APFS
private sizes in batches. Metadata is passed as concrete values with owned names,
independent of the pooled buffer's lifetime. Linux uses the standard reader.
The following comparison was measured during prototyping, before promotion:

| Measurement | Standard reader | Bulk prototype |
| --- | ---: | ---: |
| Metadata-only fixture: allocated bytes | ~402 KB | ~20 KB |
| Metadata-only fixture: time | ~1.55ms | ~0.81ms |
| Full monorepo metadata scan: mean time | 2.63s | 1.10s |
| Full monorepo scan including private sizes: mean time | 3.67s | 1.53s |
| Full clone harness: Go allocations | ~278 MiB | ~83 MiB |
| Full clone harness: mean time | 53.6s | 54.6s |

Full scans visited the same 274,092 entries. Metadata scan timings are two
alternating runs per reader with warm filesystem caches. The clone harness
uses identical clone operations with only the metadata reader swapped, two
alternating runs per reader, and `GOGC=off`; allocated bytes are total Go heap
allocations during copying, not peak resident memory.

The prototype significantly reduces allocation volume and metadata-query time,
but has not demonstrated an end-to-end clone-speed improvement. Native clone
operations still dominate copying time.

Correctness checks cover multi-batch directories, Unicode and long names,
directories, dangling symlinks, FIFOs, modification times, permissions, set-ID
bits, APFS private sizes, and malformed packed records.

Unsupported bulk APIs use the standard reader on a fresh directory cursor, only
before any entries have been delivered. Entries with missing required metadata,
mount points, and firmlinks use ordinary metadata lookup so attributes match the
directory actually traversed. Packed-record lengths and name references are
validated before decoding.

A full 39.2 GiB validation run of the promoted default completed in 55.9s with
93.95 MiB peak RSS under `GOGC=off`, with no GC cycles observed. This is a single
validation measurement, not a new timing average.

## Reproduce

From this module:

```sh
GOWORK=off go test -run '^$' -bench . -benchmem
GOWORK=off go build -gcflags='-m=2' -o /tmp/clonetree .
GOWORK=off go build -trimpath -ldflags='-s -w' -o /tmp/clonetree .

# Compare the default macOS bulk reader with the standard reader:
GOWORK=off go test -run '^$' -bench '^BenchmarkMetadataReader$' -benchmem
```
