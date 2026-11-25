package main

import (
	"fmt"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	progressbar "charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// Palette. Mid-tone brand colors stay legible on both light and dark terminals;
// primary text uses the terminal's own foreground.
var (
	colorAccent   = brandColor("#7D9473", 65)  // sage
	colorClay     = brandColor("#D2551F", 166) // burnt orange
	colorMultiply = brandColor("#8A4220", 94)  // sage × burnt orange, lifted for dark terminals
	colorMuted    = lipgloss.Color("#8C8C8C")
	colorTrack    = lipgloss.Color("#5A5A5A")
	colorDanger   = lipgloss.Color("#D9534F")
)

var (
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleMuted   = lipgloss.NewStyle().Foreground(colorMuted)
	styleAccent  = lipgloss.NewStyle().Foreground(colorAccent)
	styleHeader  = lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	styleDanger  = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
	styleCommand = lipgloss.NewStyle().Foreground(colorClay)

	markSuccess = styleAccent.Render("✓")
	markPending = styleMuted.Render("·")
	dot         = styleMuted.Render(" · ")

	spinnerFrames = spinner.MiniDot.Frames
	spinnerFPS    = spinner.MiniDot.FPS
)

// terminalProfile is the color support of stdout, detected once at startup.
var terminalProfile = colorprofile.Detect(os.Stdout, os.Environ())

// brandColor uses the exact color on true-color terminals and a chosen
// 256-color stand-in elsewhere. Automatic conversion picks the nearest color by
// RGB distance, which turns sage into gray.
func brandColor(hex string, ansi256 int) color.Color {
	if terminalProfile == colorprofile.ANSI256 || terminalProfile == colorprofile.ANSI {
		return lipgloss.Color(strconv.Itoa(ansi256))
	}
	return lipgloss.Color(hex)
}

// Live views redraw at this interval; spinners advance independently of it.
const frameInterval = 100 * time.Millisecond

// isTerminalWriter reports whether w is an interactive terminal.
func isTerminalWriter(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && isTerminal(int(file.Fd()))
}

// printf writes styled output, downsampling colors for the destination: none
// for pipes and files, or with NO_COLOR set.
func printf(w io.Writer, format string, args ...any) {
	lipgloss.Fprintf(w, format, args...)
}

// printSuccess renders "✓ title" followed by indented, muted detail lines.
func printSuccess(w io.Writer, title string, details ...string) {
	printf(w, "%s %s\n", markSuccess, styleBold.Render(title))
	for _, detail := range details {
		printf(w, "  %s\n", styleMuted.Render(detail))
	}
}

// printCommand renders a suggested next command.
func printCommand(w io.Writer, command string) {
	printf(w, "\n  %s\n", styleCommand.Render(command))
}

func newErrorLogger(w io.Writer) *log.Logger {
	logger := log.NewWithOptions(colorprofile.NewWriter(w, os.Environ()), log.Options{})
	styles := log.DefaultStyles()
	styles.Levels[log.ErrorLevel] = styleDanger.SetString("error:")
	logger.SetStyles(styles)
	return logger
}

func spinnerFrame(started, now time.Time) string {
	return styleAccent.Render(spinnerFrames[int(now.Sub(started)/spinnerFPS)%len(spinnerFrames)])
}

// newBar returns a thin solid progress bar. Percentages are rendered separately
// with a fixed width so the line does not shift as they change.
func newBar(width int) progressbar.Model {
	return progressbar.New(
		progressbar.WithWidth(width),
		progressbar.WithColors(colorAccent),
		progressbar.WithFillCharacters('━', '─'),
		progressbar.WithoutPercentage(),
	)
}

func barView(bar progressbar.Model, fraction float64) string {
	bar.EmptyColor = colorTrack
	return bar.ViewAs(min(max(fraction, 0), 1))
}

func percentText(fraction float64) string {
	return fmt.Sprintf("%3d%%", int(min(max(fraction, 0), 1)*100))
}

// column is a table column. Numeric columns are right-aligned.
type column struct {
	title string
	width int
	right bool
}

const columnGap = "   "

func pad(text string, width int, right bool) string {
	gap := strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
	if right {
		return gap + text
	}
	return text + gap
}

func tableHeader(columns []column) string {
	cells := make([]string, len(columns))
	for i, c := range columns {
		cells[i] = styleHeader.Render(pad(c.title, c.width, c.right))
	}
	return strings.TrimRight(strings.Join(cells, columnGap), " ")
}

// tableRow pads cells to their columns. A row may have fewer cells than
// columns; the last cell then spans the remaining width.
func tableRow(columns []column, cells ...string) string {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		if i == len(cells)-1 && i < len(columns)-1 || i >= len(columns) {
			parts[i] = cell
			continue
		}
		parts[i] = pad(cell, columns[i].width, columns[i].right)
	}
	return strings.TrimRight(strings.Join(parts, columnGap), " ")
}

// safeText replaces terminal control characters in names read from disk.
func safeText(text string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0 {
			return '?'
		}
		return r
	}, text)
}

// fitWidth truncates each line to the terminal width, leaving the last column
// unused so terminals never auto-wrap a live frame.
func fitWidth(view string, width int) string {
	if width <= 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(width-1, 0), "…")
	}
	return strings.Join(lines, "\n")
}
