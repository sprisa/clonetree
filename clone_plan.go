package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxUnsafeClonePaths = 4096
const maxPendingCloneExclusions = 4096

var errClonePlanFull = errors.New("native clone eligibility limit reached")

// Only unsafe directory paths are retained, with a fixed upper bound. Ordinary
// trees need no per-directory records; traversal buffers grow only with depth.
type clonePlan struct {
	entries  uint64
	unsafe   map[string]struct{}
	disabled bool
}

type cloneTotals struct {
	entries uint64
	logical uint64
}

type cloneExclusion struct {
	path   string
	parent string
	info   fileMetadata
}

func ordinaryCloneEntry(info fileMetadata) bool {
	return info.flagsKnown && info.flags == 0 && info.mode&(os.ModeSetuid|os.ModeSetgid) == 0
}

func (plan *clonePlan) allowsNative(path string, info fileMetadata) bool {
	if plan == nil || plan.disabled || !ordinaryCloneEntry(info) {
		return false
	}
	_, unsafe := plan.unsafe[filepath.Clean(path)]
	return !unsafe
}

func planCloneSource(ctx context.Context, root string, limit int) (*clonePlan, error) {
	root = filepath.Clean(root)
	plan := &clonePlan{unsafe: make(map[string]struct{})}
	markUnsafe := func(path string) error {
		for {
			if _, exists := plan.unsafe[path]; exists {
				return nil // Its ancestors were already marked too.
			}
			if len(plan.unsafe) >= limit {
				return errClonePlanFull
			}
			plan.unsafe[path] = struct{}{}
			if path == root {
				return nil
			}
			path = filepath.Dir(path)
		}
	}
	var scan func(string) error
	scan = func(path string) error {
		return forEachFile(ctx, path, false, func(_ int, child fileMetadata) error {
			if child.mode.IsDir() && strings.HasSuffix(child.name, treeSuffix) {
				return markUnsafe(path)
			}
			plan.entries++
			if !ordinaryCloneEntry(child) {
				unsafe := path
				if child.mode.IsDir() {
					unsafe = filepath.Join(path, child.name)
				}
				if err := markUnsafe(unsafe); err != nil {
					return err
				}
			}
			switch {
			case child.mode.IsRegular(), child.mode&os.ModeSymlink != 0:
				return nil
			case child.mode.IsDir():
				return scan(filepath.Join(path, child.name))
			default:
				return fmt.Errorf("unsupported file type: %s", filepath.Join(path, child.name))
			}
		})
	}
	if err := scan(root); err != nil {
		if errors.Is(err, errClonePlanFull) {
			return nil, nil // Bound memory by using the per-entry path instead.
		}
		return nil, err
	}
	return plan, nil
}

func inspectDirectoryClone(ctx context.Context, source, destination string, original fileMetadata) (cloneTotals, error) {
	var actual cloneTotals
	var exclusions []cloneExclusion
	root, err := os.Lstat(destination)
	if err != nil || !root.IsDir() {
		if err == nil {
			err = fmt.Errorf("source directory changed type while cloning %s", destination)
		}
		return actual, err
	}
	if source == "" {
		original = metadataFromInfo(root)
	}
	var scan func(string, string, fileMetadata, fileMetadata) error
	scan = func(src, dst string, info, wanted fileMetadata) error {
		err := forEachFile(ctx, dst, false, func(_ int, child fileMetadata) error {
			if child.mode.IsDir() && strings.HasSuffix(child.name, treeSuffix) {
				if len(exclusions) >= maxPendingCloneExclusions {
					return fmt.Errorf("source changed while cloning: too many excluded directories in %s", source)
				}
				exclusions = append(exclusions, cloneExclusion{filepath.Join(dst, child.name), dst, wanted})
				return nil
			}
			actual.entries++
			switch {
			case child.mode.IsRegular():
				actual.logical += uint64(child.size)
			case child.mode&os.ModeSymlink != 0:
			case child.mode.IsDir():
				childSource := ""
				childOriginal := child
				if src != "" {
					childSource = filepath.Join(src, child.name)
					stat, err := os.Lstat(childSource)
					if err != nil {
						return err
					}
					if !stat.IsDir() {
						return fmt.Errorf("source directory changed type while cloning %s", childSource)
					}
					childOriginal = metadataFromInfo(stat)
				}
				return scan(childSource, filepath.Join(dst, child.name), child, childOriginal)
			default:
				return fmt.Errorf("unsupported file type: %s", filepath.Join(dst, child.name))
			}
			return nil
		})
		if err != nil || src == "" {
			return err
		}
		if info.mode != wanted.mode {
			if err := os.Chmod(dst, wanted.mode); err != nil {
				return err
			}
		}
		if !info.modified.Equal(wanted.modified) {
			return os.Chtimes(dst, wanted.modified, wanted.modified)
		}
		return nil
	}
	if err := scan(source, destination, metadataFromInfo(root), original); err != nil {
		return actual, err
	}
	// Deletions wait until enumeration has finished so cursors remain valid.
	parents := make(map[string]fileMetadata)
	for _, exclusion := range exclusions {
		parents[exclusion.parent] = exclusion.info
	}
	for parent, info := range parents {
		if info.mode.Perm()&0700 != 0700 {
			if err := os.Chmod(parent, info.mode|0700); err != nil {
				return actual, err
			}
		}
	}
	for _, exclusion := range exclusions {
		if err := ctx.Err(); err != nil {
			return actual, err
		}
		if err := removeTree(exclusion.path); err != nil {
			return actual, err
		}
	}
	for parent, info := range parents {
		if err := preserveMetadata(parent, info); err != nil {
			return actual, err
		}
	}
	return actual, ctx.Err()
}
