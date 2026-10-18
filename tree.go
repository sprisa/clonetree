package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

const treeSuffix = ".clonetree"
const ignorePattern = "*" + treeSuffix + "/"

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

func newTree(ctx context.Context, root, id string) (result cloneResult, err error) {
	if !validID.MatchString(id) {
		return result, errors.New("ID must start with a letter or digit and contain only letters, digits, hyphens or underscores")
	}
	before, beforeErr := freeSpace(root)
	started := time.Now()
	destination := filepath.Join(root, id+treeSuffix)
	if err = os.Mkdir(destination, 0700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return result, fmt.Errorf("%s%s already exists", id, treeSuffix)
		}
		return result, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, removeTree(destination))
		}
	}()
	result.Logical, err = cloneDirectory(ctx, root, destination)
	if err != nil {
		return result, fmt.Errorf("clone failed (native copy-on-write support required): %w", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return result, err
	}
	if err = preserveMetadata(destination, rootInfo); err != nil {
		return result, err
	}
	result.Elapsed = time.Since(started)
	after, afterErr := freeSpace(root)
	if beforeErr == nil && afterErr == nil && before >= after {
		added := before - after
		result.Added = &added
	}
	return result, nil
}

func cloneDirectory(ctx context.Context, source, destination string) (uint64, error) {
	dir, err := os.Open(source)
	if err != nil {
		return 0, err
	}
	defer dir.Close()
	var logical uint64
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		// Filesystem order avoids sorting, and batches bound per-directory memory.
		entries, err := dir.ReadDir(256)
		if err != nil && err != io.EOF {
			return 0, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			if entry.IsDir() && strings.HasSuffix(entry.Name(), treeSuffix) {
				// Skip snapshots at any depth, including our own destination.
				continue
			}
			src, dst := filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())
			size, err := cloneEntry(ctx, src, dst, entry)
			if err != nil {
				return 0, err
			}
			logical += size
		}
		if err == io.EOF {
			return logical, nil
		}
	}
}

func cloneEntry(ctx context.Context, source, destination string, entry fs.DirEntry) (uint64, error) {
	if entry.Type()&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			return 0, err
		}
		return 0, os.Symlink(target, destination)
	}
	info, err := entry.Info()
	if err != nil {
		return 0, err
	}
	var logical uint64
	switch {
	case info.IsDir():
		if err := os.Mkdir(destination, 0700); err != nil {
			return 0, err
		}
		logical, err = cloneDirectory(ctx, source, destination)
		if err != nil {
			return 0, err
		}
	case info.Mode().IsRegular():
		if err := cloneFile(source, destination); err != nil {
			return 0, fmt.Errorf("%s: %w", source, err)
		}
		logical = uint64(info.Size())
		if clonePreservesMetadata {
			// APFS already preserves times and permissions, except set-ID bits.
			if info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
				return logical, os.Chmod(destination, info.Mode())
			}
			return logical, nil
		}
	default:
		return 0, fmt.Errorf("unsupported file type: %s", source)
	}
	return logical, preserveMetadata(destination, info)
}

func preserveMetadata(path string, info fs.FileInfo) error {
	if err := os.Chmod(path, info.Mode()); err != nil {
		return err
	}
	return os.Chtimes(path, info.ModTime(), info.ModTime())
}

func deleteTree(root, id string) error {
	if !validID.MatchString(id) {
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
	// Source directories may be read-only; make our own directories removable.
	if err := filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return os.Chmod(path, info.Mode()|0700)
		}
		return nil
	}); err != nil {
		return err
	}
	return os.RemoveAll(path)
}
