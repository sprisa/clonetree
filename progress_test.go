package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
