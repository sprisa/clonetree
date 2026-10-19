//go:build darwin || linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFailureRemovesPartialClone(t *testing.T) {
	root := fixture(t)
	requireCloning(t, root)
	if err := initProject(root); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "a-file"), "cloned before failure")
	fifo := filepath.Join(root, "z-fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newTree(context.Background(), root, root, "failed", nil); err == nil {
		t.Fatal("accepted FIFO")
	}
	if _, err := os.Stat(filepath.Join(root, "failed.clonetree")); !os.IsNotExist(err) {
		t.Fatal("partial clone left behind")
	}
	if _, err := os.Lstat(fifo); err != nil {
		t.Fatal("removed source FIFO")
	}
}
