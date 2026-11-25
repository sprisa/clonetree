package main

import "encoding/binary"

// Linux FIEMAP reports a file's extents and flags those shared with another
// file, as reflinked extents on XFS and Btrfs are. A clone's unique storage is
// the sum of its unshared extents. Layout from linux/fiemap.h.
const (
	fiemapHeaderSize = 32 // struct fiemap, before fm_extents
	fiemapExtentSize = 56 // struct fiemap_extent
	fiemapBatch      = 64 // extents requested per call

	fiemapExtentLast       = 0x00000001
	fiemapExtentDataInline = 0x00000200 // data stored with the metadata
	fiemapExtentDataTail   = 0x00000400 // packed with other files' tails
	fiemapExtentShared     = 0x00002000
)

// fiemapRequest fills buf with a request for up to fiemapBatch extents of the
// whole file from start onward.
func fiemapRequest(buf []byte, start uint64) {
	clear(buf[:fiemapHeaderSize])
	binary.NativeEndian.PutUint64(buf[0:], start)        // fm_start
	binary.NativeEndian.PutUint64(buf[8:], ^uint64(0))   // fm_length: to end of file
	binary.NativeEndian.PutUint32(buf[24:], fiemapBatch) // fm_extent_count
}

// fiemapUnshared sums the lengths of the unshared extents in a FIEMAP reply
// for a file of size bytes. next is the offset to continue from; done is true
// after the file's last extent, or when no extents remain.
//
// Btrfs stores very small files inline with their metadata, and a clone copies
// that data instead of sharing it. FIEMAP reports such an extent as a whole
// block, so inline and packed-tail extents count only the file's bytes.
func fiemapUnshared(buf []byte, size uint64) (unshared, next uint64, done bool) {
	mapped := int(binary.NativeEndian.Uint32(buf[20:])) // fm_mapped_extents
	if mapped == 0 {
		return 0, 0, true
	}
	mapped = min(mapped, (len(buf)-fiemapHeaderSize)/fiemapExtentSize)
	for i := range mapped {
		extent := buf[fiemapHeaderSize+i*fiemapExtentSize:]
		logical := binary.NativeEndian.Uint64(extent[0:])
		length := binary.NativeEndian.Uint64(extent[16:])
		flags := binary.NativeEndian.Uint32(extent[40:])
		if flags&fiemapExtentShared == 0 {
			if flags&(fiemapExtentDataInline|fiemapExtentDataTail) != 0 {
				length = min(length, size-min(logical, size))
			}
			unshared += length
		}
		next = logical + length
		done = flags&fiemapExtentLast != 0
	}
	return unshared, next, done
}
