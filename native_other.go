//go:build !darwin && !linux

package main

import "errors"

const clonePreservesMetadata = false

func cloneFile(source, destination string) error {
	return errors.New("native cloning requires macOS/APFS or a reflink-capable Linux filesystem")
}

func freeSpace(path string) (uint64, error) {
	return 0, errors.New("filesystem space measurement unavailable")
}

func privateSize(path string) (uint64, bool) {
	return 0, false
}
