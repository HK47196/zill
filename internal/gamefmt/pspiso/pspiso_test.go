// SPDX-License-Identifier: GPL-3.0-or-later

package pspiso_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/HK47196/zill/internal/gamefmt/pspiso"
)

const (
	testSectorSize = 2048
	testSectors    = 60
)

func TestInspectExtractBuildRoundTripPreservesEveryByte(t *testing.T) {
	t.Parallel()

	source := writeFixture(t)
	before := readFile(t, source)
	beforeHash := sha256.Sum256(before)

	image, err := pspiso.Open(source)
	if err != nil {
		t.Fatalf("Open(%q): %v", source, err)
	}
	manifest := image.Manifest()
	tree := t.TempDir()
	if err := image.Extract(tree); err != nil {
		_ = image.Close()
		t.Fatalf("Extract(%q): %v", tree, err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	output := filepath.Join(t.TempDir(), "rebuilt.iso")
	if err := pspiso.Build(output, manifest, os.DirFS(tree)); err != nil {
		t.Fatalf("Build(%q): %v", output, err)
	}

	got := readFile(t, output)
	if !bytes.Equal(before, got) {
		t.Fatal("inspect, extract, and build did not reproduce the authored image byte-for-byte")
	}
	if after := readFile(t, source); !bytes.Equal(before, after) {
		t.Fatal("round trip modified its source image")
	}
	if afterHash := sha256.Sum256(readFile(t, source)); afterHash != beforeHash {
		t.Fatal("round trip changed its source image hash")
	}
}

func TestOpenRejectsInvalidVolumeDescriptorIdentifier(t *testing.T) {
	t.Parallel()

	image := fixtureBytes()
	copy(image[16*testSectorSize+1:], "NOPE!")
	path := writeBytes(t, "invalid-id.iso", image)

	opened, err := pspiso.Open(path)
	if err == nil {
		_ = opened.Close()
		t.Fatal("Open accepted an image whose primary volume descriptor does not identify ISO 9660")
	}
}

func TestOpenRejectsDisagreeingDualEndianVolumeSize(t *testing.T) {
	t.Parallel()

	image := fixtureBytes()
	pvd := 16 * testSectorSize
	binary.BigEndian.PutUint32(image[pvd+84:pvd+88], testSectors+1)
	path := writeBytes(t, "invalid-endian.iso", image)

	opened, err := pspiso.Open(path)
	if err == nil {
		_ = opened.Close()
		t.Fatal("Open accepted a primary volume descriptor with disagreeing endian copies")
	}
}

func TestOpenAcceptsObservedPSPFileStructureVersion(t *testing.T) {
	t.Parallel()

	image := fixtureBytes()
	image[16*testSectorSize+881] = 2
	path := writeBytes(t, "psp-version-2.iso", image)
	opened, err := pspiso.Open(path)
	if err != nil {
		t.Fatalf("Open rejected the retail PSP file-structure version: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestOpenRejectsUnknownFileStructureVersion(t *testing.T) {
	t.Parallel()

	image := fixtureBytes()
	image[16*testSectorSize+881] = 3
	path := writeBytes(t, "unsupported-version.iso", image)
	opened, err := pspiso.Open(path)
	if err == nil {
		_ = opened.Close()
		t.Fatal("Open accepted an unobserved file-structure version")
	}
}

func TestOpenRejectsRootDirectoryOutsideImage(t *testing.T) {
	t.Parallel()

	image := fixtureBytes()
	root := 16*testSectorSize + 156
	putBoth32(image[root+2:root+10], testSectors+1)
	path := writeBytes(t, "invalid-root-extent.iso", image)

	opened, err := pspiso.Open(path)
	if err == nil {
		_ = opened.Close()
		t.Fatal("Open accepted a root directory extent outside the image")
	}
}

func TestOpenRejectsFileExtentOutsideImageAtUint32Boundary(t *testing.T) {
	t.Parallel()

	image := fixtureBytes()
	identifier := bytes.Index(image, []byte("EBOOT.BIN"))
	if identifier < 33 {
		t.Fatal("fixture has no EBOOT.BIN directory record")
	}
	record := identifier - 33
	putBoth32(image[record+10:record+18], math.MaxUint32)
	path := writeBytes(t, "invalid-file-extent.iso", image)

	opened, err := pspiso.Open(path)
	if err == nil {
		_ = opened.Close()
		t.Fatal("Open accepted a file extent larger than the image")
	}
}

func TestBuildFailureDoesNotPublishOutput(t *testing.T) {
	t.Parallel()

	source := writeFixture(t)
	image, err := pspiso.Open(source)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	manifest := image.Manifest()
	tree := t.TempDir()
	if err := image.Extract(tree); err != nil {
		_ = image.Close()
		t.Fatalf("Extract: %v", err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	missing := filepath.Join(tree, "PSP_GAME", "SYSDIR", "EBOOT.BIN")
	if err := os.Remove(missing); err != nil {
		t.Fatalf("remove extracted required file: %v", err)
	}
	output := filepath.Join(t.TempDir(), "should-not-exist.iso")
	if err := pspiso.Build(output, manifest, os.DirFS(tree)); err == nil {
		t.Fatal("Build succeeded despite a required extracted file being absent")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("Build published output after failure: stat error = %v", err)
	}
}

func TestBuildRejectsIncompleteManifestWithoutPublishingOutput(t *testing.T) {
	t.Parallel()

	manifest, tree := inspectAndExtractFixture(t)
	entries := manifest.Entries[:0]
	for _, entry := range manifest.Entries {
		if entry.Path != "/PSP_GAME/SYSDIR/EBOOT.BIN" {
			entries = append(entries, entry)
		}
	}
	manifest.Entries = entries
	output := filepath.Join(t.TempDir(), "should-not-exist.iso")
	if err := pspiso.Build(output, manifest, os.DirFS(tree)); err == nil {
		t.Fatal("Build accepted a manifest that omitted a directory-referenced file")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("Build published output from an incomplete manifest: stat error = %v", err)
	}
}

func TestBuildRejectsChangedPayloadWithoutPublishingOutput(t *testing.T) {
	t.Parallel()

	manifest, tree := inspectAndExtractFixture(t)
	path := filepath.Join(tree, "PSP_GAME", "SYSDIR", "EBOOT.BIN")
	data := readFile(t, path)
	data[0] ^= 0xff
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("change extracted payload: %v", err)
	}
	output := filepath.Join(t.TempDir(), "should-not-exist.iso")
	if err := pspiso.Build(output, manifest, os.DirFS(tree)); err == nil {
		t.Fatal("Build accepted file contents that do not match the source manifest")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("Build published output with changed file contents: stat error = %v", err)
	}
}

func TestBuildModifiedAuthorsSameSizeReplacement(t *testing.T) {
	t.Parallel()

	manifest, tree := inspectAndExtractFixture(t)
	path := filepath.Join(tree, "PSP_GAME", "SYSDIR", "EBOOT.BIN")
	want := readFile(t, path)
	want[0] ^= 0xff
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("change extracted payload: %v", err)
	}
	output := filepath.Join(t.TempDir(), "modified.iso")
	if err := pspiso.BuildModified(output, manifest, os.DirFS(tree)); err != nil {
		t.Fatalf("BuildModified: %v", err)
	}

	image, err := pspiso.Open(output)
	if err != nil {
		t.Fatalf("Open modified image: %v", err)
	}
	got, err := fs.ReadFile(image.PayloadFS(), "PSP_GAME/SYSDIR/EBOOT.BIN")
	closeErr := image.Close()
	if err != nil {
		t.Fatalf("read modified payload: %v", err)
	}
	if closeErr != nil {
		t.Fatalf("close modified image: %v", closeErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("BuildModified did not author the requested replacement bytes")
	}
	if hash := sha256.Sum256(readFile(t, output)); hash == manifest.SourceSHA256 {
		t.Fatal("BuildModified unexpectedly reproduced the untouched source hash")
	}
}

func TestBuildModifiedReflowsGrowingReplacement(t *testing.T) {
	t.Parallel()

	manifest, tree := inspectAndExtractFixture(t)
	path := filepath.Join(tree, "PSP_GAME", "PARAM.SFO")
	want := bytes.Repeat([]byte{0x5a}, 15*testSectorSize)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("grow extracted payload: %v", err)
	}
	output := filepath.Join(t.TempDir(), "grown.iso")
	if err := pspiso.BuildModified(output, manifest, os.DirFS(tree)); err != nil {
		t.Fatalf("BuildModified rejected a growing replacement: %v", err)
	}

	image, err := pspiso.Open(output)
	if err != nil {
		t.Fatalf("Open grown image: %v", err)
	}
	defer image.Close()
	got, err := fs.ReadFile(image.PayloadFS(), "PSP_GAME/PARAM.SFO")
	if err != nil {
		t.Fatalf("read grown payload: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("grown payload differs from its replacement")
	}
	var eboot pspiso.Entry
	for _, entry := range image.Manifest().Entries {
		if entry.Path == "/PSP_GAME/SYSDIR/EBOOT.BIN" {
			eboot = entry
			break
		}
	}
	if eboot.LBA <= 50 {
		t.Fatalf("later EBOOT.BIN was not moved out of the grown extent: LBA %d", eboot.LBA)
	}
	if eboot.LBA%16 != 50%16 {
		t.Fatalf("moved EBOOT.BIN lost its retail alignment: LBA %d", eboot.LBA)
	}
	if info, err := os.Stat(output); err != nil {
		t.Fatalf("stat grown image: %v", err)
	} else if info.Size() <= manifest.SourceSize {
		t.Fatalf("grown image size = %d, want more than %d", info.Size(), manifest.SourceSize)
	}
}

func TestBuildModifiedFailureDoesNotPublishOutput(t *testing.T) {
	t.Parallel()

	manifest, tree := inspectAndExtractFixture(t)
	path := filepath.Join(tree, "PSP_GAME", "SYSDIR", "EBOOT.BIN")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove replacement payload: %v", err)
	}
	output := filepath.Join(t.TempDir(), "should-not-exist.iso")
	if err := pspiso.BuildModified(output, manifest, os.DirFS(tree)); err == nil {
		t.Fatal("BuildModified accepted a missing replacement")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("BuildModified published output after failure: stat error = %v", err)
	}
}

func TestBuildModifiedAcceptsShrinkingReplacement(t *testing.T) {
	t.Parallel()

	manifest, tree := inspectAndExtractFixture(t)
	path := filepath.Join(tree, "PSP_GAME", "SYSDIR", "EBOOT.BIN")
	want := readFile(t, path)[:5]
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("shrink extracted payload: %v", err)
	}
	output := filepath.Join(t.TempDir(), "shrunk.iso")
	if err := pspiso.BuildModified(output, manifest, os.DirFS(tree)); err != nil {
		t.Fatalf("BuildModified rejected a shrinking replacement: %v", err)
	}
	image, err := pspiso.Open(output)
	if err != nil {
		t.Fatalf("Open shrunk image: %v", err)
	}
	defer image.Close()
	got, err := fs.ReadFile(image.PayloadFS(), "PSP_GAME/SYSDIR/EBOOT.BIN")
	if err != nil {
		t.Fatalf("read shrunk payload: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("shrunk payload differs from its replacement")
	}
}

func TestExtractDoesNotFollowParentSymlinkOutsideDestination(t *testing.T) {
	t.Parallel()

	source := writeFixture(t)
	image, err := pspiso.Open(source)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer image.Close()
	destination := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(destination, "PSP_GAME")); err != nil {
		t.Fatalf("create parent symlink: %v", err)
	}
	if err := image.Extract(destination); err == nil {
		t.Fatal("Extract followed a parent symlink outside its destination")
	}
	if _, err := os.Stat(filepath.Join(outside, "PARAM.SFO")); !os.IsNotExist(err) {
		t.Fatalf("Extract wrote through a parent symlink: stat error = %v", err)
	}
}

func TestBuildDoesNotReplaceExistingOutput(t *testing.T) {
	t.Parallel()

	manifest, tree := inspectAndExtractFixture(t)
	output := filepath.Join(t.TempDir(), "existing.iso")
	original := []byte("keep existing output")
	if err := os.WriteFile(output, original, 0o600); err != nil {
		t.Fatalf("write existing output: %v", err)
	}
	if err := pspiso.Build(output, manifest, os.DirFS(tree)); err == nil {
		t.Fatal("Build replaced an existing output")
	}
	if got := readFile(t, output); !bytes.Equal(got, original) {
		t.Fatalf("existing output changed to %q", got)
	}
}

func inspectAndExtractFixture(t *testing.T) (pspiso.Manifest, string) {
	t.Helper()
	source := writeFixture(t)
	image, err := pspiso.Open(source)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	manifest := image.Manifest()
	tree := t.TempDir()
	if err := image.Extract(tree); err != nil {
		_ = image.Close()
		t.Fatalf("Extract: %v", err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return manifest, tree
}

func writeFixture(t *testing.T) string {
	t.Helper()
	return writeBytes(t, "fixture.iso", fixtureBytes())
}

func writeBytes(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// fixtureBytes is deliberately handwritten: no production parsing or writing
// code participates in the test oracle. It models a compact PSP ISO9660 image
// with authored metadata and unallocated sectors that a faithful writer must
// retain.
func fixtureBytes() []byte {
	image := make([]byte, testSectors*testSectorSize)
	copy(image, "PSP ISO TEST SYSTEM AREA")
	for sector := 14; sector < 16; sector++ {
		for i := range image[sector*testSectorSize : (sector+1)*testSectorSize] {
			image[sector*testSectorSize+i] = ' '
		}
	}

	pvd := image[16*testSectorSize : 17*testSectorSize]
	pvd[0] = 1
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	fill(pvd[8:40], "PSP GAME")
	fill(pvd[40:72], "PSPISO TEST")
	putBoth32(pvd[80:88], testSectors)
	putBoth16(pvd[120:124], 1)
	putBoth16(pvd[124:128], 1)
	putBoth16(pvd[128:132], testSectorSize)
	putBoth32(pvd[132:140], 40)
	binary.LittleEndian.PutUint32(pvd[140:144], 18)
	binary.LittleEndian.PutUint32(pvd[144:148], 19)
	binary.BigEndian.PutUint32(pvd[148:152], 20)
	binary.BigEndian.PutUint32(pvd[152:156], 21)
	copy(pvd[156:], primaryRootRecord())
	volumeTime := []byte("2024010203040500\x00")
	copy(pvd[813:830], volumeTime)
	copy(pvd[830:847], volumeTime)
	copy(pvd[847:864], []byte("0000000000000000\x00"))
	copy(pvd[864:881], []byte("0000000000000000\x00"))
	pvd[881] = 1

	terminator := image[17*testSectorSize : 18*testSectorSize]
	terminator[0] = 255
	copy(terminator[1:6], "CD001")
	terminator[6] = 1

	little := pathTable(binary.LittleEndian)
	big := pathTable(binary.BigEndian)
	copy(image[18*testSectorSize:], little)
	copy(image[19*testSectorSize:], little)
	copy(image[20*testSectorSize:], big)
	copy(image[21*testSectorSize:], big)

	root := image[22*testSectorSize : 23*testSectorSize]
	writeDirectory(root,
		directoryRecord(22, testSectorSize, 2, []byte{0}),
		directoryRecord(22, testSectorSize, 2, []byte{1}),
		directoryRecord(23, testSectorSize, 2, []byte("PSP_GAME")),
		directoryRecord(30, 20, 0, []byte("UMD_DATA.BIN")),
	)
	pspGame := image[23*testSectorSize : 24*testSectorSize]
	writeDirectory(pspGame,
		directoryRecord(23, testSectorSize, 2, []byte{0}),
		directoryRecord(22, testSectorSize, 2, []byte{1}),
		directoryRecord(24, testSectorSize, 2, []byte("SYSDIR")),
		directoryRecord(40, 16, 0, []byte("PARAM.SFO")),
	)
	sysdir := image[24*testSectorSize : 25*testSectorSize]
	writeDirectory(sysdir,
		directoryRecord(24, testSectorSize, 2, []byte{0}),
		directoryRecord(23, testSectorSize, 2, []byte{1}),
		directoryRecord(50, 19, 0, []byte("EBOOT.BIN")),
	)

	copy(image[30*testSectorSize:], "ULJM-TEST|FIXTURE|")
	copy(image[40*testSectorSize:], []byte{'\x00', 'P', 'S', 'F', "P"[0], 'T', 'E', 'S', 'T', '\x00'})
	copy(image[50*testSectorSize:], "EBOOT FIXTURE BYTES")
	return image
}

func fill(dst []byte, value string) {
	for i := range dst {
		dst[i] = ' '
	}
	copy(dst, value)
}

func putBoth16(dst []byte, value int) {
	binary.LittleEndian.PutUint16(dst[:2], uint16(value))
	binary.BigEndian.PutUint16(dst[2:4], uint16(value))
}

func putBoth32(dst []byte, value int) {
	binary.LittleEndian.PutUint32(dst[:4], uint32(value))
	binary.BigEndian.PutUint32(dst[4:8], uint32(value))
}

func directoryRecord(lba, size, flags int, name []byte) []byte {
	recordLength := 33 + len(name)
	if len(name)%2 == 0 {
		recordLength++
	}
	// The fixture uses nonstandard System Use data on every record. ISO9660
	// readers may ignore it; faithful authoring must nevertheless retain it.
	recordLength += 4
	record := make([]byte, recordLength)
	record[0] = byte(recordLength)
	record[1] = 0
	putBoth32(record[2:10], lba)
	putBoth32(record[10:18], size)
	copy(record[18:25], []byte{124, 1, 2, 3, 4, 5, 0})
	record[25] = byte(flags)
	record[26] = 0
	record[27] = 0
	putBoth16(record[28:32], 1)
	record[32] = byte(len(name))
	copy(record[33:], name)
	systemUseStart := 33 + len(name)
	if len(name)%2 == 0 {
		systemUseStart++
	}
	copy(record[systemUseStart:], "UXA!")
	return record
}

func primaryRootRecord() []byte {
	// The root record in the PVD occupies its fixed 34-byte field and thus
	// cannot carry the directory-sector System Use bytes used elsewhere here.
	record := directoryRecord(22, testSectorSize, 2, []byte{0})
	record = record[:34]
	record[0] = byte(len(record))
	return record
}

func writeDirectory(dst []byte, records ...[]byte) {
	offset := 0
	for _, record := range records {
		copy(dst[offset:], record)
		offset += len(record)
	}
}

func pathTable(order binary.ByteOrder) []byte {
	entries := [][]byte{
		pathTableEntry(order, []byte{0}, 22, 1),
		pathTableEntry(order, []byte("PSP_GAME"), 23, 1),
		pathTableEntry(order, []byte("SYSDIR"), 24, 2),
	}
	var out []byte
	for _, entry := range entries {
		out = append(out, entry...)
	}
	return out
}

func pathTableEntry(order binary.ByteOrder, name []byte, lba, parent int) []byte {
	length := 8 + len(name)
	if len(name)%2 == 1 {
		length++
	}
	entry := make([]byte, length)
	entry[0] = byte(len(name))
	entry[1] = 0
	order.PutUint32(entry[2:6], uint32(lba))
	order.PutUint16(entry[6:8], uint16(parent))
	copy(entry[8:], name)
	return entry
}
