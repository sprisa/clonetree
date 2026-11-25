package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func benchmarkTree(b *testing.B) string {
	b.Helper()
	root := b.TempDir()
	data := make([]byte, 1024)
	for directory := range 32 {
		path := filepath.Join(root, fmt.Sprintf("package-%02d", directory))
		if err := os.Mkdir(path, 0755); err != nil {
			b.Fatal(err)
		}
		for file := range 32 {
			if err := os.WriteFile(filepath.Join(path, fmt.Sprintf("file-%02d", file)), data, 0644); err != nil {
				b.Fatal(err)
			}
		}
		if err := os.Symlink("file-00", filepath.Join(path, "link")); err != nil {
			b.Fatal(err)
		}
	}
	return root
}

func BenchmarkCountEntries(b *testing.B) {
	root := benchmarkTree(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := countEntries(context.Background(), root); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClone(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "Plain"
		if enabled {
			name = "Progress"
		}
		b.Run(name, func(b *testing.B) {
			root, directory := benchmarkTree(b), b.TempDir()
			probe := filepath.Join(directory, "probe")
			if err := cloneFile(filepath.Join(root, "package-00", "file-00"), probe); err != nil {
				b.Skipf("native cloning unavailable: %v", err)
			}
			if err := os.Remove(probe); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				var report *progress
				if enabled {
					report = &progress{}
				}
				if _, err := newTree(context.Background(), root, directory, "copy", report); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				if err := removeTree(filepath.Join(directory, "copy.clonetree")); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}

func BenchmarkTreeSize(b *testing.B) {
	root := benchmarkTree(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := treeSize(context.Background(), root); err != nil {
			b.Fatal(err)
		}
	}
}

var benchmarkFrame liveFrame

func BenchmarkProgressFrame(b *testing.B) {
	p := &progress{started: time.Now()}
	p.setTotal(223000)
	p.entries.Store(123000)
	p.bytes.Store(20 << 30)
	now := p.cloneStarted.Load().Add(30 * time.Second)
	bar := newBar(progressBarWidth)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkFrame = p.frame("Counting entries", now, bar)
	}
}
