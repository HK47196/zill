// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/HK47196/zill/internal/cdccontext"
	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/fixeddata"
	"github.com/HK47196/zill/internal/gamefmt/paa"
)

func TestBuildSelectsCompleteCrossBankScenesAndPreservesBranches(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/selected.cdc", payload: []byte("C2:35+0+0+0+0+0+0+0+0+0C5:3+35+1350035+1360001;C21:0{C20:1350035+2}C23:1350035+YE")},
		{name: "cdc/do/unselected.cdc", payload: []byte("C5:3+35+1360002;E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}

	result, err := cdccontext.Build(project, fixeddata.Terminology{}, pair, cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scenes) != 1 || result.Scenes[0].Member != "cdc/do/selected.cdc" {
		t.Fatalf("selected scenes = %#v", result.Scenes)
	}
	entries := result.Scenes[0].Entries
	if len(entries) != 5 {
		t.Fatalf("entries = %d, want the complete selected scene", len(entries))
	}
	if entries[1].MessageID != 1360001 {
		t.Fatalf("cross-bank message = %d, want 1360001", entries[1].MessageID)
	}
	if entries[2].Kind != "selection_option" || len(entries[2].Path) != 1 || entries[2].Guard != "C21:0" {
		t.Fatalf("branch context = %#v", entries[2])
	}
	if entries[0].SpeakerStatus != "inferred_from_associated_label" || entries[0].AssociatedLabelEnglish != "Tiana" {
		t.Fatalf("C5 association = %#v", entries[0])
	}
	if len(entries[0].Actors) != 1 || entries[0].Actors[0].Presence != "present" || entries[0].Actors[0].AssociatedLabelEnglish != "Tiana" {
		t.Fatalf("actor lifecycle = %#v", entries[0].Actors)
	}
}

func TestBuildDoesNotInferPresenceAcrossUnresolvedJumps(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/jump.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C69:1C3:2L1{C5:3+2+1350035;}E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, pair, cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	actors := result.Scenes[0].Entries[0].Actors
	if len(actors) != 1 || actors[0].Presence != "unknown" || actors[0].PresenceBasis != "unresolved_control_flow" {
		t.Fatalf("jump-sensitive actor lifecycle = %#v", actors)
	}
}

func TestBuildRejectsMalformedMessageConsumers(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/bad.cdc", payload: []byte("C20:1350035+0E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cdccontext.Build(project, fixeddata.Terminology{}, pair, cdccontext.Selector{Bank: 135, Record: -1}); err == nil {
		t.Fatal("malformed C20 was accepted")
	}
}

type fixtureMember struct {
	name    string
	payload []byte
}

func openPair(t *testing.T, members []fixtureMember) *paa.Pair {
	t.Helper()
	directory := t.TempDir()
	const headerSize = 0x20
	const recordSize = 0x10
	const nameSize = 0x20
	namesOffset := headerSize + len(members)*recordSize
	offsetTable := namesOffset + len(members)*nameSize
	index := make([]byte, offsetTable+len(members)*4)
	copy(index, []byte{'P', 'A', 'A', 0})
	binary.LittleEndian.PutUint32(index[8:], uint32(len(members)))
	binary.LittleEndian.PutUint32(index[16:], uint32(offsetTable))
	archive := make([]byte, 0x10)
	for memberIndex, member := range members {
		if len(member.name) >= nameSize {
			t.Fatalf("fixture member name is too long: %q", member.name)
		}
		record := headerSize + memberIndex*recordSize
		name := namesOffset + memberIndex*nameSize
		copy(index[name:], member.name)
		binary.LittleEndian.PutUint32(index[record:], uint32(name))
		binary.LittleEndian.PutUint32(index[record+4:], uint32(len(member.payload)))
		offset := align(len(archive))
		archive = append(archive, make([]byte, offset-len(archive))...)
		binary.LittleEndian.PutUint32(index[offsetTable+memberIndex*4:], uint32(offset))
		archive = append(archive, member.payload...)
	}
	archive = append(archive, make([]byte, align(len(archive))-len(archive))...)
	indexPath := filepath.Join(directory, "pa.bin")
	archivePath := filepath.Join(directory, "pa.arc")
	if err := os.WriteFile(indexPath, index, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	pair, err := paa.Open(indexPath, archivePath)
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

func align(value int) int {
	return (value + 0xf) &^ 0xf
}
