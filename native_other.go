//go:build !darwin && !linux

package main

import (
	"context"
	"errors"
	"os"
)

func isTerminal(fd int) bool {
	return false
}

func terminalWidth(fd int) int {
	return 80
}

func cloneFile(source, destination string) error {
	return errors.New("native cloning requires macOS/APFS or a reflink-capable Linux filesystem")
}

func cloneFileAt(sourceFD, destinationFD int, name string, info fileMetadata) error {
	return errors.New("native cloning requires macOS/APFS or a reflink-capable Linux filesystem")
}

func readDirectoryMetadata(ctx context.Context, dir *os.File, wantPrivate bool, visit func(int, fileMetadata) error) error {
	return readDirectoryStandard(ctx, dir, wantPrivate, visit)
}

func freeSpace(path string) (uint64, error) {
	return 0, errors.New("filesystem space measurement unavailable")
}

func privateSize(path string) (uint64, bool) {
	return 0, false
}

func privateSizeAt(fd int, path string) (uint64, bool) {
	return 0, false
}
