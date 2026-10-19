# Clonetree

Snapshot any directory using copy-on-write filesystem clones.

```sh
go install github.com/sprisa/clonetree@latest

# From the directory you want to snapshot:
clonetree init
clonetree new my-feature  # creates ./clonetree/my-feature.clonetree/
clonetree info
clonetree delete my-feature

# Choose a different snapshot parent directory:
clonetree new --dir ../snapshots my-feature
clonetree info --dir ../snapshots
clonetree delete --dir ../snapshots my-feature

# Explicitly disable live progress, including in a pseudo-terminal:
clonetree new --no-progress my-feature
```

- The source is always the current directory; no repository is required.
- `init` creates or updates `.gitignore` here with `*.clonetree/`.
- `new <id>` creates `clonetree/<id>.clonetree/`, including hidden files, ignored files, dependencies, and any `.git` or `.sl` metadata.
- `--dir` / `-d` changes the snapshot parent for `new`, `info`, and `delete`; relative paths are resolved from the current directory. Use `--dir .` to put snapshots here directly.
- Directories ending in `.clonetree` are skipped at every depth, including the destination itself.
- Reports clone time and estimated additional disk usage.
- Live progress is enabled by default when stderr is a TTY; `--no-progress` explicitly disables it.
- Interactive cloning counts entries first, then shows a percentage bar, entries/second, and estimated time remaining. Non-TTY and disabled runs skip that counting pass and keep output plain.
- Added disk usage is a filesystem free-space estimate; other activity and delayed accounting can affect it.
- `info` lists snapshots in the selected directory with live logical sizes and private storage where available (APFS); `n/a` means unavailable.
- Private storage excludes shared blocks and filesystem-wide metadata overhead.
- Files share storage until either checkout changes. Old data stays allocated while any clone needs it; a clone's private usage can grow when the original changes or is deleted.
- Uses native APFS clones on macOS and XFS/Btrfs reflinks on Linux; no full-copy fallback. Ext4 is unsupported.
- macOS reads metadata in native batches with reusable buffers; standard metadata reads handle unsupported filesystems and exceptional entries.
- Requires Go 1.24+ to install.
- Preserves permissions, modification times, and symlink targets. Stop writers while cloning; snapshots are not atomic.
- VCS metadata is copied as-is, including lock files. External stores and absolute paths retain their references.
- A cloned subdirectory is not automatically a new checkout; Git/Sapling can still discover the original parent repository.
- Run `clonetree --help` or `clonetree <command> --help` for details.
- Licensed under [MPL-2.0](LICENSE).

## Benchmark

Two non-interactive full-checkout runs on an Apple M4 Max with 128 GiB RAM, macOS 27.0.1, and APFS.

| Metric | Result |
| --- | ---: |
| Checkout size, including dependencies, Git metadata, and ignored files | **39.2 GiB** |
| Average clone time | **55.0s** |
| Individual runs | 53.8s / 56.2s |
| Additional disk usage (free-space estimate) | **~88–93 MiB** |
| Regular files | ~223,000 |
| Directories | ~36,700 |
| Symlinks | ~14,000 |
| Source files (`cloc`) | 2,587 |
| Total source lines, including comments and blanks | 627,040 |
| Lines of code | 518,265 |

Copy-on-write shares existing data blocks; additional space grows as files change.
Timings and space estimates vary with filesystem state and other disk activity.

See [performance notes](PERFORMANCE.md) for allocation benchmarks, escape-analysis
findings, and collection-disabled memory measurements.

See [testing](TESTING.md) for native XFS/Btrfs e2e tests in Ubuntu Docker or a VM,
and APFS tests on a macOS host.
