// Package paa reads and rebuilds the paired index and payload files used by
// Zill O'll Infinite Plus PAA archives.
package paa

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	headerSize        = 0x20
	recordSize        = 0x10
	archivePrefixSize = 0x10
	archiveAlignment  = 0x10
)

// Member describes one entry in archive order. LeftChild and RightChild are
// filename-search-tree member indexes or 0xffffffff for no child.
type Member struct {
	Index      int
	Name       string
	Offset     uint32
	Size       uint32
	LeftChild  uint32
	RightChild uint32
}

// Replacement selects one member by archive index.
type Replacement struct {
	Index   int
	Payload []byte
}

// IndexReplacement creates a replacement resolved by archive index.
func IndexReplacement(index int, payload []byte) Replacement {
	return Replacement{Index: index, Payload: payload}
}

// Pair is a validated PAA index/archive pair. Close releases its archive file.
type Pair struct {
	indexPath   string
	archivePath string
	index       []byte
	archive     *os.File
	prefix      [archivePrefixSize]byte
	members     []Member
	offsetTable uint32
	identity    Identity
}

// Identity describes the immutable files from which a Pair was opened. It
// remains available after Close so callers can identify derived data without
// reopening the archive.
type Identity struct {
	IndexPath          string
	ArchivePath        string
	IndexSHA256        [sha256.Size]byte
	ArchiveSize        int64
	ArchiveModTimeNano int64
	ArchiveChangeNano  int64
	ArchiveDevice      uint64
	ArchiveInode       uint64
}

// Open validates and opens a PAA pair. The source files are only opened for
// reading and are never modified.
func Open(indexPath, archivePath string) (*Pair, error) {
	index, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("read PAA index: %w", err)
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open PAA archive: %w", err)
	}
	absoluteIndexPath, err := filepath.Abs(indexPath)
	if err != nil {
		archive.Close()
		return nil, fmt.Errorf("resolve PAA index path: %w", err)
	}
	absoluteArchivePath, err := filepath.Abs(archivePath)
	if err != nil {
		archive.Close()
		return nil, fmt.Errorf("resolve PAA archive path: %w", err)
	}
	pair := &Pair{
		indexPath:   indexPath,
		archivePath: archivePath,
		index:       index,
		archive:     archive,
		identity: Identity{
			IndexPath:   absoluteIndexPath,
			ArchivePath: absoluteArchivePath,
			IndexSHA256: sha256.Sum256(index),
		},
	}
	if err := pair.validate(); err != nil {
		archive.Close()
		return nil, err
	}
	return pair, nil
}

func (p *Pair) validate() error {
	if len(p.index) < headerSize || !bytes.Equal(p.index[:4], []byte{'P', 'A', 'A', 0}) {
		return fmt.Errorf("%s: not a PAA index", p.indexPath)
	}
	count := uint64(binary.LittleEndian.Uint32(p.index[8:12]))
	offsetTable := binary.LittleEndian.Uint32(p.index[16:20])
	if uint64(headerSize)+count*recordSize > uint64(len(p.index)) {
		return fmt.Errorf("%s: member table extends past end of file", p.indexPath)
	}
	if uint64(offsetTable)+count*4 > uint64(len(p.index)) {
		return fmt.Errorf("%s: archive offset table extends past end of file", p.indexPath)
	}
	info, err := p.archive.Stat()
	if err != nil {
		return fmt.Errorf("stat PAA archive: %w", err)
	}
	if info.Size() < archivePrefixSize {
		return fmt.Errorf("%s: archive is missing its 16-byte prefix", p.archivePath)
	}
	p.identity.ArchiveSize = info.Size()
	p.identity.ArchiveModTimeNano = info.ModTime().UnixNano()
	p.identity.ArchiveChangeNano, p.identity.ArchiveDevice, p.identity.ArchiveInode = platformFileIdentity(info)
	if _, err := p.archive.ReadAt(p.prefix[:], 0); err != nil {
		return fmt.Errorf("read PAA archive prefix: %w", err)
	}

	p.offsetTable = offsetTable
	p.members = make([]Member, 0, count)
	previousEnd := uint64(archivePrefixSize)
	for i := uint64(0); i < count; i++ {
		record := uint64(headerSize) + i*recordSize
		nameOffset := uint64(binary.LittleEndian.Uint32(p.index[record : record+4]))
		if nameOffset >= uint64(len(p.index)) {
			return fmt.Errorf("%s: member %d has an invalid name field", p.indexPath, i)
		}
		nameField := p.index[nameOffset:]
		nul := bytes.IndexByte(nameField, 0)
		if nul < 0 {
			return fmt.Errorf("%s: member %d filename is not NUL-terminated", p.indexPath, i)
		}
		for _, value := range nameField[:nul] {
			if value > 0x7f {
				return fmt.Errorf("%s: member %d has a non-ASCII filename", p.indexPath, i)
			}
		}
		size := binary.LittleEndian.Uint32(p.index[record+4 : record+8])
		offsetPosition := uint64(offsetTable) + i*4
		offset := binary.LittleEndian.Uint32(p.index[offsetPosition : offsetPosition+4])
		expectedOffset := align(previousEnd)
		if uint64(offset) != expectedOffset {
			return fmt.Errorf("%s: member %d offset %#x does not follow 16-byte alignment (%#x)", p.archivePath, i, offset, expectedOffset)
		}
		end := uint64(offset) + uint64(size)
		if end > uint64(info.Size()) {
			return fmt.Errorf("%s: member %q extends past end of archive", p.archivePath, string(nameField[:nul]))
		}
		if err := requireZeroes(p.archive, int64(previousEnd), int64(uint64(offset)-previousEnd)); err != nil {
			return fmt.Errorf("%s: nonzero padding before member %d", p.archivePath, i)
		}
		p.members = append(p.members, Member{
			Index:      int(i),
			Name:       string(nameField[:nul]),
			Offset:     offset,
			Size:       size,
			LeftChild:  binary.LittleEndian.Uint32(p.index[record+8 : record+12]),
			RightChild: binary.LittleEndian.Uint32(p.index[record+12 : record+16]),
		})
		previousEnd = end
	}
	if uint64(info.Size()) != align(previousEnd) {
		return fmt.Errorf("%s: trailing data does not match 16-byte alignment", p.archivePath)
	}
	if err := requireZeroes(p.archive, int64(previousEnd), info.Size()-int64(previousEnd)); err != nil {
		return fmt.Errorf("%s: nonzero trailing padding", p.archivePath)
	}
	return nil
}

// Identity returns the source-file identity captured when p was opened.
func (p *Pair) Identity() Identity {
	return p.identity
}

func align(value uint64) uint64 {
	return (value + archiveAlignment - 1) &^ (archiveAlignment - 1)
}

func requireZeroes(source io.ReaderAt, offset, size int64) error {
	buffer := make([]byte, 32*1024)
	for size > 0 {
		chunk := int64(len(buffer))
		if size < chunk {
			chunk = size
		}
		if _, err := source.ReadAt(buffer[:chunk], offset); err != nil {
			return err
		}
		for _, value := range buffer[:chunk] {
			if value != 0 {
				return errors.New("nonzero byte")
			}
		}
		offset += chunk
		size -= chunk
	}
	return nil
}

// Members returns a copy of the archive's ordered member descriptions.
func (p *Pair) Members() []Member {
	return append([]Member(nil), p.members...)
}

// Payload reads and returns one member's exact stored bytes.
func (p *Pair) Payload(index int) ([]byte, error) {
	if p.archive == nil {
		return nil, errors.New("PAA pair is closed")
	}
	if index < 0 || index >= len(p.members) {
		return nil, fmt.Errorf("PAA member index %d is out of range", index)
	}
	member := p.members[index]
	payload := make([]byte, member.Size)
	if _, err := p.archive.ReadAt(payload, int64(member.Offset)); err != nil {
		return nil, fmt.Errorf("read PAA member %d %q: %w", index, member.Name, err)
	}
	return payload, nil
}

// Close releases the archive file held by p.
func (p *Pair) Close() error {
	if p.archive == nil {
		return nil
	}
	err := p.archive.Close()
	p.archive = nil
	return err
}

// Rebuild writes a verified rebuilt pair. Replacements are resolved before any
// output is created. Existing outputs are replaced only after both temporary
// files have been fully written and successfully reopened and verified.
func (p *Pair) Rebuild(outputIndexPath, outputArchivePath string, replacements ...Replacement) error {
	if p.archive == nil {
		return errors.New("PAA pair is closed")
	}
	resolved, err := p.resolve(replacements)
	if err != nil {
		return err
	}
	if err := rejectSourceOutput(p.indexPath, outputIndexPath); err != nil {
		return err
	}
	if err := rejectSourceOutput(p.archivePath, outputArchivePath); err != nil {
		return err
	}
	if samePath(outputIndexPath, outputArchivePath) {
		return errors.New("PAA index and archive outputs must be different files")
	}
	if err := os.MkdirAll(filepath.Dir(outputIndexPath), 0o755); err != nil {
		return fmt.Errorf("create PAA index output directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputArchivePath), 0o755); err != nil {
		return fmt.Errorf("create PAA archive output directory: %w", err)
	}
	archiveTemp, err := os.CreateTemp(filepath.Dir(outputArchivePath), ".paa-archive-*")
	if err != nil {
		return fmt.Errorf("create temporary PAA archive: %w", err)
	}
	archiveTempPath := archiveTemp.Name()
	defer os.Remove(archiveTempPath)
	indexTemp, err := os.CreateTemp(filepath.Dir(outputIndexPath), ".paa-index-*")
	if err != nil {
		archiveTemp.Close()
		return fmt.Errorf("create temporary PAA index: %w", err)
	}
	indexTempPath := indexTemp.Name()
	defer os.Remove(indexTempPath)

	rebuiltIndex, err := p.writeRebuilt(indexTemp, archiveTemp, resolved)
	if closeErr := archiveTemp.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if closeErr := indexTemp.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write rebuilt PAA pair: %w", err)
	}
	if err := verifyRebuilt(p, indexTempPath, archiveTempPath, rebuiltIndex, resolved); err != nil {
		return fmt.Errorf("verify rebuilt PAA pair: %w", err)
	}
	if err := os.Rename(archiveTempPath, outputArchivePath); err != nil {
		return fmt.Errorf("publish rebuilt PAA archive: %w", err)
	}
	archiveTempPath = ""
	if err := os.Rename(indexTempPath, outputIndexPath); err != nil {
		return fmt.Errorf("publish rebuilt PAA index: %w", err)
	}
	indexTempPath = ""
	return nil
}

func (p *Pair) resolve(replacements []Replacement) (map[int][]byte, error) {
	resolved := make(map[int][]byte, len(replacements))
	for _, replacement := range replacements {
		if replacement.Index < 0 || replacement.Index >= len(p.members) {
			return nil, fmt.Errorf("replacement member index %d not found", replacement.Index)
		}
		if _, exists := resolved[replacement.Index]; exists {
			return nil, fmt.Errorf("member %d is selected by more than one replacement", replacement.Index)
		}
		resolved[replacement.Index] = append([]byte(nil), replacement.Payload...)
	}
	return resolved, nil
}

func (p *Pair) writeRebuilt(indexDestination, archiveDestination *os.File, replacements map[int][]byte) ([]byte, error) {
	rebuiltIndex := append([]byte(nil), p.index...)
	if _, err := archiveDestination.Write(p.prefix[:]); err != nil {
		return nil, err
	}
	for _, member := range p.members {
		position, err := archiveDestination.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		offset := align(uint64(position))
		if offset > uint64(^uint32(0)) {
			return nil, errors.New("rebuilt PAA archive offset exceeds uint32")
		}
		if err := writeZeroes(archiveDestination, int64(offset)-position); err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(rebuiltIndex[int(p.offsetTable)+member.Index*4:], uint32(offset))
		payload, replaced := replacements[member.Index]
		if replaced {
			if uint64(len(payload)) > uint64(^uint32(0)) {
				return nil, fmt.Errorf("replacement member %d is too large", member.Index)
			}
			if _, err := archiveDestination.Write(payload); err != nil {
				return nil, err
			}
		} else {
			if _, err := io.CopyN(archiveDestination, io.NewSectionReader(p.archive, int64(member.Offset), int64(member.Size)), int64(member.Size)); err != nil {
				return nil, fmt.Errorf("copy member %d %q: %w", member.Index, member.Name, err)
			}
			payload = nil
		}
		size := member.Size
		if replaced {
			size = uint32(len(payload))
		}
		binary.LittleEndian.PutUint32(rebuiltIndex[headerSize+member.Index*recordSize+4:], size)
	}
	position, err := archiveDestination.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	if err := writeZeroes(archiveDestination, int64(align(uint64(position)))-position); err != nil {
		return nil, err
	}
	if _, err := indexDestination.Write(rebuiltIndex); err != nil {
		return nil, err
	}
	return rebuiltIndex, nil
}

func writeZeroes(destination io.Writer, size int64) error {
	var zeroes [archiveAlignment]byte
	for size > 0 {
		chunk := int64(len(zeroes))
		if size < chunk {
			chunk = size
		}
		if _, err := destination.Write(zeroes[:chunk]); err != nil {
			return err
		}
		size -= chunk
	}
	return nil
}

func verifyRebuilt(source *Pair, indexPath, archivePath string, expectedIndex []byte, replacements map[int][]byte) error {
	rebuilt, err := Open(indexPath, archivePath)
	if err != nil {
		return err
	}
	defer rebuilt.Close()
	if !bytes.Equal(expectedIndex, rebuilt.index) {
		return errors.New("rebuilt index bytes differ from the computed index")
	}
	if source.prefix != rebuilt.prefix {
		return errors.New("rebuilt archive prefix differs")
	}
	if len(source.members) != len(rebuilt.members) {
		return errors.New("rebuilt member count differs")
	}
	for i, original := range source.members {
		actual := rebuilt.members[i]
		if original.Index != actual.Index || original.Name != actual.Name || original.LeftChild != actual.LeftChild || original.RightChild != actual.RightChild {
			return fmt.Errorf("rebuilt member %d identity or metadata differs", i)
		}
		expected, replaced := replacements[i]
		if !replaced {
			expected, err = source.Payload(i)
			if err != nil {
				return err
			}
		}
		actualPayload, err := rebuilt.Payload(i)
		if err != nil {
			return err
		}
		if !bytes.Equal(expected, actualPayload) {
			return fmt.Errorf("rebuilt member %d %q payload differs", i, original.Name)
		}
	}
	return nil
}

func rejectSourceOutput(sourcePath, outputPath string) error {
	if samePath(sourcePath, outputPath) {
		return errors.New("PAA output must not overwrite its source")
	}
	sourceInfo, sourceErr := os.Stat(sourcePath)
	outputInfo, outputErr := os.Stat(outputPath)
	if sourceErr == nil && outputErr == nil && os.SameFile(sourceInfo, outputInfo) {
		return errors.New("PAA output must not overwrite its source")
	}
	return nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}
