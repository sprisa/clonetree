package main

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/urfave/cli/v3"
)

func init() {
	// Subcommand help (clonetree new --help, clonetree help new) uses the same
	// design as the overview. Anything else keeps the standard template.
	standard := cli.HelpPrinter
	cli.HelpPrinter = func(w io.Writer, template string, data any) {
		if cmd, ok := data.(*cli.Command); ok && cmd.Root() != cmd {
			printf(w, "%s\n", strings.Join(commandHelpText(cmd, helpWidth(w)), "\n"))
			return
		}
		standard(w, template, data)
	}
}

// commandDoc is the help for one command. Usage strings, arguments, and flags
// come from the command itself.
type commandDoc struct {
	summary  string
	examples [][2]string
	// globals are the root flags that apply to this command.
	globals  []string
	sections []helpSection
}

// helpSection is a titled block of definitions (rows) or short notes (bullets).
type helpSection struct {
	title   string
	rows    [][2]string
	bullets []string
}

const projectNote = "--dir may name the project or its clonetree/ folder."

var commandDocs = map[string]commandDoc{
	"init": {
		summary: "Add *.clonetree/ to this directory's .gitignore, so snapshots stay out of Git.",
		examples: [][2]string{
			{"clonetree init", "Run once in the directory you clone"},
		},
		sections: []helpSection{{title: "Good to know", bullets: []string{
			"Only edits .gitignore; it creates no snapshot. Use clonetree new for that.",
			"Safe to run again: the pattern is added once.",
		}}},
	},
	"new": {
		summary: "Clone the current directory, local edits and installed dependencies included, as a snapshot named <id>.",
		examples: [][2]string{
			{"clonetree new my-feature", "Into clonetree/my-feature.clonetree"},
			{"clonetree -d ../work new experiment", "Into ../work/experiment.clonetree"},
		},
		globals: []string{"dir", "no-progress"},
		sections: []helpSection{
			{title: "What's included", bullets: []string{
				"Everything here: hidden and ignored files, dependencies, build output, and .git or .sl metadata.",
				"Folders ending in .clonetree are skipped at every depth, so snapshots never contain each other.",
				"Permissions, modification times, and symlink targets are kept as they are.",
			}},
			{title: "Good to know", bullets: []string{
				"Needs APFS on macOS, or XFS or Btrfs with reflinks on Linux. There is no full-copy fallback.",
				"Files share storage until either copy changes them.",
				"It is not an atomic snapshot: stop writers first. VCS lock files are copied as they are.",
				"Linked worktrees, submodules, and absolute metadata paths still point at the original.",
				"IDs use letters, digits, - and _, and existing IDs are never overwritten.",
				"Ctrl+C cancels and removes the partial clone; press it again to exit at once.",
				"The disk added is estimated from free space, so other disk activity can affect it.",
			}},
		},
	},
	"list": {
		summary: "List snapshot IDs and paths instantly, without reading their contents.",
		examples: [][2]string{
			{"clonetree ls", "Snapshots in ./clonetree"},
			{"clonetree -d ~/code/app ls", "Another project's snapshots"},
		},
		globals: []string{"dir"},
		sections: []helpSection{{title: "Good to know", bullets: []string{
			"Use clonetree info for disk used and space saved.",
			projectNote,
		}}},
	},
	"info": {
		summary: "Measure how much disk each snapshot uses, and how much cloning saved compared with full copies.",
		examples: [][2]string{
			{"clonetree info", "All snapshots, with live progress"},
			{"clonetree info --size-only", "Logical sizes only, using less CPU"},
		},
		globals: []string{"dir", "no-progress"},
		sections: []helpSection{
			{title: "Columns", rows: [][2]string{
				{"DISK USED", "Space only this clone takes; deleting it frees about this much"},
				{"SAVED", "Share of a full copy the clone does not use"},
				{"FULL COPY", "What an ordinary copy would take (logical size)"},
			}},
			{title: "Good to know", bullets: []string{
				"DISK USED comes from APFS and shows n/a on Linux. It leaves out each clone's file metadata.",
				"Snapshots are measured concurrently, and each row fills in as soon as it is done.",
				"Progress percentages come from the previous run's entry counts, kept in your cache directory.",
				projectNote,
			}},
		},
	},
	"delete": {
		summary: "Remove a snapshot, including any changes made inside it.",
		examples: [][2]string{
			{"clonetree delete my-feature", "Remove the my-feature snapshot"},
		},
		globals: []string{"dir", "no-progress"},
		sections: []helpSection{{title: "Good to know", bullets: []string{
			"Only the snapshot is removed; the directory it was cloned from is untouched.",
			"Changes inside the snapshot are lost. There is no undo.",
			"Also removes a partial clone left by an interrupted clonetree new.",
			projectNote,
		}}},
	},
}

// commandHelpText is help for a subcommand. The logo art is only in the
// overview (clonetree, clonetree help).
func commandHelpText(cmd *cli.Command, width int) []string {
	doc := commandDocs[cmd.Name]
	bold := styleBold
	title := bold.Render("clonetree " + cmd.Name)
	if cmd.ArgsUsage != "" {
		title += " " + cmd.ArgsUsage
	}
	if len(cmd.Aliases) > 0 {
		title += styleMuted.Render("  (also: " + strings.Join(cmd.Aliases, ", ") + ")")
	}
	summary := doc.summary
	if summary == "" {
		summary = cmd.Usage + "."
	}
	// Wrap prose to the terminal, but keep lines readable on wide screens.
	wrap := min(max(width-1, 40), 80)
	lines := []string{title}
	lines = append(lines, strings.Split(ansi.Wordwrap(summary, wrap, ""), "\n")...)

	if len(doc.examples) > 0 {
		lines = append(lines, "", bold.Render("Examples"))
		lines = append(lines, twoColumns(doc.examples, styleCommand)...)
	}

	flags := cmd.VisibleFlags()
	for _, name := range doc.globals {
		for _, f := range cmd.Root().VisibleFlags() {
			if f.Names()[0] == name {
				flags = append(flags, f)
			}
		}
	}
	// Help last, after the command's own and global options.
	for i, f := range flags {
		if f.Names()[0] == "help" {
			flags = append(append(flags[:i:i], flags[i+1:]...), f)
			break
		}
	}
	lines = append(lines, "", bold.Render("Options"))
	lines = append(lines, twoColumns(flagRows(flags), bold)...)

	for _, section := range doc.sections {
		lines = append(lines, "", bold.Render(section.title))
		lines = append(lines, twoColumns(section.rows, bold)...)
		for _, bullet := range section.bullets {
			lines = append(lines, hangingBullet(bullet, wrap)...)
		}
	}
	return lines
}

// hangingBullet wraps a note to width under a "·" marker, with continuation
// lines aligned to the text.
func hangingBullet(text string, width int) []string {
	const marker, indent = "  · ", "    "
	wrapped := strings.Split(ansi.Wordwrap(text, width-lipgloss.Width(marker), ""), "\n")
	for i, line := range wrapped {
		prefix := indent
		if i == 0 {
			prefix = marker
		}
		wrapped[i] = styleMuted.Render(prefix + line)
	}
	return wrapped
}
