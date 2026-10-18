package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

func TestBytesString(t *testing.T) {
	for n, want := range map[uint64]string{
		0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 64 << 20: "64.0 MiB", 40 << 30: "40.0 GiB",
	} {
		if got := bytesString(n); got != want {
			t.Errorf("%d: %s, want %s", n, got, want)
		}
	}
}
