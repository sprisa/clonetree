package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
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

func TestProgressPercentageRateAndETA(t *testing.T) {
	p := &progress{started: time.Now(), columns: 160}
	p.setTotal(4)
	p.add(1024)
	p.add(1024)
	status := string(p.appendStatus(nil, "Counting entries", p.cloneStarted.Load().Add(2*time.Second)))
	for _, want := range []string{"50%", "2/4 entries", "1/s", "ETA 2s", "2.0 KiB"} {
		if !strings.Contains(status, want) {
			t.Errorf("missing %q in %q", want, status)
		}
	}
}

func TestProgressFormattingReusesBuffer(t *testing.T) {
	p := &progress{started: time.Now(), columns: 120}
	p.setTotal(223000)
	p.entries.Store(123000)
	p.bytes.Store(20 << 30)
	now := p.cloneStarted.Load().Add(30 * time.Second)
	var buffer [512]byte
	var status []byte
	allocations := testing.AllocsPerRun(100, func() {
		status = p.appendStatus(buffer[:0], "Cloning", now)
	})
	if allocations != 0 || len(status) == 0 {
		t.Fatalf("progress formatting: %v allocations, %d bytes", allocations, len(status))
	}
}

func TestProgressEmptyAndChangingSource(t *testing.T) {
	for _, tc := range []struct {
		name           string
		total, entries uint64
		percent, eta   string
	}{
		{"empty", 0, 0, "100%", "ETA 0s"},
		{"not-started", 4, 0, "0%", "ETA --"},
		{"extra-files", 1, 2, "100%", "ETA 0s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &progress{started: time.Now(), columns: 160}
			p.setTotal(tc.total)
			p.entries.Store(tc.entries)
			status := string(p.appendStatus(nil, "Counting entries", p.cloneStarted.Load().Add(time.Second)))
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
	p := &progress{started: time.Now(), columns: 200}
	p.bulk.Store(true)
	p.setTotal(400000)
	p.entries.Store(1756)
	p.activity.Store(&progressActivity{directory: "dependencies"})
	now := p.cloneStarted.Load().Add(6 * time.Second)
	status := string(p.appendStatus(nil, "Scanning source", now))
	if !strings.Contains(status, "Cloning directory dependencies") || !strings.Contains(status, "6s") ||
		strings.Contains(status, "ETA") || strings.Contains(status, "/s") || strings.Contains(status, "0%") {
		t.Fatalf("misleading native batch status: %q", status)
	}
	p.entries.Store(200000)
	p.activity.Store(&progressActivity{directory: "dependencies", checking: true})
	status = string(p.appendStatus(nil, "Scanning source", now))
	for _, want := range []string{"Checking dependencies", "50%", "200000/400000 entries"} {
		if !strings.Contains(status, want) {
			t.Fatalf("missing %q in %q", want, status)
		}
	}
	if strings.Contains(status, "ETA") || strings.Contains(status, "/s") {
		t.Fatalf("batched inspection extrapolated an ETA: %q", status)
	}
	var buffer [512]byte
	if allocations := testing.AllocsPerRun(100, func() { p.appendStatus(buffer[:0], "Scanning source", now) }); allocations != 0 {
		t.Fatalf("batch status allocated: %v", allocations)
	}
}

func TestProgressAcknowledgesStoppingAndCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &progress{ctx: ctx, started: time.Now(), columns: 160}
	p.setTotal(100)
	cancel()
	status := string(p.appendStatus(nil, "Scanning source", time.Now()))
	if !strings.Contains(status, "Stopping") || strings.Contains(status, "Cloning") || strings.Contains(status, "ETA") {
		t.Fatalf("cancelled progress still reported cloning: %q", status)
	}
	p.cleaning.Store(true)
	status = string(p.appendStatus(nil, "Scanning source", time.Now()))
	if !strings.Contains(status, "Removing partial clone") || strings.Contains(status, "ETA") {
		t.Fatalf("cleanup phase was not shown: %q", status)
	}
}

func TestProgressDetailsStayOnSecondLine(t *testing.T) {
	p := &progress{started: time.Now(), columns: 80}
	p.setTotal(400000)
	p.entries.Store(200000)
	p.bulk.Store(true)
	now := p.cloneStarted.Load().Add(6 * time.Second)
	var details string
	for _, name := range []string{"short", strings.Repeat("react-native-pager-view@7.0.2_", 12)} {
		p.activity.Store(&progressActivity{directory: name, checking: true})
		status := string(p.appendStatus(nil, "Scanning source", now))
		lines := strings.Split(status, "\n")
		if len(lines) != 2 || lines[0] != "Checking "+name {
			t.Fatalf("unexpected activity line: %q", status)
		}
		if !strings.HasPrefix(lines[1], "  [██████░░░░░░] 50% · 200000/400000 entries") {
			t.Fatalf("missing second-line progress: %q", status)
		}
		if details != "" && lines[1] != details {
			t.Fatalf("directory length changed progress details: %q != %q", lines[1], details)
		}
		details = lines[1]
	}
	p.activity.Store(&progressActivity{directory: "dependencies"})
	status := string(p.appendStatus(nil, "Scanning source", now))
	if !strings.HasPrefix(status, "Cloning directory dependencies\n  ") || strings.Contains(status, "%") {
		t.Fatalf("native cloning should show timing below the activity, not a percentage: %q", status)
	}
}

func TestProgressLinesFitTerminal(t *testing.T) {
	for _, columns := range []int{1, 2, 20, 80, 120} {
		for _, name := range []string{
			strings.Repeat("long-dependency-name_", 20),
			strings.Repeat("目录e\u0301", 40),
			"directory\nwith\rterminal\tcontrols\x1b[2J",
		} {
			p := &progress{started: time.Now(), columns: columns}
			p.setTotal(400000)
			p.entries.Store(200000)
			p.activity.Store(&progressActivity{directory: name, checking: true})
			buffer := p.appendStatus([]byte("⠋ "), "Scanning source", time.Now())
			status := string(fitProgressLines(buffer, columns))
			lines := strings.Split(status, "\n")
			if !utf8.ValidString(status) || len(lines) != 2 || strings.ContainsAny(status, "\r\t\x1b") {
				t.Fatalf("invalid terminal status: %q", status)
			}
			for _, line := range lines {
				if width := runewidth.StringWidth(line); width >= columns {
					t.Fatalf("line uses %d columns in %d-column terminal: %q", width, columns, line)
				}
			}
		}
	}
}

func TestProgressRenderClearsBothLines(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	p := &progress{
		started: time.Now(), display: &progressDisplay{},
		done: make(chan struct{}), finished: make(chan struct{}),
	}
	close(p.done) // Render one frame, then clear it on shutdown.
	p.render(writer, "Scanning source")
	writer.Close()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(output), "⠋ Scanning source\n  ") ||
		!strings.HasSuffix(string(output), "\r\x1b[2K\x1b[A\r\x1b[2K") || p.display.lines != 0 {
		t.Fatalf("progress did not clear both lines: %q", output)
	}
}

func TestProgressFrameFormattingReusesBuffer(t *testing.T) {
	p := &progress{started: time.Now(), columns: 80}
	p.setTotal(400000)
	p.activity.Store(&progressActivity{directory: strings.Repeat("long-dependency_", 12), checking: true})
	now := time.Now()
	var buffer [1024]byte
	if allocations := testing.AllocsPerRun(100, func() {
		status := p.appendStatus(buffer[:0], "Scanning source", now)
		fitProgressLines(status, p.columns)
	}); allocations != 0 {
		t.Fatalf("progress frame allocated: %v", allocations)
	}
}
