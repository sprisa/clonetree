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
		Usage: "Copy-on-write snapshots of the current directory",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "dir",
				Aliases: []string{"d"},
				Value:   "clonetree",
				Usage:   "Parent directory for snapshots used by new, info, and delete",
			},
			&cli.BoolFlag{
				Name:  "no-progress",
				Usage: "Disable live progress and its initial entry-counting pass",
			},
		},
		Description: `Clones the current working directory, including hidden files, ignored files,
installed dependencies, and any .git or .sl metadata. Snapshots are created as
clonetree/<id>.clonetree/ by default. Use --dir to choose a different parent
directory (relative to the current directory, or absolute). The source is always
the current directory; no repository is required. Directories ending in
.clonetree are excluded at every depth.

VCS metadata is copied as-is. Linked/shared stores and absolute metadata paths
remain external references. Cloning a subdirectory does not create a new VCS
checkout: Git or Sapling may still discover the original parent repository.

Requires APFS on macOS or a reflink-capable Linux filesystem (XFS/Btrfs).
Uses native cloning only: no full-copy fallback. Ext4 is unsupported.
Symlinks retain their original targets, including absolute paths.
Storage grows as either checkout changes; clones keep old data alive even if
it is changed or deleted in the original.
Stop writers while cloning: this is not an atomic directory-wide snapshot.
VCS operations in progress and lock files are copied without modification.`,
		Commands: []*cli.Command{
			{
				Name:  "init",
				Usage: "Add *.clonetree/ to .gitignore in the current directory",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if err := arguments(cmd, 0); err != nil {
						return err
					}
					root, err := os.Getwd()
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

Live progress is enabled when stderr is a terminal, unless --no-progress is set.
It first counts entries, then shows percentage, entries/second, and estimated time
remaining. Non-interactive and disabled runs skip the counting pass.

The disk-saving summary compares logical file size with the change in filesystem
free space. Added disk usage is an estimate: other disk activity and delayed
filesystem accounting can affect it. An unavailable estimate is never shown as zero.`,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if err := arguments(cmd, 1); err != nil {
						return err
					}
					root, err := os.Getwd()
					if err != nil {
						return err
					}
					directory, err := snapshotDirectory(cmd)
					if err != nil {
						return err
					}
					id := cmd.Args().First()
					fmt.Fprintf(cmd.Root().Writer, "Cloning into %s…\n", filepath.Join(cmd.String("dir"), id+treeSuffix))
					p := startProgress(cmd.Root().ErrWriter, "Counting entries", cmd.Bool("no-progress"))
					defer p.stop()
					result, err := newTree(ctx, root, directory, id, p)
					p.stop()
					if err != nil {
						return err
					}
					added := "disk estimate unavailable"
					if result.Added != nil {
						added = "~" + bytesString(*result.Added) + " added on disk (estimate)"
					}
					fmt.Fprintf(cmd.Root().Writer, "%s → %s in %s\n\n  cd %s\n",
						bytesString(result.Logical), added, result.Elapsed.Round(time.Millisecond),
						filepath.Join(directory, id+treeSuffix))
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
					directory, err := snapshotDirectory(cmd)
					if err != nil {
						return err
					}
					id := cmd.Args().First()
					p := startProgress(cmd.Root().ErrWriter, "Deleting "+id+treeSuffix, cmd.Bool("no-progress"))
					defer p.stop()
					if err := deleteTree(directory, id); err != nil {
						return err
					}
					p.stop()
					fmt.Fprintf(cmd.Root().Writer, "Deleted %s\n", filepath.Join(cmd.String("dir"), id+treeSuffix))
					return nil
				},
			},
			{
				Name:  "info",
				Usage: "List snapshots in --dir and their storage usage",
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

func snapshotDirectory(cmd *cli.Command) (string, error) {
	directory := cmd.String("dir")
	if directory == "" {
		return "", fmt.Errorf("--dir must not be empty")
	}
	return filepath.Abs(directory)
}

func showInfo(ctx context.Context, cmd *cli.Command) error {
	if err := arguments(cmd, 0); err != nil {
		return err
	}
	directory, err := snapshotDirectory(cmd)
	if err != nil {
		return err
	}
	p := startProgress(cmd.Root().ErrWriter, "Measuring snapshots", cmd.Bool("no-progress"))
	defer p.stop()
	entries, err := os.ReadDir(directory)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	w := tabwriter.NewWriter(cmd.Root().Writer, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSIZE\tPRIVATE")
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), treeSuffix) {
			continue
		}
		size, err := treeSize(ctx, filepath.Join(directory, entry.Name()))
		if err != nil {
			return fmt.Errorf("size of %s: %w", entry.Name(), err)
		}
		private := "n/a"
		if size.Private != nil {
			private = bytesString(*size.Private)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", strings.TrimSuffix(entry.Name(), treeSuffix), bytesString(size.Logical), private)
	}
	p.stop()
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.Root().Writer, "\nPRIVATE excludes shared blocks and filesystem metadata; n/a = unavailable.")
	return nil
}
