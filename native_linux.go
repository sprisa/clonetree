package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

const clonePreservesMetadata = false

func cloneFile(source, destination string) (err error) {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, dst.Close()) }()
	return unix.IoctlFileClone(int(dst.Fd()), int(src.Fd()))
}

func freeSpace(path string) (uint64, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(path, &stat)
	return stat.Bavail * uint64(stat.Bsize), err
}

func privateSize(path string) (uint64, bool) {
	// st_blocks/du include shared reflink extents, so they cannot tell us this.
	return 0, false
}
