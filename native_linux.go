package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"unsafe"

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

// privateSize is the storage only path uses: its unshared extents. st_blocks
// and du count shared reflink extents too, so FIEMAP's shared flags are used
// instead. Only regular files hold extents; directories and symlinks count as
// zero.
func privateSize(path string) (uint64, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, false
	}
	return privateSizeOf(unix.AT_FDCWD, metadataFromInfo(info))
}

func privateSizeAt(dirfd int, name string) (uint64, bool) {
	var stat unix.Stat_t
	if err := unix.Fstatat(dirfd, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return 0, false
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return 0, true
	}
	return unsharedExtents(dirfd, name, uint64(stat.Size))
}

func privateSizeOf(dirfd int, entry fileMetadata) (uint64, bool) {
	if !entry.mode.IsRegular() {
		return 0, true
	}
	return unsharedExtents(dirfd, entry.name, uint64(max(entry.size, 0)))
}

// fsIocFiemap is FS_IOC_FIEMAP, _IOWR('f', 11, struct fiemap), which
// golang.org/x/sys/unix does not export. It is the same on amd64 and arm64.
const fsIocFiemap = 0xC020660B

var fiemapBuffers = sync.Pool{New: func() any {
	return new([fiemapHeaderSize + fiemapBatch*fiemapExtentSize]byte)
}}

// unsharedExtents sums the extents of a regular file that no other file
// shares. It reports false where FIEMAP is unsupported or the file cannot be
// opened, so the measurement shows as unavailable rather than wrong.
func unsharedExtents(dirfd int, name string, size uint64) (uint64, bool) {
	fd, err := unix.Openat(dirfd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, false
	}
	defer unix.Close(fd)
	storage := fiemapBuffers.Get().(*[fiemapHeaderSize + fiemapBatch*fiemapExtentSize]byte)
	defer fiemapBuffers.Put(storage)
	buf := storage[:]
	var total, start uint64
	for {
		fiemapRequest(buf, start)
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), fsIocFiemap, uintptr(unsafe.Pointer(&buf[0])))
		if errno == unix.EINTR {
			continue
		}
		if errno != 0 {
			return 0, false
		}
		unshared, next, done := fiemapUnshared(buf, size)
		total += unshared
		if done || next <= start {
			return total, true
		}
		start = next
	}
}
