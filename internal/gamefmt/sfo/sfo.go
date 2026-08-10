// Package sfo applies the one authenticated PARAM.SFO transform supported by
// the Zill release builder.
package sfo

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

const (
	manifestFormat  = "zill-param-sfo-patch"
	manifestVersion = 1
	manifestTarget  = "PARAM.SFO"
	headerSize      = 20
	indexSize       = 16
	formatBinary    = 0x0004
	formatString    = 0x0204
)

// Manifest describes the authenticated PARAM.SFO input and its fixed MEMSIZE
// transform.
type Manifest struct {
	Format            string `toml:"format"`
	Version           int    `toml:"version"`
	Target            string `toml:"target"`
	SourceSHA256      string `toml:"source_sha256"`
	SourceSize        int    `toml:"source_size"`
	Magic             uint32 `toml:"magic"`
	SFOVersion        uint32 `toml:"sfo_version"`
	ExpectedAbsentKey string `toml:"expected_absent_key"`
	AppendKey         string `toml:"append_key"`
	EntryFormat       uint16 `toml:"entry_format"`
	Value             uint32 `toml:"value"`
	Alignment         int    `toml:"alignment"`
}

// ParseManifest strictly decodes and validates a PARAM.SFO patch manifest.
func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode PARAM.SFO patch manifest: %w", err)
	}
	if err := manifest.validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) validate() error {
	if m.Format != manifestFormat || m.Version != manifestVersion || m.Target != manifestTarget {
		return fmt.Errorf("unsupported PARAM.SFO patch manifest identity")
	}
	if _, err := decodeHash(m.SourceSHA256); err != nil {
		return fmt.Errorf("invalid PARAM.SFO source_sha256: %w", err)
	}
	if m.SourceSize <= 0 {
		return fmt.Errorf("invalid PARAM.SFO source_size %d", m.SourceSize)
	}
	if m.Magic != 0x46535000 || m.SFOVersion != 0x00000101 {
		return fmt.Errorf("unsupported PARAM.SFO magic or version")
	}
	if m.ExpectedAbsentKey != "MEMSIZE" || m.AppendKey != "MEMSIZE" || m.EntryFormat != 0x0404 || m.Value != 1 || m.Alignment != 16 {
		return fmt.Errorf("unsupported PARAM.SFO transform")
	}
	return nil
}

// Apply authenticates source and returns a newly allocated PARAM.SFO with TITLE
// replaced and a four-byte integer MEMSIZE=1 entry appended. Source is never
// modified.
func Apply(source []byte, manifest Manifest, title string) ([]byte, error) {
	if err := manifest.validate(); err != nil {
		return nil, err
	}
	if title == "" || strings.TrimSpace(title) != title || strings.ContainsAny(title, "\x00\r\n") || !utf8.ValidString(title) {
		return nil, fmt.Errorf("invalid PARAM.SFO title %q", title)
	}
	if len(source) != manifest.SourceSize {
		return nil, fmt.Errorf("unsupported PARAM.SFO size: got %d, want %d", len(source), manifest.SourceSize)
	}
	wantHash, _ := decodeHash(manifest.SourceSHA256)
	gotHash := sha256.Sum256(source)
	if gotHash != wantHash {
		return nil, fmt.Errorf("unsupported PARAM.SFO fingerprint")
	}

	parsed, err := parse(source, manifest)
	if err != nil {
		return nil, err
	}
	if _, exists := parsed.entries[manifest.ExpectedAbsentKey]; exists {
		return nil, fmt.Errorf("unsupported PARAM.SFO: key %q is already present", manifest.ExpectedAbsentKey)
	}
	titleIndex, exists := parsed.entries["TITLE"]
	if !exists {
		return nil, fmt.Errorf("unsupported PARAM.SFO: TITLE is absent")
	}
	titleEntry := parsed.indexTable[titleIndex*indexSize : (titleIndex+1)*indexSize]
	if binary.LittleEndian.Uint16(titleEntry[2:4]) != formatString {
		return nil, fmt.Errorf("unsupported PARAM.SFO: TITLE is not a string")
	}
	titleValue := append([]byte(title), 0)
	titleMaximumLength := int(binary.LittleEndian.Uint32(titleEntry[8:12]))
	if len(titleValue) > titleMaximumLength {
		return nil, fmt.Errorf("PARAM.SFO title requires %d bytes; capacity is %d", len(titleValue), titleMaximumLength)
	}
	titleDataOffset := int(binary.LittleEndian.Uint32(titleEntry[12:16]))
	clear(parsed.dataTable[titleDataOffset : titleDataOffset+titleMaximumLength])
	copy(parsed.dataTable[titleDataOffset:], titleValue)
	binary.LittleEndian.PutUint32(titleEntry[4:8], uint32(len(titleValue)))

	key := append([]byte(manifest.AppendKey), 0)
	newKeyOffset := len(parsed.keyTable)
	if newKeyOffset > 0xffff {
		return nil, fmt.Errorf("PARAM.SFO key table exceeds uint16 offsets")
	}
	newKeyTable := append(append([]byte(nil), parsed.keyTable...), key...)
	newKeyTable = append(newKeyTable, make([]byte, align(len(newKeyTable), 4)-len(newKeyTable))...)
	newKeyStart := headerSize + (parsed.entryCount+1)*indexSize
	newDataStart := newKeyStart + len(newKeyTable)

	result := make([]byte, 0, newDataStart+parsed.dataExtent+4+len(parsed.trailing)+manifest.Alignment)
	result = binary.LittleEndian.AppendUint32(result, manifest.Magic)
	result = binary.LittleEndian.AppendUint32(result, manifest.SFOVersion)
	result = binary.LittleEndian.AppendUint32(result, uint32(newKeyStart))
	result = binary.LittleEndian.AppendUint32(result, uint32(newDataStart))
	result = binary.LittleEndian.AppendUint32(result, uint32(parsed.entryCount+1))
	result = append(result, parsed.indexTable...)
	result = binary.LittleEndian.AppendUint16(result, uint16(newKeyOffset))
	result = binary.LittleEndian.AppendUint16(result, manifest.EntryFormat)
	result = binary.LittleEndian.AppendUint32(result, 4)
	result = binary.LittleEndian.AppendUint32(result, 4)
	result = binary.LittleEndian.AppendUint32(result, uint32(parsed.dataExtent))
	result = append(result, newKeyTable...)
	result = append(result, parsed.dataTable...)
	result = binary.LittleEndian.AppendUint32(result, manifest.Value)
	result = append(result, parsed.trailing...)
	result = append(result, make([]byte, align(len(result), manifest.Alignment)-len(result))...)
	return result, nil
}

type parsedSFO struct {
	entryCount int
	indexTable []byte
	keyTable   []byte
	dataTable  []byte
	dataExtent int
	trailing   []byte
	entries    map[string]int
}

func parse(source []byte, manifest Manifest) (parsedSFO, error) {
	if len(source) < headerSize {
		return parsedSFO{}, fmt.Errorf("truncated PARAM.SFO header")
	}
	magic := binary.LittleEndian.Uint32(source[0:4])
	version := binary.LittleEndian.Uint32(source[4:8])
	keyStart64 := uint64(binary.LittleEndian.Uint32(source[8:12]))
	dataStart64 := uint64(binary.LittleEndian.Uint32(source[12:16]))
	entryCount64 := uint64(binary.LittleEndian.Uint32(source[16:20]))
	indexEnd64 := uint64(headerSize) + entryCount64*indexSize
	if magic != manifest.Magic || version != manifest.SFOVersion {
		return parsedSFO{}, fmt.Errorf("unsupported PARAM.SFO header")
	}
	if indexEnd64 != keyStart64 || keyStart64 > dataStart64 || dataStart64 > uint64(len(source)) || dataStart64%4 != 0 {
		return parsedSFO{}, fmt.Errorf("malformed PARAM.SFO table bounds")
	}
	keyStart, dataStart, entryCount := int(keyStart64), int(dataStart64), int(entryCount64)
	entries := make(map[string]int, entryCount)
	dataExtent := 0
	for i := 0; i < entryCount; i++ {
		entry := source[headerSize+i*indexSize : headerSize+(i+1)*indexSize]
		keyOffset := int(binary.LittleEndian.Uint16(entry[0:2]))
		valueFormat := binary.LittleEndian.Uint16(entry[2:4])
		valueLength := uint64(binary.LittleEndian.Uint32(entry[4:8]))
		maximumLength := uint64(binary.LittleEndian.Uint32(entry[8:12]))
		dataOffset := uint64(binary.LittleEndian.Uint32(entry[12:16]))
		keyPosition := keyStart + keyOffset
		if keyPosition < keyStart || keyPosition >= dataStart {
			return parsedSFO{}, fmt.Errorf("PARAM.SFO entry %d key offset is out of bounds", i)
		}
		keyEndRelative := bytes.IndexByte(source[keyPosition:dataStart], 0)
		if keyEndRelative < 0 {
			return parsedSFO{}, fmt.Errorf("PARAM.SFO entry %d has an unterminated key", i)
		}
		keyBytes := source[keyPosition : keyPosition+keyEndRelative]
		if len(keyBytes) == 0 || !isASCII(keyBytes) {
			return parsedSFO{}, fmt.Errorf("PARAM.SFO entry %d has an invalid key", i)
		}
		key := string(keyBytes)
		if _, duplicate := entries[key]; duplicate {
			return parsedSFO{}, fmt.Errorf("PARAM.SFO has duplicate key %q", key)
		}
		entries[key] = i
		if valueLength > maximumLength || dataOffset%4 != 0 || dataOffset+maximumLength > uint64(len(source)-dataStart) {
			return parsedSFO{}, fmt.Errorf("PARAM.SFO entry %q has invalid value bounds", key)
		}
		value := source[dataStart+int(dataOffset) : dataStart+int(dataOffset+valueLength)]
		switch valueFormat {
		case formatBinary:
		case formatString:
			if len(value) == 0 || value[len(value)-1] != 0 || !utf8.Valid(value[:len(value)-1]) {
				return parsedSFO{}, fmt.Errorf("PARAM.SFO entry %q has an invalid string value", key)
			}
		case manifest.EntryFormat:
			if valueLength != 4 || maximumLength != 4 {
				return parsedSFO{}, fmt.Errorf("PARAM.SFO entry %q has an invalid integer value", key)
			}
		default:
			return parsedSFO{}, fmt.Errorf("PARAM.SFO entry %q has unsupported format %#04x", key, valueFormat)
		}
		end := int(dataOffset + maximumLength)
		if end > dataExtent {
			dataExtent = end
		}
	}
	if dataExtent%4 != 0 {
		return parsedSFO{}, fmt.Errorf("PARAM.SFO data table is not four-byte aligned")
	}
	return parsedSFO{
		entryCount: entryCount,
		indexTable: append([]byte(nil), source[headerSize:keyStart]...),
		keyTable:   append([]byte(nil), source[keyStart:dataStart]...),
		dataTable:  append([]byte(nil), source[dataStart:dataStart+dataExtent]...),
		dataExtent: dataExtent,
		trailing:   append([]byte(nil), source[dataStart+dataExtent:]...),
		entries:    entries,
	}, nil
}

func decodeHash(value string) ([sha256.Size]byte, error) {
	var hash [sha256.Size]byte
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return hash, fmt.Errorf("expected 64 lowercase hexadecimal characters")
	}
	if hex.EncodeToString(decoded) != value {
		return hash, fmt.Errorf("expected 64 lowercase hexadecimal characters")
	}
	copy(hash[:], decoded)
	return hash, nil
}

func align(value, alignment int) int {
	return (value + alignment - 1) & -alignment
}

func isASCII(value []byte) bool {
	for _, b := range value {
		if b > 0x7f {
			return false
		}
	}
	return true
}
