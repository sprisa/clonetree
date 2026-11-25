package main

import (
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/urfave/cli/v3"
)

func init() {
	// clonetree, clonetree help, and clonetree --help all show the overview.
	// Subcommand help keeps the standard layout.
	cli.ShowRootCommandHelp = showRootHelp
}

// logoSize is the art's height in rows and the distance between the two
// pines' apexes. The icon size fits beside the title and tagline alone.
type logoSize struct{ rows, gap int }

var (
	logoFull = logoSize{rows: 8, gap: 4}
	logoIcon = logoSize{rows: 3, gap: 2}
)

const (
	helpGap = 4
	// The art starts one row down, beside the tagline, so it does not touch
	// the command line above the output.
	helpArtTop = 1
)

// logoArt draws the logo with terminal characters: two outlined pines whose
// overlap is filled solid, like overprinted inks. Without color it falls back
// to plain ASCII.
func logoArt(size logoSize, color bool) []string {
	sage := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	orange := lipgloss.NewStyle().Foreground(colorClay).Bold(true)
	shared := lipgloss.NewStyle().Foreground(colorMultiply).Bold(true)
	paint := func(style lipgloss.Style, s string) string {
		if !color {
			return s
		}
		return style.Render(s)
	}
	leftApex, rightApex := size.rows, size.rows+size.gap
	width := rightApex + size.rows + 1
	lines := make([]string, 0, size.rows+1)
	for row := range size.rows {
		var b strings.Builder
		leftL, leftR := leftApex-row, leftApex+1+row
		rightL, rightR := rightApex-row, rightApex+1+row
		for col := range width {
			inLeft, inRight := leftL < col && col < leftR, rightL < col && col < rightR
			onLeft, onRight := col == leftL || col == leftR, col == rightL || col == rightR
			glyph := "\\"
			if col == leftL || col == rightL {
				glyph = "/"
			}
			base := row == size.rows-1
			switch {
			case onLeft && (onRight || inRight), onRight && inLeft:
				// Outlines inside the other pine overprint, like the fill.
				b.WriteString(paint(shared, glyph))
			case onLeft:
				b.WriteString(paint(sage, glyph))
			case onRight:
				b.WriteString(paint(orange, glyph))
			case inLeft && inRight:
				if color {
					b.WriteString(shared.Render("█"))
				} else {
					b.WriteByte('#')
				}
			case base && inLeft:
				b.WriteString(paint(sage, map[bool]string{true: "▁", false: "_"}[color]))
			case base && inRight:
				b.WriteString(paint(orange, map[bool]string{true: "▁", false: "_"}[color]))
			default:
				b.WriteByte(' ')
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	trunk := strings.Repeat(" ", leftApex+size.gap/2)
	if color {
		trunk += sage.Render("▐") + orange.Render("▌")
	} else {
		trunk += "||"
	}
	return append(lines, trunk)
}

// helpText is the left column: what clonetree does, how to start, then every
// command and option.
func helpText(root *cli.Command) []string {
	bold := styleBold
	lines := []string{
		bold.Render("clonetree") + " " + styleMuted.Render(root.Version),
		"Copy your repo, node_modules and all, in seconds.",
		lipgloss.NewStyle().Foreground(colorClay).Render("Only changes take up disk space."),
		"",
		bold.Render("Get started"),
	}
	lines = append(lines, twoColumns([][2]string{
		{"clonetree init", "Ignore snapshots in git"},
		{"clonetree new my-feature", "Clone this directory"},
		{"cd clonetree/my-feature.clonetree", ""},
		{"clonetree info", "See the space you saved"},
	}, styleCommand)...)

	lines = append(lines, "", bold.Render("Commands"))
	var commands [][2]string
	for _, c := range root.VisibleCommands() {
		name := strings.Join(append([]string{c.Name}, c.Aliases...), ", ")
		if c.ArgsUsage != "" {
			name += " " + c.ArgsUsage
		}
		commands = append(commands, [2]string{name, c.Usage})
	}
	lines = append(lines, twoColumns(commands, bold)...)

	lines = append(lines, "", bold.Render("Options"))
	options := flagRows(root.VisibleFlags())
	lines = append(lines, twoColumns(options, bold)...)
	return append(lines, "", styleMuted.Render("Run clonetree <command> --help for details."))
}

// flagRows formats flags as "-d, --dir <dir>" with their usage. Deprecated
// long aliases are left out: only short names and the primary name show.
func flagRows(flags []cli.Flag) [][2]string {
	var rows [][2]string
	for _, f := range flags {
		var short []string
		long := ""
		for _, n := range f.Names() {
			if len(n) == 1 {
				short = append(short, "-"+n)
			} else if long == "" {
				long = "--" + n
			}
		}
		name := strings.Join(append(short, long), ", ")
		usage := ""
		if doc, ok := f.(cli.DocGenerationFlag); ok {
			usage = doc.GetUsage()
		}
		if s, ok := f.(*cli.StringFlag); ok {
			name += " <" + s.Name + ">"
			if s.Value != "" {
				usage += " (default: " + s.Value + ")"
			}
		}
		rows = append(rows, [2]string{name, sentenceCase(usage)})
	}
	return rows
}

// twoColumns renders indented name/description rows with aligned descriptions.
// Rows without a description do not widen the name column.
func twoColumns(rows [][2]string, nameStyle lipgloss.Style) []string {
	width := 0
	for _, r := range rows {
		if r[1] != "" {
			width = max(width, lipgloss.Width(r[0]))
		}
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		if r[1] == "" {
			lines[i] = "  " + nameStyle.Render(r[0])
			continue
		}
		lines[i] = "  " + nameStyle.Render(pad(r[0], width, false)) + "  " + styleMuted.Render(r[1])
	}
	return lines
}

func sentenceCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// helpLayout places the art beside the top of the text, just right of the
// lines it sits next to; longer lines further down pass underneath it. It
// reports false when that would reach the last column of width.
func helpLayout(text, art []string, width int) (string, bool) {
	column, artWidth := 0, 0
	for i, l := range text {
		if i >= helpArtTop && i < helpArtTop+len(art) {
			column = max(column, lipgloss.Width(l))
		}
	}
	column += helpGap
	for _, l := range art {
		artWidth = max(artWidth, lipgloss.Width(l))
	}
	if column+artWidth > width-1 {
		return "", false
	}
	lines := make([]string, max(len(text), helpArtTop+len(art)))
	for i := range lines {
		left := ""
		if i < len(text) {
			left = text[i]
		}
		if row := i - helpArtTop; row >= 0 && row < len(art) && art[row] != "" {
			left = pad(left, column, false) + art[row]
		}
		lines[i] = left
	}
	return strings.Join(lines, "\n"), true
}

// rootHelp lays out the overview for a terminal width: the full art when it
// fits beside the text, a small icon when only that fits, and no art in
// narrower terminals, so the text always comes first.
func rootHelp(root *cli.Command, color bool, width int) string {
	text := helpText(root)
	for _, size := range []logoSize{logoFull, logoIcon} {
		if layout, ok := helpLayout(text, logoArt(size, color), width); ok {
			return layout
		}
	}
	return strings.Join(text, "\n")
}

func showRootHelp(cmd *cli.Command) error {
	w := cmd.Root().Writer
	printf(w, "%s\n", rootHelp(cmd.Root(), helpColor(w), helpWidth(w)))
	return nil
}

// helpColor reports whether w shows color, honoring NO_COLOR and pipes.
func helpColor(w io.Writer) bool {
	return colorprofile.Detect(w, os.Environ()) > colorprofile.ASCII
}

func helpWidth(w io.Writer) int {
	if file, ok := w.(*os.File); ok && isTerminal(int(file.Fd())) {
		return terminalWidth(int(file.Fd()))
	}
	return 80
}
