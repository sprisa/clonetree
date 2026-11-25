package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestRootHelpOverview(t *testing.T) {
	for _, args := range [][]string{{}, {"help"}, {"--help"}} {
		output, err := runCLI(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		for _, want := range []string{
			"Copy your repo, node_modules and all, in seconds.",
			"Get started", "clonetree new my-feature",
			"Commands", "list, ls", "delete <id>",
			"Options", "-d, --dir <dir>", "(default: clonetree)",
			"/##", // Plain ASCII art: no color in a buffer.
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("%v: missing %q in:\n%s", args, want, output)
			}
		}
		if strings.ContainsAny(output, "\x1b█") {
			t.Fatalf("%v: color or block art in plain output:\n%s", args, output)
		}
	}
}

func TestCommandHelp(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
		not  []string
	}{
		{[]string{"new", "--help"}, []string{"clonetree new <id>", "Examples", "clonetree new my-feature", "-d, --dir <dir>", "--no-progress", "What's included", "Good to know"}, nil},
		{[]string{"help", "new"}, []string{"clonetree new <id>", "Examples"}, nil},
		{[]string{"info", "--help"}, []string{"Columns", "DISK USED", "SAVED", "FULL COPY", "--size-only", "less CPU"}, []string{"--skip-private-size"}},
		{[]string{"ls", "--help"}, []string{"clonetree list", "(also: ls)", "-d, --dir <dir>"}, []string{"--no-progress"}},
		{[]string{"init", "--help"}, []string{"clonetree init", ".gitignore"}, []string{"--dir", "--no-progress"}},
		{[]string{"delete", "--help"}, []string{"clonetree delete <id>", "There is no undo."}, nil},
	} {
		output, err := runCLI(t, tc.args...)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(output, want) {
				t.Errorf("%v: missing %q in:\n%s", tc.args, want, output)
			}
		}
		for _, not := range append(tc.not, "USAGE", "GLOBAL OPTIONS", "Get started") {
			if strings.Contains(output, not) {
				t.Errorf("%v: unexpected %q in:\n%s", tc.args, not, output)
			}
		}
	}
	// Every command has docs, without the logo art (that is only in the
	// overview), and fits 80 columns without reaching the last one.
	root := command()
	for _, cmd := range root.VisibleCommands() {
		if _, ok := commandDocs[cmd.Name]; !ok {
			t.Errorf("no help docs for %s", cmd.Name)
		}
		lines := commandHelpText(cmd, 80)
		if strings.Contains(strings.Join(lines, "\n"), "/\\") {
			t.Errorf("%s: logo art in subcommand help:\n%s", cmd.Name, strings.Join(lines, "\n"))
		}
		for _, line := range lines {
			if lipgloss.Width(line) >= 80 {
				t.Errorf("%s: line reaches the last column: %q", cmd.Name, line)
			}
		}
	}
}

func TestRootHelpLayout(t *testing.T) {
	root := command()
	render := func(width int) []string {
		layout := rootHelp(root, true, width)
		// Lines carrying art must not reach the last column, where terminals
		// wrap. Text alone may wrap in a tiny terminal.
		for i, line := range strings.Split(layout, "\n") {
			if i < helpArtTop+len(logoArt(logoFull, true)) && i < len(helpText(root)) && lipgloss.Width(line) >= width && lipgloss.Width(line) > lipgloss.Width(helpText(root)[i]) {
				t.Fatalf("art reaches the last column at width %d: %q", width, ansi.Strip(line))
			}
		}
		return strings.Split(ansi.Strip(layout), "\n")
	}
	full, icon := logoArt(logoFull, false), logoArt(logoIcon, false)
	// Wide: the full art beside the first lines, starting below the title so
	// it does not touch the command line above.
	got := render(80)
	if strings.ContainsAny(got[0], "/\\") || !strings.HasSuffix(got[helpArtTop], strings.TrimSpace(full[0])) || !strings.Contains(strings.Join(got, "\n"), "██████████") {
		t.Fatalf("full art not beside the text at 80 columns:\n%s", strings.Join(got, "\n"))
	}
	// Narrower: the small icon beside the title and tagline.
	got = render(66)
	if !strings.HasSuffix(got[helpArtTop], strings.TrimSpace(icon[0])) || strings.Contains(strings.Join(got, "\n"), "██████████") {
		t.Fatalf("icon not used at 66 columns:\n%s", strings.Join(got, "\n"))
	}
	// Too narrow for either: text only. Art never pushes the text down.
	got = render(40)
	if strings.Contains(strings.Join(got, "\n"), "/\\") || got[0] != "clonetree "+root.Version {
		t.Fatalf("art shown at 40 columns:\n%s", strings.Join(got, "\n"))
	}
	// Plain and color art have the same shape.
	for _, size := range []logoSize{logoFull, logoIcon} {
		plain, colored := logoArt(size, false), logoArt(size, true)
		for i := range plain {
			if utf8.RuneCountInString(plain[i]) != lipgloss.Width(colored[i]) {
				t.Fatalf("row %d differs: %q vs %q", i, plain[i], ansi.Strip(colored[i]))
			}
		}
	}
}
