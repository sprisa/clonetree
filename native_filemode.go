//go:build darwin || linux

package main

import (
	"io/fs"

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
