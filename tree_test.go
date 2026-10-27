package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func requireCloning(t *testing.T, root string) {
	t.Helper()
	source, target := filepath.Join(root, "probe"), filepath.Join(root, "probe-clone")
	write(t, source, "native cloning probe")
	err := cloneFile(source, target)
	defer os.Remove(source)
	defer os.Remove(target)
	if err != nil {
		// Integration tests require a reflink-capable test filesystem.
		if strings.Contains(err.Error(), "not supported") || strings.Contains(err.Error(), "not implemented") ||
			runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
			t.Skipf("native cloning unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	cmd := command()
	cmd.Writer = &output
	cmd.ErrWriter = &output
	err := cmd.Run(context.Background(), append([]string{"clonetree"}, args...))
	return output.String(), err
}

func TestCommands(t *testing.T) {
	root := fixture(t)
	requireCloning(t, root)
	t.Chdir(root)
	write(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(root, ".sl", "store", "bookmarks"), "main\n")
	write(t, filepath.Join(root, "local.txt"), "local changes")
	write(t, filepath.Join(root, "node_modules", "dependency"), "installed")
	write(t, filepath.Join(root, ".env"), "ignored")
	write(t, filepath.Join(root, ".gitignore"), "node_modules/\n.env\n")
	if _, err := runCLI(t, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "init"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, ".gitignore")); got != "node_modules/\n.env\n*.clonetree/\n" {
		t.Fatalf("ignore rules: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "clonetree")); !os.IsNotExist(err) {
		t.Fatal("init created a clonetree directory")
	}
	write(t, filepath.Join(root, "README.md"), "keep docs")
	write(t, filepath.Join(root, "clonetree", "existing.clonetree", "keep"), "existing clone")
	write(t, filepath.Join(root, "packages", "nested.clonetree", "keep"), "nested clone")
	write(t, filepath.Join(root, "packages", "local", "keep"), "ordinary directory")
	write(t, filepath.Join(root, "settings.clonetree"), "ordinary file with suffix")
	output, err := runCLI(t, "new", "feature_1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "→") || !strings.Contains(output, " in ") || !strings.Contains(output, "clonetree/feature_1.clonetree") {
		t.Fatalf("missing size/time summary: %s", output)
	}
	if strings.ContainsAny(output, "\r\x1b") {
		t.Fatalf("terminal progress leaked into redirected output: %q", output)
	}
	tree := filepath.Join(root, "clonetree", "feature_1.clonetree")
	for _, name := range []string{"local.txt", ".git/HEAD", ".sl/store/bookmarks", ".env", "node_modules/dependency", "README.md", ".gitignore", "packages/local/keep", "settings.clonetree"} {
		if read(t, filepath.Join(tree, name)) != read(t, filepath.Join(root, name)) {
			t.Errorf("content mismatch: %s", name)
		}
	}
	for _, name := range []string{"clonetree/existing.clonetree", "clonetree/feature_1.clonetree", "packages/nested.clonetree"} {
		if _, err := os.Lstat(filepath.Join(tree, name)); !os.IsNotExist(err) {
			t.Fatalf("clonetree was included: %s", name)
		}
	}
	write(t, filepath.Join(tree, "local.txt"), "changed clone")
	write(t, filepath.Join(tree, ".git", "HEAD"), "changed clone metadata")
	write(t, filepath.Join(tree, ".sl", "store", "bookmarks"), "changed Sapling metadata")
	if read(t, filepath.Join(root, "local.txt")) != "local changes" ||
		read(t, filepath.Join(root, ".git", "HEAD")) != "ref: refs/heads/main\n" ||
		read(t, filepath.Join(root, ".sl", "store", "bookmarks")) != "main\n" {
		t.Fatal("clone edit changed original")
	}
	if _, err := runCLI(t, "new", "feature_1"); err == nil {
		t.Fatal("existing ID was accepted")
	}
	if read(t, filepath.Join(tree, "local.txt")) != "changed clone" {
		t.Fatal("existing snapshot overwritten")
	}
	output, err = runCLI(t, "info")
	if err != nil || !strings.Contains(output, "feature_1") || !strings.Contains(output, "existing") {
		t.Fatalf("info: %s, %v", output, err)
	}
	if strings.Contains(output, "packages") || strings.Contains(output, "settings") {
		t.Fatalf("info included a non-clonetree: %s", output)
	}
	if _, err := runCLI(t, "delete", "feature_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(tree); !os.IsNotExist(err) {
		t.Fatal("deleted snapshot still exists")
	}
	if read(t, filepath.Join(root, "clonetree", "existing.clonetree", "keep")) != "existing clone" {
		t.Fatal("deleted another snapshot")
	}
}

func TestClonePreservesModesTimesAndSymlinks(t *testing.T) {
	root := fixture(t)
	requireCloning(t, root)
	if err := initProject(root); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "bin", "tool")
	write(t, source, "executable")
	if err := os.Chmod(source, 0751); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1700000000, 123456789)
	if err := os.Chtimes(source, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{
		"readonly": 0440,
		"setuid":   0751 | os.ModeSetuid,
		"setgid":   0751 | os.ModeSetgid,
	} {
		path := filepath.Join(root, "bin", name)
		write(t, path, name)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{"relative": "bin/tool", "absolute": source, "broken": "missing"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Dir(source), 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Dir(source), 0755) })
	if _, err := newTree(context.Background(), root, root, "clone", nil); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(root, "clone.clonetree")
	for _, name := range []string{"tool", "readonly", "setuid", "setgid"} {
		original, err := os.Stat(filepath.Join(root, "bin", name))
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Join(tree, "bin", name))
		if err != nil || info.Mode() != original.Mode() || !info.ModTime().Equal(original.ModTime()) {
			t.Fatalf("%s metadata: %v, %v; original %v", name, info, err, original)
		}
	}
	for name, target := range map[string]string{"relative": "bin/tool", "absolute": source, "broken": "missing"} {
		got, err := os.Readlink(filepath.Join(tree, name))
		if err != nil || got != target {
			t.Fatalf("symlink %s: %q, %v", name, got, err)
		}
	}
	if err := deleteTree(root, "clone"); err != nil {
		t.Fatalf("delete read-only directories: %v", err)
	}
}

func TestCloneSpansDirectoryBatches(t *testing.T) {
	root := fixture(t)
	requireCloning(t, root)
	if err := initProject(root); err != nil {
		t.Fatal(err)
	}
	expected := uint64(len(read(t, filepath.Join(root, ".gitignore"))))
	for i := range 600 {
		name := fmt.Sprintf("file-%03d", i)
		write(t, filepath.Join(root, name), name)
		expected += uint64(len(name))
	}
	result, err := newTree(context.Background(), root, root, "many-files", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Logical != expected {
		t.Fatalf("cloned %d bytes, want %d", result.Logical, expected)
	}
	for i := range 600 {
		name := fmt.Sprintf("file-%03d", i)
		if got := read(t, filepath.Join(root, "many-files.clonetree", name)); got != name {
			t.Fatalf("%s content: %q", name, got)
		}
	}
}

func TestValidationAndSymlinkProtection(t *testing.T) {
	root := fixture(t)
	if err := initProject(root); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", ".", "..", "../escape", "a/b", "a\\b", "-option", "two words"} {
		if _, err := newTree(context.Background(), root, root, id, nil); err == nil {
			t.Errorf("new accepted %q", id)
		}
		if err := deleteTree(root, id); err == nil {
			t.Errorf("delete accepted %q", id)
		}
	}
	write(t, filepath.Join(root, "outside", "keep"), "keep")
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "linked.clonetree")); err != nil {
		t.Fatal(err)
	}
	if _, err := newTree(context.Background(), root, root, "linked", nil); err == nil {
		t.Fatal("new accepted symlink destination")
	}
	if err := deleteTree(root, "linked"); err == nil {
		t.Fatal("delete accepted symlink destination")
	}
	write(t, filepath.Join(root, "file.clonetree"), "keep file")
	if _, err := newTree(context.Background(), root, root, "file", nil); err == nil {
		t.Fatal("new accepted a regular-file destination")
	}
	if err := deleteTree(root, "file"); err == nil {
		t.Fatal("delete accepted a regular-file destination")
	}
	if read(t, filepath.Join(root, "file.clonetree")) != "keep file" {
		t.Fatal("changed existing regular file")
	}
	if read(t, filepath.Join(root, "outside", "keep")) != "keep" {
		t.Fatal("symlink target changed")
	}
	t.Chdir(root)
	for _, args := range [][]string{{"new"}, {"new", "a", "b"}, {"delete"}, {"init", "extra"}, {"info", "extra"}, {"typo"}} {
		if _, err := runCLI(t, args...); err == nil {
			t.Errorf("accepted arguments %v", args)
		}
	}
}

func TestCancellationCleansOnlyNewTree(t *testing.T) {
	root := fixture(t)
	if err := initProject(root); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "existing.clonetree", "keep"), "keep")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newTree(ctx, root, root, "cancelled", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "cancelled.clonetree")); !os.IsNotExist(err) {
		t.Fatal("partial snapshot left behind")
	}
	if read(t, filepath.Join(root, "existing.clonetree", "keep")) != "keep" {
		t.Fatal("removed existing snapshot")
	}
}

func TestInitPreservesIgnoreRulesAndRejectsLinks(t *testing.T) {
	root := fixture(t)
	ignore := filepath.Join(root, ".gitignore")
	write(t, ignore, "# user rule\n*.log")
	if err := initProject(root); err != nil {
		t.Fatal(err)
	}
	if got := read(t, ignore); got != "# user rule\n*.log\n*.clonetree/\n" {
		t.Fatalf("ignore rules: %q", got)
	}
	if err := initProject(root); err != nil {
		t.Fatal(err)
	}
	if strings.Count(read(t, ignore), "*.clonetree/\n") != 1 {
		t.Fatal("init duplicated ignore rule")
	}
	if err := os.Remove(ignore); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "outside"), "keep")
	if err := os.Symlink(filepath.Join(root, "outside"), ignore); err != nil {
		t.Fatal(err)
	}
	if err := initProject(root); err == nil {
		t.Fatal("init followed ignore symlink")
	}
	if read(t, filepath.Join(root, "outside")) != "keep" {
		t.Fatal("init overwrote symlink target")
	}
}

func TestCommandsUseCurrentDirectory(t *testing.T) {
	for _, kind := range []string{"ordinary-directory", "inside-repository"} {
		t.Run(kind, func(t *testing.T) {
			parent := fixture(t)
			requireCloning(t, parent)
			if kind == "inside-repository" {
				write(t, filepath.Join(parent, ".git", "HEAD"), "parent metadata")
				write(t, filepath.Join(parent, ".sl", "store", "bookmarks"), "parent metadata")
			}
			write(t, filepath.Join(parent, ".gitignore"), "# parent rules\n")
			write(t, filepath.Join(parent, "clonetree", "same.clonetree", "keep"), "parent snapshot")
			write(t, filepath.Join(parent, "clonetree", "parent-only.clonetree", "keep"), "parent only")
			current := filepath.Join(parent, "child")
			write(t, filepath.Join(current, "data"), "child data")
			t.Chdir(current)
			if _, err := runCLI(t, "init"); err != nil {
				t.Fatal(err)
			}
			if read(t, filepath.Join(current, ".gitignore")) != "*.clonetree/\n" ||
				read(t, filepath.Join(parent, ".gitignore")) != "# parent rules\n" {
				t.Fatal("init did not stay in the current directory")
			}
			if _, err := runCLI(t, "new", "same"); err != nil {
				t.Fatal(err)
			}
			tree := filepath.Join(current, "clonetree", "same.clonetree")
			if read(t, filepath.Join(tree, "data")) != "child data" {
				t.Fatal("did not clone current directory")
			}
			for _, name := range []string{".git", ".sl", "child"} {
				if _, err := os.Lstat(filepath.Join(tree, name)); !os.IsNotExist(err) {
					t.Fatalf("included data from parent directory: %s", name)
				}
			}
			output, err := runCLI(t, "info")
			if err != nil || !strings.Contains(output, "same") || strings.Contains(output, "parent-only") {
				t.Fatalf("info did not stay in current directory: %s, %v", output, err)
			}
			if _, err := runCLI(t, "delete", "parent-only"); err == nil {
				t.Fatal("delete found a snapshot in the parent directory")
			}
			if _, err := runCLI(t, "delete", "same"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(tree); !os.IsNotExist(err) {
				t.Fatal("current snapshot was not deleted")
			}
			if read(t, filepath.Join(parent, "clonetree", "same.clonetree", "keep")) != "parent snapshot" ||
				read(t, filepath.Join(parent, "clonetree", "parent-only.clonetree", "keep")) != "parent only" {
				t.Fatal("changed snapshots outside the current directory")
			}
		})
	}
}

func TestVCSReferencesAreCopiedVerbatim(t *testing.T) {
	root := fixture(t)
	requireCloning(t, root)
	external := t.TempDir()
	write(t, filepath.Join(root, ".git"), "gitdir: "+filepath.Join(external, "git-metadata")+"\n")
	write(t, filepath.Join(root, ".sl", "sharedpath"), filepath.Join(external, "sapling-store")+"\n")
	write(t, filepath.Join(external, "sapling-store", "keep"), "external data")
	if err := os.Symlink(filepath.Join(external, "sapling-store"), filepath.Join(root, ".sl", "store")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if _, err := runCLI(t, "new", "copy"); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(root, "clonetree", "copy.clonetree")
	for _, name := range []string{".git", ".sl/sharedpath"} {
		if read(t, filepath.Join(tree, name)) != read(t, filepath.Join(root, name)) {
			t.Fatalf("rewrote VCS metadata: %s", name)
		}
	}
	target, err := os.Readlink(filepath.Join(tree, ".sl", "store"))
	if err != nil || target != filepath.Join(external, "sapling-store") {
		t.Fatalf("external symlink: %q, %v", target, err)
	}
	if read(t, filepath.Join(external, "sapling-store", "keep")) != "external data" {
		t.Fatal("changed external store")
	}
}

func TestRemoveTreeStreamsReadonlyDirectoryWithoutFollowingLinks(t *testing.T) {
	root, outside := fixture(t), fixture(t)
	write(t, filepath.Join(outside, "keep"), "outside")
	target := filepath.Join(root, "copy.clonetree")
	for i := range 1000 {
		write(t, filepath.Join(target, "readonly", fmt.Sprintf("file-%04d", i)), "data")
	}
	if err := os.Symlink(outside, filepath.Join(target, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(target, "readonly"), 0555); err != nil {
		t.Fatal(err)
	}
	if err := removeTree(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("removed tree still exists")
	}
	if read(t, filepath.Join(outside, "keep")) != "outside" {
		t.Fatal("cleanup followed an external symlink")
	}
}
