package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

//go:embed version.txt
var embeddedVersion string

func main() {
	ctx, stop := interruptContext(os.Stderr)
	defer stop()
	if err := command().Run(ctx, os.Args); err != nil {
		if err == context.Canceled {
			printf(os.Stderr, "%s\n", styleMuted.Render("Cancelled."))
			stop()
			os.Exit(130)
		}
		newErrorLogger(os.Stderr).Error(err)
		os.Exit(1)
	}
}

func command() *cli.Command {
	return &cli.Command{
		Name:    "clonetree",
		Usage:   "Copy your repo, node_modules and all, in seconds. Only changes take up disk space.",
		Version: strings.TrimSpace(embeddedVersion),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "dir",
				Aliases: []string{"d"},
				Value:   "clonetree",
				Usage:   "Snapshot folder",
			},
			&cli.BoolFlag{
				Name:  "no-progress",
				Usage: "Plain output, without live progress",
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "init",
				Usage: "Add *.clonetree/ to .gitignore",
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
					w := cmd.Root().Writer
					printSuccess(w, "Ready", ".gitignore ignores "+ignorePattern+" in "+displayPath(root))
					printCommand(w, "clonetree new <id>")
					return nil
				},
			},
			{
				Name:      "new",
				Usage:     "Clone this directory as <id>",
				ArgsUsage: "<id>",
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
					p := startProgress(ctx, cmd.Root().ErrWriter, "Scanning source", cmd.Bool("no-progress"))
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
					w := cmd.Root().Writer
					printSuccess(w, "Cloned "+id+" in "+stopwatch(result.Elapsed),
						bytesString(result.Logical)+" of files"+" · "+added)
					printCommand(w, "cd "+displayPath(filepath.Join(directory, id+treeSuffix)))
					return nil
				},
			},
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "List snapshots, instantly",
				Action:  showList,
			},
			{
				Name:  "info",
				Usage: "Measure disk used and space saved",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "size-only",
						Aliases: []string{"skip-private-size"},
						Usage:   "Logical sizes only, using less CPU",
					},
				},
				Action: showInfo,
			},
			{
				Name:      "delete",
				Usage:     "Remove a snapshot and its changes",
				ArgsUsage: "<id>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if err := arguments(cmd, 1); err != nil {
						return err
					}
					directory, err := snapshotDirectory(cmd)
					if err != nil {
						return err
					}
					id, dir := cmd.Args().First(), cmd.String("dir")
					// Like ls and info, accept the project folder: when the ID is not
					// in --dir itself, use the project's clonetree/ folder.
					if validID(id) && !exists(filepath.Join(directory, id+treeSuffix)) &&
						exists(filepath.Join(directory, projectSnapshots, id+treeSuffix)) {
						directory, dir = filepath.Join(directory, projectSnapshots), filepath.Join(dir, projectSnapshots)
					}
					p := startProgress(ctx, cmd.Root().ErrWriter, "Deleting "+id, cmd.Bool("no-progress"))
					defer p.stop()
					if err := deleteTree(directory, id); err != nil {
						if errors.Is(err, fs.ErrNotExist) {
							return fmt.Errorf("no snapshot %s in %s (see clonetree ls)", id, dir)
						}
						return err
					}
					p.stop()
					printSuccess(cmd.Root().Writer, "Deleted "+id, filepath.Join(dir, id+treeSuffix))
					return nil
				},
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 0 {
				return fmt.Errorf("unknown command %q", cmd.Args().First())
			}
			return showRootHelp(cmd)
		},
	}
}

func arguments(cmd *cli.Command, count int) error {
	if cmd.Args().Len() != count {
		return fmt.Errorf("usage: clonetree %s %s", cmd.Name, cmd.ArgsUsage)
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// displayPath shows path relative to the current directory when it is inside it.
func displayPath(path string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return rel
		}
	}
	return path
}

func snapshotDirectory(cmd *cli.Command) (string, error) {
	directory := cmd.String("dir")
	if directory == "" {
		return "", fmt.Errorf("--dir must not be empty")
	}
	return filepath.Abs(directory)
}
