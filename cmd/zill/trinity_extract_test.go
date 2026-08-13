// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/HK47196/zill/internal/cp932"
)

func TestTrinityExtractDoesNotPublishWhenSelectedTextCannotBeDecoded(t *testing.T) {
	gameDir := filepath.Join(t.TempDir(), "TrinityEN")
	usrDir := filepath.Join(gameDir, "PS3_GAME", "USRDIR")
	trophyDir := filepath.Join(gameDir, "PS3_GAME", "TROPDIR", "NPWR01148_00")
	if err := os.MkdirAll(usrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trophyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTrinityLINKDATAFixture(t, usrDir)
	if err := os.WriteFile(filepath.Join(usrDir, "EBOOT.BIN"), []byte("SCE\x00fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "PS3_GAME", "PARAM.SFO"), trinitySFOFixture(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trophyDir, "TROPHY.TRP"), trinityTRPFixture(), 0o644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "extracted")
	var stdout, stderr bytes.Buffer
	if code := runTrinityExtract([]string{"--game-dir", gameDir, "--output", output}, &stdout, &stderr); code != 1 {
		t.Fatalf("runTrinityExtract code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "decode compressed entry 0") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("failed extraction published output: %v", err)
	}
	staging, err := filepath.Glob(filepath.Join(filepath.Dir(output), ".trinity-extract-stage-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(staging) != 0 {
		t.Fatalf("failed extraction left staging directories: %v", staging)
	}
}

func TestTrinityTextMemberInventoryIsTheAuthenticatedRetailSet(t *testing.T) {
	want := []struct {
		index int
		size  uint32
	}{{0, 490256}, {2, 62200}, {4, 9016}, {5, 2156472}, {6, 769300}, {14, 59684}, {58, 9048}, {275, 121328}}
	if len(trinityTextMembers) != len(want) {
		t.Fatalf("text member count = %d, want %d", len(trinityTextMembers), len(want))
	}
	for index, expected := range want {
		got := trinityTextMembers[index]
		if got.index != expected.index || got.english.size != expected.size || got.japanese.size == 0 {
			t.Errorf("text member %d = index %d EN size %d JP size %d, want index %d EN size %d and a JP size", index, got.index, got.english.size, got.japanese.size, expected.index, expected.size)
		}
	}
}

func TestTrinityExtractRejectsOversizedTextMemberBeforePublishing(t *testing.T) {
	gameDir := filepath.Join(t.TempDir(), "TrinityEN")
	usrDir := filepath.Join(gameDir, "PS3_GAME", "USRDIR")
	trophyDir := filepath.Join(gameDir, "PS3_GAME", "TROPDIR", "NPWR01148_00")
	if err := os.MkdirAll(usrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trophyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTrinityLINKDATAFixture(t, usrDir)
	indexPath := filepath.Join(usrDir, "LINKDATA.IDX")
	index, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint32(index[:4], ^uint32(0))
	if err := os.WriteFile(indexPath, index, 0o644); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(usrDir, "LINKDATA.BIN")
	bin, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint32(bin[4:8], ^uint32(0))
	if err := os.WriteFile(binPath, bin, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "PS3_GAME", "PARAM.SFO"), trinitySFOFixture(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trophyDir, "TROPHY.TRP"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "extracted")
	var stdout, stderr bytes.Buffer
	if code := runTrinityExtract([]string{"--game-dir", gameDir, "--output", output}, &stdout, &stderr); code != 1 {
		t.Fatalf("runTrinityExtract code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "want authenticated BLUS30503 retail size 490256") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("oversized member published output: %v", err)
	}
}

func TestTrinityELFCandidatesExcludeExecutableSections(t *testing.T) {
	const sectionTable = 64
	const dataOffset = 256
	data := make([]byte, 320)
	copy(data[:4], "\x7fELF")
	data[4], data[5] = 2, 2
	binary.BigEndian.PutUint16(data[16:18], 2)
	binary.BigEndian.PutUint16(data[18:20], 21)
	binary.BigEndian.PutUint64(data[40:48], sectionTable)
	binary.BigEndian.PutUint16(data[58:60], 64)
	binary.BigEndian.PutUint16(data[60:62], 3)
	allocated := data[sectionTable+64 : sectionTable+128]
	binary.BigEndian.PutUint32(allocated[4:8], 1)
	binary.BigEndian.PutUint64(allocated[8:16], 2)
	binary.BigEndian.PutUint64(allocated[24:32], dataOffset)
	binary.BigEndian.PutUint64(allocated[32:40], 16)
	executable := data[sectionTable+128 : sectionTable+192]
	binary.BigEndian.PutUint32(executable[4:8], 1)
	binary.BigEndian.PutUint64(executable[8:16], 6)
	binary.BigEndian.PutUint64(executable[24:32], dataOffset+16)
	binary.BigEndian.PutUint64(executable[32:40], 16)
	copy(data[dataOffset:], "Visible text\x00")
	copy(data[dataOffset+16:], "Hidden code\x00")

	candidates, err := trinityELFCandidates(data, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Offset != dataOffset || candidates[0].Text != "Visible text" || !candidates[0].TerminatedByNUL {
		t.Fatalf("trinityELFCandidates() = %#v", candidates)
	}
}

func TestTrinityELFCandidatesRejectOverlappingScannedSections(t *testing.T) {
	const sectionTable = 64
	const dataOffset = 256
	data := make([]byte, 320)
	copy(data[:4], "\x7fELF")
	data[4], data[5] = 2, 2
	binary.BigEndian.PutUint16(data[16:18], 2)
	binary.BigEndian.PutUint16(data[18:20], 21)
	binary.BigEndian.PutUint64(data[40:48], sectionTable)
	binary.BigEndian.PutUint16(data[58:60], 64)
	binary.BigEndian.PutUint16(data[60:62], 3)
	for index := 1; index <= 2; index++ {
		header := data[sectionTable+index*64 : sectionTable+(index+1)*64]
		binary.BigEndian.PutUint32(header[4:8], 1)
		binary.BigEndian.PutUint64(header[8:16], 2)
		binary.BigEndian.PutUint64(header[24:32], dataOffset)
		binary.BigEndian.PutUint64(header[32:40], 16)
	}
	copy(data[dataOffset:], "Repeated text\x00")
	if candidates, err := trinityELFCandidates(data, false); err == nil {
		t.Fatalf("accepted overlapping ELF sections with candidates %#v", candidates)
	}
}

func TestTrinityJapaneseCandidatesAreReadableCP932(t *testing.T) {
	candidates := trinityCandidates([]byte("\x93\xfa\x96\x7b\x8c\xea\x00"), true)
	if len(candidates) != 1 || candidates[0].Offset != 0 || candidates[0].Text != "日本語" || !candidates[0].TerminatedByNUL {
		t.Fatalf("trinityCandidates() = %#v", candidates)
	}
}

func TestTrinitySearchDecodesRetailJapaneseWaveDash(t *testing.T) {
	text, ok := decodeTrinitySearchText([]byte("\x81\x60\x81\x40\x88\xea\x94\x4e\x91\x4f\x81\x40\x81\x60"), true)
	if !ok || text != "～　一年前　～" {
		t.Fatalf("decodeTrinitySearchText() = %q, %t", text, ok)
	}
}

func TestTrinityStringPairsFollowRecordAndSlotAcrossLocalizedOffsets(t *testing.T) {
	english := trinityDirectTableMemberFixture([]string{"Fine match", "One year earlier"}, false)
	japanese := trinityDirectTableMemberFixture([]string{"いい試合", "一年前"}, true)
	pairs, err := pairTrinityMemberStrings(4, english, japanese)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 {
		t.Fatalf("pair count = %d, want 2: %#v", len(pairs), pairs)
	}
	if !slices.Equal(pairs[0].Path, []int{0, 0}) || pairs[0].English != "Fine match" || pairs[0].Japanese != "いい試合" || pairs[0].EnglishOffset != pairs[0].JapaneseOffset {
		t.Fatalf("first pair = %#v", pairs[0])
	}
	if !slices.Equal(pairs[1].Path, []int{0, 1}) || pairs[1].English != "One year earlier" || pairs[1].Japanese != "一年前" || pairs[1].EnglishOffset == pairs[1].JapaneseOffset {
		t.Fatalf("second pair did not preserve its slot across shifted offsets: %#v", pairs[1])
	}
}

func TestTrinityStringPairsExcludeReorderedRowsWithoutUniqueIdentity(t *testing.T) {
	englishMetadata := make([]byte, 1200*9)
	japaneseMetadata := make([]byte, 1200*9)
	englishMetadata[0], japaneseMetadata[0] = 1, 2
	englishLabels, japaneseLabels := make([][]byte, 135), make([][]byte, 135)
	englishLabels[0] = []byte("Novice historians\x00")
	japaneseLabel, err := cp932.Encode("歴史を語る男たち")
	if err != nil {
		t.Fatal(err)
	}
	japaneseLabels[0] = append(japaneseLabel, 0)
	auxiliary := make([]byte, 1200*4)
	englishRows, japaneseRows := make([][]byte, 1200), make([][]byte, 1200)
	for index := range englishRows {
		englishRows[index], japaneseRows[index] = []byte{0}, []byte{0}
	}
	englishRows[0], japaneseRows[0] = []byte("wrong English\x00"), []byte("wrong Japanese\x00")
	verifiedJapanese, err := cp932.Encode("対応する日本語")
	if err != nil {
		t.Fatal(err)
	}
	englishRows[1], japaneseRows[1] = []byte("verified English\x00"), append(verifiedJapanese, 0)
	english := trinitySizedTableFixture([][]byte{englishMetadata, trinityOffsetTableFixture(englishLabels), auxiliary, trinityOffsetTableFixture(englishRows)})
	japanese := trinitySizedTableFixture([][]byte{japaneseMetadata, trinityOffsetTableFixture(japaneseLabels), auxiliary, trinityOffsetTableFixture(japaneseRows)})

	pairs, err := pairTrinityMemberStrings(275, english, japanese)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || !slices.Equal(pairs[0].Path, []int{1, 0}) || pairs[0].English != "Novice historians" || pairs[0].Japanese != "歴史を語る男たち" {
		t.Fatalf("pairs = %#v", pairs)
	}
}

func trinityOffsetTableFixture(children [][]byte) []byte {
	headerSize := 4 + len(children)*4
	data := make([]byte, headerSize)
	binary.BigEndian.PutUint32(data, uint32(len(children)))
	for index, child := range children {
		if len(child) == 0 {
			child = []byte{0}
		}
		binary.BigEndian.PutUint32(data[4+index*4:], uint32(len(data)))
		data = append(data, child...)
	}
	return data
}

func trinitySizedTableFixture(children [][]byte) []byte {
	headerSize := 4 + len(children)*8
	data := make([]byte, headerSize)
	binary.BigEndian.PutUint32(data, uint32(len(children)))
	for index, child := range children {
		position := 4 + index*8
		binary.BigEndian.PutUint32(data[position:], uint32(len(data)))
		binary.BigEndian.PutUint32(data[position+4:], uint32(len(child)))
		data = append(data, child...)
	}
	return data
}

func trinityDirectTableMemberFixture(stringsFound []string, japanese bool) []byte {
	headerSize := 4 + len(stringsFound)*4
	record := make([]byte, headerSize)
	binary.BigEndian.PutUint32(record, uint32(len(stringsFound)))
	for index, text := range stringsFound {
		binary.BigEndian.PutUint32(record[4+index*4:], uint32(len(record)))
		var encoded []byte
		if japanese {
			encoded, _ = cp932.Encode(text)
		} else {
			encoded = []byte(text)
		}
		record = append(record, encoded...)
		record = append(record, 0)
	}
	data := make([]byte, 12)
	binary.BigEndian.PutUint32(data, 2)
	binary.BigEndian.PutUint32(data[4:], 12)
	binary.BigEndian.PutUint32(data[8:], uint32(12+len(record)))
	data = append(data, record...)
	return append(data, 0, 0)
}

func writeTrinityLINKDATAFixture(t *testing.T, directory string) {
	t.Helper()
	const memberCount = 6057
	index := make([]byte, memberCount*12)
	textMembers := make(map[int]uint32, len(trinityTextMembers))
	for _, member := range trinityTextMembers {
		textMembers[member.index] = member.english.size
	}
	var data []byte
	for member := 0; member < memberCount; member++ {
		record := index[member*12 : member*12+12]
		decodedSize, selected := textMembers[member]
		if !selected {
			continue
		}
		if member == 0 {
			binary.BigEndian.PutUint32(record[:4], decodedSize)
			binary.BigEndian.PutUint32(record[4:8], 0x100)
			binary.BigEndian.PutUint32(record[8:12], 1)
			payload := make([]byte, 0x800)
			binary.BigEndian.PutUint32(payload[:4], 1)
			binary.BigEndian.PutUint32(payload[4:8], decodedSize)
			binary.BigEndian.PutUint32(payload[8:12], 1)
			payload[0x80] = 0xff // valid layout, invalid raw-DEFLATE stream
			data = append(data, payload...)
			continue
		}
		binary.BigEndian.PutUint32(record[:4], decodedSize)
		binary.BigEndian.PutUint32(record[4:8], 8)
		binary.BigEndian.PutUint32(record[8:12], 1)
		payload := make([]byte, 0x800)
		data = append(data, payload...)
	}
	if err := os.WriteFile(filepath.Join(directory, "LINKDATA.IDX"), index, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "LINKDATA.BIN"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func trinitySFOFixture() []byte {
	const keyStart = 36
	const dataStart = 48
	data := make([]byte, 60)
	binary.LittleEndian.PutUint32(data[:4], 0x46535000)
	binary.LittleEndian.PutUint32(data[4:8], 0x00000101)
	binary.LittleEndian.PutUint32(data[8:12], keyStart)
	binary.LittleEndian.PutUint32(data[12:16], dataStart)
	binary.LittleEndian.PutUint32(data[16:20], 1)
	binary.LittleEndian.PutUint16(data[22:24], 0x0204)
	binary.LittleEndian.PutUint32(data[24:28], 10)
	binary.LittleEndian.PutUint32(data[28:32], 12)
	copy(data[keyStart:], "TITLE_ID\x00")
	copy(data[dataStart:], "BLUS30503\x00")
	return data
}

func trinityTRPFixture() []byte {
	contents := []byte("<trophyconf></trophyconf>\n")
	data := make([]byte, 0x80+len(contents))
	binary.BigEndian.PutUint32(data[:4], 0xdca24d00)
	binary.BigEndian.PutUint32(data[4:8], 1)
	binary.BigEndian.PutUint64(data[8:16], uint64(len(data)))
	binary.BigEndian.PutUint32(data[16:20], 1)
	binary.BigEndian.PutUint32(data[20:24], 0x40)
	copy(data[0x40:0x60], "TROP.SFM\x00")
	binary.BigEndian.PutUint64(data[0x60:0x68], 0x80)
	binary.BigEndian.PutUint64(data[0x68:0x70], uint64(len(contents)))
	copy(data[0x80:], contents)
	return data
}
