package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestTreeSizeDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "file"), "hello")
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	size, err := treeSize(context.Background(), root)
	if err != nil || size.Logical != 5 {
		t.Fatalf("size: %+v, %v", size, err)
	}
}

// walkSize is a sequential reference implementation.
func walkSize(t *testing.T, root string) (logical, entries uint64) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		entries++
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			logical += uint64(info.Size())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return logical, entries
}

func sizeFixture(t *testing.T, name string, width, depth int) string {
	t.Helper()
	root := filepath.Join(fixture(t), name)
	var fill func(string, int)
	fill = func(dir string, level int) {
		for i := range width {
			write(t, filepath.Join(dir, fmt.Sprintf("file-%d", i)), strings.Repeat("x", i+level))
			if level < depth {
				fill(filepath.Join(dir, fmt.Sprintf("dir-%d", i)), level+1)
			}
		}
	}
	fill(root, 0)
	if err := os.Symlink("file-0", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestConcurrentSizesMatchSequentialWalk(t *testing.T) {
	roots := []string{sizeFixture(t, "wide", 6, 3), sizeFixture(t, "small", 2, 1), sizeFixture(t, "deep", 2, 6)}
	file := filepath.Join(fixture(t), "file")
	write(t, file, "plain")
	roots = append(roots, file)
	for _, workers := range []int{1, 3, 16} {
		m, err := startSizes(context.Background(), roots, true, workers)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.wait(); err != nil {
			t.Fatal(err)
		}
		for i, root := range roots[:3] {
			logical, entries := walkSize(t, root)
			job := m.jobs[i]
			if job.logical.Load() != logical || job.entries.Load() != entries || job.finished.Load() == nil {
				t.Fatalf("%d workers, %s: %d bytes/%d entries, want %d/%d",
					workers, root, job.logical.Load(), job.entries.Load(), logical, entries)
			}
			if runtime.GOOS == "darwin" && job.sizes().Private == nil {
				t.Fatalf("APFS private size unavailable for %s", root)
			}
		}
		if got := m.jobs[3].sizes().Logical; got != 5 {
			t.Fatalf("file root: %d bytes", got)
		}
	}
}

func TestSizeErrorsAndCancellation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := sizeFixture(t, "locked", 3, 2)
	locked := filepath.Join(root, "dir-1", "dir-2")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0755)
	m, err := startSizes(context.Background(), []string{sizeFixture(t, "ok", 3, 2), root}, false, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.wait(); !os.IsPermission(err) {
		t.Fatalf("unreadable directory: %v", err)
	}
	if job, _ := m.failed(); job != m.jobs[1] {
		t.Fatal("error attributed to the wrong tree")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := treeSizeWithPrivate(ctx, sizeFixture(t, "cancelled", 2, 2), false); err != context.Canceled {
		t.Fatalf("cancelled measurement: %v", err)
	}
}

func TestSkipPrivateSize(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "nested", "data"), "hello")
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	got, err := treeSizeWithPrivate(context.Background(), root, false)
	if err != nil || got.Logical != 5 || got.Private != nil {
		t.Fatalf("size without private accounting: %+v, %v", got, err)
	}
	parent := fixture(t)
	write(t, filepath.Join(parent, "clonetree", "snapshot.clonetree", "data"), "hello")
	t.Chdir(parent)
	for _, flag := range []string{"--size-only", "--skip-private-size"} {
		output, err := runCLI(t, "info", flag)
		if err != nil || !strings.Contains(output, "snapshot") || !strings.Contains(output, "5 B") ||
			!strings.Contains(output, "SIZE") || strings.Contains(output, "DISK USED") {
			t.Fatalf("info %s: %q, %v", flag, output, err)
		}
	}
	output, err := runCLI(t, "info")
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if err != nil || len(lines) != 2 || !strings.Contains(lines[0], "DISK USED") || !strings.Contains(lines[0], "SAVED") ||
		!strings.Contains(lines[0], "FULL COPY") || !strings.HasSuffix(lines[1], "5 B") {
		t.Fatalf("info: %q, %v", output, err)
	}
	help, err := runCLI(t, "info", "--help")
	if err != nil || !strings.Contains(help, "--size-only") || !strings.Contains(help, "less CPU") {
		t.Fatalf("size-only flag help: %q, %v", help, err)
	}
}

func TestSavedPercent(t *testing.T) {
	for _, tc := range []struct {
		used, full uint64
		want       string
	}{
		{0, 0, "—"}, {0, 100, "100%"}, {1, 1000, "99.9%"}, {1, 100000, "99.9%"},
		{50, 100, "50.0%"}, {100, 100, "0%"}, {4096, 10, "0%"},
	} {
		if got := savedPercent(tc.used, tc.full); got != tc.want {
			t.Errorf("savedPercent(%d, %d) = %q, want %q", tc.used, tc.full, got, tc.want)
		}
	}
}

func TestListDoesNotMeasure(t *testing.T) {
	parent := fixture(t)
	t.Chdir(parent)
	output, err := runCLI(t, "ls")
	if err != nil || !strings.Contains(output, "No snapshots in clonetree\n") {
		t.Fatalf("empty list: %q, %v", output, err)
	}
	write(t, filepath.Join(parent, "clonetree", "beta.clonetree", "data"), "hello")
	write(t, filepath.Join(parent, "clonetree", "alpha.clonetree", "data"), "hello")
	write(t, filepath.Join(parent, "clonetree", "notes", "data"), "not a snapshot")
	locked := filepath.Join(parent, "clonetree", "beta.clonetree")
	if err := os.Chmod(locked, 0); err != nil { // Listing must not read snapshot contents.
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0755)
	for _, name := range []string{"list", "ls"} {
		output, err := runCLI(t, name)
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if err != nil || len(lines) != 3 || !strings.HasPrefix(lines[0], "ID") || !strings.Contains(lines[0], "PATH") ||
			!strings.HasPrefix(lines[1], "alpha") || !strings.Contains(lines[1], "clonetree/alpha.clonetree") ||
			!strings.HasPrefix(lines[2], "beta") || strings.ContainsAny(output, "\x1b") {
			t.Fatalf("%s: %q, %v", name, output, err)
		}
	}
}

func TestInfoFrameResolvesRowsIndependently(t *testing.T) {
	started := time.Now()
	roots := []string{"/s/done", "/s/estimated", "/s/unknown", "/s/waiting"}
	snapshots := []snapshot{{id: "done", path: roots[0]}, {id: "estimated", path: roots[1]}, {id: "unknown", path: roots[2]}, {id: "waiting", path: roots[3]}}
	m := &sizeMeasurement{}
	for _, root := range roots {
		m.jobs = append(m.jobs, &sizeJob{root: root, done: make(chan struct{})})
	}
	m.jobs[0].logical.Store(5 << 30)
	m.jobs[0].privateKnown.Store(true)
	m.jobs[0].private.Store(1 << 20)
	m.jobs[0].finish()
	m.jobs[1].started.Store(&started)
	m.jobs[1].entries.Store(250)
	m.jobs[1].logical.Store(3 << 30)
	m.jobs[2].started.Store(&started)
	m.jobs[2].entries.Store(1500)
	estimates := map[string]uint64{roots[1]: 1000}
	m.jobs[0].started.Store(&started)
	frame := infoFrame(context.Background(), snapshots, infoColumns(snapshots, true), m, estimates, started, started.Add(19900*time.Millisecond))
	lines := strings.Split(ansi.Strip(frame.content), "\n")
	if len(lines) < 5 || !strings.Contains(lines[0], "DISK USED") ||
		!strings.Contains(lines[1], "1.0 MiB") || !strings.Contains(lines[1], "99.9%") || !strings.Contains(lines[1], "5.0 GiB") || !strings.Contains(lines[1], "✓") ||
		!strings.Contains(lines[2], " 25%") || !strings.Contains(lines[2], "3.0 GiB") || !strings.Contains(lines[2], "19.9s") ||
		strings.Contains(lines[3], "%") || !strings.Contains(lines[3], "1.5k entries") ||
		!strings.Contains(lines[4], "Waiting") {
		t.Fatalf("info frame:\n%s", strings.Join(lines, "\n"))
	}
	// Values stay right-aligned under their headers whether a row is done or running.
	column := func(line, text string) int {
		return utf8.RuneCountInString(line[:strings.LastIndex(line, text)+len(text)])
	}
	fullEnd := column(lines[0], "FULL COPY")
	for _, line := range lines[1:3] {
		if end := column(line, " GiB"); end != fullEnd {
			t.Fatalf("FULL COPY misaligned (%d, header ends at %d):\n%s", end, fullEnd, strings.Join(lines, "\n"))
		}
	}
	if !strings.Contains(lines[len(lines)-1], "Measuring 3 of 4") || frame.progress == nil {
		t.Fatalf("info footer: %q", lines[len(lines)-1])
	}
}

func TestEntryEstimatesRoundTrip(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("HOME", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	directory := filepath.Join(cache, "snapshots")
	other := filepath.Join(cache, "elsewhere", "kept.clonetree")
	saveEstimates(filepath.Dir(other), map[string]uint64{other: 7})
	saveEstimates(directory, map[string]uint64{filepath.Join(directory, "old.clonetree"): 3})
	saveEstimates(directory, map[string]uint64{filepath.Join(directory, "new.clonetree"): 42})
	got := loadEstimates()
	want := map[string]uint64{other: 7, filepath.Join(directory, "new.clonetree"): 42}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("estimates: %v, want %v", got, want)
	}
	path, _ := estimatesPath()
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := loadEstimates(); len(got) != 0 {
		t.Fatalf("corrupt cache: %v", got)
	}
}

func TestBytesString(t *testing.T) {
	for n, want := range map[uint64]string{
		0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 64 << 20: "64.0 MiB", 40 << 30: "40.0 GiB",
	} {
		if got := bytesString(n); got != want {
			t.Errorf("%d: %s, want %s", n, got, want)
		}
	}
}

// --dir may name the project instead of its clonetree/ folder for commands
// that read existing snapshots, as running from the project without --dir does.
func TestDirAcceptsProjectFolder(t *testing.T) {
	project := fixture(t)
	write(t, filepath.Join(project, "clonetree", "feature.clonetree", "data"), "hello")
	write(t, filepath.Join(project, "clonetree", "other.clonetree", "data"), "hi")
	t.Chdir(filepath.Join(project, "clonetree"))

	output, err := runCLI(t, "--dir", "..", "ls")
	if err != nil || !strings.Contains(output, "feature") || !strings.Contains(output, "../clonetree/feature.clonetree") {
		t.Fatalf("ls with project --dir: %q, %v", output, err)
	}
	output, err = runCLI(t, "--dir", "..", "info", "--size-only")
	if err != nil || !strings.Contains(output, "feature") || !strings.Contains(output, "5 B") {
		t.Fatalf("info with project --dir: %q, %v", output, err)
	}
	output, err = runCLI(t, "--dir", "..", "delete", "feature")
	if err != nil || !strings.Contains(output, "../clonetree/feature.clonetree") {
		t.Fatalf("delete with project --dir: %q, %v", output, err)
	}
	if _, err := os.Stat(filepath.Join(project, "clonetree", "feature.clonetree")); !os.IsNotExist(err) {
		t.Fatal("snapshot not deleted through the project folder")
	}
	if _, err := runCLI(t, "--dir", "..", "delete", "missing"); err == nil || !strings.Contains(err.Error(), "no snapshot missing in ..") {
		t.Fatalf("delete of a missing ID: %v", err)
	}

	// A snapshot directly in --dir wins over the project's clonetree/ folder.
	write(t, filepath.Join(project, "direct.clonetree", "data"), "direct")
	output, err = runCLI(t, "--dir", "..", "ls")
	if err != nil || !strings.Contains(output, "direct") || strings.Contains(output, "other") {
		t.Fatalf("ls with snapshots directly in --dir: %q, %v", output, err)
	}

	// Without snapshots anywhere, the empty state names the folder plainly.
	empty := fixture(t)
	if output, err := runCLI(t, "--dir", empty, "ls"); err != nil || !strings.Contains(output, "No snapshots in "+empty+"\n") {
		t.Fatalf("empty --dir: %q, %v", output, err)
	}
}
