package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := command().Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "clonetree:", err)
		os.Exit(1)
	}
}

func command() *cli.Command {
	return &cli.Command{
		Name:  "clonetree",
		Usage: "Independent repo snapshots, without copying all the data",
		Description: `Clones the entire checkout, including .git, local changes, ignored files,
and installed dependencies. Snapshots are created as <id>.clonetree/ in the
repository root. Directories ending in .clonetree are excluded at every depth.
Run from your repository root or any subdirectory. Each clone is a standalone
checkout at the same revision; creating one does not create a branch.

Requires APFS on macOS or a reflink-capable Linux filesystem (XFS/Btrfs).
Uses native cloning only: no full-copy fallback. Ext4 is unsupported.
Symlinks retain their original targets, including absolute paths.
Storage grows as either checkout changes; clones keep old data alive even if
it is changed or deleted in the original.
Stop writers while cloning: this is not an atomic repository-wide snapshot.`,
		Commands: []*cli.Command{
			{
				Name:  "init",
				Usage: "Add *.clonetree/ to the repository's .gitignore",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if err := arguments(cmd, 0); err != nil {
						return err
					}
					root, err := projectRoot()
					if err != nil {
						return err
					}
					if err := initProject(root); err != nil {
						return err
					}
					fmt.Fprintf(cmd.Root().Writer, "Ready: %s ignores %s\n", filepath.Join(root, ".gitignore"), ignorePattern)
					return nil
				},
			},
			{
				Name:      "new",
				Usage:     "Create an independent copy-on-write snapshot",
				ArgsUsage: "<id>",
				Description: `IDs contain letters, digits, hyphens or underscores and start with a letter
or digit. Existing IDs are never overwritten. Permissions and modification times
are preserved. Failed or cancelled clones are removed.

The disk-saving summary compares logical file size with the change in filesystem
free space. Added disk usage is an estimate: other disk activity and delayed
filesystem accounting can affect it. An unavailable estimate is never shown as zero.`,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if err := arguments(cmd, 1); err != nil {
						return err
					}
					root, err := projectRoot()
					if err != nil {
						return err
					}
					id := cmd.Args().First()
					fmt.Fprintf(cmd.Root().Writer, "Cloning into %s%s…\n", id, treeSuffix)
					result, err := newTree(ctx, root, id)
					if err != nil {
						return err
					}
					added := "disk estimate unavailable"
					if result.Added != nil {
						added = "~" + bytesString(*result.Added) + " added on disk (estimate)"
					}
					fmt.Fprintf(cmd.Root().Writer, "%s → %s in %s\n\n  cd %s\n",
						bytesString(result.Logical), added, result.Elapsed.Round(time.Millisecond),
						filepath.Join(root, id+treeSuffix))
					return nil
				},
			},
			{
				Name:      "delete",
				Usage:     "Remove a clonetree and all its local changes",
				ArgsUsage: "<id>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if err := arguments(cmd, 1); err != nil {
						return err
					}
					root, err := projectRoot()
					if err != nil {
						return err
					}
					id := cmd.Args().First()
					if err := deleteTree(root, id); err != nil {
						return err
					}
					fmt.Fprintf(cmd.Root().Writer, "Deleted %s%s\n", id, treeSuffix)
					return nil
				},
			},
			{
				Name:  "info",
				Usage: "List clonetrees and their storage usage",
				Description: `SIZE is logical file content. PRIVATE is storage not shared with another clone
or filesystem snapshot, where the filesystem exposes it (APFS). It excludes
filesystem-wide metadata overhead. Shared blocks are not counted as private.
Unsupported measurements are shown as n/a. Sizes are scanned live.`,
				Action: showInfo,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 0 {
				return fmt.Errorf("unknown command %q", cmd.Args().First())
			}
			return cli.ShowAppHelp(cmd)
		},
	}
}

func arguments(cmd *cli.Command, count int) error {
	if cmd.Args().Len() != count {
		return fmt.Errorf("usage: clonetree %s %s", cmd.Name, cmd.ArgsUsage)
	}
	return nil
}

func showInfo(ctx context.Context, cmd *cli.Command) error {
	if err := arguments(cmd, 0); err != nil {
		return err
	}
	root, err := projectRoot()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(cmd.Root().Writer, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSIZE\tPRIVATE")
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), treeSuffix) {
			continue
		}
		size, err := treeSize(ctx, filepath.Join(root, entry.Name()))
		if err != nil {
			return fmt.Errorf("size of %s: %w", entry.Name(), err)
		}
		private := "n/a"
		if size.Private != nil {
			private = bytesString(*size.Private)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", strings.TrimSuffix(entry.Name(), treeSuffix), bytesString(size.Logical), private)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.Root().Writer, "\nPRIVATE excludes shared blocks and filesystem metadata; n/a = unavailable.")
	return nil
}
