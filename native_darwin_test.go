package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateSizeTracksCopyOnWrite(t *testing.T) {
	root := t.TempDir()
	requireCloning(t, root)
	source, target := filepath.Join(root, "data"), filepath.Join(root, "clone")
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i)
	}
	if err := os.WriteFile(source, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := cloneFile(source, target); err != nil {
		t.Fatal(err)
	}
	before, ok := privateSize(target)
	if !ok {
		t.Fatal("APFS private size unavailable")
	}
	if before >= uint64(len(data)) {
		t.Fatalf("shared data counted as private: %d", before)
	}
	f, err := os.OpenFile(target, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 99
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	after, ok := privateSize(target)
	if !ok || after <= before {
		t.Fatalf("private size did not increase after edit: %d → %d (available %t)", before, after, ok)
	}
	if got := read(t, source); got[0] != 0 {
		t.Fatal("clone edit changed source")
	}
}

func TestInfoSizeGrowsWhenOriginalChanges(t *testing.T) {
	for _, operation := range []string{"overwrite", "delete"} {
		t.Run(operation, func(t *testing.T) {
			root := fixture(t)
			requireCloning(t, root)
			if err := initProject(root); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "data")
			data := make([]byte, 1024*1024)
			if err := os.WriteFile(source, data, 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := newTree(context.Background(), root, "snapshot"); err != nil {
				t.Fatal(err)
			}
			tree := filepath.Join(root, "snapshot.clonetree")
			before, err := treeSize(context.Background(), tree)
			if err != nil || before.Private == nil {
				t.Fatalf("private size unavailable: %+v, %v", before, err)
			}
			if operation == "delete" {
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			} else {
				data[0] = 1
				if err := os.WriteFile(source, data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			after, err := treeSize(context.Background(), tree)
			if err != nil || after.Private == nil || *after.Private <= *before.Private {
				t.Fatalf("snapshot did not acquire private storage: before %+v, after %+v, %v", before, after, err)
			}
			if after.Logical != before.Logical || read(t, filepath.Join(tree, "data"))[0] != 0 {
				t.Fatal("original change altered snapshot content or logical size")
			}
		})
	}
}
