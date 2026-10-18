package main

import (
	"encoding/binary"
	"unsafe"

	"golang.org/x/sys/unix"
)

const clonePreservesMetadata = true

func cloneFile(source, destination string) error {
	return unix.Clonefile(source, destination, unix.CLONE_NOFOLLOW)
}

func freeSpace(path string) (uint64, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(path, &stat)
	return stat.Bavail * uint64(stat.Bsize), err
}

func privateSize(path string) (uint64, bool) {
	name, err := unix.BytePtrFromString(path)
	if err != nil {
		return 0, false
	}
	// getattrlist packs attributes on 4-byte boundaries. Ask for returned bits
	// too, so an unsupported measurement cannot be mistaken for zero usage.
	const returnedAttrs = 0x80000000 // ATTR_CMN_RETURNED_ATTRS
	const privateAttr = 0x00000008   // ATTR_CMNEXT_PRIVATESIZE
	attrs := struct {
		Count, Reserved               uint16
		Common, Volume, Dir, File, Ex uint32
	}{Count: 5, Common: returnedAttrs, Ex: privateAttr}
	var buffer [32]byte // length + attribute_set_t + off_t
	_, _, errno := unix.Syscall6(unix.SYS_GETATTRLIST,
		uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&attrs)),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)),
		0x21, 0) // FSOPT_NOFOLLOW | FSOPT_ATTR_CMN_EXTENDED
	if errno != 0 || binary.LittleEndian.Uint32(buffer[:4]) < 32 ||
		binary.LittleEndian.Uint32(buffer[20:24])&privateAttr == 0 {
		return 0, false
	}
	return binary.LittleEndian.Uint64(buffer[24:32]), true
}
