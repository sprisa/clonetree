package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCloneDeepMixedDirectories(t *testing.T) {
	root := fixture(t)
	requireCloning(t, root)
	path := root
	for level := range 48 {
		write(t, filepath.Join(path, "before"), "before")
		write(t, filepath.Join(path, "after"), "after")
		path = filepath.Join(path, "d")
		if level == 47 {
			write(t, filepath.Join(path, "leaf"), "deep leaf")
		}
	}
	store := fixture(t)
	if _, err := newTree(context.Background(), root, store, "deep", nil); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(store, "deep.clonetree")
	for range 48 {
		if read(t, filepath.Join(path, "before")) != "before" || read(t, filepath.Join(path, "after")) != "after" {
			t.Fatal("lost an entry while reusing directory handles")
		}
		path = filepath.Join(path, "d")
	}
	if read(t, filepath.Join(path, "leaf")) != "deep leaf" {
		t.Fatal("missing deep leaf")
	}
}
