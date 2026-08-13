// Package trinitylink reads Trinity LINKDATA IDX/BIN archive pairs.
package trinitylink

import (
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	indexRecordSize = 12
	entryAlignment  = 0x800
	chunkAlignment  = 0x80
	copyBufferSize  = 32 * 1024
)

// Kind identifies how a LINKDATA entry is stored.
type Kind string

const (
	KindAbsent   Kind = "absent"
	KindRaw      Kind = "raw"
	KindDeflate  Kind = "deflate"
	KindZeroFill Kind = "zero-fill"
)

// Entry describes one LINKDATA member in IDX order.
type Entry struct {
	Index       int
	Offset      int64
	DecodedSize uint32
	StoredSize  uint32
	Kind        Kind
}

type member struct {
	Entry
	chunkCount uint32
	chunkTable int64
	payload    int64
}

// Archive is a validated, read-only LINKDATA IDX/BIN pair. Close releases its
// BIN file.
type Archive struct {
	indexPath string
	dataPath  string
	bin       *os.File
	members   []member
}

// Open opens a LINKDATA IDX/BIN pair and validates every member extent and
// compressed-member layout. Neither source file is modified.
func Open(indexPath, dataPath string) (*Archive, error) {
	index, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("read LINKDATA IDX: %w", err)
	}
	if len(index)%indexRecordSize != 0 {
		return nil, fmt.Errorf("%s: IDX size is not a multiple of %d", indexPath, indexRecordSize)
	}
	bin, err := os.Open(dataPath)
	if err != nil {
		return nil, fmt.Errorf("open LINKDATA BIN: %w", err)
	}
	archive := &Archive{indexPath: indexPath, dataPath: dataPath, bin: bin, members: make([]member, 0, len(index)/indexRecordSize)}
	if err := archive.validate(index); err != nil {
		bin.Close()
		return nil, err
	}
	return archive, nil
}

func (p *Archive) validate(data []byte) error {
	info, err := p.bin.Stat()
	if err != nil {
		return fmt.Errorf("stat LINKDATA BIN: %w", err)
	}
	binSize := info.Size()
	var offset uint64
	for entryIndex := 0; entryIndex < len(data)/indexRecordSize; entryIndex++ {
		record := entryIndex * indexRecordSize
		member := member{Entry: Entry{
			Index:       entryIndex,
			Offset:      int64(offset),
			DecodedSize: binary.BigEndian.Uint32(data[record : record+4]),
			StoredSize:  binary.BigEndian.Uint32(data[record+4 : record+8]),
		}}
		flag := binary.BigEndian.Uint32(data[record+8 : record+12])
		if flag > 1 {
			return fmt.Errorf("%s: entry %d has unsupported flag %d", p.indexPath, entryIndex, flag)
		}
		if member.StoredSize == 0 {
			member.Kind = KindAbsent
		} else if flag == 0 {
			member.Kind = KindRaw
		} else {
			member.Kind = KindDeflate
		}
		if member.Kind == KindRaw && member.DecodedSize != member.StoredSize {
			return fmt.Errorf("%s: raw entry %d decoded size %d differs from stored size %d", p.indexPath, entryIndex, member.DecodedSize, member.StoredSize)
		}
		extent := align(uint64(member.StoredSize), entryAlignment)
		if extent > ^uint64(0)-offset || offset+extent > uint64(binSize) {
			return fmt.Errorf("%s: entry %d extends past BIN bounds", p.dataPath, entryIndex)
		}
		if member.Kind == KindDeflate {
			if err := p.validateCompressed(&member); err != nil {
				return fmt.Errorf("%s: compressed entry %d: %w", p.dataPath, entryIndex, err)
			}
		}
		p.members = append(p.members, member)
		offset += extent
	}
	if offset != uint64(binSize) {
		return fmt.Errorf("%s: BIN size does not match final entry extent", p.dataPath)
	}
	return nil
}

func (p *Archive) validateCompressed(member *member) error {
	if member.StoredSize < 8 {
		return errors.New("header extends past stored member")
	}
	var header [8]byte
	if _, err := p.bin.ReadAt(header[:], member.Offset); err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	member.chunkCount = binary.BigEndian.Uint32(header[:4])
	if member.chunkCount == 0 {
		if binary.BigEndian.Uint32(header[4:]) != 0 {
			return errors.New("zero-fill header decoded size is not zero")
		}
		member.Kind = KindZeroFill
		return nil
	}
	if binary.BigEndian.Uint32(header[4:]) != member.DecodedSize {
		return errors.New("header decoded size differs from IDX")
	}
	headerSize := uint64(8) + uint64(member.chunkCount)*4
	member.payload = int64(align(headerSize, chunkAlignment))
	if uint64(member.payload) > uint64(member.StoredSize) {
		return errors.New("chunk table extends past stored member")
	}
	member.chunkTable = member.Offset + 8
	cursor := uint64(member.payload)
	for chunk := uint32(0); chunk < member.chunkCount; chunk++ {
		var sizeBytes [4]byte
		if _, err := p.bin.ReadAt(sizeBytes[:], member.chunkTable+int64(chunk)*4); err != nil {
			return fmt.Errorf("read chunk %d size: %w", chunk, err)
		}
		size := binary.BigEndian.Uint32(sizeBytes[:])
		if size == 0 || uint64(size) > uint64(member.StoredSize)-cursor {
			return fmt.Errorf("chunk %d exceeds stored member", chunk)
		}
		cursor = align(cursor+uint64(size), chunkAlignment)
		if cursor > uint64(member.StoredSize) {
			return fmt.Errorf("chunk %d padding exceeds stored member", chunk)
		}
	}
	if cursor != uint64(member.StoredSize) {
		return errors.New("chunk extents do not end at stored member boundary")
	}
	return nil
}

func align(value uint64, alignment uint64) uint64 {
	return (value + alignment - 1) &^ (alignment - 1)
}

// Entries returns a copy of the archive's member descriptions in IDX order.
func (p *Archive) Entries() []Entry {
	entries := make([]Entry, len(p.members))
	for i, member := range p.members {
		entries[i] = member.Entry
	}
	return entries
}

// WriteEntry decodes entry index and writes it to destination without
// retaining the decoded member in memory.
func (p *Archive) WriteEntry(index int, destination io.Writer) error {
	if p.bin == nil {
		return errors.New("LINKDATA pair is closed")
	}
	if index < 0 || index >= len(p.members) {
		return fmt.Errorf("LINKDATA entry index %d is out of range", index)
	}
	member := p.members[index]
	if member.Kind == KindAbsent {
		return nil
	}
	if member.Kind == KindRaw {
		_, err := io.Copy(destination, io.NewSectionReader(p.bin, member.Offset, int64(member.StoredSize)))
		if err != nil {
			return fmt.Errorf("write raw entry %d: %w", index, err)
		}
		return nil
	}
	if member.Kind == KindZeroFill {
		if err := p.requireZeroes(member.Offset, int64(member.StoredSize)); err != nil {
			return fmt.Errorf("decode zero-filled entry %d: %w", index, err)
		}
		return writeZeroes(destination, int64(member.DecodedSize))
	}
	return p.writeCompressed(index, member, destination)
}

func (p *Archive) writeCompressed(index int, member member, destination io.Writer) error {
	limited := &limitedWriter{destination: destination, remaining: int64(member.DecodedSize)}
	cursor := member.payload
	for chunk := uint32(0); chunk < member.chunkCount; chunk++ {
		var sizeBytes [4]byte
		if _, err := p.bin.ReadAt(sizeBytes[:], member.chunkTable+int64(chunk)*4); err != nil {
			return fmt.Errorf("read chunk %d size: %w", chunk, err)
		}
		size := int64(binary.BigEndian.Uint32(sizeBytes[:]))
		_, err := inflate(io.NewSectionReader(p.bin, member.Offset+cursor, size), limited)
		if err != nil {
			return fmt.Errorf("decode compressed entry %d chunk %d: %w", index, chunk, err)
		}
		cursor = int64(align(uint64(cursor)+uint64(size), chunkAlignment))
	}
	if limited.remaining != 0 {
		return fmt.Errorf("decode compressed entry %d: decoded %d bytes, want %d", index, int64(member.DecodedSize)-limited.remaining, member.DecodedSize)
	}
	return nil
}

type limitedWriter struct {
	destination io.Writer
	remaining   int64
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		return 0, errors.New("decoded stream exceeds IDX size")
	}
	n, err := w.destination.Write(data)
	w.remaining -= int64(n)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}

func inflate(source io.Reader, destination io.Writer) (int64, error) {
	exact := &exactReader{source: source}
	reader := flate.NewReader(exact)
	written, copyErr := io.Copy(destination, reader)
	closeErr := reader.Close()
	if copyErr != nil {
		return written, copyErr
	}
	if closeErr != nil {
		return written, closeErr
	}
	var trailing [1]byte
	if _, err := exact.Read(trailing[:]); err != io.EOF {
		if err == nil {
			return written, errors.New("compressed chunk has trailing bytes")
		}
		return written, err
	}
	return written, nil
}

// exactReader implements flate.Reader so flate does not insert a buffering
// reader that could consume bytes beyond the end of one DEFLATE stream.
type exactReader struct {
	source io.Reader
}

func (r *exactReader) Read(data []byte) (int, error) {
	return r.source.Read(data)
}

func (r *exactReader) ReadByte() (byte, error) {
	var data [1]byte
	_, err := io.ReadFull(r.source, data[:])
	return data[0], err
}

func (p *Archive) requireZeroes(offset, size int64) error {
	buffer := make([]byte, copyBufferSize)
	for size > 0 {
		chunk := int64(len(buffer))
		if size < chunk {
			chunk = size
		}
		if _, err := p.bin.ReadAt(buffer[:chunk], offset); err != nil {
			return err
		}
		for _, value := range buffer[:chunk] {
			if value != 0 {
				return errors.New("allocation is not all zero")
			}
		}
		offset += chunk
		size -= chunk
	}
	return nil
}

func writeZeroes(destination io.Writer, size int64) error {
	var zeroes [copyBufferSize]byte
	for size > 0 {
		chunk := int64(len(zeroes))
		if size < chunk {
			chunk = size
		}
		n, err := destination.Write(zeroes[:chunk])
		if err != nil {
			return err
		}
		if n != int(chunk) {
			return io.ErrShortWrite
		}
		size -= chunk
	}
	return nil
}

// Close releases the BIN file held by p.
func (p *Archive) Close() error {
	if p.bin == nil {
		return nil
	}
	err := p.bin.Close()
	p.bin = nil
	return err
}
