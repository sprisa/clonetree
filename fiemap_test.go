package main

import (
	"encoding/binary"
	"testing"
)

// fiemapReply builds a FIEMAP reply with the given extents.
func fiemapReply(extents ...[3]uint64) []byte {
	buf := make([]byte, fiemapHeaderSize+fiemapBatch*fiemapExtentSize)
	binary.NativeEndian.PutUint32(buf[20:], uint32(len(extents)))
	for i, e := range extents { // logical, length, flags
		extent := buf[fiemapHeaderSize+i*fiemapExtentSize:]
		binary.NativeEndian.PutUint64(extent[0:], e[0])
		binary.NativeEndian.PutUint64(extent[16:], e[1])
		binary.NativeEndian.PutUint32(extent[40:], uint32(e[2]))
	}
	return buf
}

func TestFiemapUnshared(t *testing.T) {
	// Shared extents are left out; unshared ones (including delayed or
	// unwritten data) count.
	unshared, next, done := fiemapUnshared(fiemapReply(
		[3]uint64{0, 4096, fiemapExtentShared},
		[3]uint64{4096, 8192, 0},
		[3]uint64{12288, 4096, 0x4}, // delalloc
		[3]uint64{16384, 4096, fiemapExtentShared | fiemapExtentLast},
	), 20480)
	if unshared != 12288 || next != 20480 || !done {
		t.Fatalf("unshared %d, next %d, done %v", unshared, next, done)
	}
	// A full batch without the last flag continues from the last extent's end.
	extents := make([][3]uint64, fiemapBatch)
	for i := range extents {
		extents[i] = [3]uint64{uint64(i) * 4096, 4096, 0}
	}
	unshared, next, done = fiemapUnshared(fiemapReply(extents...), fiemapBatch*4096)
	if unshared != fiemapBatch*4096 || next != fiemapBatch*4096 || done {
		t.Fatalf("batch: unshared %d, next %d, done %v", unshared, next, done)
	}
	// An empty reply (an empty or sparse file) is done.
	if unshared, _, done := fiemapUnshared(fiemapReply(), 0); unshared != 0 || !done {
		t.Fatalf("empty: %d, %v", unshared, done)
	}
	// A count beyond the buffer is clamped rather than read past the end.
	buf := fiemapReply([3]uint64{0, 4096, fiemapExtentLast})
	binary.NativeEndian.PutUint32(buf[20:], 1<<20)
	if unshared, _, _ := fiemapUnshared(buf, 4096); unshared != 4096 {
		t.Fatalf("clamped: %d", unshared)
	}
	// Btrfs inline data and packed tails count the file's bytes, not a block.
	if unshared, _, _ := fiemapUnshared(fiemapReply([3]uint64{0, 4096, fiemapExtentDataInline | fiemapExtentLast}), 3); unshared != 3 {
		t.Fatalf("inline: %d", unshared)
	}
	if unshared, _, _ := fiemapUnshared(fiemapReply(
		[3]uint64{0, 8192, 0},
		[3]uint64{8192, 4096, fiemapExtentDataTail | fiemapExtentLast},
	), 8200); unshared != 8200 {
		t.Fatalf("tail: %d", unshared)
	}
	// Requests ask for the whole file from the offset, in fixed batches.
	request := make([]byte, fiemapHeaderSize)
	fiemapRequest(request, 8192)
	if binary.NativeEndian.Uint64(request[0:]) != 8192 || binary.NativeEndian.Uint64(request[8:]) != ^uint64(0) ||
		binary.NativeEndian.Uint32(request[24:]) != fiemapBatch {
		t.Fatalf("request header: %x", request)
	}
}
