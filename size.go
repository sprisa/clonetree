package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type sizes struct {
	Logical uint64
	Private *uint64
}

// sizeJob is the live measurement of one tree. Counters may be read while the
// measurement runs; the result is final once done is closed.
type sizeJob struct {
	root         string
	logical      atomic.Uint64
	private      atomic.Uint64
	privateKnown atomic.Bool
	entries      atomic.Uint64
	started      atomic.Pointer[time.Time]
	finished     atomic.Pointer[time.Time]
	done         chan struct{}

	// Guarded by sizeMeasurement.mu.
	stack   []dirItem
	pending int
}

// dirItem is a directory waiting to be read. Children are opened relative to
// their parent's descriptor, so each open resolves one path component instead of
// walking (and contending on) every ancestor again.
type dirItem struct {
	parent *dirRef
	name   string
	path   string
}

// dirRef keeps a directory open until all of its child directories are opened.
type dirRef struct {
	file *os.File
	refs atomic.Int64
}

func (r *dirRef) release() {
	if r != nil && r.refs.Add(-1) == 0 {
		r.file.Close()
	}
}

func (item dirItem) open() (*os.File, error) {
	if item.parent == nil {
		return os.Open(item.path)
	}
	defer item.parent.release()
	return openDirectoryAt(item.parent.file, item.name, item.path)
}

func (j *sizeJob) sizes() sizes {
	result := sizes{Logical: j.logical.Load()}
	if j.privateKnown.Load() {
		private := j.private.Load()
		result.Private = &private
	}
	return result
}

// sizeMeasurement scans directories from several trees with a shared, bounded
// set of workers. Each tree keeps its own depth-first stack. Workers prefer the
// first unfinished tree and spill over to later ones only when it has no
// directories ready, so results arrive in order, one tree at a time, while the
// total work and the time to measure everything stay the same.
type sizeMeasurement struct {
	jobs    []*sizeJob
	mu      sync.Mutex
	wake    *sync.Cond
	pending int
	err     error
	errJob  *sizeJob
}

// APFS serializes much of getattrlistbulk internally: beyond about six
// concurrent readers, kernel lock contention costs more CPU than it saves in
// wall time (see PERFORMANCE.md). A few readers still overlap storage latency
// on cold caches.
func sizeWorkers() int {
	return min(runtime.GOMAXPROCS(0), 6)
}

func treeSize(ctx context.Context, root string) (sizes, error) {
	return treeSizeWithPrivate(ctx, root, true)
}

func treeSizeWithPrivate(ctx context.Context, root string, wantPrivate bool) (sizes, error) {
	m, err := startSizes(ctx, []string{root}, wantPrivate, sizeWorkers())
	if err != nil {
		return sizes{}, err
	}
	if err := m.wait(); err != nil {
		return sizes{}, err
	}
	return m.jobs[0].sizes(), nil
}

// startSizes begins measuring roots in the background. Call wait for the result.
func startSizes(ctx context.Context, roots []string, wantPrivate bool, workers int) (*sizeMeasurement, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m := &sizeMeasurement{}
	m.wake = sync.NewCond(&m.mu)
	for _, root := range roots {
		job := &sizeJob{root: root, done: make(chan struct{})}
		m.jobs = append(m.jobs, job)
		info, err := os.Lstat(root)
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			job.logical.Store(uint64(info.Size()))
		}
		if wantPrivate {
			private, known := privateSize(root)
			job.private.Store(private)
			job.privateKnown.Store(known)
		}
		if info.IsDir() {
			job.stack = append(job.stack, dirItem{path: root})
			job.pending = 1
			m.pending++
		} else {
			job.finish()
		}
	}
	var workersDone sync.WaitGroup
	for range max(workers, 1) {
		workersDone.Add(1)
		go func() {
			defer workersDone.Done()
			m.work(ctx)
		}()
	}
	go func() {
		workersDone.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.err == nil {
			return
		}
		// Close directories still queued after a failure, and release anyone
		// waiting on an individual tree.
		for _, job := range m.jobs {
			for _, item := range job.stack {
				item.parent.release()
			}
			job.stack = nil
			job.finish()
		}
	}()
	return m, nil
}

func (j *sizeJob) finish() {
	now := time.Now()
	if j.finished.CompareAndSwap(nil, &now) {
		close(j.done)
	}
}

// wait blocks until every tree is measured, returning the first error.
func (m *sizeMeasurement) wait() error {
	for _, job := range m.jobs {
		<-job.done
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

func (m *sizeMeasurement) failed() (*sizeJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.errJob, m.err
}

func (m *sizeMeasurement) work(ctx context.Context) {
	var children []dirItem
	for {
		job, item := m.take()
		if job == nil {
			return
		}
		children = children[:0]
		err := ctx.Err()
		if err == nil {
			err = job.read(ctx, item, &children)
		} else {
			item.parent.release()
		}
		m.mu.Lock()
		if err != nil {
			if m.err == nil {
				m.err, m.errJob = err, job
			}
			m.wake.Broadcast()
			m.mu.Unlock()
			return
		}
		job.stack = append(job.stack, children...)
		job.pending += len(children) - 1
		m.pending += len(children) - 1
		if job.pending == 0 {
			job.finish()
		}
		if len(children) > 0 || m.pending == 0 {
			m.wake.Broadcast()
		}
		m.mu.Unlock()
	}
}

// take returns the next directory to read, or nil when the measurement has
// finished or failed.
func (m *sizeMeasurement) take() (*sizeJob, dirItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for m.err == nil && m.pending > 0 {
		for _, job := range m.jobs {
			if last := len(job.stack) - 1; last >= 0 {
				if job.started.Load() == nil {
					now := time.Now()
					job.started.Store(&now)
				}
				item := job.stack[last]
				job.stack[last] = dirItem{}
				job.stack = job.stack[:last]
				return job, item
			}
		}
		m.wake.Wait()
	}
	return nil, dirItem{}
}

func (j *sizeJob) read(ctx context.Context, item dirItem, children *[]dirItem) error {
	dir, err := item.open()
	if err != nil {
		return err
	}
	wantPrivate := j.privateKnown.Load()
	var logical, private, entries uint64
	known := wantPrivate
	first := len(*children)
	ref := &dirRef{file: dir}
	err = readDirectoryMetadata(ctx, dir, wantPrivate, func(_ int, child fileMetadata) error {
		entries++
		if child.mode.IsRegular() {
			logical += uint64(child.size)
		}
		if known {
			known = child.privateKnown
			private += child.private
		}
		if child.mode.IsDir() {
			*children = append(*children, dirItem{parent: ref, name: child.name, path: filepath.Join(item.path, child.name)})
		}
		return nil
	})
	j.logical.Add(logical)
	j.private.Add(private)
	j.entries.Add(entries)
	if wantPrivate && !known {
		j.privateKnown.Store(false)
	}
	if err != nil {
		*children = (*children)[:first]
		dir.Close()
		return err
	}
	// The reader's own reference keeps the directory open until every child is queued.
	ref.refs.Store(int64(len(*children)-first) + 1)
	ref.release()
	return nil
}
