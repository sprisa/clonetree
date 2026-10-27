package main

import (
	"context"
	"io"
	"io/fs"
	"os"
	"time"
)

type fileMetadata struct {
	name         string
	mode         fs.FileMode
	size         int64
	modified     time.Time
	private      uint64
	privateKnown bool
	flags        uint32
	flagsKnown   bool
}

func metadataFromInfo(info fs.FileInfo) fileMetadata {
	return fileMetadata{name: info.Name(), mode: info.Mode(), size: info.Size(), modified: info.ModTime()}
}

func forEachFile(ctx context.Context, source string, wantPrivate bool, visit func(int, fileMetadata) error) error {
	dir, err := os.Open(source)
	if err != nil {
		return err
	}
	defer dir.Close()
	return readDirectoryMetadata(ctx, dir, wantPrivate, visit)
}

func readDirectoryStandard(ctx context.Context, dir *os.File, wantPrivate bool, visit func(int, fileMetadata) error) error {
	fd := int(dir.Fd())
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		files, err := dir.Readdir(256)
		if err != nil && err != io.EOF {
			return err
		}
		for _, info := range files {
			if err := ctx.Err(); err != nil {
				return err
			}
			entry := metadataFromInfo(info)
			if wantPrivate {
				entry.private, entry.privateKnown = privateSizeAt(fd, entry.name)
			}
			if err := visit(fd, entry); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}
