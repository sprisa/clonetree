package main

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
)

type sizes struct {
	Logical uint64
	Private *uint64
}

func treeSize(ctx context.Context, root string) (sizes, error) {
	var result sizes
	var private uint64
	known := true
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			result.Logical += uint64(info.Size())
		}
		if known {
			size, ok := privateSize(path)
			known = ok
			private += size
		}
		return nil
	})
	if known && err == nil {
		result.Private = &private
	}
	return result, err
}

func bytesString(n uint64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	value := float64(n)
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == "EiB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	panic("unreachable")
}
