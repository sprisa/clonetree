# Clonetree

Independent repo snapshots using copy-on-write filesystem clones.

```sh
go install github.com/sprisa/clonetree@latest

# From your project's checkout:
clonetree init
clonetree new my-feature  # creates ./my-feature.clonetree/
clonetree info
clonetree delete my-feature
```

- `init` adds `*.clonetree/` to the repository's `.gitignore`.
- `new <id>` creates `<id>.clonetree/` at the repo root, including `.git`, local changes, ignored files, and dependencies.
- Directories ending in `.clonetree` are skipped at every depth, including the destination itself.
- Reports clone time and estimated additional disk usage.
- Added disk usage is a filesystem free-space estimate; other activity and delayed accounting can affect it.
- `info` shows live logical sizes and private storage where available (APFS); `n/a` means unavailable.
- Private storage excludes shared blocks and filesystem-wide metadata overhead.
- Files share storage until either checkout changes. Old data stays allocated while any clone needs it; a clone's private usage can grow when the original changes or is deleted.
- Uses native APFS clones on macOS and XFS/Btrfs reflinks on Linux; no full-copy fallback. Ext4 is unsupported.
- Requires Go 1.24+ to install and a checkout with its own `.git` directory.
- Preserves permissions, modification times, and symlink targets. Stop writers while cloning; snapshots are not atomic.
- Run `clonetree --help` or `clonetree <command> --help` for details.
- Licensed under [MPL-2.0](LICENSE).

## Benchmark

Two full-checkout runs on an Apple M4 Max with 128 GiB RAM, macOS 27.0.1, and APFS.

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
