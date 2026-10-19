//go:build darwin || linux

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var binary string

func TestMain(m *testing.M) {
	binary = os.Getenv("CLONETREE_E2E_BINARY")
	var directory string
	if os.Getenv("CLONETREE_E2E_ROOT") != "" && binary == "" {
		var err error
		directory, err = os.MkdirTemp("", "clonetree-e2e-bin-")
		if err == nil {
			binary = filepath.Join(directory, "clonetree")
			cmd := exec.Command("go", "build", "-o", binary, "..")
			cmd.Env = append(os.Environ(), "GOWORK=off")
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			err = cmd.Run()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "build CLI for e2e:", err)
			if directory != "" {
				os.RemoveAll(directory)
			}
			os.Exit(1)
		}
	}
	status := m.Run()
	if directory != "" {
		os.RemoveAll(directory)
	}
	os.Exit(status)
}

func fixture(t *testing.T) string {
	t.Helper()
	base := os.Getenv("CLONETREE_E2E_ROOT")
	if base == "" {
		t.Skip("set CLONETREE_E2E_ROOT and CLONETREE_E2E_FS to run native filesystem e2e tests")
	}
	expected := os.Getenv("CLONETREE_E2E_FS")
	if expected == "" {
		t.Fatal("CLONETREE_E2E_FS is required; filesystem tests must not silently run on another filesystem")
	}
	actual, err := filesystemType(base)
	if err != nil || actual != expected {
		t.Fatalf("filesystem: got %q, want %q: %v", actual, expected, err)
	}
	if binary == "" || !filepath.IsAbs(binary) {
		t.Fatal("CLONETREE_E2E_BINARY must be an absolute executable path")
	}
	return temporaryDirectory(t, base)
}

func temporaryDirectory(t *testing.T, base string) string {
	t.Helper()
	path, err := os.MkdirTemp(base, "clonetree-case-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return os.Chmod(path, 0700)
			}
			return nil
		})
		if err == nil {
			err = os.RemoveAll(path)
		}
		if err != nil {
			t.Errorf("clean fixture: %v", err)
		}
	})
	return path
}

func run(t *testing.T, cwd string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("CLI command timed out: %v", args)
	}
	return string(output), err
}

func success(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	output, err := run(t, cwd, args...)
	if err != nil {
		t.Fatalf("clonetree %v: %v\n%s", args, err, output)
	}
	if strings.ContainsAny(output, "\r\x1b") {
		t.Fatalf("terminal animation in captured output: %q", output)
	}
	return output
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func absent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no entry at %s: %v", path, err)
	}
}

func syncFile(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
}

func TestCLI(t *testing.T) {
	root := fixture(t)
	t.Logf("native %s CLI e2e", os.Getenv("CLONETREE_E2E_FS"))
	write(t, filepath.Join(root, ".gitignore"), []byte("# existing rules\n.env\n"))
	success(t, root, "init")
	success(t, root, "init")
	if got := string(read(t, filepath.Join(root, ".gitignore"))); got != "# existing rules\n.env\n*.clonetree/\n" {
		t.Fatalf("ignore rules: %q", got)
	}
	absent(t, filepath.Join(root, "clonetree"))
	absent(t, filepath.Join(root, "README.md"))
	payload := make([]byte, 4<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "payload")
	write(t, source, payload)
	syncFile(t, source)
	for _, name := range []string{".env", ".git/HEAD", ".sl/store/state", "node_modules/dependency", "café_日本"} {
		write(t, filepath.Join(root, name), []byte(name))
	}
	for i := range 900 {
		name := fmt.Sprintf("many/file-%04d", i)
		write(t, filepath.Join(root, name), []byte(name))
	}
	tool := filepath.Join(root, "bin", "tool")
	write(t, tool, []byte("executable"))
	if err := os.Chmod(tool, 0751); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 123456789)
	if err := os.Chtimes(tool, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(tool), 0555); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"relative": "payload", "absolute": source, "dangling": "missing"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(root, "older.clonetree", "keep"), []byte("older"))
	write(t, filepath.Join(root, "nested", "older.clonetree", "keep"), []byte("nested"))
	write(t, filepath.Join(root, "ordinary.clonetree"), []byte("regular file"))
	output := success(t, root, "new", "snapshot")
	if !strings.Contains(output, "clonetree/snapshot.clonetree") || !strings.Contains(output, "→") || !strings.Contains(output, " in ") {
		t.Fatalf("missing destination/size/time summary: %s", output)
	}
	tree := filepath.Join(root, "clonetree", "snapshot.clonetree")
	clone := filepath.Join(tree, "payload")
	for _, name := range []string{"payload", ".env", ".git/HEAD", ".sl/store/state", "node_modules/dependency", "café_日本", "many/file-0000", "many/file-0899", "ordinary.clonetree"} {
		if !bytes.Equal(read(t, filepath.Join(root, name)), read(t, filepath.Join(tree, name))) {
			t.Fatalf("content mismatch: %s", name)
		}
	}
	for _, name := range []string{"older.clonetree", "nested/older.clonetree", "clonetree/snapshot.clonetree"} {
		absent(t, filepath.Join(tree, name))
	}
	originalInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	cloneInfo, err := os.Stat(clone)
	if err != nil || os.SameFile(originalInfo, cloneInfo) {
		t.Fatalf("clone has no independent inode: %v", err)
	}
	originalTool, err := os.Stat(tool)
	if err != nil {
		t.Fatal(err)
	}
	clonedTool, err := os.Stat(filepath.Join(tree, "bin", "tool"))
	if err != nil || clonedTool.Mode() != originalTool.Mode() || !clonedTool.ModTime().Equal(originalTool.ModTime()) {
		t.Fatalf("file metadata changed: %v", err)
	}
	clonedDirectory, err := os.Stat(filepath.Join(tree, "bin"))
	if err != nil || clonedDirectory.Mode().Perm() != 0555 {
		t.Fatalf("read-only directory permissions changed: %v", err)
	}
	for name, target := range map[string]string{"relative": "payload", "absolute": source, "dangling": "missing"} {
		got, err := os.Readlink(filepath.Join(tree, name))
		if err != nil || got != target {
			t.Fatalf("symlink %s changed: %q, %v", name, got, err)
		}
	}
	syncFile(t, clone)
	verifyShared(t, root, source, clone, len(payload))
	if output, err := run(t, root, "new", "snapshot"); err == nil {
		t.Fatalf("overwrote an existing ID: %s", output)
	}
	changed := bytes.Repeat([]byte{0xa5}, len(payload))
	write(t, source, changed)
	syncFile(t, source)
	if !bytes.Equal(read(t, clone), payload) {
		t.Fatal("editing the original changed the clone")
	}
	verifyPrivateGrowth(t, root, len(payload))
	write(t, clone, bytes.Repeat([]byte{0x5a}, len(payload)))
	syncFile(t, clone)
	if !bytes.Equal(read(t, source), changed) {
		t.Fatal("editing the clone changed the original")
	}
	info := success(t, root, "info", "--no-progress")
	if !strings.Contains(info, "snapshot") || !strings.Contains(info, "PRIVATE") {
		t.Fatalf("info did not list the snapshot: %s", info)
	}
	success(t, root, "delete", "snapshot", "--no-progress")
	absent(t, tree)
	if string(read(t, filepath.Join(root, "older.clonetree", "keep"))) != "older" {
		t.Fatal("deleted an unrelated snapshot")
	}
}

func TestCurrentDirectoryAndCustomDestination(t *testing.T) {
	parent := fixture(t)
	write(t, filepath.Join(parent, ".git", "HEAD"), []byte("parent metadata"))
	write(t, filepath.Join(parent, ".gitignore"), []byte("# parent\n"))
	current := filepath.Join(parent, "child")
	write(t, filepath.Join(current, "data"), []byte("child data"))
	success(t, current, "init")
	if string(read(t, filepath.Join(parent, ".gitignore"))) != "# parent\n" {
		t.Fatal("init modified the parent repository")
	}
	for _, directory := range []string{"snapshots/nested", ".", temporaryDirectory(t, os.Getenv("CLONETREE_E2E_ROOT"))} {
		success(t, current, "new", "copy", "--dir", directory, "--no-progress")
		base := directory
		if !filepath.IsAbs(base) {
			base = filepath.Join(current, base)
		}
		tree := filepath.Join(base, "copy.clonetree")
		if string(read(t, filepath.Join(tree, "data"))) != "child data" {
			t.Fatal("did not clone the current working directory")
		}
		absent(t, filepath.Join(tree, ".git"))
		if !strings.Contains(success(t, current, "info", "--dir", directory), "copy") {
			t.Fatal("info ignored --dir")
		}
		success(t, current, "delete", "copy", "--dir", directory)
		absent(t, tree)
	}
}

func TestFailuresPreserveSourceAndCleanDestination(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "data"), []byte("keep"))
	outside := temporaryDirectory(t, os.Getenv("CLONETREE_E2E_ROOT"))
	write(t, filepath.Join(outside, "keep"), []byte("outside"))
	for _, id := range []string{"../escape", "..", "a/b"} {
		if output, err := run(t, root, "new", id); err == nil {
			t.Fatalf("accepted invalid ID %q: %s", id, output)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "clonetree"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "clonetree", "linked.clonetree")); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"new", "delete"} {
		if output, err := run(t, root, command, "linked"); err == nil {
			t.Fatalf("%s followed a snapshot symlink: %s", command, output)
		}
	}
	fifo := filepath.Join(root, "unsupported-fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := run(t, root, "new", "failed"); err == nil {
		t.Fatalf("silently accepted a FIFO: %s", output)
	}
	absent(t, filepath.Join(root, "clonetree", "failed.clonetree"))
	if string(read(t, filepath.Join(root, "data"))) != "keep" || string(read(t, filepath.Join(outside, "keep"))) != "outside" {
		t.Fatal("failure modified existing data")
	}
}

func TestCrossFilesystemCloneFailsWithoutCopyFallback(t *testing.T) {
	root := fixture(t)
	other := os.Getenv("CLONETREE_E2E_OTHER_ROOT")
	if other == "" {
		t.Skip("no second filesystem configured")
	}
	var left, right unix.Stat_t
	if err := unix.Stat(root, &left); err != nil {
		t.Fatal(err)
	}
	if err := unix.Stat(other, &right); err != nil {
		t.Fatal(err)
	}
	if left.Dev == right.Dev {
		t.Fatal("CLONETREE_E2E_OTHER_ROOT must be on a different filesystem")
	}
	store := temporaryDirectory(t, other)
	write(t, filepath.Join(root, "data"), bytes.Repeat([]byte{0x42}, 1<<20))
	if output, err := run(t, root, "new", "cross", "--dir", store, "--no-progress"); err == nil {
		t.Fatalf("cross-filesystem clone fell back to copying: %s", output)
	}
	absent(t, filepath.Join(store, "cross.clonetree"))
	if len(read(t, filepath.Join(root, "data"))) != 1<<20 {
		t.Fatal("cross-filesystem failure changed source")
	}
}
