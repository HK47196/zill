// SPDX-License-Identifier: GPL-3.0-or-later

package pspiso

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const (
	systemAreaSectors = 16
	pvdSector         = 16
)

type inspector struct {
	f        *os.File
	sectors  uint32
	occupied []bool
	manifest Manifest
	seenDirs map[dirKey]bool
}

type dirKey struct {
	lba  uint32
	size uint32
}

func inspect(f *os.File) (Manifest, error) {
	info, err := f.Stat()
	if err != nil {
		return Manifest{}, fmt.Errorf("stat PSP ISO: %w", err)
	}
	if info.Size() <= 0 || info.Size()%SectorSize != 0 {
		return Manifest{}, fmt.Errorf("PSP ISO size %d is not a positive multiple of %d", info.Size(), SectorSize)
	}
	if info.Size()/SectorSize > int64(^uint32(0)) {
		return Manifest{}, fmt.Errorf("PSP ISO is too large")
	}
	hash, err := hashFile(f)
	if err != nil {
		return Manifest{}, err
	}

	in := &inspector{
		f:        f,
		sectors:  uint32(info.Size() / SectorSize),
		occupied: make([]bool, int(info.Size()/SectorSize)),
		manifest: Manifest{
			SectorSize:   SectorSize,
			SourceSize:   info.Size(),
			SourceSHA256: hash,
		},
		seenDirs: make(map[dirKey]bool),
	}
	if in.sectors <= pvdSector {
		return Manifest{}, fmt.Errorf("PSP ISO has no primary volume descriptor")
	}
	if err := in.readSystemArea(); err != nil {
		return Manifest{}, err
	}
	pvd, err := in.readDescriptors()
	if err != nil {
		return Manifest{}, err
	}
	if err := in.readPVD(pvd); err != nil {
		return Manifest{}, err
	}
	if err := in.classifyFill(); err != nil {
		return Manifest{}, err
	}
	return in.manifest, nil
}

func hashFile(f *os.File) ([32]byte, error) {
	var zero [32]byte
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return zero, fmt.Errorf("seek PSP ISO: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return zero, fmt.Errorf("hash PSP ISO: %w", err)
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

func (in *inspector) readSystemArea() error {
	b, err := in.readSectors(0, systemAreaSectors)
	if err != nil {
		return err
	}
	in.manifest.SystemArea = b
	return in.mark(0, systemAreaSectors, "system area")
}

func (in *inspector) readDescriptors() ([]byte, error) {
	var pvd []byte
	for lba := uint32(pvdSector); ; lba++ {
		if lba >= in.sectors {
			return nil, fmt.Errorf("ISO descriptor sequence has no terminator")
		}
		sector, err := in.readSectors(lba, 1)
		if err != nil {
			return nil, err
		}
		if string(sector[1:6]) != "CD001" || sector[6] != 1 {
			return nil, fmt.Errorf("invalid ISO volume descriptor at LBA %d", lba)
		}
		if err := in.mark(lba, 1, "volume descriptor"); err != nil {
			return nil, err
		}
		in.manifest.Descriptors = append(in.manifest.Descriptors, Descriptor{LBA: lba, Data: sector})
		switch sector[0] {
		case 1:
			if pvd != nil {
				return nil, fmt.Errorf("multiple primary volume descriptors")
			}
			pvd = sector
		case 255:
			if pvd == nil {
				return nil, fmt.Errorf("ISO has no primary volume descriptor")
			}
			return pvd, nil
		}
	}
}

func (in *inspector) readPVD(pvd []byte) error {
	if _, err := dual16(pvd[120:124], "volume set size"); err != nil {
		return err
	}
	if _, err := dual16(pvd[124:128], "volume sequence number"); err != nil {
		return err
	}
	volumeSpace, err := dual32(pvd[80:88], "volume space size")
	if err != nil {
		return err
	}
	if volumeSpace != in.sectors {
		return fmt.Errorf("PVD volume space size %d does not match image size %d sectors", volumeSpace, in.sectors)
	}
	in.manifest.VolumeSpaceSize = volumeSpace
	blockSize, err := dual16(pvd[128:132], "logical block size")
	if err != nil {
		return err
	}
	if blockSize != SectorSize {
		return fmt.Errorf("ISO logical block size %d, want %d", blockSize, SectorSize)
	}
	// ECMA-119 specifies version 1, while the supported retail PSP image uses
	// the otherwise structurally identical authored value 2.
	if pvd[881] != 1 && pvd[881] != 2 {
		return fmt.Errorf("unsupported ISO file structure version %d", pvd[881])
	}

	pathTableSize, err := dual32(pvd[132:140], "path table size")
	if err != nil {
		return err
	}
	if pathTableSize == 0 {
		return fmt.Errorf("ISO path table is empty")
	}
	locations := []struct {
		lba       uint32
		bigEndian bool
	}{
		{binary.LittleEndian.Uint32(pvd[140:144]), false},
		{binary.LittleEndian.Uint32(pvd[144:148]), false},
		{binary.BigEndian.Uint32(pvd[148:152]), true},
		{binary.BigEndian.Uint32(pvd[152:156]), true},
	}
	for _, location := range locations {
		if location.lba == 0 {
			continue
		}
		blocks := blocksFor(pathTableSize)
		data, err := in.readSectors(location.lba, blocks)
		if err != nil {
			return fmt.Errorf("read path table at LBA %d: %w", location.lba, err)
		}
		if err := validatePathTable(data[:pathTableSize], location.bigEndian); err != nil {
			return fmt.Errorf("path table at LBA %d: %w", location.lba, err)
		}
		if err := in.mark(location.lba, blocks, "path table"); err != nil {
			return err
		}
		in.manifest.PathTables = append(in.manifest.PathTables, PathTable{
			LBA: location.lba, Size: pathTableSize, BigEndian: location.bigEndian, Data: data,
		})
	}
	if len(in.manifest.PathTables) < 2 {
		return fmt.Errorf("ISO has fewer than two path table copies")
	}

	root, err := parseRecord(pvd[156:])
	if err != nil {
		return fmt.Errorf("root directory record: %w", err)
	}
	if !root.isDir || !isDot(root.identifier) {
		return fmt.Errorf("invalid root directory record")
	}
	if err := in.visitDirectory("/", root, pvd[156:156+root.length]); err != nil {
		return err
	}
	if !hasPSPGame(in.manifest.Entries) {
		return fmt.Errorf("ISO is not a PSP game image: missing /PSP_GAME directory")
	}
	return nil
}

func (in *inspector) visitDirectory(dirPath string, record isoRecord, rawRecord []byte) error {
	key := dirKey{record.lba, record.size}
	if in.seenDirs[key] {
		return fmt.Errorf("directory %q reuses extent LBA %d size %d", dirPath, record.lba, record.size)
	}
	in.seenDirs[key] = true
	blocks := blocksFor(record.size)
	if blocks == 0 {
		return fmt.Errorf("directory %q has zero length", dirPath)
	}
	data, err := in.readSectors(record.lba, blocks)
	if err != nil {
		return fmt.Errorf("read directory %q: %w", dirPath, err)
	}
	if err := in.mark(record.lba, blocks, "directory"); err != nil {
		return err
	}
	in.manifest.Directories = append(in.manifest.Directories, Directory{Path: dirPath, LBA: record.lba, Size: record.size, Data: data})
	in.manifest.Entries = append(in.manifest.Entries, entryFromRecord(dirPath, record, rawRecord))

	for offset := uint32(0); offset < record.size; {
		sectorOffset := offset % SectorSize
		if data[offset] == 0 {
			offset += SectorSize - sectorOffset
			continue
		}
		recordOffset := offset
		parsed, err := parseRecord(data[offset:record.size])
		if err != nil {
			return fmt.Errorf("directory %q offset %d: %w", dirPath, offset, err)
		}
		if uint32(parsed.length) > SectorSize-sectorOffset {
			return fmt.Errorf("directory %q offset %d: record crosses sector boundary", dirPath, offset)
		}
		raw := data[offset : offset+uint32(parsed.length)]
		offset += uint32(parsed.length)
		if isDot(parsed.identifier) || isDotDot(parsed.identifier) {
			continue
		}
		name := normalizedIdentifier(parsed.identifier)
		if name == "" || strings.Contains(name, "/") || strings.ContainsRune(name, 0) {
			return fmt.Errorf("directory %q contains unsafe ISO identifier %q", dirPath, parsed.identifier)
		}
		entryPath := path.Join(dirPath, name)
		if parsed.isDir {
			if err := in.visitDirectory(entryPath, parsed, raw); err != nil {
				return err
			}
			continue
		}
		if parsed.multiExtent || parsed.fileUnitSize != 0 || parsed.interleaveGap != 0 || parsed.xarLength != 0 {
			return fmt.Errorf("file %q uses unsupported ISO extent features", entryPath)
		}
		blocks := blocksFor(parsed.size)
		if blocks != 0 {
			if err := in.mark(parsed.lba, blocks, "file payload"); err != nil {
				return fmt.Errorf("file %q: %w", entryPath, err)
			}
		}
		entry := entryFromRecord(entryPath, parsed, raw)
		entry.DirectoryLBA = record.lba
		entry.DirectoryOffset = recordOffset
		if tail := parsed.size % SectorSize; tail != 0 {
			last, err := in.readSectors(parsed.lba+blocks-1, 1)
			if err != nil {
				return err
			}
			entry.Padding = append([]byte(nil), last[tail:]...)
		}
		in.manifest.Entries = append(in.manifest.Entries, entry)
	}
	return nil
}

func (in *inspector) classifyFill() error {
	var start uint32
	for lba := uint32(0); lba < in.sectors; {
		if in.occupied[lba] {
			lba++
			continue
		}
		sector, err := in.readSectors(lba, 1)
		if err != nil {
			return err
		}
		fill := sector[0]
		if !allByte(sector, fill) {
			return fmt.Errorf("unclassified non-uniform sector at LBA %d", lba)
		}
		start = lba
		lba++
		for lba < in.sectors && !in.occupied[lba] {
			next, err := in.readSectors(lba, 1)
			if err != nil {
				return err
			}
			if !allByte(next, fill) {
				break
			}
			lba++
		}
		in.manifest.FillRanges = append(in.manifest.FillRanges, FillRange{StartLBA: start, EndLBA: lba, Byte: fill})
	}
	return nil
}

func (in *inspector) readSectors(lba, count uint32) ([]byte, error) {
	if count == 0 || lba > in.sectors || count > in.sectors-lba {
		return nil, fmt.Errorf("sector range LBA %d count %d is outside image", lba, count)
	}
	b := make([]byte, int(count)*SectorSize)
	if _, err := in.f.ReadAt(b, int64(lba)*SectorSize); err != nil {
		return nil, fmt.Errorf("read sectors LBA %d count %d: %w", lba, count, err)
	}
	return b, nil
}

func (in *inspector) mark(lba, count uint32, kind string) error {
	if count == 0 || lba > in.sectors || count > in.sectors-lba {
		return fmt.Errorf("%s range LBA %d count %d is outside image", kind, lba, count)
	}
	for n := lba; n < lba+count; n++ {
		if in.occupied[n] {
			return fmt.Errorf("%s overlaps an existing extent at LBA %d", kind, n)
		}
		in.occupied[n] = true
	}
	return nil
}

type isoRecord struct {
	length        byte
	xarLength     byte
	lba           uint32
	size          uint32
	flags         byte
	fileUnitSize  byte
	interleaveGap byte
	identifier    []byte
	isDir         bool
	multiExtent   bool
}

func parseRecord(data []byte) (isoRecord, error) {
	if len(data) == 0 || data[0] == 0 {
		return isoRecord{}, fmt.Errorf("zero-length directory record")
	}
	n := int(data[0])
	if n < 34 || n > len(data) {
		return isoRecord{}, fmt.Errorf("invalid directory record length %d", n)
	}
	r := isoRecord{length: data[0], xarLength: data[1], flags: data[25], fileUnitSize: data[26], interleaveGap: data[27]}
	var err error
	if r.lba, err = dual32(data[2:10], "extent LBA"); err != nil {
		return isoRecord{}, err
	}
	if r.size, err = dual32(data[10:18], "data length"); err != nil {
		return isoRecord{}, err
	}
	if _, err := dual16(data[28:32], "volume sequence number"); err != nil {
		return isoRecord{}, err
	}
	idLen := int(data[32])
	if idLen == 0 || 33+idLen > n {
		return isoRecord{}, fmt.Errorf("invalid file identifier length %d", idLen)
	}
	r.identifier = append([]byte(nil), data[33:33+idLen]...)
	r.isDir = r.flags&0x02 != 0
	r.multiExtent = r.flags&0x80 != 0
	if r.multiExtent {
		return isoRecord{}, fmt.Errorf("multi-extent directory record")
	}
	if r.isDir && (r.xarLength != 0 || r.fileUnitSize != 0 || r.interleaveGap != 0) {
		return isoRecord{}, fmt.Errorf("directory uses unsupported ISO extent features")
	}
	return r, nil
}

func entryFromRecord(p string, r isoRecord, raw []byte) Entry {
	return Entry{Path: p, Identifier: append([]byte(nil), r.identifier...), LBA: r.lba, Size: r.size, IsDir: r.isDir, Flags: r.flags, Record: append([]byte(nil), raw...)}
}

func validatePathTable(data []byte, bigEndian bool) error {
	entryNumber := 0
	for offset := 0; offset < len(data); {
		if len(data)-offset < 8 {
			return fmt.Errorf("truncated entry at byte %d", offset)
		}
		nameLen := int(data[offset])
		if nameLen == 0 || offset+8+nameLen > len(data) {
			return fmt.Errorf("invalid entry at byte %d", offset)
		}
		var parent uint16
		if bigEndian {
			parent = binary.BigEndian.Uint16(data[offset+6 : offset+8])
		} else {
			parent = binary.LittleEndian.Uint16(data[offset+6 : offset+8])
		}
		entryNumber++
		if parent == 0 || int(parent) > entryNumber {
			return fmt.Errorf("invalid parent directory number %d", parent)
		}
		offset += 8 + nameLen
		if nameLen%2 != 0 {
			offset++
		}
	}
	return nil
}

func dual32(b []byte, field string) (uint32, error) {
	if len(b) != 8 {
		return 0, fmt.Errorf("invalid %s field length", field)
	}
	le, be := binary.LittleEndian.Uint32(b[:4]), binary.BigEndian.Uint32(b[4:])
	if le != be {
		return 0, fmt.Errorf("%s endian values disagree: %d != %d", field, le, be)
	}
	return le, nil
}

func dual16(b []byte, field string) (uint16, error) {
	if len(b) != 4 {
		return 0, fmt.Errorf("invalid %s field length", field)
	}
	le, be := binary.LittleEndian.Uint16(b[:2]), binary.BigEndian.Uint16(b[2:])
	if le != be {
		return 0, fmt.Errorf("%s endian values disagree: %d != %d", field, le, be)
	}
	return le, nil
}

func blocksFor(size uint32) uint32 {
	if size == 0 {
		return 0
	}
	return (size-1)/SectorSize + 1
}

func allByte(data []byte, want byte) bool {
	for _, got := range data {
		if got != want {
			return false
		}
	}
	return true
}

func isDot(id []byte) bool    { return len(id) == 1 && id[0] == 0 }
func isDotDot(id []byte) bool { return len(id) == 1 && id[0] == 1 }

func normalizedIdentifier(id []byte) string {
	s := string(id)
	if cut := strings.LastIndexByte(s, ';'); cut >= 0 {
		s = s[:cut]
	}
	return s
}

func hasPSPGame(entries []Entry) bool {
	for _, entry := range entries {
		if entry.Path == "/PSP_GAME" && entry.IsDir {
			return true
		}
	}
	return false
}
