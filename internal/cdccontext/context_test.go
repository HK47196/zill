// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
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
	if entries[0].PortraitRequested == nil || !*entries[0].PortraitRequested || entries[0].NameLabelRequested == nil || !*entries[0].NameLabelRequested || entries[0].ForcedStateThree == nil || *entries[0].ForcedStateThree || entries[0].PortraitStatus != "requested_availability_unresolved" {
		t.Fatalf("C5 requested display features = %#v", entries[0])
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
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
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
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
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
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
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
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
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
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
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
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
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
	if len(target.Conditions) != 2 || target.Conditions[0].Kind != "choice_context" || target.Conditions[0].OptionCount == nil || *target.Conditions[0].OptionCount != 2 || target.Conditions[1].Kind != "choice_result_equals" || target.Conditions[1].SelectedIndex == nil || *target.Conditions[1].SelectedIndex != 1 {
		t.Fatalf("choice condition semantics = %#v", target.Conditions)
	}
}

func TestBuildCreatesABoundedTargetReviewPacketWithAlternateChoiceArms(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/review.cdc", payload: []byte("C20:1350038+2{C21:0{C5:3+2+1350033;}}C5:3+2+1350037;C20:1350038+2{C21:0{C5:3+2+1350036;}C21:1{C5:3+2+1350035;}}C5:3+2+1350034;E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ReviewPackets) != 1 {
		t.Fatalf("review packets = %#v", result.ReviewPackets)
	}
	packet := result.ReviewPackets[0]
	if packet.TargetMessageID != 1350035 || packet.SceneMember != "cdc/do/review.cdc" || packet.SourceArchive != "pa" || len(packet.Context) != 1 || packet.Context[0].Role != "target" {
		t.Fatalf("review packet = %#v", packet)
	}
	if len(packet.AlternateArms) != 1 || packet.AlternateArms[0].Role != "alternate_choice_arm" || packet.AlternateArms[0].Entry.MessageID != 1350036 {
		t.Fatalf("alternate choice arms = %#v", packet.AlternateArms)
	}
	if len(result.Scenes[0].Entries) != 9 {
		t.Fatalf("lossless scene was filtered: %#v", result.Scenes[0].Entries)
	}
}

func TestBuildBoundsReviewReferencesAndAlternateArmEntries(t *testing.T) {
	var payload strings.Builder
	for argument := 1; argument <= 20; argument++ {
		fmt.Fprintf(&payload, "C76:%d", argument)
	}
	payload.WriteString("C20:1350038+2{C21:0{")
	for id := 1350000; id < 1350015; id++ {
		fmt.Fprintf(&payload, "C5:3+2+%d;", id)
	}
	payload.WriteString("}C21:1{C5:3+2+1350035;}}E")
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/bounded-review.cdc", payload: []byte(payload.String())},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	packet := result.ReviewPackets[0]
	if len(packet.References) != 12 || packet.ReferencesOmitted != 8 {
		t.Fatalf("bounded references = %d + %d omitted", len(packet.References), packet.ReferencesOmitted)
	}
	if len(packet.AlternateArms) != 12 || packet.AlternateArmsOmitted != 3 {
		t.Fatalf("bounded alternates = %d + %d omitted", len(packet.AlternateArms), packet.AlternateArmsOmitted)
	}
	if len(result.Scenes[0].References) != 20 || len(result.Scenes[0].Entries) != 18 {
		t.Fatalf("full scene was bounded with review: references=%d entries=%d", len(result.Scenes[0].References), len(result.Scenes[0].Entries))
	}
}

func TestBuildExplainsVerifiedComparatorsWithoutGuessingSelectorMeanings(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/conditions.cdc", payload: []byte("C32:2+57+5+Q{C36:1+4+U{C45:3+6+W{C58:2+c15+N{C5:3+2+1350035;}}}}E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	conditions := result.Scenes[0].Entries[0].Conditions
	if len(conditions) != 4 {
		t.Fatalf("conditions = %#v", conditions)
	}
	if conditions[0].Kind != "runtime_compare" || conditions[0].Status != "verified_block_skip_behavior" || conditions[0].Comparator != "equal" || conditions[0].Operand1Raw == nil || *conditions[0].Operand1Raw != 2 || conditions[0].Operand2Raw == nil || *conditions[0].Operand2Raw != 57 || conditions[0].EntitySelectorRaw != nil {
		t.Fatalf("C32 semantics = %#v", conditions[0])
	}
	if conditions[1].Kind != "runtime_existence_or_compare" || conditions[1].Comparator != "greater_or_equal" || conditions[1].Operand1Raw == nil || *conditions[1].Operand1Raw != 1 || conditions[1].SelectorRaw != nil {
		t.Fatalf("C36 semantics = %#v", conditions[1])
	}
	if conditions[2].Kind != "runtime_compare" || conditions[2].Comparator != "less_or_equal" {
		t.Fatalf("C45 semantics = %#v", conditions[2])
	}
	if conditions[3].Kind != "entity_predicate" || conditions[3].PredicateFamilyRaw != "c" || conditions[3].PredicateIndexRaw == nil || *conditions[3].PredicateIndexRaw != 15 || conditions[3].Polarity != "negated" {
		t.Fatalf("C58 semantics = %#v", conditions[3])
	}
}

func TestBuildStructuresOtherVerifiedPredicatesWithoutNamingUnknownValues(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/other-predicates.cdc", payload: []byte("C75:4+O{C80:33+45+Q{C5:3+2+1350035;}}E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	conditions := result.Scenes[0].Entries[0].Conditions
	if len(conditions) != 2 || conditions[0].Kind != "slot_predicate" || conditions[0].SlotRaw == nil || *conditions[0].SlotRaw != 4 || conditions[0].Polarity != "negated" || conditions[0].Status != "verified_block_skip_behavior" {
		t.Fatalf("C75 condition = %#v", conditions)
	}
	if conditions[1].Kind != "entity_subselector_condition" || conditions[1].EntitySelectorRaw == nil || *conditions[1].EntitySelectorRaw != 33 || conditions[1].Operand2Raw == nil || *conditions[1].Operand2Raw != 45 || conditions[1].ComparatorRaw != "Q" || conditions[1].Comparator != "" || conditions[1].Status != "verified_block_skip_behavior" {
		t.Fatalf("C80 condition = %#v", conditions[1])
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
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	entry := result.Scenes[0].Entries[0]
	if len(entry.Actors) != 1 || entry.Actors[0].Presence != "unknown" || entry.Actors[0].PresenceBasis != "insufficient_lifecycle_evidence" {
		t.Fatalf("placement lifecycle = %#v", entry)
	}
}

func TestBuildCarriesPathStableStagingEvidenceToDialogue(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/staging.cdc", payload: []byte("C2:2+100+200+0+0+0+0+0+0+0C6:2+300+400+0+0+0+0C17:2+24+O+9+WC18:2+7C5:2+2+1350035;E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	entry := result.Scenes[0].Entries[0]
	if entry.PortraitRequested == nil || *entry.PortraitRequested || entry.NameLabelRequested == nil || !*entry.NameLabelRequested || entry.PortraitStatus != "not_requested" {
		t.Fatalf("mode-2 display request = %#v", entry)
	}
	if len(entry.Actors) != 1 {
		t.Fatalf("actors = %#v", entry.Actors)
	}
	actor := entry.Actors[0]
	if actor.Position == nil || actor.Position.Component2 != 300 || actor.Position.Component3 != 400 || actor.Position.Source != "C6" || actor.Position.Status != "coordinate_like_pair" {
		t.Fatalf("position evidence = %#v", actor.Position)
	}
	if actor.Action == nil || actor.Action.ActionIDRaw != 24 || actor.Action.ModifierRaw == nil || *actor.Action.ModifierRaw != 9 || !actor.Action.OptionO || actor.Action.C5AssociationBehaviorFlag != "W" || actor.Action.Status != "per_entity_action" {
		t.Fatalf("action evidence = %#v", actor.Action)
	}
	if actor.Relation == nil || actor.Relation.ModeOrValueRaw != 7 || actor.Relation.Status != "opaque_per_entity_state" {
		t.Fatalf("relation evidence = %#v", actor.Relation)
	}
}

func TestBuildRanksOtherPresentActorsAsPossibleAddresseesWithoutConfirmingThem(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/addressee.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C2:3+0+0+0+0+0+0+0+0+0C5:3+2+1350035;E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	candidates := result.Scenes[0].Entries[0].PossibleAddressees
	if len(candidates) != 1 || candidates[0].Handle != 3 || candidates[0].Confidence != "low" || candidates[0].Status != "possible" || candidates[0].Evidence != "path_local_present_actor_other_than_c5_association" {
		t.Fatalf("possible addressees = %#v", candidates)
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
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	entry := result.Scenes[0].Entries[0]
	if len(entry.Actors) != 1 || entry.Actors[0].Presence != "unknown" || entry.Actors[0].PresenceBasis != "state_disagreement" {
		t.Fatalf("placement after lifecycle join = %#v", entry)
	}
}

func TestBuildTreatsInactiveC71AsAVerifiedNoOp(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/unsupported.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C0:0+0+O{C71:}C5:3+2+1350035;E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	entry := result.Scenes[0].Entries[0]
	if entry.Reachability != "supported" || len(entry.Actors) != 1 || entry.Actors[0].Presence != "present" {
		t.Fatalf("inactive C71 flow = %#v", entry)
	}
}

func TestBuildDoesNotInventAFalseBranchForC1ActionScope(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/action-scope.cdc", payload: []byte("C2:2+0+0+0+0+0+0+0+0+0C1:0+4+F{C3:2}C5:3+2+1350035;E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	entry := result.Scenes[0].Entries[0]
	if entry.Reachability != "supported" || len(entry.Actors) != 1 || entry.Actors[0].Presence != "absent" {
		t.Fatalf("C1 action scope created an optional branch: %#v", entry)
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
	if _, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: 135, Record: -1}); err == nil {
		t.Fatal("malformed C20 was accepted")
	}
}

func TestBuildAddsVerifiedExecutableRelationshipsWithoutChangingReachability(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/relationship.cdc", payload: []byte("C5:3+2+467;C5:3+2+451;E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 467})
	if err != nil {
		t.Fatal(err)
	}
	entry := result.Scenes[0].Entries[0]
	if entry.Reachability != "supported" || len(entry.Relationships) != 2 {
		t.Fatalf("relationship entry = %#v", entry)
	}
	if entry.Relationships[0].Kind != "same_selector_companion" || entry.Relationships[0].TargetMessage != 160048 || entry.Relationships[1].TargetMessage != 160049 || entry.Relationships[0].Status != "verified_executable_formula" || entry.Relationships[0].RuntimeStatus != "selection_runtime_dependent" || entry.Relationships[0].TargetJapanese == "" {
		t.Fatalf("relationships = %#v", entry.Relationships)
	}
	if len(entry.ConsumerEvidence) != 1 || entry.ConsumerEvidence[0].Disposition != "verified_consumer" || entry.ConsumerEvidence[0].Role != "location_label" || entry.ConsumerEvidence[0].Confidence != "high" {
		t.Fatalf("source consumer evidence = %#v", entry.ConsumerEvidence)
	}
	if len(result.Scenes[0].Entries[1].Relationships) != 0 || len(result.Scenes[0].Entries[1].ConsumerEvidence) != 0 {
		t.Fatalf("unresolved record gained a relationship: %#v", result.Scenes[0].Entries[1].Relationships)
	}
}

func TestBuildPreservesBothVerifiedUsesOfMixedEquipmentNameRecords(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/mixed-consumer.cdc", payload: []byte("C5:3+2+1449;C5:3+2+1136;E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1449})
	if err != nil {
		t.Fatal(err)
	}
	evidence := result.Scenes[0].Entries[0].ConsumerEvidence
	if len(evidence) != 2 || evidence[0].Role != "equipment_name" || evidence[1].Role != "name_label" || evidence[0].Category != "mixed-use" || evidence[1].Category != "mixed-use" {
		t.Fatalf("mixed consumer evidence = %#v", evidence)
	}
	nonDisplay := result.Scenes[0].Entries[1].ConsumerEvidence
	if len(nonDisplay) != 1 || nonDisplay[0].Disposition != "verified_non_display" || nonDisplay[0].RuntimeStatus != "not_displayed_by_bounded_resolvers" {
		t.Fatalf("affirmative non-display evidence = %#v", nonDisplay)
	}
}

func TestBuildKeepsDynamicBankAndAuthoringTableEvidenceDistinctFromRuntimeEdges(t *testing.T) {
	pair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/evidence.cdc", payload: []byte("C5:3+2+1590000;C5:3+2+1950001;E")},
	})
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	dynamic, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1590000})
	if err != nil {
		t.Fatal(err)
	}
	if len(dynamic.Scenes[0].SourceEvidence) != 1 || dynamic.Scenes[0].SourceEvidence[0].Kind != "dynamic_bank_route" || dynamic.Scenes[0].SourceEvidence[0].RuntimeStatus != "record_selector_unresolved" {
		t.Fatalf("dynamic bank evidence = %#v", dynamic.Scenes[0].SourceEvidence)
	}
	authoring, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1950001})
	if err != nil {
		t.Fatal(err)
	}
	entry := authoring.Scenes[0].Entries[1]
	if len(authoring.Scenes[0].SourceEvidence) != 1 || authoring.Scenes[0].SourceEvidence[0].Kind != "source_authoring_table" || entry.AuthoringMetadata == nil || entry.AuthoringMetadata.TableKind != "event_selector_label" || entry.AuthoringMetadata.RuntimeStatus != "unresolved" || len(entry.Relationships) != 0 {
		t.Fatalf("authoring evidence = scene %#v entry %#v", authoring.Scenes[0].SourceEvidence, entry)
	}
}

func TestBuildExposesGroupDependentScenarioCandidatesWithoutChoosingAnActiveGroup(t *testing.T) {
	members := []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/selected.cdc", payload: []byte("C12:2+0C14:2C14:1003C76:858C5:3+2+1350035;E")},
	}
	for slot := 1; slot <= 914; slot++ {
		members = append(members, fixtureMember{name: fmt.Sprintf("cdc/01/s%04d.cdc", slot), payload: []byte("E")})
	}
	pair := openPair(t, members)
	defer pair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, oneArchive(pair), cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	references := result.Scenes[0].References
	if len(references) != 4 {
		t.Fatalf("references = %#v", references)
	}
	conditional := references[0]
	if conditional.Opcode != "C12" || conditional.ExecutionStatus != "runtime_dependent" || conditional.ResolutionStatus != "group_runtime_dependent" || conditional.CandidateGroupsFound != 1 || conditional.CandidateGroupsExpected != 13 || len(conditional.ScenarioCandidates) != 1 || conditional.ScenarioCandidates[0].Member != "cdc/01/s0002.cdc" || conditional.ScenarioCandidates[0].Confidence != "archive_order_correlation" {
		t.Fatalf("conditional scenario candidates = %#v", conditional)
	}
	direct := references[1]
	if direct.Opcode != "C14" || direct.ExecutionStatus != "direct_request" || len(direct.ScenarioCandidates) != 1 {
		t.Fatalf("direct scenario candidates = %#v", direct)
	}
	room := references[2]
	if room.ScenarioSlotTableIndex == nil || *room.ScenarioSlotTableIndex != 3 || room.ResolutionStatus != "room_runtime_dependent" || len(room.ScenarioCandidates) != 0 {
		t.Fatalf("room-dependent scenario reference = %#v", room)
	}
	resource := references[3]
	if resource.Resource == nil || resource.Resource.LogicalKey != "cdcDo/ID0858" || resource.ResolutionStatus != "logical_key_only" || len(resource.ScenarioCandidates) != 0 {
		t.Fatalf("logical resource reference = %#v", resource)
	}
}

func TestBuildReturnsTheCompleteBankAsAStaticSceneWhenNoConsumerReferencesRecord(t *testing.T) {
	paPair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/unrelated.cdc", payload: []byte("C5:3+2+1350035;E")},
	})
	defer paPair.Close()
	pamiPair := openPair(t, []fixtureMember{
		{name: "message/msgsec034.dat", payload: messageBankFixture(73, map[int]int{8: 415})},
		{name: "message/msgsec036.dat", payload: messageBankFixture(100, nil)},
	})
	defer pamiPair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}

	archives := []cdccontext.Archive{{Name: "pa", Pair: paPair}, {Name: "pami", Pair: pamiPair}}
	result, err := cdccontext.Build(project, fixeddata.Terminology{}, archives, cdccontext.Selector{Bank: -1, Record: 340008})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scenes) != 1 {
		t.Fatalf("scenes = %#v", result.Scenes)
	}
	scene := result.Scenes[0]
	if scene.Member != "message/msgsec034.dat" || scene.SourceArchive != "pami" || scene.SourceKind != "message_bank" || scene.Ordering != "storage_order_only" || scene.EvidenceStatus != "no_resolved_static_consumer_reference" {
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

	exact, err := cdccontext.Build(project, fixeddata.Terminology{}, archives, cdccontext.Selector{Bank: -1, Record: 360001})
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

func TestBuildRejectsDuplicateMessageBanksAcrossRetailArchives(t *testing.T) {
	paPair := openPair(t, []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/unrelated.cdc", payload: []byte("E")},
		{name: "message/msgsec034.dat", payload: messageBankFixture(73, nil)},
	})
	defer paPair.Close()
	pamiPair := openPair(t, []fixtureMember{
		{name: "message/msgsec034.dat", payload: messageBankFixture(73, nil)},
	})
	defer pamiPair.Close()
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	_, err = cdccontext.Build(project, fixeddata.Terminology{}, []cdccontext.Archive{{Name: "pa", Pair: paPair}, {Name: "pami", Pair: pamiPair}}, cdccontext.Selector{Bank: -1, Record: 340008})
	if err == nil || !strings.Contains(err.Error(), "duplicate message/msgsec034.dat") {
		t.Fatalf("duplicate bank error = %v", err)
	}
}

type fixtureMember struct {
	name    string
	payload []byte
}

func oneArchive(pair *paa.Pair) []cdccontext.Archive {
	return []cdccontext.Archive{{Name: "pa", Pair: pair}}
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
