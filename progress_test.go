package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCloneProgressCountsContentOnce(t *testing.T) {
	root := fixture(t)
	requireCloning(t, root)
	write(t, filepath.Join(root, "top"), "top")
	write(t, filepath.Join(root, "nested", "data"), "nested")
	write(t, filepath.Join(root, "existing.clonetree", "keep"), "excluded")
	if err := os.Symlink("nested", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	report := &progress{}
	result, err := newTree(context.Background(), root, root, "copy", report)
	if err != nil {
		t.Fatal(err)
	}
	entries, total, bytes := report.entries.Load(), report.total.Load(), report.bytes.Load()
	if entries != 4 || total != entries || bytes != 9 || bytes != result.Logical {
		t.Fatalf("progress: %d/%d entries, %d bytes; logical size %d", entries, total, bytes, result.Logical)
	}
}

// frameText renders a progress frame without styling.
func frameText(p *progress, label string, now time.Time) string {
	return ansi.Strip(p.frame(label, now, newBar(progressBarWidth)).content)
}

func TestProgressPercentageRateAndETA(t *testing.T) {
	p := &progress{started: time.Now()}
	p.setTotal(4)
	p.add(1024)
	p.add(1024)
	status := frameText(p, "Counting entries", p.cloneStarted.Load().Add(2*time.Second))
	for _, want := range []string{" 50%", "2/4 entries", "1/s", "ETA 2s", "2.0 KiB"} {
		if !strings.Contains(status, want) {
			t.Errorf("missing %q in %q", want, status)
		}
	}
}

func TestProgressEmptyAndChangingSource(t *testing.T) {
	for _, tc := range []struct {
		name           string
		total, entries uint64
		percent, eta   string
	}{
		{"empty", 0, 0, "100%", "ETA 0s"},
		{"not-started", 4, 0, "  0%", "ETA --"},
		{"extra-files", 1, 2, "100%", "ETA 0s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &progress{started: time.Now()}
			p.setTotal(tc.total)
			p.entries.Store(tc.entries)
			status := frameText(p, "Counting entries", p.cloneStarted.Load().Add(time.Second))
			if !strings.Contains(status, tc.percent) || !strings.Contains(status, tc.eta) ||
				strings.Contains(status, "NaN") || strings.Contains(status, "Inf") {
				t.Fatalf("invalid progress: %s", status)
			}
		})
	}
}

func TestCountingFailureCleansDestination(t *testing.T) {
	for _, kind := range []string{"cancelled", "missing-source"} {
		t.Run(kind, func(t *testing.T) {
			root := fixture(t)
			source := root
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			} else {
				source = filepath.Join(root, "missing")
			}
			directory := filepath.Join(root, "snapshots")
			report := &progress{}
			if _, err := newTree(ctx, source, directory, "failed", report); err == nil {
				t.Fatal("ignored counting failure")
			}
			if _, err := os.Lstat(filepath.Join(directory, "failed.clonetree")); !os.IsNotExist(err) {
				t.Fatal("partial snapshot left after counting failure")
			}
			if report.cloneStarted.Load() != nil {
				t.Fatal("cloning started after counting failed")
			}
		})
	}
}

func TestNativeBatchProgressShowsActivityWithoutETA(t *testing.T) {
	p := &progress{started: time.Now()}
	p.bulk.Store(true)
	p.setTotal(400000)
	p.entries.Store(1756)
	p.activity.Store(&progressActivity{directory: "dependencies"})
	now := p.started.Add(6 * time.Second)
	status := frameText(p, "Scanning source", now)
	if !strings.Contains(status, "Cloning dependencies") || !strings.Contains(status, "6.0s") ||
		!strings.Contains(status, "  0% · 1,756/400,000 entries") ||
		strings.Contains(status, "ETA") || strings.Contains(status, "/s") {
		t.Fatalf("misleading native batch status: %q", status)
	}
	p.entries.Store(200000)
	p.activity.Store(&progressActivity{directory: "dependencies", checking: true})
	status = frameText(p, "Scanning source", now)
	for _, want := range []string{"Checking dependencies", " 50%", "200,000/400,000 entries"} {
		if !strings.Contains(status, want) {
			t.Fatalf("missing %q in %q", want, status)
		}
	}
	if strings.Contains(status, "ETA") || strings.Contains(status, "/s") {
		t.Fatalf("batched inspection extrapolated an ETA: %q", status)
	}
}

func TestProgressAcknowledgesStoppingAndCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &progress{ctx: ctx, started: time.Now()}
	p.setTotal(100)
	cancel()
	status := frameText(p, "Scanning source", time.Now())
	if !strings.Contains(status, "Stopping") || !strings.Contains(status, "Ctrl+C again") ||
		strings.Contains(status, "Cloning") || strings.Contains(status, "ETA") {
		t.Fatalf("cancelled progress still reported cloning: %q", status)
	}
	p.cleaning.Store(true)
	status = frameText(p, "Scanning source", time.Now())
	if !strings.Contains(status, "Removing partial clone") || strings.Contains(status, "ETA") {
		t.Fatalf("cleanup phase was not shown: %q", status)
	}
}

// The second line keeps the bar in place through every phase, so it never
// appears and disappears as native directory clones alternate with checking.
func TestProgressBarStaysInPlaceAcrossPhases(t *testing.T) {
	p := &progress{started: time.Now()}
	now := p.started.Add(time.Second)
	phases := []func(){
		func() {},
		func() { p.bulk.Store(true); p.setTotal(400000) },
		func() { p.activity.Store(&progressActivity{directory: "pkg-0001"}) },
		func() {
			p.activity.Store(&progressActivity{directory: "pkg-0001", checking: true})
			p.entries.Store(1000)
		},
		func() { p.activity.Store(&progressActivity{directory: "a-much-longer-package-name"}) },
		func() { p.activity.Store(nil); p.entries.Store(400000) },
	}
	for i, phase := range phases {
		phase()
		lines := strings.Split(frameText(p, "Scanning source", now), "\n")
		bar := strings.IndexAny(lines[1], "━─")
		if len(lines) != 2 || bar != 2 || !strings.Contains(lines[1], "% · ") {
			t.Fatalf("phase %d moved the bar: %q", i, lines)
		}
	}
}

func TestProgressDetailsStayOnSecondLine(t *testing.T) {
	p := &progress{started: time.Now()}
	p.setTotal(400000)
	p.entries.Store(200000)
	p.bulk.Store(true)
	now := p.started.Add(6 * time.Second)
	var details string
	for _, name := range []string{"short", strings.Repeat("react-native-pager-view@7.0.2_", 12)} {
		p.activity.Store(&progressActivity{directory: name, checking: true})
		lines := strings.Split(frameText(p, "Scanning source", now), "\n")
		if len(lines) != 2 || !strings.HasSuffix(lines[0], "Checking "+name) {
			t.Fatalf("unexpected activity line: %q", lines)
		}
		if !strings.Contains(lines[1], " 50% · 200,000/400,000 entries") {
			t.Fatalf("missing second-line progress: %q", lines[1])
		}
		if details != "" && lines[1] != details {
			t.Fatalf("directory length changed progress details: %q != %q", lines[1], details)
		}
		details = lines[1]
	}
}

func TestStopwatchKeepsTenths(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                                   "0.0s",
		50 * time.Millisecond:               "0.0s",
		19900 * time.Millisecond:            "19.9s",
		20 * time.Second:                    "20.0s",
		20100 * time.Millisecond:            "20.1s",
		59999 * time.Millisecond:            "59.9s",
		time.Minute:                         "1m00.0s",
		time.Minute + 2300*time.Millisecond: "1m02.3s",
		time.Hour + 2*time.Minute + 3*time.Second: "1h02m03.0s",
	} {
		if got := stopwatch(d); got != want {
			t.Errorf("%v: got %q, want %q", d, got, want)
		}
	}
}

func TestLiveFramesFitTerminal(t *testing.T) {
	for _, width := range []int{1, 2, 20, 80, 120} {
		for _, name := range []string{
			strings.Repeat("long-dependency-name_", 20),
			strings.Repeat("目录é", 40),
			"directory\nwith\rterminal\tcontrols\x1b[2J",
		} {
			p := &progress{started: time.Now()}
			p.setTotal(400000)
			p.entries.Store(200000)
			p.activity.Store(&progressActivity{directory: name, checking: true})
			frame := fitWidth(p.frame("Scanning source", time.Now(), newBar(progressBarWidth)).content, width)
			lines := strings.Split(ansi.Strip(frame), "\n")
			if len(lines) != 2 || strings.ContainsAny(ansi.Strip(frame), "\r\t\x1b") {
				t.Fatalf("invalid terminal frame: %q", frame)
			}
			for _, line := range strings.Split(frame, "\n") {
				if w := lipgloss.Width(line); w >= width && width > 1 {
					t.Fatalf("line uses %d columns in %d-column terminal: %q", w, width, line)
				}
			}
		}
	}
}
