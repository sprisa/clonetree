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
- Reports clone time and savings, e.g. `40.0 GiB → ~64.0 MiB added on disk (estimate) in 54s`.
- Added disk usage is a filesystem free-space estimate; other activity and delayed accounting can affect it.
- `info` shows live logical sizes and private storage where available (APFS); `n/a` means unavailable.
- Private storage excludes shared blocks and filesystem-wide metadata overhead.
- Files share storage until either checkout changes. Old data stays allocated while any clone needs it; a clone's private usage can grow when the original changes or is deleted.
- Uses native APFS clones on macOS and XFS/Btrfs reflinks on Linux; no full-copy fallback. Ext4 is unsupported.
- Requires Go 1.24+ to install and a checkout with its own `.git` directory.
- Preserves permissions, modification times, and symlink targets. Stop writers while cloning; snapshots are not atomic.
- Run `clonetree --help` or `clonetree <command> --help` for details.
- Licensed under [MPL-2.0](LICENSE).
