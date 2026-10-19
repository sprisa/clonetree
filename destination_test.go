package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomSnapshotDirectory(t *testing.T) {
	for _, kind := range []string{"relative", "absolute", "current"} {
		t.Run(kind, func(t *testing.T) {
			root := fixture(t)
			requireCloning(t, root)
			t.Chdir(root)
			write(t, filepath.Join(root, "source"), "source content")
			write(t, filepath.Join(root, "clonetree", "copy.clonetree", "keep"), "default snapshot")
			write(t, filepath.Join(root, "clonetree", "default-only.clonetree", "keep"), "default only")
			directory := "snapshots/nested"
			args := []string{"new", "-d", directory, "copy"}
			if kind == "absolute" {
				directory = filepath.Join(t.TempDir(), "shared snapshots")
				args = []string{"--dir", directory, "new", "copy"}
			} else if kind == "current" {
				directory = "."
				args = []string{"new", "copy", "--dir", directory}
			}
			parent, err := filepath.Abs(directory)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runCLI(t, args...); err != nil {
				t.Fatal(err)
			}
			tree := filepath.Join(parent, "copy.clonetree")
			if read(t, filepath.Join(tree, "source")) != "source content" {
				t.Fatal("destination flag changed the source directory")
			}
			if _, err := os.Lstat(filepath.Join(tree, "clonetree", "copy.clonetree")); !os.IsNotExist(err) {
				t.Fatal("included an existing snapshot")
			}
			if kind == "relative" {
				if _, err := os.Lstat(filepath.Join(tree, "snapshots", "nested", "copy.clonetree")); !os.IsNotExist(err) {
					t.Fatal("clone recursed into its custom destination")
				}
			}
			output, err := runCLI(t, "info", "--dir", directory)
			if err != nil || !strings.Contains(output, "copy") || strings.Contains(output, "default-only") {
				t.Fatalf("custom info: %s, %v", output, err)
			}
			if _, err := runCLI(t, "delete", "--dir", directory, "copy"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(tree); !os.IsNotExist(err) {
				t.Fatal("custom snapshot was not deleted")
			}
			if read(t, filepath.Join(root, "clonetree", "copy.clonetree", "keep")) != "default snapshot" {
				t.Fatal("custom delete changed the default snapshot")
			}
		})
	}
}

func TestInfoDoesNotCreateSnapshotDirectory(t *testing.T) {
	root := fixture(t)
	t.Chdir(root)
	if _, err := runCLI(t, "info"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "clonetree")); !os.IsNotExist(err) {
		t.Fatal("info created the snapshot directory")
	}
	for _, command := range []string{"new", "info", "delete"} {
		args := []string{command, "--dir", ""}
		if command != "info" {
			args = append(args, "copy")
		}
		if _, err := runCLI(t, args...); err == nil {
			t.Fatalf("%s accepted an empty directory", command)
		}
	}
}

func TestCustomDestinationFailurePreservesOtherSnapshots(t *testing.T) {
	root := fixture(t)
	directory := filepath.Join(fixture(t), "nested", "snapshots")
	write(t, filepath.Join(directory, "existing.clonetree", "keep"), "keep")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newTree(ctx, root, directory, "cancelled", nil); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := os.Lstat(filepath.Join(directory, "cancelled.clonetree")); !os.IsNotExist(err) {
		t.Fatal("partial custom snapshot left behind")
	}
	if read(t, filepath.Join(directory, "existing.clonetree", "keep")) != "keep" {
		t.Fatal("removed an existing custom snapshot")
	}
	blocked := filepath.Join(root, "blocked")
	write(t, blocked, "regular file")
	if _, err := newTree(context.Background(), root, blocked, "copy", nil); err == nil {
		t.Fatal("accepted a regular file as the snapshot parent")
	}
	if read(t, blocked) != "regular file" {
		t.Fatal("overwrote an invalid snapshot parent")
	}
}
