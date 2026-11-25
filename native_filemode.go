//go:build darwin || linux

package main

import (
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func unixMode(mode fs.FileMode) uint32 {
	bits := uint32(mode.Perm())
	if mode&fs.ModeSetuid != 0 {
		bits |= unix.S_ISUID
	}
	if mode&fs.ModeSetgid != 0 {
		bits |= unix.S_ISGID
	}
	if mode&fs.ModeSticky != 0 {
		bits |= unix.S_ISVTX
	}
	return bits
}

// openDirectoryAt opens name relative to parent. path is used for errors and
// for callers that need to reopen the directory.
func openDirectoryAt(parent *os.File, name, path string) (*os.File, error) {
	for {
		fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, &os.PathError{Op: "open", Path: path, Err: err}
		}
		return os.NewFile(uintptr(fd), path), nil
	}
}
