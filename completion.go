package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/urfave/cli/v3"
)

// configureCompletion shows the completion command in help and makes the bash
// script work without the bash-completion package. The PowerShell script is
// left unlisted: it expects to be saved as a .ps1 named after the command, and
// is untested here.
func configureCompletion(completion *cli.Command) {
	completion.Hidden = false
	completion.Usage = "Print a shell completion script"
	completion.ArgsUsage = "<shell>"
	for _, shell := range completion.Commands {
		switch shell.Name {
		case "bash":
			shell.Action = appendToScript(shell.Action, bashInitFallback)
		case "pwsh":
			shell.Hidden = true
		}
	}
}

// bashInitFallback replaces the generated script's word-splitting helper. The
// stock helper needs the bash-completion package, which macOS's bash 3.2 and
// many minimal setups lack; without it, completion silently does nothing.
// When bash-completion is installed, it is still used.
const bashInitFallback = `
# Read the words directly when the bash-completion package is not installed.
__clonetree_init_completion() {
  COMPREPLY=()
  if declare -F _comp_initialize >/dev/null 2>&1; then
    _comp_initialize "$@"
  elif declare -F _get_comp_words_by_ref >/dev/null 2>&1; then
    _get_comp_words_by_ref "$@" cur prev words cword
  else
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    words=("${COMP_WORDS[@]}")
    cword=$COMP_CWORD
  fi
}
`

// appendToScript wraps a completion subcommand so its script ends with extra.
func appendToScript(action cli.ActionFunc, extra string) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		root := cmd.Root()
		out := root.Writer
		var script bytes.Buffer
		root.Writer = &script
		err := action(ctx, cmd)
		root.Writer = out
		if err != nil {
			return err
		}
		_, err = io.WriteString(out, script.String()+extra)
		return err
	}
}

// completeSnapshotIDs suggests snapshot IDs, resolving --dir like ls, and
// falls back to flag suggestions while an option is being typed.
func completeSnapshotIDs(ctx context.Context, cmd *cli.Command) {
	if args := cmd.Args().Slice(); len(args) > 0 && strings.HasPrefix(args[len(args)-1], "-") {
		cli.DefaultCompleteWithFlags(ctx, cmd)
		return
	}
	_, _, snapshots, err := findSnapshots(cmd)
	if err != nil {
		return
	}
	for _, s := range snapshots {
		fmt.Fprintln(cmd.Root().Writer, s.id)
	}
}
