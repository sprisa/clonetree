//go:build darwin || linux

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// removalFixture builds a tree with nested and wide directories, read-only
// directories and files, and symlinks that must not be followed.
func removalFixture(t *testing.T, outside string) (root string, entries uint64) {
	t.Helper()
	root = filepath.Join(fixture(t), "snapshot.clonetree")
	for i := range 8 {
		for j := range 5 {
			write(t, filepath.Join(root, fmt.Sprintf("d%d", i), fmt.Sprintf("e%d", j), "file"), "x")
		}
	}
	write(t, filepath.Join(root, "locked", "inner", "file"), "x")
	write(t, filepath.Join(root, "readonly-file"), "x")
	if err := os.Chmod(filepath.Join(root, "readonly-file"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"link-to-dir", "d0/link-to-outside"} {
		if err := os.Symlink(outside, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"locked/inner", "locked"} {
		if err := os.Chmod(filepath.Join(root, dir), 0o555); err != nil {
			t.Fatal(err)
		}
	}
	err := filepath.WalkDir(root, func(string, os.DirEntry, error) error { entries++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	return root, entries
}

func TestParallelRemoval(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	outside := filepath.Join(fixture(t), "keep")
	write(t, filepath.Join(outside, "important"), "do not delete")
	for _, workers := range []int{1, 4, 16} {
		root, entries := removalFixture(t, outside)
		var removed atomic.Uint64
		if err := removeTreeParallel(context.Background(), root, workers, &removed); err != nil {
			t.Fatalf("%d workers: %v", workers, err)
		}
		if _, err := os.Lstat(root); !os.IsNotExist(err) {
			t.Fatalf("%d workers: tree remains: %v", workers, err)
		}
		if removed.Load() != entries {
			t.Fatalf("%d workers: removed %d of %d entries", workers, removed.Load(), entries)
		}
		if read(t, filepath.Join(outside, "important")) != "do not delete" {
			t.Fatal("followed a symlink out of the tree")
		}
	}
	if err := removeTree(filepath.Join(fixture(t), "missing")); err != nil {
		t.Fatalf("missing path: %v", err)
	}
}

func TestRemovalStopsWhenCancelled(t *testing.T) {
	root, _ := removalFixture(t, fixture(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := removeTreeContext(ctx, root, nil); err != context.Canceled {
		t.Fatalf("cancelled removal: %v", err)
	}
	if _, err := os.Lstat(root); err != nil {
		t.Fatal("cancelled removal deleted the tree")
	}
	// Cleanup still finishes the job without a context.
	if err := removeTree(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("tree remains after cleanup")
	}
}
