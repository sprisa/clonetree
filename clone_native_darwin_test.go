package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func requireDirectoryCloning(t *testing.T, root string) {
	t.Helper()
	source, target := filepath.Join(root, "directory-probe"), filepath.Join(root, "directory-probe-copy")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(source)
	defer os.Remove(target)
	cloned, err := cloneDirectoryNative(source, target)
	if err != nil {
		t.Fatal(err)
	}
	if !cloned {
		t.Skip("native directory cloning is unavailable")
	}
}

func plannedNativeDirectory(t *testing.T, plan *clonePlan, path string) bool {
	t.Helper()
	var info fileMetadata
	found := false
	err := forEachFile(context.Background(), filepath.Dir(path), false, func(_ int, entry fileMetadata) error {
		if entry.name == filepath.Base(path) {
			info, found = entry, true
		}
		return nil
	})
	if err != nil || !found {
		t.Fatalf("directory metadata for %s: found=%t error=%v", path, found, err)
	}
	return plan.allowsNative(path, info)
}

func TestNativeDirectoryClonePreservesContentAndMetadata(t *testing.T) {
	root := fixture(t)
	requireDirectoryCloning(t, root)
	source := filepath.Join(root, "source")
	data := bytes.Repeat([]byte("native clone data"), 1<<16)
	write(t, filepath.Join(source, "native", "deep", "café_日本"), string(data))
	write(t, filepath.Join(source, "native", ".hidden"), "hidden")
	file := filepath.Join(source, "native", "deep", "café_日本")
	if err := os.Chmod(file, 0751); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"relative": "deep/café_日本", "absolute": file, "dangling": "missing", "loop": "."} {
		if err := os.Symlink(target, filepath.Join(source, "native", name)); err != nil {
			t.Fatal(err)
		}
	}
	mtime := time.Unix(1700000000, 123456789)
	for _, path := range []string{file, filepath.Join(source, "native", "deep"), filepath.Join(source, "native"), source} {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	readonly := filepath.Join(source, "native", "deep")
	if err := os.Chmod(readonly, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readonly, 0755) })
	plan, err := prepareClonePlan(context.Background(), source)
	if err != nil || plan == nil || !plannedNativeDirectory(t, plan, filepath.Join(source, "native")) {
		t.Fatalf("native subtree was not planned: %+v, %v", plan, err)
	}
	report := &progress{}
	result, err := newTree(context.Background(), source, filepath.Join(root, "outputs"), "copy", report)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "outputs", "copy.clonetree")
	t.Cleanup(func() {
		if err := removeTree(filepath.Join(root, "outputs")); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	})
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		copyPath := filepath.Join(target, relative)
		before, err := os.Lstat(path)
		if err != nil {
			return err
		}
		after, err := os.Lstat(copyPath)
		if err != nil {
			return err
		}
		if before.Mode() != after.Mode() || before.Mode()&os.ModeSymlink == 0 && !before.ModTime().Equal(after.ModTime()) {
			t.Fatalf("metadata changed for %s: before %v/%v, after %v/%v", relative, before.Mode(), before.ModTime(), after.Mode(), after.ModTime())
		}
		if before.Mode()&os.ModeSymlink != 0 {
			want, _ := os.Readlink(path)
			got, err := os.Readlink(copyPath)
			if err != nil || got != want {
				t.Fatalf("symlink %s: %q, want %q: %v", relative, got, want, err)
			}
		} else if before.Mode().IsRegular() {
			if os.SameFile(before, after) || read(t, path) != read(t, copyPath) {
				t.Fatalf("file was not independently cloned: %s", relative)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Logical != uint64(len(data)+len("hidden")) || report.entries.Load() != report.total.Load() || report.bytes.Load() != result.Logical {
		t.Fatalf("native clone size/progress: %+v; %d/%d entries, %d bytes", result, report.entries.Load(), report.total.Load(), report.bytes.Load())
	}
	copyFile := filepath.Join(target, "native", "deep", "café_日本")
	private, known := privateSize(copyFile)
	if !known || private >= uint64(len(data)) {
		t.Fatalf("native subtree did not share data blocks: %d/%t", private, known)
	}
	if err := os.WriteFile(copyFile, []byte("changed clone"), 0751); err != nil {
		t.Fatal(err)
	}
	if read(t, file) != string(data) {
		t.Fatal("native directory clone edit changed the original")
	}
	if err := deleteTree(filepath.Join(root, "outputs"), "copy"); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCloneKeepsPerFileHardlinkIndependence(t *testing.T) {
	root := fixture(t)
	requireDirectoryCloning(t, root)
	source := filepath.Join(root, "source")
	first := filepath.Join(source, "nested", "first")
	write(t, first, "original")
	if err := os.Link(first, filepath.Join(source, "nested", "second")); err != nil {
		t.Fatal(err)
	}
	if _, err := newTree(context.Background(), source, root, "copy", nil); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "copy.clonetree", "nested")
	write(t, filepath.Join(target, "first"), "changed")
	if read(t, first) != "original" || read(t, filepath.Join(target, "second")) != "original" {
		t.Fatal("native directory cloning introduced shared writable files")
	}
}

func TestClonePlanFallsBackForSpecialMetadata(t *testing.T) {
	root := fixture(t)
	requireDirectoryCloning(t, root)
	write(t, filepath.Join(root, "ordinary", "data"), "ordinary")
	setID := filepath.Join(root, "setid", "data")
	write(t, setID, "setid")
	if err := os.Chmod(setID, 0755|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	flagged := filepath.Join(root, "flagged", "data")
	write(t, flagged, "flagged")
	if err := unix.Chflags(flagged, unix.UF_NODUMP); err != nil {
		t.Fatal(err)
	}
	flaggedDirectory := filepath.Join(root, "flagged-directory")
	write(t, filepath.Join(flaggedDirectory, "ordinary", "data"), "ordinary")
	if err := unix.Chflags(flaggedDirectory, unix.UF_NODUMP); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "excluded", "old.clonetree", "data"), "excluded")
	write(t, filepath.Join(root, "excluded", "ordinary", "data"), "ordinary")
	plan, err := prepareClonePlan(context.Background(), root)
	if err != nil || plan == nil {
		t.Fatalf("clone plan: %v", err)
	}
	if !plannedNativeDirectory(t, plan, filepath.Join(root, "ordinary")) ||
		plannedNativeDirectory(t, plan, filepath.Join(root, "setid")) ||
		plannedNativeDirectory(t, plan, filepath.Join(root, "flagged")) ||
		plannedNativeDirectory(t, plan, filepath.Join(root, "excluded")) {
		t.Fatal("unsafe directory was eligible for bulk cloning")
	}
	if !plannedNativeDirectory(t, plan, filepath.Join(root, "excluded", "ordinary")) {
		t.Fatal("excluded ancestor disabled an unrelated safe subtree")
	}
	if plannedNativeDirectory(t, plan, flaggedDirectory) || !plannedNativeDirectory(t, plan, filepath.Join(flaggedDirectory, "ordinary")) {
		t.Fatal("directory flags were not isolated to their fallback subtree")
	}
}

func TestClonePlanRetainsOnlyBoundedUnsafePaths(t *testing.T) {
	root := fixture(t)
	requireDirectoryCloning(t, root)
	for i := range 100 {
		write(t, filepath.Join(root, fmt.Sprintf("ordinary-%03d", i), "data"), "data")
	}
	plan, err := planCloneSource(context.Background(), root, 4, maxNativeCloneEntries)
	if err != nil || plan == nil || len(plan.unsafe) != 0 || plan.entries != 200 {
		t.Fatalf("ordinary directories were retained: %+v, %v", plan, err)
	}
	for i := range 12 {
		path := filepath.Join(root, fmt.Sprintf("unsafe-%03d", i), "data")
		write(t, path, "flagged")
		if err := unix.Chflags(path, unix.UF_NODUMP); err != nil {
			t.Fatal(err)
		}
	}
	plan, err = planCloneSource(context.Background(), root, 4, maxNativeCloneEntries)
	if err != nil || plan != nil {
		t.Fatalf("eligibility overflow did not fall back: %+v, %v", plan, err)
	}
}

func TestClonePlanSplitsLargeNativeBatches(t *testing.T) {
	root := fixture(t)
	requireDirectoryCloning(t, root)
	write(t, filepath.Join(root, "small", "data"), "small")
	for _, name := range []string{"first", "second"} {
		for i := range 3 {
			write(t, filepath.Join(root, "large", name, fmt.Sprintf("file-%d", i)), "data")
		}
	}
	plan, err := planCloneSource(context.Background(), root, maxUnsafeClonePaths, 4)
	if err != nil || plan == nil {
		t.Fatalf("batch plan: %v", err)
	}
	if plannedNativeDirectory(t, plan, filepath.Join(root, "large")) {
		t.Fatal("oversized subtree was allowed in one native operation")
	}
	for _, path := range []string{"small", "large/first", "large/second"} {
		if !plannedNativeDirectory(t, plan, filepath.Join(root, path)) {
			t.Fatalf("small subtree was not eligible: %s", path)
		}
	}
}

func TestNativeCloneInspectionRejectsSymlinkRoot(t *testing.T) {
	root, outside := fixture(t), fixture(t)
	write(t, filepath.Join(outside, "keep.clonetree", "data"), "outside")
	link := filepath.Join(root, "linked-directory")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectDirectoryClone(context.Background(), "", link, fileMetadata{}, nil); err == nil {
		t.Fatal("inspection accepted a symlink in place of the cloned directory")
	}
	if read(t, filepath.Join(outside, "keep.clonetree", "data")) != "outside" {
		t.Fatal("inspection followed a symlink and pruned external data")
	}
}

func TestNativeCloneInspectionPrunesLateExclusions(t *testing.T) {
	root := fixture(t)
	readonly := filepath.Join(root, "readonly")
	write(t, filepath.Join(readonly, "keep"), "keep")
	for _, name := range []string{"first.clonetree", "second.clonetree"} {
		write(t, filepath.Join(readonly, name, "data"), "excluded")
	}
	outside := fixture(t)
	write(t, filepath.Join(outside, "keep"), "outside")
	if err := os.Symlink(outside, filepath.Join(root, "link.clonetree")); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1700000000, 123456789)
	if err := os.Chtimes(readonly, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readonly, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(readonly, 0755) })
	plan, err := inspectDirectoryClone(context.Background(), "", root, fileMetadata{}, nil)
	if err != nil || plan.logical != 4 || plan.entries != 3 {
		t.Fatalf("inspected clone: %+v, %v", plan, err)
	}
	for _, name := range []string{"first.clonetree", "second.clonetree"} {
		if _, err := os.Lstat(filepath.Join(readonly, name)); !os.IsNotExist(err) {
			t.Fatalf("late exclusion was included: %s", name)
		}
	}
	info, err := os.Stat(readonly)
	if err != nil || info.Mode().Perm() != 0555 || !info.ModTime().Equal(mtime) || read(t, filepath.Join(outside, "keep")) != "outside" {
		t.Fatal("pruning changed parent metadata or an external symlink target")
	}
}

type observeInspectionProgress struct {
	context.Context
	report   *progress
	observed *bool
}

func (ctx observeInspectionProgress) Err() error {
	if ctx.report.entries.Load() > 0 {
		*ctx.observed = true
	}
	return ctx.Context.Err()
}

func TestNativeInspectionReportsProgressDuringScan(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "first"), "first")
	write(t, filepath.Join(root, "nested", "second"), "second")
	report := &progress{}
	observed := false
	ctx := observeInspectionProgress{context.Background(), report, &observed}
	totals, err := inspectDirectoryClone(ctx, "", root, fileMetadata{}, report)
	if err != nil || !observed || report.entries.Load() != totals.entries || report.bytes.Load() != totals.logical {
		t.Fatalf("inspection progress was delayed until the batch ended: observed=%t totals=%+v entries=%d bytes=%d error=%v",
			observed, totals, report.entries.Load(), report.bytes.Load(), err)
	}
}

type cancelAfterClone struct {
	context.Context
	path   string
	cancel context.CancelFunc
}

func (ctx cancelAfterClone) Err() error {
	if _, err := os.Lstat(ctx.path); err == nil {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestCancellationAfterNativeDirectoryCloneCleansDestination(t *testing.T) {
	root := fixture(t)
	requireDirectoryCloning(t, root)
	source, outputs := filepath.Join(root, "source"), filepath.Join(root, "outputs")
	write(t, filepath.Join(source, "native", "data"), "source")
	write(t, filepath.Join(outputs, "existing.clonetree", "keep"), "existing")
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := cancelAfterClone{base, filepath.Join(outputs, "copy.clonetree", "native", "data"), cancel}
	if _, err := newTree(ctx, source, outputs, "copy", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation after native clone: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(outputs, "copy.clonetree")); !os.IsNotExist(err) {
		t.Fatal("cancelled native clone was left behind")
	}
	if read(t, filepath.Join(outputs, "existing.clonetree", "keep")) != "existing" || read(t, filepath.Join(source, "native", "data")) != "source" {
		t.Fatal("cancellation changed existing data")
	}
}
