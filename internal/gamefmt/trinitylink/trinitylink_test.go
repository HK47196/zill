package trinitylink_test

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/HK47196/zill/internal/gamefmt/trinitylink"
)

func TestWriteMemberDecodesRawCompressedZeroFilledAndAbsentEntries(t *testing.T) {
	directory := t.TempDir()
	raw := []byte("plain LINKDATA entry")
	first := []byte("a compressed first chunk")
	last := []byte(" and a final partial chunk")
	indexPath, binPath := writePair(t, directory, []fixtureEntry{
		{decoded: uint32(len(raw)), stored: uint32(len(raw)), flag: 0, payload: raw},
		compressedEntry(t, append(append([]byte(nil), first...), last...), first, last),
		{decoded: 900, stored: 0x800, flag: 1, payload: make([]byte, 0x800)},
		{decoded: 123, stored: 0, flag: 1},
	})

	pair, err := trinitylink.Open(indexPath, binPath)
	if err != nil {
		t.Fatal(err)
	}
	defer pair.Close()
	entries := pair.Entries()
	if len(entries) != 4 || entries[0].Kind != trinitylink.KindRaw || entries[1].Kind != trinitylink.KindDeflate || entries[2].Kind != trinitylink.KindZeroFill || entries[2].DecodedSize != 900 || entries[3].Kind != trinitylink.KindAbsent || entries[3].StoredSize != 0 {
		t.Fatalf("Entries() = %#v", entries)
	}
	for index, want := range [][]byte{raw, append(first, last...), make([]byte, 900), nil} {
		var got bytes.Buffer
		if err := pair.WriteEntry(index, &got); err != nil {
			t.Fatalf("WriteEntry(%d): %v", index, err)
		}
		if !bytes.Equal(got.Bytes(), want) {
			t.Errorf("WriteEntry(%d) = %q, want %q", index, got.Bytes(), want)
		}
	}
}

func TestOpenRejectsMalformedIndexAndMemberExtents(t *testing.T) {
	directory := t.TempDir()
	valid := []fixtureEntry{{decoded: 1, stored: 1, flag: 0, payload: []byte{'x'}}}
	tests := []struct {
		name   string
		change func([]byte, []byte) ([]byte, []byte)
	}{
		{"truncated IDX record", func(index, bin []byte) ([]byte, []byte) { return index[:len(index)-1], bin }},
		{"unsupported flag", func(index, bin []byte) ([]byte, []byte) { binary.BigEndian.PutUint32(index[8:], 2); return index, bin }},
		{"raw size mismatch", func(index, bin []byte) ([]byte, []byte) { binary.BigEndian.PutUint32(index[:4], 2); return index, bin }},
		{"member beyond BIN", func(index, bin []byte) ([]byte, []byte) { return index, bin[:1] }},
		{"trailing BIN extent", func(index, bin []byte) ([]byte, []byte) { return index, append(bin, 0) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			indexPath, binPath := writePair(t, directory, valid)
			index, err := os.ReadFile(indexPath)
			if err != nil {
				t.Fatal(err)
			}
			bin, err := os.ReadFile(binPath)
			if err != nil {
				t.Fatal(err)
			}
			index, bin = test.change(index, bin)
			if err := os.WriteFile(indexPath, index, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binPath, bin, 0o644); err != nil {
				t.Fatal(err)
			}
			if pair, err := trinitylink.Open(indexPath, binPath); err == nil {
				pair.Close()
				t.Fatal("Open accepted malformed pair")
			}
		})
	}
}

func TestOpenRejectsMalformedCompressedLayout(t *testing.T) {
	directory := t.TempDir()
	entry := compressedEntry(t, []byte("decoded"), []byte("decoded"))
	indexPath, binPath := writePair(t, directory, []fixtureEntry{entry})
	bin, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint32(bin[8:], 0) // a declared compressed chunk cannot be empty
	if err := os.WriteFile(binPath, bin, 0o644); err != nil {
		t.Fatal(err)
	}
	if pair, err := trinitylink.Open(indexPath, binPath); err == nil {
		pair.Close()
		t.Fatal("Open accepted a zero-sized compressed chunk")
	}
}

func TestOpenRejectsCompressedMemberShorterThanItsHeader(t *testing.T) {
	directory := t.TempDir()
	indexPath, binPath := writePair(t, directory, []fixtureEntry{{decoded: 1, stored: 1, flag: 1, payload: []byte{0}}})
	if pair, err := trinitylink.Open(indexPath, binPath); err == nil {
		pair.Close()
		t.Fatal("Open accepted a compressed member shorter than its header")
	}
}

func TestWriteMemberRejectsTrailingCompressedBytes(t *testing.T) {
	directory := t.TempDir()
	entry := compressedEntry(t, []byte("decoded"), []byte("decoded"))
	declared := binary.BigEndian.Uint32(entry.payload[8:12])
	entry.payload[0x80+declared] = 0x7f
	binary.BigEndian.PutUint32(entry.payload[8:12], declared+1)
	indexPath, binPath := writePair(t, directory, []fixtureEntry{entry})
	pair, err := trinitylink.Open(indexPath, binPath)
	if err != nil {
		t.Fatal(err)
	}
	defer pair.Close()
	if err := pair.WriteEntry(0, &bytes.Buffer{}); err == nil {
		t.Fatal("WriteEntry accepted bytes after the DEFLATE stream")
	}
}

func TestWriteMemberRejectsInvalidCompressedDataAndNonzeroZeroFill(t *testing.T) {
	directory := t.TempDir()
	entry := compressedEntry(t, []byte("decoded"), []byte("decoded"))
	indexPath, binPath := writePair(t, directory, []fixtureEntry{entry})
	bin, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	bin[0x80] ^= 0xff
	if err := os.WriteFile(binPath, bin, 0o644); err != nil {
		t.Fatal(err)
	}
	pair, err := trinitylink.Open(indexPath, binPath)
	if err != nil {
		t.Fatal(err)
	}
	defer pair.Close()
	if err := pair.WriteEntry(0, &bytes.Buffer{}); err == nil {
		t.Fatal("WriteEntry decoded corrupt DEFLATE data")
	}

	zeroIndex, zeroBin := writePair(t, directory, []fixtureEntry{{decoded: 8, stored: 0x800, flag: 1, payload: make([]byte, 0x800)}})
	bin, err = os.ReadFile(zeroBin)
	if err != nil {
		t.Fatal(err)
	}
	bin[len(bin)-1] = 1
	if err := os.WriteFile(zeroBin, bin, 0o644); err != nil {
		t.Fatal(err)
	}
	zeroPair, err := trinitylink.Open(zeroIndex, zeroBin)
	if err != nil {
		t.Fatal(err)
	}
	defer zeroPair.Close()
	var destination bytes.Buffer
	if err := zeroPair.WriteEntry(0, &destination); err == nil {
		t.Fatal("WriteEntry accepted nonzero zero-fill allocation")
	}
	if destination.Len() != 0 {
		t.Fatalf("invalid zero-fill wrote %d bytes", destination.Len())
	}
}

type fixtureEntry struct {
	decoded uint32
	stored  uint32
	flag    uint32
	payload []byte
}

func compressedEntry(t *testing.T, decoded []byte, chunks ...[]byte) fixtureEntry {
	t.Helper()
	compressed := make([][]byte, len(chunks))
	stored := uint64(8 + len(chunks)*4)
	stored = align(stored, 0x80)
	for index, chunk := range chunks {
		compressed[index] = deflate(t, chunk)
		stored = align(stored+uint64(len(compressed[index])), 0x80)
	}
	payload := make([]byte, stored)
	binary.BigEndian.PutUint32(payload[:4], uint32(len(chunks)))
	binary.BigEndian.PutUint32(payload[4:8], uint32(len(decoded)))
	cursor := uint64(align(uint64(8+len(chunks)*4), 0x80))
	for index, chunk := range compressed {
		binary.BigEndian.PutUint32(payload[8+index*4:], uint32(len(chunk)))
		copy(payload[cursor:], chunk)
		cursor = align(cursor+uint64(len(chunk)), 0x80)
	}
	return fixtureEntry{decoded: uint32(len(decoded)), stored: uint32(stored), flag: 1, payload: payload}
}

func deflate(t *testing.T, input []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer, err := flate.NewWriter(&output, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func writePair(t *testing.T, directory string, entries []fixtureEntry) (string, string) {
	t.Helper()
	index := make([]byte, len(entries)*12)
	var bin []byte
	for entryIndex, entry := range entries {
		record := entryIndex * 12
		binary.BigEndian.PutUint32(index[record:], entry.decoded)
		binary.BigEndian.PutUint32(index[record+4:], entry.stored)
		binary.BigEndian.PutUint32(index[record+8:], entry.flag)
		if len(entry.payload) != int(entry.stored) {
			t.Fatalf("entry %d payload length = %d, stored = %d", entryIndex, len(entry.payload), entry.stored)
		}
		bin = append(bin, entry.payload...)
		bin = append(bin, make([]byte, int(align(uint64(entry.stored), 0x800))-len(entry.payload))...)
	}
	indexPath := filepath.Join(directory, "fixture.idx")
	binPath := filepath.Join(directory, "fixture.bin")
	if err := os.WriteFile(indexPath, index, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, bin, 0o644); err != nil {
		t.Fatal(err)
	}
	return indexPath, binPath
}

func align(value, alignment uint64) uint64 { return (value + alignment - 1) &^ (alignment - 1) }
