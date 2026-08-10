package sfo_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"

	"github.com/HK47196/zill/internal/gamefmt/sfo"
)

func TestProductionManifestParses(t *testing.T) {
	data, err := os.ReadFile("../../../patches/system/param-sfo.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sfo.ParseManifest(data); err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
}

func TestApplySetsTranslatedTitleAppendsMEMSIZEAndPreservesSource(t *testing.T) {
	source := oneEntrySFO("TITLE", 0x0204, []byte("Retail title\x00"), 64)
	manifest := syntheticManifest(source)
	original := append([]byte(nil), source...)
	title := "Zill O'll Infinite Plus [English v1.0-alpha]"

	got, err := sfo.Apply(source, manifest, title)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !bytes.Equal(source, original) {
		t.Fatal("Apply mutated the authenticated source")
	}
	if len(got)%16 != 0 {
		t.Fatalf("result length %d is not aligned to 16", len(got))
	}
	if count := binary.LittleEndian.Uint32(got[16:20]); count != 2 {
		t.Fatalf("entry count = %d, want 2", count)
	}
	keyStart := int(binary.LittleEndian.Uint32(got[8:12]))
	dataStart := int(binary.LittleEndian.Uint32(got[12:16]))
	if string(got[keyStart:keyStart+6]) != "TITLE\x00" {
		t.Fatal("existing TITLE key was not preserved")
	}
	first := got[20:36]
	if length := binary.LittleEndian.Uint32(first[4:8]); length != uint32(len(title)+1) {
		t.Fatalf("TITLE length = %d, want %d", length, len(title)+1)
	}
	second := got[36:52]
	memKeyOffset := int(binary.LittleEndian.Uint16(second[0:2]))
	if string(got[keyStart+memKeyOffset:keyStart+memKeyOffset+8]) != "MEMSIZE\x00" {
		t.Fatal("MEMSIZE was not appended to the key table")
	}
	if format := binary.LittleEndian.Uint16(second[2:4]); format != 0x0404 {
		t.Fatalf("MEMSIZE format = %#x, want 0x0404", format)
	}
	if value := binary.LittleEndian.Uint32(got[dataStart+int(binary.LittleEndian.Uint32(second[12:16])):]); value != 1 {
		t.Fatalf("MEMSIZE value = %d, want 1", value)
	}
	if string(got[dataStart:dataStart+len(title)+1]) != title+"\x00" {
		t.Fatalf("TITLE value = %q, want translated versioned title", got[dataStart:dataStart+len(title)+1])
	}
}

func TestExistingMEMSIZEFailsWithoutOutputOrMutation(t *testing.T) {
	source := oneEntrySFO("MEMSIZE", 0x0404, []byte{1, 0, 0, 0}, 4)
	manifest := syntheticManifest(source)
	original := append([]byte(nil), source...)

	got, err := sfo.Apply(source, manifest, "Zill O'll Infinite Plus [English v-test]")
	if err == nil || got != nil {
		t.Fatal("Apply accepted an already-present MEMSIZE entry")
	}
	if !bytes.Equal(source, original) {
		t.Fatal("Apply mutated source before rejecting existing MEMSIZE")
	}
}

func TestUnsupportedFingerprintFailsWithoutOutputOrMutation(t *testing.T) {
	source := oneEntrySFO("TITLE", 0x0204, []byte("Zill\x00"), 8)
	manifest := syntheticManifest(source)
	otherHash := sha256.Sum256([]byte("another PARAM.SFO"))
	manifest.SourceSHA256 = hex.EncodeToString(otherHash[:])
	original := append([]byte(nil), source...)

	got, err := sfo.Apply(source, manifest, "Zill O'll Infinite Plus [English v-test]")
	if err == nil || got != nil {
		t.Fatal("Apply exposed output for an unsupported PARAM.SFO fingerprint")
	}
	if !bytes.Equal(source, original) {
		t.Fatal("Apply mutated an unsupported PARAM.SFO")
	}
}

func TestMalformedTableFailsWithoutOutputOrMutation(t *testing.T) {
	source := oneEntrySFO("TITLE", 0x0204, []byte("Zill\x00"), 8)
	binary.LittleEndian.PutUint32(source[8:12], 35)
	manifest := syntheticManifest(source)
	original := append([]byte(nil), source...)

	got, err := sfo.Apply(source, manifest, "Zill O'll Infinite Plus [English v-test]")
	if err == nil || got != nil {
		t.Fatal("Apply accepted malformed table bounds")
	}
	if !bytes.Equal(source, original) {
		t.Fatal("Apply mutated malformed source")
	}
}

func oneEntrySFO(key string, format uint16, value []byte, maximumLength uint32) []byte {
	keyStart := 20 + 16
	keyTable := append([]byte(key), 0)
	for len(keyTable)%4 != 0 {
		keyTable = append(keyTable, 0)
	}
	dataStart := keyStart + len(keyTable)
	data := append([]byte(nil), value...)
	for len(data) < int(maximumLength) {
		data = append(data, 0)
	}
	result := make([]byte, 0, dataStart+len(data)+16)
	for _, field := range []uint32{0x46535000, 0x00000101, uint32(keyStart), uint32(dataStart), 1} {
		result = binary.LittleEndian.AppendUint32(result, field)
	}
	result = binary.LittleEndian.AppendUint16(result, 0)
	result = binary.LittleEndian.AppendUint16(result, format)
	result = binary.LittleEndian.AppendUint32(result, uint32(len(value)))
	result = binary.LittleEndian.AppendUint32(result, maximumLength)
	result = binary.LittleEndian.AppendUint32(result, 0)
	result = append(result, keyTable...)
	result = append(result, data...)
	for len(result)%16 != 0 {
		result = append(result, 0)
	}
	return result
}

func syntheticManifest(source []byte) sfo.Manifest {
	sum := sha256.Sum256(source)
	return sfo.Manifest{
		Format:            "zill-param-sfo-patch",
		Version:           1,
		Target:            "PARAM.SFO",
		SourceSHA256:      hex.EncodeToString(sum[:]),
		SourceSize:        len(source),
		Magic:             0x46535000,
		SFOVersion:        0x00000101,
		ExpectedAbsentKey: "MEMSIZE",
		AppendKey:         "MEMSIZE",
		EntryFormat:       0x0404,
		Value:             1,
		Alignment:         16,
	}
}
