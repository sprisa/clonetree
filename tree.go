package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const treeSuffix = ".clonetree"
const ignorePattern = "*" + treeSuffix + "/"

func validID(id string) bool {
	if id == "" {
		return false
	}
	for i := range len(id) {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		if i == 0 || c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func initProject(root string) error {
	path := filepath.Join(root, ".gitignore")
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New(".gitignore must be a regular file")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSuffix(line, "\r") == ignorePattern {
			return nil
		}
	}
	if len(data) != 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return os.WriteFile(path, append(data, []byte(ignorePattern+"\n")...), 0644)
}

type cloneResult struct {
	Logical uint64
	Added   *uint64
	Elapsed time.Duration
}

func newTree(ctx context.Context, root, directory, id string, report *progress) (result cloneResult, err error) {
	if !validID(id) {
		return result, errors.New("ID must start with a letter or digit and contain only letters, digits, hyphens or underscores")
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return result, err
	}
	before, beforeErr := freeSpace(directory)
	started := time.Now()
	destination := filepath.Join(directory, id+treeSuffix)
	if err = os.Mkdir(destination, 0700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return result, fmt.Errorf("%s already exists", destination)
		}
		return result, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, removeTree(destination))
		}
	}()
	plan, err := prepareClonePlan(ctx, root)
	if err != nil {
		return result, fmt.Errorf("scan clone source: %w", err)
	}
	if report != nil && plan != nil {
		report.setTotal(plan.entries)
	} else if report != nil {
		total, err := countEntries(ctx, root)
		if err != nil {
			return result, fmt.Errorf("count entries: %w", err)
		}
		report.setTotal(total)
	}
	result.Logical, err = cloneDirectory(ctx, root, destination, report, plan)
	if err != nil {
		return result, fmt.Errorf("clone failed (native copy-on-write support required): %w", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return result, err
	}
	if err = preserveMetadata(destination, metadataFromInfo(rootInfo)); err != nil {
		return result, err
	}
	result.Elapsed = time.Since(started)
	after, afterErr := freeSpace(directory)
	if beforeErr == nil && afterErr == nil && before >= after {
		added := before - after
		result.Added = &added
	}
	return result, nil
}

func forEachEntry(ctx context.Context, source string, visit func(fs.DirEntry) error) error {
	dir, err := os.Open(source)
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Filesystem order avoids sorting, and batches bound per-directory memory.
		entries, err := dir.ReadDir(256)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() && strings.HasSuffix(entry.Name(), treeSuffix) {
				// Skip snapshots at any depth, including our own destination.
				continue
			}
			if err := visit(entry); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}

func countEntries(ctx context.Context, source string) (uint64, error) {
	var total uint64
	err := forEachEntry(ctx, source, func(entry fs.DirEntry) error {
		total++
		if entry.IsDir() {
			children, err := countEntries(ctx, filepath.Join(source, entry.Name()))
			if err != nil {
				return err
			}
			total += children
		}
		return nil
	})
	return total, err
}

func cloneDirectory(ctx context.Context, source, destination string, report *progress, plan *clonePlan) (uint64, error) {
	var target *os.File
	defer func() {
		if target != nil {
			target.Close()
		}
	}()
	var logical uint64
	err := forEachFile(ctx, source, false, func(sourceFD int, info fileMetadata) error {
		if info.mode.IsDir() && strings.HasSuffix(info.name, treeSuffix) {
			return nil
		}
		var size uint64
		if info.mode.IsRegular() {
			if target == nil {
				var err error
				target, err = os.Open(destination)
				if err != nil {
					return err
				}
			}
			// Reuse directory handles and the existing name: no full paths per file.
			if err := cloneFileAt(sourceFD, int(target.Fd()), info.name, info); err != nil {
				return fmt.Errorf("%s: %w", filepath.Join(source, info.name), err)
			}
			size = uint64(info.size)
			if report != nil {
				report.add(size)
			}
		} else {
			if info.mode.IsDir() && target != nil {
				// Do not keep a destination handle at every level of a deep tree.
				err := target.Close()
				target = nil
				if err != nil {
					return err
				}
			}
			src, dst := filepath.Join(source, info.name), filepath.Join(destination, info.name)
			var err error
			size, err = cloneEntry(ctx, src, dst, info, report, plan)
			if err != nil {
				return err
			}
		}
		logical += size
		return nil
	})
	return logical, err
}

func cloneEntry(ctx context.Context, source, destination string, info fileMetadata, report *progress, plan *clonePlan) (uint64, error) {
	if info.mode&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			return 0, err
		}
		if err := os.Symlink(target, destination); err != nil {
			return 0, err
		}
		if report != nil {
			report.add(0)
		}
		return 0, nil
	}
	if !info.mode.IsDir() {
		return 0, fmt.Errorf("unsupported file type: %s", source)
	}
	if plan.allowsNative(source, info) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		cloned, err := cloneDirectoryNative(source, destination)
		if err != nil {
			return 0, err
		}
		if cloned {
			actual, err := inspectDirectoryClone(ctx, source, destination, info)
			if err != nil {
				return 0, err
			}
			if report != nil {
				report.entries.Add(actual.entries + 1)
				report.bytes.Add(actual.logical)
			}
			return actual.logical, nil
		}
		// Avoid retrying an unsupported directory operation for every descendant.
		plan.disabled = true
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return 0, err
	}
	logical, err := cloneDirectory(ctx, source, destination, report, plan)
	if err != nil {
		return 0, err
	}
	if err := preserveMetadata(destination, info); err != nil {
		return 0, err
	}
	if report != nil {
		report.add(0) // Descendants have already reported their content.
	}
	return logical, nil
}

func preserveMetadata(path string, info fileMetadata) error {
	if err := os.Chmod(path, info.mode); err != nil {
		return err
	}
	return os.Chtimes(path, info.modified, info.modified)
}

func deleteTree(root, id string) error {
	if !validID(id) {
		return errors.New("invalid clonetree ID")
	}
	path := filepath.Join(root, id+treeSuffix)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s%s is not a directory", id, treeSuffix)
	}
	return removeTree(path)
}

func removeTree(path string) error {
	if err := makeTreeRemovable(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func makeTreeRemovable(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return err
	}
	if info.Mode().Perm()&0700 != 0700 {
		if err := os.Chmod(path, info.Mode()|0700); err != nil {
			return err
		}
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		// WalkDir sorts and retains every entry in a wide directory. Bounded
		// batches keep cancellation cleanup and late-exclusion pruning flat too.
		entries, err := dir.ReadDir(256)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if err := makeTreeRemovable(filepath.Join(path, entry.Name())); err != nil {
					return err
				}
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}
