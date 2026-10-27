package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	attrName       = 0x00000001
	attrObjectType = 0x00000008
	attrModified   = 0x00000400
	attrAccess     = 0x00020000
	attrFlags      = 0x00040000
	attrError      = 0x20000000
	attrReturned   = 0x80000000
	attrMount      = 0x00000004
	attrDataLength = 0x00000200
	attrPrivate    = 0x00000008
	flagFirmlink   = 0x00800000
)

var metadataBuffers = sync.Pool{New: func() any { return new([64 << 10]byte) }}
var errBulkUnsupported = errors.New("required bulk metadata is unavailable")

func readDirectoryMetadata(ctx context.Context, dir *os.File, wantPrivate bool, visit func(int, fileMetadata) error) error {
	storage := metadataBuffers.Get().(*[64 << 10]byte)
	defer metadataBuffers.Put(storage)
	buffer := storage[:]
	attrs := struct {
		Count, Reserved               uint16
		Common, Volume, Dir, File, Ex uint32
	}{
		Count: 5, Common: attrReturned | attrError | attrName | attrObjectType | attrModified | attrAccess | attrFlags,
		Dir: attrMount, File: attrDataLength,
	}
	options := uintptr(8) // FSOPT_PACK_INVAL_ATTRS keeps unsupported fields in place.
	if wantPrivate {
		attrs.Ex = attrPrivate
		options |= 0x20 // FSOPT_ATTR_CMN_EXTENDED
	}
	fd := int(dir.Fd())
	delivered := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, _, errno := unix.Syscall6(unix.SYS_GETATTRLISTBULK, uintptr(fd),
			uintptr(unsafe.Pointer(&attrs)), uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)), options, 0)
		if errno == unix.EINTR {
			continue
		}
		if errno != 0 {
			if !delivered && (errno == unix.ENOTSUP || errno == unix.ENOSYS || errno == unix.EINVAL) {
				return reopenStandardMetadata(ctx, dir, wantPrivate, visit)
			}
			return &os.PathError{Op: "getattrlistbulk", Path: dir.Name(), Err: errno}
		}
		if count == 0 {
			return nil
		}
		if count > uintptr(len(buffer)/36) {
			return errors.New("invalid bulk metadata entry count")
		}
		offset := 0
		for range int(count) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if offset+4 > len(buffer) {
				return errors.New("truncated bulk metadata record")
			}
			length := int(binary.LittleEndian.Uint32(buffer[offset:]))
			if length < 36 || length%8 != 0 || length > len(buffer)-offset {
				return fmt.Errorf("invalid bulk metadata record length %d", length)
			}
			entry, lookup, err := decodeBulkMetadata(buffer[offset:offset+length], wantPrivate)
			offset += length
			if err != nil {
				if !delivered && errors.Is(err, errBulkUnsupported) {
					return reopenStandardMetadata(ctx, dir, wantPrivate, visit)
				}
				return fmt.Errorf("bulk metadata in %s: %w", dir.Name(), err)
			}
			if lookup {
				entry, err = lookupMetadata(dir, entry.name, wantPrivate)
				if err != nil {
					return err
				}
			}
			if err := visit(fd, entry); err != nil {
				return err
			}
			delivered = true
		}
	}
}

func reopenStandardMetadata(ctx context.Context, dir *os.File, wantPrivate bool, visit func(int, fileMetadata) error) error {
	// Darwin does not support mixing readdir and getattrlistbulk on one cursor.
	fresh, err := os.Open(dir.Name())
	if err != nil {
		return err
	}
	defer fresh.Close()
	return readDirectoryStandard(ctx, fresh, wantPrivate, visit)
}

func lookupMetadata(dir *os.File, name string, wantPrivate bool) (fileMetadata, error) {
	info, err := os.Lstat(filepath.Join(dir.Name(), name))
	if err != nil {
		return fileMetadata{}, err
	}
	entry := metadataFromInfo(info)
	if wantPrivate {
		entry.private, entry.privateKnown = privateSizeAt(int(dir.Fd()), name)
	}
	return entry, nil
}

func decodeBulkMetadata(record []byte, wantPrivate bool) (entry fileMetadata, lookup bool, err error) {
	if len(record) < 36 {
		return entry, false, errors.New("truncated bulk metadata header")
	}
	common := binary.LittleEndian.Uint32(record[4:])
	if common&attrError != 0 {
		if code := binary.LittleEndian.Uint32(record[24:]); code != 0 {
			return entry, false, unix.Errno(code)
		}
	}
	if common&attrName == 0 {
		return entry, false, errBulkUnsupported
	}
	nameOffset := 28 + int(int32(binary.LittleEndian.Uint32(record[28:])))
	nameLength := int(binary.LittleEndian.Uint32(record[32:]))
	if nameOffset < 36 || nameOffset > len(record) || nameLength < 2 || nameLength > len(record)-nameOffset || record[nameOffset+nameLength-1] != 0 {
		return entry, false, errors.New("invalid bulk metadata name reference")
	}
	// Own the name so callbacks may retain it independently of the pooled buffer.
	entry.name = string(record[nameOffset : nameOffset+nameLength-1])
	if entry.name == "." || entry.name == ".." || strings.ContainsAny(entry.name, "/\x00") {
		return entry, false, errors.New("invalid bulk metadata entry name")
	}
	required := uint32(attrObjectType | attrModified | attrAccess)
	if common&required != required {
		return entry, true, nil
	}
	if len(record) < 64 || nameOffset < 64 {
		return entry, false, errors.New("truncated bulk metadata attributes")
	}
	kind, access := binary.LittleEndian.Uint32(record[36:]), binary.LittleEndian.Uint32(record[56:])
	entry.mode = metadataMode(kind, access)
	entry.modified = time.Unix(int64(binary.LittleEndian.Uint64(record[40:])), int64(binary.LittleEndian.Uint64(record[48:])))
	entry.flags, entry.flagsKnown = binary.LittleEndian.Uint32(record[60:]), common&attrFlags != 0
	field := 64
	if entry.mode.IsDir() {
		field += 4 // Directory mount status; file attributes are omitted for directories.
	} else {
		field += 8 // File data length; directory attributes are omitted for other entries.
	}
	minimum := field
	if wantPrivate {
		minimum += 8
	}
	if len(record) < minimum || nameOffset < minimum {
		return entry, false, errors.New("truncated bulk metadata payload")
	}
	if entry.mode.IsDir() {
		flags := binary.LittleEndian.Uint32(record[60:])
		directory := binary.LittleEndian.Uint32(record[12:])
		mount := binary.LittleEndian.Uint32(record[64:])
		// Bulk attributes describe the covered mount point/firmlink, not its target.
		lookup = common&attrFlags == 0 || directory&attrMount == 0 || mount&1 != 0 || flags&flagFirmlink != 0
	} else if entry.mode.IsRegular() {
		files := binary.LittleEndian.Uint32(record[16:])
		lookup = files&attrDataLength == 0
		entry.size = int64(binary.LittleEndian.Uint64(record[64:]))
	}
	if wantPrivate && binary.LittleEndian.Uint32(record[20:])&attrPrivate != 0 {
		entry.private, entry.privateKnown = binary.LittleEndian.Uint64(record[field:]), true
	}
	return entry, lookup, nil
}

func metadataMode(kind, access uint32) fs.FileMode {
	mode := fs.FileMode(access & 0777)
	if access&unix.S_ISUID != 0 {
		mode |= fs.ModeSetuid
	}
	if access&unix.S_ISGID != 0 {
		mode |= fs.ModeSetgid
	}
	if access&unix.S_ISVTX != 0 {
		mode |= fs.ModeSticky
	}
	switch kind {
	case 1:
	case 2:
		mode |= fs.ModeDir
	case 3:
		mode |= fs.ModeDevice
	case 4:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case 5:
		mode |= fs.ModeSymlink
	case 6:
		mode |= fs.ModeSocket
	case 7:
		mode |= fs.ModeNamedPipe
	default:
		mode |= fs.ModeIrregular
	}
	return mode
}
