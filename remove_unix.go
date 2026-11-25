//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

// removeDir is a directory being removed. pending counts the work left before
// it can be removed itself: its own scan plus each child directory.
type removeDir struct {
	parent  *removeDir
	path    string
	pending atomic.Int64
}

// parallelRemover deletes a tree with a bounded set of workers. Workers take
// directories, unlink their files, and queue their subdirectories; each
// directory is removed as soon as everything inside it is gone.
type parallelRemover struct {
	mu      sync.Mutex
	wake    *sync.Cond
	stack   []*removeDir
	active  int // directories queued or being scanned
	err     error
	removed *atomic.Uint64
}

// removeTreeParallel removes path and everything under it. removed, if not
// nil, counts entries as they are deleted. Directories whose copied
// permissions block removal are made writable first.
func removeTreeParallel(ctx context.Context, path string, workers int, removed *atomic.Uint64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.Remove(path)
	}
	if removed == nil {
		removed = new(atomic.Uint64)
	}
	r := &parallelRemover{removed: removed}
	r.wake = sync.NewCond(&r.mu)
	root := &removeDir{path: path}
	root.pending.Store(1)
	r.stack, r.active = []*removeDir{root}, 1
	var wg sync.WaitGroup
	for range max(workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.work(ctx)
		}()
	}
	wg.Wait()
	return r.err
}

func (r *parallelRemover) work(ctx context.Context) {
	var children []*removeDir
	for {
		dir := r.take()
		if dir == nil {
			return
		}
		children = children[:0]
		err := ctx.Err()
		if err == nil {
			err = r.scan(dir, &children)
		}
		r.mu.Lock()
		if err != nil {
			if r.err == nil {
				r.err = err
			}
			r.wake.Broadcast()
			r.mu.Unlock()
			return
		}
		r.stack = append(r.stack, children...)
		r.active += len(children) - 1
		if len(children) > 0 || r.active == 0 {
			r.wake.Broadcast()
		}
		r.mu.Unlock()
		// The scan itself is done; remove the directory if nothing is left.
		if err := r.release(dir); err != nil {
			r.fail(err)
			return
		}
	}
}

func (r *parallelRemover) take() *removeDir {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.err == nil && r.active > 0 {
		if last := len(r.stack) - 1; last >= 0 {
			dir := r.stack[last]
			r.stack[last] = nil
			r.stack = r.stack[:last]
			return dir
		}
		r.wake.Wait()
	}
	return nil
}

func (r *parallelRemover) fail(err error) {
	r.mu.Lock()
	if r.err == nil {
		r.err = err
	}
	r.wake.Broadcast()
	r.mu.Unlock()
}

// scan unlinks the files in dir and collects its subdirectories.
func (r *parallelRemover) scan(dir *removeDir, children *[]*removeDir) error {
	f, err := openForRemoval(dir.path)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())
	writable := false
	for {
		entries, readErr := f.ReadDir(256)
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		for _, entry := range entries {
			if entry.IsDir() {
				child := &removeDir{parent: dir, path: filepath.Join(dir.path, entry.Name())}
				child.pending.Store(1)
				dir.pending.Add(1)
				*children = append(*children, child)
				continue
			}
			err := unix.Unlinkat(fd, entry.Name(), 0)
			if errors.Is(err, unix.EACCES) && !writable {
				// Copied directory permissions can be read-only.
				writable = true
				if err = makeWritable(f); err == nil {
					err = unix.Unlinkat(fd, entry.Name(), 0)
				}
			}
			if err != nil && !errors.Is(err, unix.ENOENT) {
				return &os.PathError{Op: "unlink", Path: filepath.Join(dir.path, entry.Name()), Err: err}
			}
			r.removed.Add(1)
		}
		if readErr == io.EOF {
			return nil
		}
	}
}

// release marks one piece of dir's work done, removing dir and then any
// parents that become empty.
func (r *parallelRemover) release(dir *removeDir) error {
	for dir != nil && dir.pending.Add(-1) == 0 {
		err := unix.Rmdir(dir.path)
		if errors.Is(err, unix.EACCES) && dir.parent != nil {
			if err = chmodWritable(dir.parent.path); err == nil {
				err = unix.Rmdir(dir.path)
			}
		}
		if err != nil && !errors.Is(err, unix.ENOENT) {
			return &os.PathError{Op: "rmdir", Path: dir.path, Err: err}
		}
		r.removed.Add(1)
		dir = dir.parent
	}
	return nil
}

// openForRemoval opens a directory for listing, making it readable first if
// its copied permissions do not allow that.
func openForRemoval(path string) (*os.File, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrPermission) {
		if err := chmodWritable(path); err != nil {
			return nil, err
		}
		f, err = os.Open(path)
	}
	return f, err
}

func makeWritable(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	return f.Chmod(info.Mode().Perm() | 0700)
}

func chmodWritable(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return os.Chmod(path, info.Mode().Perm()|0700)
}
