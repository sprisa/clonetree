package main

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	return err == nil
}

func terminalWidth(fd int) int {
	size, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil || size.Col == 0 {
		return 80
	}
	return int(size.Col)
}

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

func cloneFileAt(sourceFD, destinationFD int, name string, info fileMetadata) (err error) {
	src, err := unix.Openat(sourceFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(src)
	dst, err := unix.Openat(destinationFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(dst)) }()
	if err := unix.IoctlFileClone(dst, src); err != nil {
		return err
	}
	if err := unix.Fchmod(dst, unixMode(info.mode)); err != nil {
		return err
	}
	ts, err := unix.TimeToTimespec(info.modified)
	if err != nil {
		return err
	}
	times := [2]unix.Timespec{ts, ts}
	return unix.UtimesNanoAt(destinationFD, name, times[:], 0)
}

func readDirectoryMetadata(ctx context.Context, dir *os.File, wantPrivate bool, visit func(int, fileMetadata) error) error {
	return readDirectoryStandard(ctx, dir, wantPrivate, visit)
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

func privateSizeAt(fd int, path string) (uint64, bool) {
	return 0, false
}
