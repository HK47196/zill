package paa_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/HK47196/zill/internal/gamefmt/paa"
)

func TestRebuildPreservesPairAndAppliesNameAndIndexReplacements(t *testing.T) {
	directory := t.TempDir()
	indexPath, archivePath, sourceIndex, sourceArchive := writeFixture(t, directory)

	pair, err := paa.Open(indexPath, archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer pair.Close()
	members := pair.Members()
	if names := []string{members[0].Name, members[1].Name, members[2].Name, members[3].Name}; !equalStrings(names, []string{"first.dat", "duplicate.dat", "duplicate.dat", "last.dat"}) {
		t.Fatalf("member order/names = %q", names)
	}
	if payload, err := pair.Payload(2); err != nil || !bytes.Equal(payload, []byte("third")) {
		t.Fatalf("Payload(2) = %q, %v", payload, err)
	}

	noopIndex := filepath.Join(directory, "noop.bin")
	noopArchive := filepath.Join(directory, "noop.arc")
	if err := pair.Rebuild(noopIndex, noopArchive); err != nil {
		t.Fatal(err)
	}
	assertFileBytes(t, noopIndex, sourceIndex)
	assertFileBytes(t, noopArchive, sourceArchive)

	rebuiltIndexPath := filepath.Join(directory, "rebuilt.bin")
	rebuiltArchivePath := filepath.Join(directory, "rebuilt.arc")
	indexedPayload := []byte("replacement large enough to shift later offsets")
	namedPayload := []byte("L")
	if err := pair.Rebuild(
		rebuiltIndexPath,
		rebuiltArchivePath,
		paa.IndexReplacement(2, indexedPayload),
		paa.NameReplacement("last.dat", namedPayload),
	); err != nil {
		t.Fatal(err)
	}

	rebuilt, err := paa.Open(rebuiltIndexPath, rebuiltArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer rebuilt.Close()
	rebuiltMembers := rebuilt.Members()
	if len(rebuiltMembers) != len(members) {
		t.Fatalf("member count = %d, want %d", len(rebuiltMembers), len(members))
	}
	for i := range members {
		if members[i].Index != rebuiltMembers[i].Index || members[i].Name != rebuiltMembers[i].Name || members[i].Metadata != rebuiltMembers[i].Metadata {
			t.Errorf("member %d identity/metadata changed: %#v -> %#v", i, members[i], rebuiltMembers[i])
		}
	}
	for index, want := range map[int][]byte{
		0: []byte("one"),
		1: {},
		2: indexedPayload,
		3: namedPayload,
	} {
		got, err := rebuilt.Payload(index)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("rebuilt Payload(%d) = %q, %v; want %q", index, got, err, want)
		}
	}
	if rebuiltMembers[3].Offset == members[3].Offset {
		t.Error("downstream member offset was not recomputed after a size change")
	}
	rebuiltIndex := mustRead(t, rebuiltIndexPath)
	for i := range sourceIndex {
		if isSizeOrOffsetField(i, len(members), int(binary.LittleEndian.Uint32(sourceIndex[16:20]))) {
			continue
		}
		if sourceIndex[i] != rebuiltIndex[i] {
			t.Fatalf("unrelated index byte %#x changed from %#x to %#x", i, sourceIndex[i], rebuiltIndex[i])
		}
	}
	if got := mustRead(t, rebuiltArchivePath)[:16]; !bytes.Equal(got, sourceArchive[:16]) {
		t.Fatalf("archive prefix = %x, want %x", got, sourceArchive[:16])
	}
}

func TestRebuildRejectsAmbiguousOrConflictingSelectionsWithoutChangingInputs(t *testing.T) {
	directory := t.TempDir()
	indexPath, archivePath, sourceIndex, sourceArchive := writeFixture(t, directory)
	pair, err := paa.Open(indexPath, archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer pair.Close()

	outputIndex := filepath.Join(directory, "failed.bin")
	outputArchive := filepath.Join(directory, "failed.arc")
	if err := pair.Rebuild(outputIndex, outputArchive, paa.NameReplacement("duplicate.dat", []byte("x"))); err == nil {
		t.Fatal("ambiguous name replacement succeeded")
	}
	if _, err := os.Stat(outputIndex); !os.IsNotExist(err) {
		t.Fatalf("failed rebuild created index output: %v", err)
	}
	if _, err := os.Stat(outputArchive); !os.IsNotExist(err) {
		t.Fatalf("failed rebuild created archive output: %v", err)
	}
	if err := pair.Rebuild(outputIndex, outputArchive, paa.IndexReplacement(0, []byte("x")), paa.NameReplacement("first.dat", []byte("y"))); err == nil {
		t.Fatal("two selectors for one member succeeded")
	}
	if err := pair.Rebuild(indexPath, outputArchive, paa.IndexReplacement(0, []byte("x"))); err == nil {
		t.Fatal("rebuild overwrote its source index")
	}
	assertFileBytes(t, indexPath, sourceIndex)
	assertFileBytes(t, archivePath, sourceArchive)
}

func TestOpenRejectsNonzeroAlignmentGap(t *testing.T) {
	directory := t.TempDir()
	indexPath, archivePath, _, archive := writeFixture(t, directory)
	archive[20] = 1
	if err := os.WriteFile(archivePath, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	if pair, err := paa.Open(indexPath, archivePath); err == nil {
		pair.Close()
		t.Fatal("Open accepted a nonzero inter-member gap")
	}
}

func TestOpenAcceptsObservedRetailNameFieldBoundaries(t *testing.T) {
	directory := t.TempDir()
	indexPath, archivePath, index, _ := writeFixture(t, directory)
	fullWidthName := "sound/audio/ebgmdreamatmosphere."
	copy(index[0x60:0x80], fullWidthName)
	finalNameOffset := len(index)
	index = append(index, make([]byte, 16)...)
	binary.LittleEndian.PutUint32(index[0x20+3*0x10:], uint32(finalNameOffset))
	if err := os.WriteFile(indexPath, index, 0o644); err != nil {
		t.Fatal(err)
	}

	pair, err := paa.Open(indexPath, archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer pair.Close()
	members := pair.Members()
	if members[0].Name != fullWidthName {
		t.Fatalf("full-width member name = %q, want %q", members[0].Name, fullWidthName)
	}
	if members[3].Name != "" {
		t.Fatalf("final short reserved name = %q, want empty", members[3].Name)
	}
}

func writeFixture(t *testing.T, directory string) (string, string, []byte, []byte) {
	t.Helper()
	const memberCount = 4
	const offsetTable = 0xe0
	index := bytes.Repeat([]byte{0xcc}, offsetTable+memberCount*4)
	copy(index, []byte{'P', 'A', 'A', 0})
	binary.LittleEndian.PutUint32(index[4:], 0x11223344)
	binary.LittleEndian.PutUint32(index[8:], memberCount)
	binary.LittleEndian.PutUint32(index[12:], 0x55667788)
	binary.LittleEndian.PutUint32(index[16:], offsetTable)
	binary.LittleEndian.PutUint32(index[20:], 0x99aabbcc)

	names := []string{"first.dat", "duplicate.dat", "duplicate.dat", "last.dat"}
	nameOffsets := []int{0x60, 0x80, 0xa0, 0xc0}
	sizes := []uint32{3, 0, 5, 2}
	offsets := []uint32{0x10, 0x20, 0x20, 0x30}
	for i := 0; i < memberCount; i++ {
		record := 0x20 + i*0x10
		binary.LittleEndian.PutUint32(index[record:], uint32(nameOffsets[i]))
		binary.LittleEndian.PutUint32(index[record+4:], sizes[i])
		binary.LittleEndian.PutUint32(index[record+8:], uint32(0x100+i))
		binary.LittleEndian.PutUint32(index[record+12:], uint32(0x200+i))
		copy(index[nameOffsets[i]:], names[i])
		index[nameOffsets[i]+len(names[i])] = 0
		binary.LittleEndian.PutUint32(index[offsetTable+i*4:], offsets[i])
	}
	archive := make([]byte, 0x40)
	copy(archive[:16], []byte("prefix-is-exact!"))
	copy(archive[0x10:], []byte("one"))
	copy(archive[0x20:], []byte("third"))
	copy(archive[0x30:], []byte("ok"))
	indexPath := filepath.Join(directory, "source.bin")
	archivePath := filepath.Join(directory, "source.arc")
	if err := os.WriteFile(indexPath, index, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	return indexPath, archivePath, index, archive
}

func isSizeOrOffsetField(position, count, offsetTable int) bool {
	for i := 0; i < count; i++ {
		if position >= 0x20+i*0x10+4 && position < 0x20+i*0x10+8 {
			return true
		}
		if position >= offsetTable+i*4 && position < offsetTable+i*4+4 {
			return true
		}
	}
	return false
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	if got := mustRead(t, path); !bytes.Equal(got, want) {
		t.Fatalf("%s differs from expected bytes", path)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
