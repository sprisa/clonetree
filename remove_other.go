//go:build !darwin && !linux

package main

import (
	"context"
	"os"
	"sync/atomic"
)

// removeTreeParallel falls back to os.RemoveAll where unlinkat is unavailable.
func removeTreeParallel(ctx context.Context, path string, workers int, removed *atomic.Uint64) error {
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}
