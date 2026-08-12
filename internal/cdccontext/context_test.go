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
	if entries[0].Reachability != "supported" || entries[0].Actors[0].PresenceBasis != "cfg_lifecycle" {
		t.Fatalf("entry analysis = %#v", entries[0])
	}
}

func TestBuildFollowsJumpAndRetainsUnreachableAuthoredMessages(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/jump.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C69:1C3:2C5:3+2+1350036;L1{C5:3+2+1350035;}E")},
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
	entries := result.Scenes[0].Entries
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].MessageID != 1350036 || entries[0].Reachability != "unreachable" || len(entries[0].Actors) != 0 {
		t.Fatalf("jumped-over authored message = %#v", entries[0])
	}
	actors := entries[1].Actors
	if entries[1].MessageID != 1350035 || entries[1].Reachability != "supported" || len(actors) != 1 || actors[0].Presence != "present" || actors[0].PresenceBasis != "cfg_lifecycle" {
		t.Fatalf("jump target lifecycle = entry %#v, actors %#v", entries[1], actors)
	}
}

func TestBuildPropagatesLifecycleThroughCallAndReturn(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/call.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C70:1C5:3+2+1350035;RL1{C3:2C5:3+2+1350036;C71:}E")},
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
	entries := result.Scenes[0].Entries
	if len(entries) != 2 || entries[0].MessageID != 1350035 || entries[1].MessageID != 1350036 {
		t.Fatalf("source-ordered call scene = %#v", entries)
	}
	for _, entry := range entries {
		if entry.Reachability != "supported" || len(entry.Actors) != 1 || entry.Actors[0].Presence != "absent" || entry.Actors[0].PresenceBasis != "cfg_lifecycle" {
			t.Fatalf("call lifecycle = %#v", entry)
		}
	}
}

func TestBuildClearsTheSavedReturnWhenCallPathJumps(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/call-jump.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C70:1C5:3+2+1350036;RL1{C69:2}L2{C5:3+2+1350035;C71:}E")},
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
	entries := result.Scenes[0].Entries
	if len(entries) != 2 || entries[0].MessageID != 1350036 || entries[0].Reachability != "unreachable" {
		t.Fatalf("discarded call continuation = %#v", entries)
	}
	if entries[1].MessageID != 1350035 || entries[1].Reachability != "supported" || len(entries[1].Actors) != 1 || entries[1].Actors[0].Presence != "present" {
		t.Fatalf("jumped call target = %#v", entries[1])
	}
}

func TestBuildResolvesDuplicateLabelsInTheNearestVisibleScope(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/scoped-label.cdc", payload: []byte("C0:0+0+O{L1{C2:3R}}C2:2+0+0+0+0+0+0+0+0+0C70:1C5:3+2+1350035;RL1{C3:2C71:}E")},
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
	entry := result.Scenes[0].Entries[0]
	if entry.Reachability != "supported" || len(entry.Actors) != 1 || entry.Actors[0].Handle != 2 || entry.Actors[0].Presence != "absent" {
		t.Fatalf("scoped label lifecycle = %#v", entry)
	}
}

func TestBuildConvergesAndJoinsLifecycleAcrossBackwardJump(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/loop.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C69:1L1{C5:3+2+1350035;C3:2C69:1}E")},
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
	entry := result.Scenes[0].Entries[0]
	if entry.Reachability != "supported" || len(entry.Actors) != 1 || entry.Actors[0].Presence != "unknown" || entry.Actors[0].PresenceBasis != "state_disagreement" {
		t.Fatalf("loop join = %#v", entry)
	}
}

func TestBuildKeepsChoiceArmsPathSensitive(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/choice.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C20:1350036+2{C21:0{C3:2}C21:1{C5:3+2+1350035;}}E")},
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
	entries := result.Scenes[0].Entries
	if len(entries) != 3 {
		t.Fatalf("entries = %#v", entries)
	}
	target := entries[2]
	if target.MessageID != 1350035 || target.Reachability != "supported" || len(target.Actors) != 1 || target.Actors[0].Presence != "present" || target.Actors[0].PresenceBasis != "cfg_lifecycle" {
		t.Fatalf("choice-specific lifecycle = %#v", target)
	}
}

func TestBuildDoesNotTreatPlacementAsCreation(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/place.cdc", payload: []byte("C6:2+0+0+0+0+0+0+0+0C5:3+2+1350035;E")},
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
	entry := result.Scenes[0].Entries[0]
	if len(entry.Actors) != 1 || entry.Actors[0].Presence != "unknown" || entry.Actors[0].PresenceBasis != "insufficient_lifecycle_evidence" {
		t.Fatalf("placement lifecycle = %#v", entry)
	}
}

func TestBuildDoesNotEraseAStateDisagreementAtPlacement(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/place-join.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C0:0+0+O{C3:2}C6:2+0+0+0+0+0+0C5:3+2+1350035;E")},
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
	entry := result.Scenes[0].Entries[0]
	if len(entry.Actors) != 1 || entry.Actors[0].Presence != "unknown" || entry.Actors[0].PresenceBasis != "state_disagreement" {
		t.Fatalf("placement after lifecycle join = %#v", entry)
	}
}

func TestBuildScopesUnsupportedFlowToTheAffectedBranch(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/unsupported.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C0:0+0+O{C71:}C5:3+2+1350035;E")},
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
	entry := result.Scenes[0].Entries[0]
	if entry.Reachability != "mixed" || len(entry.Actors) != 1 || entry.Actors[0].Presence != "present" {
		t.Fatalf("scoped unsupported flow = %#v", entry)
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

func TestBuildReturnsTheCompleteBankAsAStaticSceneWhenNoConsumerReferencesRecord(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/unrelated.cdc", payload: []byte("C5:3+2+1350035;E")},
		{name: "message/msgsec034.dat", payload: messageBankFixture(73, map[int]int{8: 415})},
		{name: "message/msgsec036.dat", payload: messageBankFixture(100, nil)},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}

	result, err := cdccontext.Build(project, fixeddata.Terminology{}, pair, cdccontext.Selector{Bank: -1, Record: 340008})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scenes) != 1 {
		t.Fatalf("scenes = %#v", result.Scenes)
	}
	scene := result.Scenes[0]
	if scene.Member != "message/msgsec034.dat" || scene.SourceKind != "message_bank" || scene.Ordering != "storage_order_only" || scene.EvidenceStatus != "no_resolved_static_consumer_reference" {
		t.Fatalf("bank scene provenance = %#v", scene)
	}
	if scene.FirstRecordMessageID == nil || *scene.FirstRecordMessageID != 340000 || scene.FirstRecordJapanese != "旅立ち０７メッセージ<end>" {
		t.Fatalf("bank first record = %#v", scene)
	}
	if len(scene.SourceEvidence) != 1 {
		t.Fatalf("source evidence = %#v", scene.SourceEvidence)
	}
	evidence := scene.SourceEvidence[0]
	if evidence.Kind != "scenario_reserve_marker" || evidence.Status != "source_authoring_candidate" || evidence.Confidence != "low" || evidence.EventNumber != 7 || evidence.MarkerLabel != "バーニン親子鷹" || evidence.RuntimeStatus != "unresolved" || len(evidence.MarkerMessageIDs) != 10 || len(evidence.Candidates) != 1 || evidence.Candidates[0].MessageID != 1940007 || evidence.Candidates[0].LabelMatch {
		t.Fatalf("scenario evidence = %#v", evidence)
	}
	entries := scene.Entries
	expected := 0
	for _, item := range project.Items {
		if item.Translation.ID/10_000 == 34 {
			expected++
		}
	}
	if len(entries) != expected {
		t.Fatalf("bank entries = %d, want complete bank of %d records", len(entries), expected)
	}
	var target *cdccontext.Entry
	for index := range entries {
		if entries[index].MessageID == 340008 {
			target = &entries[index]
			break
		}
	}
	if target == nil || target.Kind != "bank_record" || target.Reachability != "unresolved" || target.English == "" || !target.Selected || target.Offset != 415 || target.OffsetBasis != "message_bank_byte_offset" || target.SpeakerStatus != "" {
		t.Fatalf("target fallback context = %#v", target)
	}
	conditional := entries[17]
	if conditional.MessageID != 340017 || len(conditional.SourceControls) != 1 || conditional.SourceControls[0].Kind != "conditional" || conditional.SourceControls[0].Evidence != "retail_message_bytecode" || len(conditional.SourceControls[0].Blocks) != 2 || conditional.SourceControls[0].Blocks[0].Condition != "<value:$29><equal>%0" || conditional.SourceControls[0].Blocks[1].Role != "fallback" {
		t.Fatalf("record-local source controls = %#v", conditional.SourceControls)
	}
	for _, entry := range entries {
		if entry.MessageID != 340008 && entry.Selected {
			t.Fatalf("non-target record was selected: %#v", entry)
		}
	}

	exact, err := cdccontext.Build(project, fixeddata.Terminology{}, pair, cdccontext.Selector{Bank: -1, Record: 360001})
	if err != nil {
		t.Fatal(err)
	}
	if len(exact.Scenes) != 1 || len(exact.Scenes[0].SourceEvidence) != 1 {
		t.Fatalf("exact source evidence scenes = %#v", exact.Scenes)
	}
	exactEvidence := exact.Scenes[0].SourceEvidence[0]
	if exactEvidence.Status != "source_authoring_match" || exactEvidence.Confidence != "high" || exactEvidence.EventNumber != 9 || len(exactEvidence.Candidates) != 1 || exactEvidence.Candidates[0].MessageID != 1940009 || !exactEvidence.Candidates[0].LabelMatch || exactEvidence.Basis != "reserve_marker_event_number_and_title" {
		t.Fatalf("exact source evidence = %#v", exactEvidence)
	}
}

type fixtureMember struct {
	name    string
	payload []byte
}

func messageBankFixture(recordCount int, forcedOffsets map[int]int) []byte {
	tableEnd := 2 + recordCount*2
	offsets := make([]int, recordCount)
	next := tableEnd
	for index := range offsets {
		if forced, ok := forcedOffsets[index]; ok && forced > next {
			next = forced
		}
		offsets[index] = next
		next++
	}
	data := make([]byte, next)
	binary.LittleEndian.PutUint16(data, uint16(recordCount))
	for index, offset := range offsets {
		binary.LittleEndian.PutUint16(data[2+index*2:], uint16(offset))
	}
	return data
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
