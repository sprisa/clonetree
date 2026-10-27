package main

import (
	"context"
	"os"

	"golang.org/x/sys/unix"
)

func prepareClonePlan(ctx context.Context, root string) (*clonePlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var volume unix.Statfs_t
	if err := unix.Statfs(root, &volume); err != nil {
		return nil, err
	}
	if unix.ByteSliceToString(volume.Fstypename[:]) != "apfs" {
		return nil, nil
	}
	return planCloneSource(ctx, root, maxUnsafeClonePaths)
}

func cloneDirectoryNative(source, destination string) (bool, error) {
	err := unix.Clonefile(source, destination, unix.CLONE_NOFOLLOW)
	if err == unix.ENOTSUP || err == unix.EOPNOTSUPP || err == unix.ENOSYS || err == unix.EINVAL || err == unix.EISDIR {
		// Directory cloning is not supported by every macOS/filesystem version.
		// A fallback is safe only if the native operation created nothing.
		if _, statErr := os.Lstat(destination); os.IsNotExist(statErr) {
			return false, nil
		}
	}
	return err == nil, err
}
