//go:build darwin || linux

package e2e

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sprisa/clonetree/internal/demotree"
)

var (
	valueToken = regexp.MustCompile(`(\d+(\.\d)? (B|KiB|MiB|GiB|TiB|PiB|EiB)|\d+(\.\d)?%|—|n/a)`)
	// Right-aligned value columns, in any order they appear in the header.
	valueTitles = []string{"SIZE", "DISK USED", "SAVED", "FULL COPY"}
	// Elapsed-time tokens: "0.4s", "1m02.3s". ETAs are whole seconds by design.
	timeToken     = regexp.MustCompile(`(^|[ ·])((\d+h)?(\d+m)?\d+(\.\d+)?s)(\s|$)`)
	stopwatchForm = regexp.MustCompile(`^(\d+h\d\dm\d\d|\d+m\d\d|\d+)\.\ds$`)
)

// barSpan returns the column and width of the first progress bar on a line.
func barSpan(line string) (start, width int, ok bool) {
	column := 0
	for _, r := range line {
		if r == '━' || r == '─' {
			if !ok {
				start, ok = column, true
			}
			width++
		} else if ok {
			break
		}
		column++
	}
	return start, width, ok && width >= 8
}

// rowLabel identifies a row across frames: its first word for table rows, or
// "progress" for an indented detail line under a headline.
func rowLabel(line string) string {
	if strings.HasPrefix(line, "  ") {
		return "progress"
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// checkLiveLayout reports layout jumps across a sequence of frames: bars must
// keep their column and width and never vanish while their row is still
// running; table values must stay aligned under their headers; elapsed times
// must keep their tenths digit; and live frames must not reach the last column,
// where terminals wrap and redraws break.
func checkLiveLayout(t *testing.T, name string, frames []frame, width int) {
	t.Helper()
	type span struct{ start, width int }
	bars := map[string]span{}
	for i, f := range frames {
		var header string
		live := stillLive(f)
		for y, line := range f.lines {
			where := fmt.Sprintf("%s frame %d (%s) line %d", name, i, f.at, y)
			if live && width > 0 && utf8.RuneCountInString(line) >= width {
				t.Errorf("%s fills the terminal width: %q", where, line)
			}
			for _, m := range timeToken.FindAllStringSubmatch(line, -1) {
				before := line[:strings.Index(line, m[2])]
				if !strings.HasSuffix(before, "ETA ") && !stopwatchForm.MatchString(m[2]) {
					t.Errorf("%s: timer %q lacks a fixed tenths digit: %q", where, m[2], line)
				}
			}
			if strings.HasPrefix(line, "ID ") && (strings.Contains(line, "SIZE") || strings.Contains(line, "DISK USED")) {
				header = line
				continue
			}
			if line == "" {
				header = "" // A table ends at the first blank line.
			}
			label := rowLabel(line)
			start, w, ok := barSpan(line)
			if previous, seen := bars[label]; seen && ok && (previous != span{start, w}) {
				t.Errorf("%s: bar for %q moved from column %d (width %d) to %d (width %d):\n%s",
					where, label, previous.start, previous.width, start, w, f)
			}
			if ok {
				bars[label] = span{start, w}
			} else if _, seen := bars[label]; seen && i > 0 && !strings.Contains(line, "✓") && live {
				t.Errorf("%s: bar for %q disappeared while running:\n%s", where, label, f)
			}
			if header != "" && label != "" && !strings.HasPrefix(line, " ") {
				checkAligned(t, where, header, line)
			}
		}
	}
}

// stillLive reports whether a frame still shows a live view: a spinner,
// waiting row, or measuring footer.
func stillLive(f frame) bool {
	for _, line := range f.lines {
		if strings.ContainsAny(line, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") || strings.Contains(line, "Measuring ") {
			return true
		}
	}
	return false
}

// checkAligned verifies that each value cell of a table row ends where its
// header ends.
func checkAligned(t *testing.T, where, header, row string) {
	t.Helper()
	var ends []int
	for _, title := range valueTitles {
		if i := strings.Index(header, title); i >= 0 {
			ends = append(ends, utf8.RuneCountInString(header[:i])+len(title))
		}
	}
	slices.Sort(ends)
	// Skip the ID cell, which may itself contain digits.
	offset := strings.IndexByte(row, ' ')
	if offset < 0 {
		t.Errorf("%s: row has no values: %q", where, row)
		return
	}
	matches := valueToken.FindAllStringIndex(row[offset:], len(ends))
	if len(matches) < len(ends) {
		t.Errorf("%s: row lacks %d value cells: %q", where, len(ends), row)
		return
	}
	for i, end := range ends {
		if got := utf8.RuneCountInString(row[:offset+matches[i][1]]); got != end {
			t.Errorf("%s: cell %d ends at column %d, header at %d:\n%s\n%s", where, i, got, end, header, row)
		}
	}
}

const layoutWidth, layoutHeight = 100, 30

func TestLiveLayoutIsStable(t *testing.T) {
	root := fixture(t)
	source := filepath.Join(root, "acme-app")
	if err := demotree.Create(source, demotree.Options{Packages: 250, SmallFiles: true}); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	record := func(args ...string) []frame {
		t.Helper()
		frames, err := recordFrames(t, source, layoutWidth, layoutHeight, []string{"HOME=" + home, "XDG_CACHE_HOME=" + home}, args...)
		if err != nil {
			t.Fatalf("clonetree %v: %v\n%s", args, err, frames[len(frames)-1])
		}
		checkLiveLayout(t, strings.Join(args, " "), frames, layoutWidth)
		return frames
	}
	withBar := func(frames []frame) (n int) {
		for _, f := range frames {
			for _, line := range f.lines {
				if _, _, ok := barSpan(line); ok {
					n++
					break
				}
			}
		}
		return n
	}
	if frames := record("new", "one"); withBar(frames) < 3 {
		t.Fatalf("new showed too few progress frames (%d) to check the layout", withBar(frames))
	}
	success(t, source, "--no-progress", "new", "two")
	success(t, source, "--no-progress", "new", "three")
	record("info") // Counts only: no estimates yet.
	// Fast filesystems measure this small tree in a few frames; one with a bar
	// shows the estimate path rendered.
	if frames := record("info"); withBar(frames) < 1 {
		t.Fatal("info with estimates showed no progress bar")
	}
	record("ls")
	record("delete", "two") // Live count with an estimate from the info runs above.
}

// TestRecordedFramesAreStable checks text frames written by VHS (Output *.txt),
// as produced by docs/demo/record.sh, with the same layout rules.
func TestRecordedFramesAreStable(t *testing.T) {
	path := os.Getenv("CLONETREE_UI_FRAMES")
	if path == "" {
		t.Skip("set CLONETREE_UI_FRAMES to a VHS text recording; see docs/demo/record.sh")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var frames []frame
	current := frame{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " ")
		if line != "" && strings.Trim(line, "─") == "" && utf8.RuneCountInString(line) >= 40 {
			frames = append(frames, current)
			current = frame{}
			continue
		}
		current.lines = append(current.lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	frames = append(frames, current)
	if len(frames) < 10 {
		t.Fatalf("only %d frames in %s", len(frames), path)
	}
	checkLiveLayout(t, filepath.Base(path), frames, 0)
}
