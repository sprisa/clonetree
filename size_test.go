package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	output, err := runCLI(t, "info", "--skip-private-size")
	if err != nil || !strings.Contains(output, "snapshot") || !strings.Contains(output, "5 B") || strings.Contains(output, "PRIVATE") {
		t.Fatalf("skip-private-size info: %q, %v", output, err)
	}
	help, err := runCLI(t, "info", "--help")
	if err != nil || !strings.Contains(help, "--skip-private-size") || !strings.Contains(help, "less CPU") {
		t.Fatalf("private-size flag help: %q, %v", help, err)
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
