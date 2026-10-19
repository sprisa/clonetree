package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBulkMetadataMatchesStat(t *testing.T) {
	root := fixture(t)
	for i := range 900 {
		write(t, filepath.Join(root, fmt.Sprintf("file-%04d", i)), "data")
	}
	for _, name := range []string{".hidden", "café_日本", strings.Repeat("x", 240)} {
		write(t, filepath.Join(root, name), "special name")
	}
	write(t, filepath.Join(root, "directory", "child"), "nested")
	if err := os.Symlink("missing", filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, private := range []bool{false, true} {
		got := make(map[string]fileMetadata)
		if err := forEachFile(context.Background(), root, private, func(_ int, entry fileMetadata) error {
			got[entry.name] = entry
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(entries) {
			t.Fatalf("entry count: %d, want %d", len(got), len(entries))
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				t.Fatal(err)
			}
			actual := got[info.Name()]
			if actual.mode != info.Mode() || !actual.modified.Equal(info.ModTime()) ||
				info.Mode().IsRegular() && actual.size != info.Size() {
				t.Fatalf("metadata mismatch %s: %+v; mode %v size %d time %v", info.Name(), actual, info.Mode(), info.Size(), info.ModTime())
			}
			if private {
				size, known := privateSize(filepath.Join(root, info.Name()))
				if actual.privateKnown != known || known && actual.private != size {
					t.Fatalf("private size mismatch %s: %d/%t, want %d/%t", info.Name(), actual.private, actual.privateKnown, size, known)
				}
			}
		}
	}
}

func directoryRecord() []byte {
	// Darwin's packed ABI: length, returned attrs, error, name reference, type,
	// timespec, access mask, flags, directory mount status, then the name.
	record := make([]byte, 80)
	binary.LittleEndian.PutUint32(record, uint32(len(record)))
	binary.LittleEndian.PutUint32(record[4:], attrReturned|attrError|attrName|attrObjectType|attrModified|attrAccess|attrFlags)
	binary.LittleEndian.PutUint32(record[12:], attrMount)
	binary.LittleEndian.PutUint32(record[28:], 68-28)
	binary.LittleEndian.PutUint32(record[32:], 6)
	binary.LittleEndian.PutUint32(record[36:], 2) // VDIR
	binary.LittleEndian.PutUint64(record[40:], 1700000000)
	binary.LittleEndian.PutUint32(record[56:], 0755)
	copy(record[68:], "entry\x00")
	return record
}

func TestBulkDirectoryLookupConditions(t *testing.T) {
	for _, name := range []string{"ordinary", "mount", "firmlink", "missing-mount-status", "missing-flags", "missing-time"} {
		t.Run(name, func(t *testing.T) {
			record := directoryRecord()
			switch name {
			case "mount":
				binary.LittleEndian.PutUint32(record[64:], 1)
			case "firmlink":
				binary.LittleEndian.PutUint32(record[60:], flagFirmlink)
			case "missing-mount-status":
				binary.LittleEndian.PutUint32(record[12:], 0)
			case "missing-flags":
				binary.LittleEndian.PutUint32(record[4:], binary.LittleEndian.Uint32(record[4:])&^attrFlags)
			case "missing-time":
				binary.LittleEndian.PutUint32(record[4:], binary.LittleEndian.Uint32(record[4:])&^attrModified)
			}
			entry, lookup, err := decodeBulkMetadata(record, false)
			if err != nil || entry.name != "entry" || lookup != (name != "ordinary") {
				t.Fatalf("lookup=%t metadata=%+v error=%v", lookup, entry, err)
			}
		})
	}
}

func TestBulkFallbackStartsWithFreshCursor(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "one"), "one")
	write(t, filepath.Join(root, "two"), "two")
	dir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.Readdir(-1); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool)
	if err := reopenStandardMetadata(context.Background(), dir, false, func(_ int, entry fileMetadata) error {
		got[entry.name] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["one"] || !got["two"] {
		t.Fatalf("fallback reused an exhausted cursor: %v", got)
	}
}

func TestBulkEntryLookupUsesActualMetadata(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "entry", "child"), "data")
	if err := os.Chmod(filepath.Join(root, "entry"), 0711); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	record := directoryRecord()
	binary.LittleEndian.PutUint32(record[64:], 1)
	entry, lookup, err := decodeBulkMetadata(record, false)
	if err != nil || !lookup {
		t.Fatalf("mount lookup: %t, %v", lookup, err)
	}
	actual, err := lookupMetadata(dir, entry.name, true)
	if err != nil || actual.mode.Perm() != 0711 || actual.modified.Equal(entry.modified) {
		t.Fatalf("lookup retained placeholder metadata: %+v, %v", actual, err)
	}
}

func TestBulkRecordErrors(t *testing.T) {
	for _, name := range []string{"short", "bad-offset", "bad-length", "no-terminator", "no-name", "entry-error", "parent-name"} {
		t.Run(name, func(t *testing.T) {
			record := directoryRecord()
			switch name {
			case "short":
				record = record[:20]
			case "bad-offset":
				binary.LittleEndian.PutUint32(record[28:], ^uint32(0))
			case "bad-length":
				binary.LittleEndian.PutUint32(record[32:], 1000)
			case "no-terminator":
				record[73] = 'x'
			case "no-name":
				binary.LittleEndian.PutUint32(record[4:], attrReturned)
			case "entry-error":
				binary.LittleEndian.PutUint32(record[24:], uint32(unix.EACCES))
			case "parent-name":
				binary.LittleEndian.PutUint32(record[32:], 3)
				copy(record[68:], "..\x00")
			}
			_, _, err := decodeBulkMetadata(record, false)
			if err == nil {
				t.Fatal("accepted invalid metadata")
			}
			if name == "no-name" && !errors.Is(err, errBulkUnsupported) {
				t.Fatalf("missing name did not request fallback: %v", err)
			}
			if name == "entry-error" && !errors.Is(err, unix.EACCES) {
				t.Fatalf("lost entry error: %v", err)
			}
		})
	}
}

func FuzzDecodeBulkMetadata(f *testing.F) {
	f.Add(directoryRecord(), false)
	f.Add([]byte{}, true)
	f.Fuzz(func(t *testing.T, record []byte, private bool) {
		decodeBulkMetadata(record, private)
	})
}

func benchmarkMetadataWalk(ctx context.Context, path string, standard, private bool) (uint64, error) {
	dir, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer dir.Close()
	var total uint64
	visit := func(_ int, entry fileMetadata) error {
		total++
		if entry.mode.IsDir() {
			children, err := benchmarkMetadataWalk(ctx, filepath.Join(path, entry.name), standard, private)
			if err != nil {
				return err
			}
			total += children
		}
		return nil
	}
	if standard {
		err = readDirectoryStandard(ctx, dir, private, visit)
	} else {
		err = readDirectoryMetadata(ctx, dir, private, visit)
	}
	return total, err
}

func BenchmarkMetadataReader(b *testing.B) {
	for _, private := range []bool{false, true} {
		for _, standard := range []bool{true, false} {
			name := "Bulk"
			if standard {
				name = "Standard"
			}
			if private {
				name += "Private"
			}
			b.Run(name, func(b *testing.B) {
				root := benchmarkTree(b)
				if _, err := benchmarkMetadataWalk(context.Background(), root, standard, private); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					count, err := benchmarkMetadataWalk(context.Background(), root, standard, private)
					if err != nil || count != 1088 {
						b.Fatalf("count=%d error=%v", count, err)
					}
				}
			})
		}
	}
}
