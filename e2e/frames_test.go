//go:build darwin || linux

package e2e

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// frame is the visible terminal screen at a moment when output paused.
type frame struct {
	at    time.Duration
	lines []string
}

func (f frame) String() string { return strings.Join(f.lines, "\n") }

// Output arriving within this gap belongs to the same frame. Live views redraw
// every 100ms and write each frame at once, so a short pause marks a complete
// frame rather than one split across pty reads.
const frameGap = 8 * time.Millisecond

// recordFrames runs the CLI in a pseudo-terminal of the given size and replays
// its output through a terminal emulator, returning every distinct screen.
func recordFrames(t *testing.T, cwd string, width, height int, env []string, args ...string) ([]frame, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Env = append(append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor"), env...)
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	emulator := vt.NewEmulator(width, height)
	go io.Copy(io.Discard, emulator) // Drain replies to terminal queries, if any.
	chunks := make(chan []byte)
	go func() {
		defer close(chunks)
		buffer := make([]byte, 64<<10)
		for {
			n, err := tty.Read(buffer)
			if n > 0 {
				chunks <- append([]byte(nil), buffer[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	started := time.Now()
	deadline := time.After(2 * time.Minute)
	var frames []frame
	pending := false
	snapshot := func() {
		f := frame{at: time.Since(started), lines: screenLines(emulator, width, height)}
		if len(frames) == 0 || f.String() != frames[len(frames)-1].String() {
			frames = append(frames, f)
		}
		pending = false
	}
	for {
		var gap <-chan time.Time
		if pending {
			gap = time.After(frameGap)
		}
		select {
		case chunk, ok := <-chunks:
			if !ok {
				if pending {
					snapshot()
				}
				err := cmd.Wait()
				var exit *exec.ExitError
				if errors.As(err, &exit) {
					return frames, err
				}
				if err != nil {
					t.Fatal(err)
				}
				return frames, nil
			}
			emulator.Write(chunk)
			pending = true
		case <-gap:
			snapshot()
		case <-deadline:
			cmd.Process.Kill()
			t.Fatalf("clonetree %v did not finish", args)
		}
	}
}

func screenLines(emulator *vt.Emulator, width, height int) []string {
	lines := make([]string, height)
	for y := range height {
		var b strings.Builder
		for x := 0; x < width; x++ {
			cell := emulator.CellAt(x, y)
			if cell == nil || cell.Content == "" {
				if cell != nil && cell.Width == 0 {
					continue // Trailing half of a wide character.
				}
				b.WriteByte(' ')
				continue
			}
			b.WriteString(cell.Content)
		}
		lines[y] = strings.TrimRight(b.String(), " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
