// SPDX-License-Identifier: GPL-3.0-or-later

package pspiso

import "slices"

const SectorSize = 2048

// Entry is a file or directory in ISO directory-record order. Path is an ISO
// path, not a host filesystem path.
type Entry struct {
	Path            string
	Identifier      []byte
	LBA             uint32
	Size            uint32
	IsDir           bool
	Flags           byte
	DirectoryLBA    uint32
	DirectoryOffset uint32
	Record          []byte
	// Padding is the bytes after Size in this extent's final sector.
	// It is metadata owned by the layout, rather than file payload.
	Padding []byte
}

// Descriptor is one complete ISO volume descriptor sector.
type Descriptor struct {
	LBA  uint32
	Data []byte
}

// PathTable is one exact on-disc path-table copy.
type PathTable struct {
	LBA       uint32
	Size      uint32
	BigEndian bool
	Data      []byte
}

// Directory is a complete allocated directory extent. Data includes the
// sector padding following the directory records.
type Directory struct {
	Path string
	LBA  uint32
	Size uint32
	Data []byte
}

// FillRange describes an unallocated run whose sectors consist entirely of
// Byte. EndLBA is exclusive. It never includes file payload or metadata.
type FillRange struct {
	StartLBA uint32
	EndLBA   uint32
	Byte     byte
}

// Manifest contains enough non-payload information for a source-independent
// deterministic rebuild. The writer must obtain every file's bytes from an
// extracted tree; this type intentionally stores no file payloads.
type Manifest struct {
	SectorSize      uint32
	VolumeSpaceSize uint32
	SourceSize      int64
	SourceSHA256    [32]byte

	SystemArea  []byte
	Descriptors []Descriptor
	PathTables  []PathTable
	Directories []Directory
	Entries     []Entry
	FillRanges  []FillRange
}

func (m Manifest) clone() Manifest {
	copyBytes := func(b []byte) []byte { return slices.Clone(b) }
	out := m
	out.SystemArea = copyBytes(m.SystemArea)
	out.Descriptors = make([]Descriptor, len(m.Descriptors))
	for i := range m.Descriptors {
		out.Descriptors[i] = m.Descriptors[i]
		out.Descriptors[i].Data = copyBytes(m.Descriptors[i].Data)
	}
	out.PathTables = make([]PathTable, len(m.PathTables))
	for i := range m.PathTables {
		out.PathTables[i] = m.PathTables[i]
		out.PathTables[i].Data = copyBytes(m.PathTables[i].Data)
	}
	out.Directories = make([]Directory, len(m.Directories))
	for i := range m.Directories {
		out.Directories[i] = m.Directories[i]
		out.Directories[i].Data = copyBytes(m.Directories[i].Data)
	}
	out.Entries = make([]Entry, len(m.Entries))
	for i := range m.Entries {
		out.Entries[i] = m.Entries[i]
		out.Entries[i].Identifier = copyBytes(m.Entries[i].Identifier)
		out.Entries[i].Record = copyBytes(m.Entries[i].Record)
		out.Entries[i].Padding = copyBytes(m.Entries[i].Padding)
	}
	out.FillRanges = slices.Clone(m.FillRanges)
	return out
}
