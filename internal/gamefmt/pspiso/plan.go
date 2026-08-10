// SPDX-License-Identifier: GPL-3.0-or-later

package pspiso

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"sort"
)

const pspFileAlignment = 16

func planModifiedManifest(source Manifest, tree fs.FS) (Manifest, error) {
	planned := source.clone()
	order := make([]int, 0, len(planned.Entries))
	for index, entry := range planned.Entries {
		if !entry.IsDir {
			order = append(order, index)
		}
	}
	sort.SliceStable(order, func(left, right int) bool {
		return planned.Entries[order[left]].LBA < planned.Entries[order[right]].LBA
	})

	var cursor uint64
	var oldLastEnd uint64
	for _, index := range order {
		entry := &planned.Entries[index]
		oldSize := entry.Size
		oldEnd := uint64(entry.LBA) + uint64(sectorsFor(oldSize))
		if oldEnd > oldLastEnd {
			oldLastEnd = oldEnd
		}
		path, err := treePath(entry.Path)
		if err != nil {
			return Manifest{}, err
		}
		file, err := tree.Open(path)
		if err != nil {
			return Manifest{}, fmt.Errorf("open replacement PSP ISO file %q: %w", entry.Path, err)
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		if statErr != nil {
			return Manifest{}, fmt.Errorf("stat replacement PSP ISO file %q: %w", entry.Path, statErr)
		}
		if closeErr != nil {
			return Manifest{}, fmt.Errorf("close replacement PSP ISO file %q: %w", entry.Path, closeErr)
		}
		if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > math.MaxUint32 {
			return Manifest{}, fmt.Errorf("replacement PSP ISO file %q has unsupported size %d", entry.Path, info.Size())
		}
		newSize := uint32(info.Size())
		if oldSize == 0 && newSize != 0 {
			return Manifest{}, fmt.Errorf("zero-length PSP ISO file %q cannot be assigned a new extent", entry.Path)
		}

		start := uint64(entry.LBA)
		if start < cursor {
			start = alignedLike(cursor, uint64(entry.LBA), pspFileAlignment)
		}
		blocks := uint64(sectorsFor(newSize))
		if start > math.MaxUint32 || start+blocks > math.MaxUint32 {
			return Manifest{}, errors.New("modified PSP ISO file layout exceeds ISO 9660 limits")
		}
		entry.LBA = uint32(start)
		entry.Size = newSize
		if newSize != oldSize {
			entry.Padding = make([]byte, int(blocks*SectorSize-uint64(newSize)))
		}
		if err := patchFileRecord(&planned, entry); err != nil {
			return Manifest{}, err
		}
		if blocks != 0 {
			cursor = start + blocks
		}
	}

	trailing := uint64(0)
	if uint64(source.VolumeSpaceSize) > oldLastEnd {
		trailing = uint64(source.VolumeSpaceSize) - oldLastEnd
	}
	volumeSize := uint64(source.VolumeSpaceSize)
	if cursor+trailing > volumeSize {
		volumeSize = cursor + trailing
	}
	if volumeSize > math.MaxUint32 {
		return Manifest{}, errors.New("modified PSP ISO volume exceeds ISO 9660 limits")
	}
	planned.VolumeSpaceSize = uint32(volumeSize)
	planned.SourceSize = int64(volumeSize) * SectorSize
	planned.SourceSHA256 = [32]byte{}
	patchVolumeSize(&planned, planned.VolumeSpaceSize)
	rebuildFillRanges(&planned, source)
	return planned, nil
}

func alignedLike(at, original uint64, alignment uint64) uint64 {
	want := original % alignment
	got := at % alignment
	if got <= want {
		return at + want - got
	}
	return at + alignment - got + want
}

func patchFileRecord(manifest *Manifest, entry *Entry) error {
	var directory *Directory
	for index := range manifest.Directories {
		if manifest.Directories[index].LBA == entry.DirectoryLBA {
			directory = &manifest.Directories[index]
			break
		}
	}
	if directory == nil || uint64(entry.DirectoryOffset)+18 > uint64(len(directory.Data)) {
		return fmt.Errorf("PSP ISO file %q has no writable directory record", entry.Path)
	}
	patchRecordLocation(directory.Data[entry.DirectoryOffset:], entry.LBA, entry.Size)
	if len(entry.Record) < 18 {
		return fmt.Errorf("PSP ISO file %q has a truncated directory record", entry.Path)
	}
	patchRecordLocation(entry.Record, entry.LBA, entry.Size)
	return nil
}

func patchRecordLocation(record []byte, lba, size uint32) {
	binary.LittleEndian.PutUint32(record[2:6], lba)
	binary.BigEndian.PutUint32(record[6:10], lba)
	binary.LittleEndian.PutUint32(record[10:14], size)
	binary.BigEndian.PutUint32(record[14:18], size)
}

func patchVolumeSize(manifest *Manifest, sectors uint32) {
	for index := range manifest.Descriptors {
		descriptor := manifest.Descriptors[index].Data
		if len(descriptor) == SectorSize && descriptor[0] == 1 {
			binary.LittleEndian.PutUint32(descriptor[80:84], sectors)
			binary.BigEndian.PutUint32(descriptor[84:88], sectors)
		}
	}
}

func rebuildFillRanges(manifest *Manifest, source Manifest) {
	occupied := make([]bool, manifest.VolumeSpaceSize)
	mark := func(start, count uint32) {
		for lba := start; lba < start+count; lba++ {
			occupied[lba] = true
		}
	}
	mark(0, 16)
	for _, descriptor := range manifest.Descriptors {
		mark(descriptor.LBA, 1)
	}
	for _, table := range manifest.PathTables {
		mark(table.LBA, sectorsFor(table.Size))
	}
	for _, directory := range manifest.Directories {
		mark(directory.LBA, sectorsFor(directory.Size))
	}
	for _, entry := range manifest.Entries {
		if !entry.IsDir && entry.Size != 0 {
			mark(entry.LBA, sectorsFor(entry.Size))
		}
	}

	fill := make([]byte, manifest.VolumeSpaceSize)
	for _, sourceRange := range source.FillRanges {
		end := min(sourceRange.EndLBA, manifest.VolumeSpaceSize)
		for lba := sourceRange.StartLBA; lba < end; lba++ {
			fill[lba] = sourceRange.Byte
		}
	}
	manifest.FillRanges = nil
	for lba := uint32(0); lba < manifest.VolumeSpaceSize; {
		if occupied[lba] {
			lba++
			continue
		}
		start, value := lba, fill[lba]
		lba++
		for lba < manifest.VolumeSpaceSize && !occupied[lba] && fill[lba] == value {
			lba++
		}
		manifest.FillRanges = append(manifest.FillRanges, FillRange{StartLBA: start, EndLBA: lba, Byte: value})
	}
}
