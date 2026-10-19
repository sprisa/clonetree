package main

import (
	"context"
	"os"
	"path/filepath"
)

type sizes struct {
	Logical uint64
	Private *uint64
}

func treeSize(ctx context.Context, root string) (sizes, error) {
	var result sizes
	if err := ctx.Err(); err != nil {
		return result, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return result, err
	}
	if info.Mode().IsRegular() {
		result.Logical = uint64(info.Size())
	}
	private, known := privateSize(root)
	var scan func(string) error
	scan = func(path string) error {
		return forEachFile(ctx, path, known, func(_ int, child fileMetadata) error {
			if child.mode.IsRegular() {
				result.Logical += uint64(child.size)
			}
			if known {
				known = child.privateKnown
				private += child.private
			}
			if child.mode.IsDir() {
				return scan(filepath.Join(path, child.name))
			}
			return nil
		})
	}
	if info.IsDir() {
		err = scan(root)
	}
	if known && err == nil {
		result.Private = &private
	}
	return result, err
}
