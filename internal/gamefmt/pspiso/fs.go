// SPDX-License-Identifier: GPL-3.0-or-later

package pspiso

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// PayloadFS returns a read-only filesystem backed by the open image. Paths use
// fs.FS syntax without a leading slash. Files opened from it remain dependent
// on the image and must be consumed before Close.
func (i *Image) PayloadFS() fs.FS {
	return imageFS{image: i}
}

type imageFS struct {
	image *Image
}

func (f imageFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if f.image == nil || f.image.file == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrClosed}
	}
	wanted := "/"
	if name != "." {
		wanted += name
	}
	for _, entry := range f.image.manifest.Entries {
		if entry.Path != wanted {
			continue
		}
		file := &imageFile{entry: entry}
		if !entry.IsDir {
			file.reader = io.NewSectionReader(f.image.file, int64(entry.LBA)*SectorSize, int64(entry.Size))
		}
		return file, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

type imageFile struct {
	entry  Entry
	reader *io.SectionReader
}

func (f *imageFile) Read(data []byte) (int, error) {
	if f.entry.IsDir {
		return 0, &fs.PathError{Op: "read", Path: f.entry.Path, Err: fmt.Errorf("is a directory")}
	}
	return f.reader.Read(data)
}

func (f *imageFile) Close() error { return nil }
func (f *imageFile) Stat() (fs.FileInfo, error) {
	return imageFileInfo{entry: f.entry}, nil
}

type imageFileInfo struct {
	entry Entry
}

func (i imageFileInfo) Name() string {
	if i.entry.Path == "/" {
		return "."
	}
	return path.Base(strings.TrimSuffix(i.entry.Path, "/"))
}
func (i imageFileInfo) Size() int64 { return int64(i.entry.Size) }
func (i imageFileInfo) Mode() fs.FileMode {
	if i.entry.IsDir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i imageFileInfo) ModTime() time.Time { return time.Time{} }
func (i imageFileInfo) IsDir() bool        { return i.entry.IsDir }
func (i imageFileInfo) Sys() any           { return nil }
