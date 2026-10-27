<div align="center">
  <img src="docs/assets/clonetree.svg" alt="Clonetree: geometric canopies with fine tree branches and a new side branch" width="128" height="128">
  <h1>clonetree</h1>
  <p><strong>Clone your checkout. Share its storage.</strong></p>
  <p>Keep local edits, installed dependencies, and build output.</p>
  <p>
    <a href="#quick-start"><strong>Quick start</strong></a> ·
    <a href="#coming-from-git-worktrees"><strong>Git worktrees</strong></a> ·
    <a href="#everyday-usage"><strong>Usage</strong></a> ·
    <a href="#how-it-works"><strong>How it works</strong></a>
  </p>
  <p>
    <a href="https://github.com/sprisa/clonetree/actions/workflows/test.yml"><img src="https://github.com/sprisa/clonetree/actions/workflows/test.yml/badge.svg" alt="Tests"></a>
    <a href="https://www.npmjs.com/package/clonetree"><img src="https://img.shields.io/npm/v/clonetree?color=A7B7A0" alt="npm version"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-MPL%202.0-blue" alt="License: MPL 2.0"></a>
  </p>
</div>

**clonetree** creates a working copy of the current directory using filesystem
clones. It provides a create/list/delete workflow similar to Git worktrees,
starting from the files on disk rather than a commit.

- **Clone local edits.** Uncommitted changes, untracked files, and ignored files
  are included.
- **Keep dependencies installed.** Existing dependency directories and build
  output are included alongside source files.
- **Share file storage.** Files are separate, but their existing data is not
  copied again. More space is used as either copy changes.

In our [APFS benchmark](#benchmark), a **57.1 GiB checkout** with **320,652 files**
cloned in **9.9 seconds** on average.

**Requires macOS with APFS, or Linux with reflink-enabled XFS/Btrfs.** Source and
destination must be on the same filesystem. No Git repository is required.

## Install

```sh
# Homebrew
brew install sprisa/tap/clonetree

# npm
npx clonetree

# Go 1.24+
go run github.com/sprisa/clonetree@latest
```

Or run without a global installation: `npx clonetree --help`.

## Quick start

From the directory you want to clone:

```sh
clonetree init               # Add *.clonetree/ to this directory's .gitignore
clonetree new my-feature     # Clone into ./clonetree/my-feature.clonetree/
cd clonetree/my-feature.clonetree

# Files, local edits, and installed dependencies are included.
```

`init` is a one-time convenience for ignoring snapshots. It doesn't create a
repository or a snapshot; `new` does the cloning. Directories ending in
`.clonetree` are skipped, so new snapshots don't contain your older ones.

Back in the original directory, inspect or remove your workspace:

```sh
clonetree info
clonetree delete my-feature  # Removes the workspace, including its local changes
```

## Coming from Git worktrees

The equivalent commands:

| What you want to do | Git worktree | clonetree |
| --- | --- | --- |
| Create a workspace | `git worktree add ../my-feature -b my-feature` | `clonetree new my-feature` |
| List workspaces | `git worktree list` | `clonetree info` |
| Remove a workspace | `git worktree remove ../my-feature` | `clonetree delete my-feature` |

The important difference is **what you start with**:

| | Git worktree | clonetree |
| --- | --- | --- |
| Starting point | A checked-out commit or branch | Your current directory, including local edits |
| Dependencies and ignored files | Not populated from the original workspace | Included if present in the source |
| Working-file storage | A separate working tree | Shared data blocks until files change |
| Git relationship | Linked to the same repository | Repository metadata is cloned as-is |

**Git worktrees already share Git objects.** Clonetree also shares the storage
behind working files, so a large `node_modules/` or build directory can come along
without another full copy of its contents.

A clonetree ID names a directory, not a branch. In a regular Git checkout with its
own `.git` directory, the clone gets its own Git metadata too: you can use the same
branch independently, or create a new one after entering the clone:

```sh
git switch -c my-feature
```

Commits and refs in that clone aren't automatically synchronized with the source;
share them through your usual Git remote. Clonetrees aren't registered in
`git worktree list`. If your source is a linked worktree or uses external metadata,
read the [repository metadata details](#repository-metadata) below.

## Everyday usage

### Choose a snapshot directory

The source is always your current directory. `--dir` (or `-d`) changes the parent
directory used by `new`, `info`, and `delete`:

```sh
clonetree --dir ../workspaces new experiment
clonetree --dir ../workspaces info
clonetree --dir ../workspaces delete experiment
```

This creates `../workspaces/experiment.clonetree/`. Relative paths are resolved
from the current directory; use the same parent when listing or deleting. Use
`--dir .` to create snapshots directly here instead of inside `./clonetree/`.

### Inspect disk usage

`new` reports clone time and estimated additional disk usage. `info` scans your
snapshots and shows their logical size and private storage where available:

| Column | Meaning |
| --- | --- |
| `SIZE` | Logical file content, including data shared with other clones |
| `PRIVATE` | Storage not shared with another clone or filesystem snapshot (APFS); `n/a` when unavailable |

Measuring `PRIVATE` asks APFS how much file data is unshared. When you only need
the `SIZE` column, skip that work to use less CPU:

```sh
clonetree info --skip-private-size
```

This shows `ID` and `SIZE`, omitting private-storage queries and the `PRIVATE`
column. It still walks the files to calculate `SIZE`.

### Control progress output

Interactive runs show the current phase and elapsed time. APFS clones identify the
directory being cloned and advance checked-entry progress during metadata
validation. Per-file rates and ETAs are omitted for native batches.

For plain output:

```sh
clonetree --no-progress new experiment
```

Non-interactive runs disable progress automatically.

### Stop a running command

Press **Ctrl+C** to cancel. Clonetree acknowledges the request immediately and
shows when it is removing the partial clone. Large partial trees can take several
seconds to delete.

Press **Ctrl+C again** to exit without waiting for cleanup. If a partial clone
remains, remove it with `clonetree delete <id>` using the same `--dir` parent.

## Benchmark

A **57.1 GiB** directory containing **320,652 files**, **59,778 directories**, and
**16,844 symlinks** was cloned on an Apple M4 Max with 128 GiB RAM, macOS 27.0.1,
APFS, and Go 1.27.3:

| Measurement | Result |
| --- | ---: |
| Mean clone time | **9.9s** |
| Mean process CPU time | **8.4s** |
| Mean peak resident memory (RSS) | **14.0 MiB** |
| Sampled peak Go heap | **4.9 MiB** |

Timing and RSS are means of two runs. Heap was sampled separately. Source
preparation, output verification, and deletion are excluded from the timings;
filesystem caches were not cleared. Results depend on entry count, filesystem
state, and other disk activity.

See [performance](PERFORMANCE.md) for workload details, memory scaling, and
benchmark commands.

## How it works

### Native copy-on-write

Clonetree scans the current directory. On APFS, ordinary subtrees use native
directory cloning; exclusions, special metadata, and unsupported directory-clone
operations use the native per-file path. The completed subtrees are checked for
exclusions and their directory timestamps are restored.

The new files initially share data blocks with the originals. Editing either file
allocates storage for its changes without changing the other file's contents.

| Platform | Native cloning |
| --- | --- |
| macOS | APFS clones |
| Linux | XFS/Btrfs reflinks; reflink support must be enabled |

Clonetree uses native cloning only, with no full-copy fallback. Ext4, Windows, and
cross-filesystem clones are unsupported. On macOS, metadata is read in native
batches with reusable buffers; standard metadata reads handle unsupported
filesystems and exceptional entries.

### Included files

- Hidden files, ignored files, installed dependencies, and any `.git` or `.sl`
  metadata are included. Git's ignore rules don't filter the clone.
- Directories ending in `.clonetree` are excluded at every depth, including the
  destination itself.
- Permissions, modification times, and symlink targets are preserved. Absolute
  symlinks still point to their original targets.
- Existing IDs are never overwritten. Failed or cancelled clones are removed.

### Repository metadata

Clonetree copies VCS metadata as-is, including lock files. It doesn't rewrite
linked/shared stores or absolute metadata paths. For a self-contained Git
workspace, start at a repository root with its own `.git` directory.

A linked Git worktree's `.git` file still points to its original metadata after
cloning. Submodules and external stores retain their references too. Cloning a
subdirectory doesn't automatically create a new checkout: Git or Sapling may still
discover the original parent repository.

### Storage and consistency

- Files share storage until either workspace changes. Old data stays allocated
  while any clone needs it; a clone's private usage can grow when the original
  changes or is deleted.
- Added disk usage is estimated from filesystem free space. Other disk activity
  and delayed accounting can affect it.
- Private storage excludes shared blocks and filesystem-wide metadata overhead.
  Measurements are scanned live, and unavailable values appear as `n/a`.
- Cloning is not an atomic directory-wide snapshot. Stop writers and let VCS
  operations finish before cloning if you need a consistent starting point.

## Learn more

| Looking for… | Start here |
| --- | --- |
| Commands and flags | `clonetree --help` or `clonetree <command> --help` |
| Runtime, memory use, and benchmark commands | [Performance](PERFORMANCE.md) |
| Native APFS tests and XFS/Btrfs tests in Ubuntu Docker or a VM | [Testing](TESTING.md) |
| Logo and social-preview artwork | [Brand assets](docs/BRANDING.md) |

Licensed under [MPL 2.0](LICENSE).
