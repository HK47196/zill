// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HK47196/zill/internal/cdccontext"
	"github.com/HK47196/zill/internal/corpus"
)

func TestBankContextDefaultsToASceneCatalogue(t *testing.T) {
	result := cdccontext.Result{
		Selector: cdccontext.Selector{Bank: 3, Record: -1},
		Scenes: []cdccontext.Scene{
			{
				ID: "bank/003", Member: "message/msgsec003.dat", SourceArchive: "pami",
				SourceKind: "message_bank", Ordering: "storage_order_only", EvidenceStatus: "retail_storage_source",
				Entries: []cdccontext.Entry{
					{MessageID: 30020, State: corpus.Translated, Japanese: "storage twenty", English: "Storage twenty"},
					{MessageID: 30021, State: corpus.Translated, Japanese: "storage twenty-one", English: "Storage twenty-one"},
					{MessageID: 30022, State: corpus.Todo, Japanese: "storage twenty-two"},
				},
			},
			{
				ID: "scenario/20/7a4fa780fc60", Aliases: []string{"pa:cdc/01/ancsri01.cdc"},
				Member: "cdc/01/ancsri01.cdc", SourceArchive: "pa", SourceKind: "cdc_program",
				Ordering: "source_order_with_static_control_flow", EvidenceStatus: "static_consumer_reference",
				Scenario: &cdccontext.ScenarioScene{Slot: 20, ContentSHA256: strings.Repeat("7a", 32), EquivalentGroups: []string{"01", "02"}},
				Entries: []cdccontext.Entry{{
					Kind: "dialogue_association", MessageID: 30021, State: corpus.Translated,
					SpeakerEnglish: "Notun High Priest", SpeakerJapanese: "ノトゥーン神官長",
					SpeakerStatus: "inferred_from_associated_label", Japanese: "scene Japanese", English: "scene English",
				}, {
					Kind: "notification", MessageID: 1470163, State: corpus.KeepJapanese,
					Japanese: "other-bank Japanese",
				}},
			},
		},
	}
	var output bytes.Buffer
	writeContextReviewText(&output, result, "PSP_GAME")
	text := output.String()
	for _, wanted := range []string{
		"Bank 003", "Records: 3", "Recovered scenes: 1", "scenario/20/7a4fa780fc60",
		"Records: 30021 · other scene records: 1470163", "Speakers: Notun High Priest / ノトゥーン神官長",
		"Storage-only records: 30020, 30022", "Storage container: bank/003", "--scene scenario/20/7a4fa780fc60",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("scene catalogue does not contain %q:\n%s", wanted, text)
		}
	}
	for _, unwanted := range []string{"scene Japanese", "offset=", "Static references", "content_sha256="} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("default bank catalogue contains diagnostic/full-scene text %q:\n%s", unwanted, text)
		}
	}
}

func TestGlobalSceneCatalogueListsEveryRecoveredSceneCompactly(t *testing.T) {
	result := cdccontext.Result{
		Selector: cdccontext.Selector{Bank: -1, Record: -1, ListScenes: true},
		Scenes: []cdccontext.Scene{{
			ID: "cdc/pa/cdc/do/example.cdc", Member: "cdc/do/example.cdc", SourceArchive: "pa",
			SourceKind: "cdc_program", Ordering: "source_order_with_static_control_flow",
			Entries: []cdccontext.Entry{{
				MessageID: 1350035, State: corpus.Translated, Japanese: "hidden Japanese", English: "hidden English",
				SpeakerEnglish: "Via", SpeakerStatus: "inferred_from_associated_label",
			}},
		}, {
			ID: "ambient/pami/room/id0020.par#anctsrni.imd", Member: "room/id0020.par", EmbeddedMember: "anctsrni.imd",
			SourceArchive: "pami", SourceKind: "ambient_interaction", Ordering: "room_entity_record_order",
			Entries: []cdccontext.Entry{{MessageID: 30028, State: corpus.Translated}},
		}},
	}
	var output bytes.Buffer
	writeContextReviewText(&output, result, "PSP_GAME")
	text := output.String()
	for _, wanted := range []string{
		"Recovered scenes: 2", "Scene IDs (first column; pass one to --scene):",
		"cdc/pa/cdc/do/example.cdc\tCDC dialogue scene\trecords=1350035\tspeakers=Via",
		"ambient/pami/room/id0020.par#anctsrni.imd\tambient interaction group\trecords=30028",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("global scene catalogue does not contain %q:\n%s", wanted, text)
		}
	}
	for _, unwanted := range []string{"hidden Japanese", "hidden English", "Dialogue:", "offset="} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("global scene catalogue contains expanded scene data %q:\n%s", unwanted, text)
		}
	}

	document := buildContextReviewDocument(result, "PSP_GAME")
	if document.View != "scene_catalogue" || !document.Query.ListScenes || len(document.Scenes) != 2 {
		t.Fatalf("global scene catalogue JSON projection = %#v", document)
	}
}

func TestRecordContextMapsToScenesWithoutDumpingThem(t *testing.T) {
	result := cdccontext.Result{
		Selector: cdccontext.Selector{Bank: -1, Record: 30021},
		Scenes: []cdccontext.Scene{{
			ID: "scenario/20/7a4fa780fc60", Member: "cdc/01/ancsri01.cdc", SourceArchive: "pa",
			SourceKind: "cdc_program", Ordering: "source_order_with_static_control_flow", EvidenceStatus: "static_consumer_reference",
			Entries: []cdccontext.Entry{{
				Kind: "dialogue_association", MessageID: 30021, State: corpus.Translated,
				Japanese: "record Japanese", English: "record English",
			}},
		}},
	}
	var output bytes.Buffer
	writeContextReviewText(&output, result, "PSP_GAME")
	text := output.String()
	for _, wanted := range []string{
		"Record 30021", "State: translated", "Editable: translations/messages/msgsec003.toml",
		"Japanese: record Japanese", "English: record English", "Recovered scenes: 1",
		"Open: ./zill context --game-dir PSP_GAME --scene scenario/20/7a4fa780fc60",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("record mapping does not contain %q:\n%s", wanted, text)
		}
	}
	if strings.Contains(text, "Dialogue:") || strings.Contains(text, "Speaker:") {
		t.Fatalf("record mapping unexpectedly rendered full scene:\n%s", text)
	}
}

func TestStorageOnlyRecordGetsBoundedQualifiedNeighbors(t *testing.T) {
	entries := make([]cdccontext.Entry, 9)
	for index := range entries {
		entries[index] = cdccontext.Entry{
			Kind: "bank_record", MessageID: 340000 + index, Position: index,
			State: corpus.Translated, Japanese: "JP" + string(rune('0'+index)), English: "EN" + string(rune('0'+index)),
		}
	}
	entries[2].SourceControls = []cdccontext.SourceControl{{
		Kind: "selection", Selector: "<value:$20>%3", Evidence: "retail_message_bytecode",
		Blocks: []cdccontext.SourceBlock{
			{Position: 0, Role: "selection_arm", Japanese: "controlled neighbor JP", English: "controlled neighbor EN"},
			{Position: 1, Role: "selection_arm", Japanese: "second controlled JP", English: "second controlled EN"},
			{Position: 2, Role: "selection_arm", Japanese: "forbidden third JP", English: "forbidden third EN"},
		},
	}}
	result := cdccontext.Result{
		Selector: cdccontext.Selector{Bank: -1, Record: 340004},
		Scenes: []cdccontext.Scene{{
			ID: "bank/034", Member: "message/msgsec034.dat", SourceArchive: "pa",
			SourceKind: "message_bank", Ordering: "storage_order_only", Entries: entries,
		}},
	}
	var output bytes.Buffer
	writeContextReviewText(&output, result, "PSP_GAME")
	text := output.String()
	for _, wanted := range []string{
		"Recovered scenes: 0", "No recovered scene references this record.",
		"storage order only; they are not verified chronology", "340002 [translated] record variants:", "JP: controlled neighbor JP", "Additional variants omitted: 1", "> 340004", "Open storage:",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("storage-only review does not contain %q:\n%s", wanted, text)
		}
	}
	if strings.Contains(text, "340000") || strings.Contains(text, "340008") || strings.Contains(text, "forbidden third JP") {
		t.Fatalf("storage-only neighbors were not bounded around the target:\n%s", text)
	}
}

func TestSceneContextRendersACompleteTranslationReview(t *testing.T) {
	count := 2
	result := cdccontext.Result{
		Selector: cdccontext.Selector{Bank: -1, Record: -1, Scene: "scenario/20/7a4fa780fc60"},
		Scenes: []cdccontext.Scene{{
			ID: "scenario/20/7a4fa780fc60", Aliases: []string{"pa:cdc/01/ancsri01.cdc", "pa:cdc/v7/ancsriv7.cdc"},
			Member: "cdc/01/ancsri01.cdc", SourceArchive: "pa", SourceKind: "cdc_program",
			Ordering: "source_order_with_static_control_flow", EvidenceStatus: "static_consumer_reference",
			Scenario: &cdccontext.ScenarioScene{Slot: 20, EquivalentGroups: []string{"01", "v7"}},
			Entries: []cdccontext.Entry{{
				Kind: "dialogue_association", MessageID: 30021, Position: 0, State: corpus.Translated,
				SpeakerJapanese: "ノトゥーン神官長", SpeakerEnglish: "Notun High Priest",
				SpeakerStatus: "inferred_from_associated_label", SpeakerSource: "c5_associated_label",
				Conditions: []cdccontext.Condition{{Kind: "choice_selected_index", Status: "verified_control_behavior", SelectedIndex: intPointer(0), OptionCount: &count}},
				SourceControls: []cdccontext.SourceControl{{
					Kind: "selection", Selector: "<value:$20>%2", Evidence: "retail_message_bytecode", ExpectedBlocks: &count,
					Blocks: []cdccontext.SourceBlock{
						{Position: 0, Role: "selection_arm", Japanese: "first JP", English: "first EN"},
						{Position: 1, Role: "selection_arm", Japanese: "second JP", English: "second EN"},
					},
				}},
				Japanese: "raw controlled Japanese", English: "raw controlled English",
				Terminology: []cdccontext.TerminologyEntry{{Kind: "name", Japanese: "ディンガル", English: "Dyneskal"}},
			}, {
				Kind: "selection_option", MessageID: 30035, Position: 1, State: corpus.Todo,
				Japanese: "町から出る<end>",
			}},
		}},
	}
	var output bytes.Buffer
	writeContextReviewText(&output, result, "PSP_GAME")
	text := output.String()
	for _, wanted := range []string{
		"Scene: scenario/20/7a4fa780fc60", "Equivalent scenario groups: 01, v7",
		"Participants:", "Notun High Priest / ノトゥーン神官長 — inferred_from_associated_label",
		"Dialogue:", "[0] Dialogue 30021 · translated", "Branch: choice option 1 of 2",
		"Record variants: selection", "Variant 1: selection arm", "JP: first JP", "EN: second EN",
		"[1] Choice option 30035 · todo", "Speaker: unknown", "Terminology:", "ディンガル → Dyneskal",
		"Context boundaries:", "not confirmed vocal-speaker data",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("scene review does not contain %q:\n%s", wanted, text)
		}
	}
	for _, unwanted := range []string{"raw controlled Japanese", "<value:$20>", "retail_message_bytecode", "offset=", "C12 @", "Actor lifecycle"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("scene review contains diagnostic/duplicated text %q:\n%s", unwanted, text)
		}
	}
}

func TestDefaultJSONIsACompactVersionedReviewDocument(t *testing.T) {
	result := cdccontext.Result{
		Selector: cdccontext.Selector{Bank: -1, Record: -1, Scene: "cdc/pa/cdc/do/example.cdc"},
		Scenes: []cdccontext.Scene{{
			ID: "cdc/pa/cdc/do/example.cdc", Member: "cdc/do/example.cdc", SourceArchive: "pa",
			SourceKind: "cdc_program", Ordering: "source_order_with_static_control_flow",
			References: []cdccontext.Reference{{Opcode: "C12", Raw: "C12:20+0"}},
			Entries: []cdccontext.Entry{{
				Kind: "dialogue_association", MessageID: 1350035, State: corpus.Translated,
				Japanese: "JP", English: "EN", Actors: []cdccontext.Actor{{Handle: 2, Presence: "present"}},
			}, {
				Kind: "dialogue_association", MessageID: 1350036, State: corpus.Translated,
				Japanese: "<select><value:$20>%1raw<end>", English: "<select><value:$20>%1raw<end>",
				SourceControls: []cdccontext.SourceControl{{
					Kind: "selection", Selector: "<value:$20>%1", Evidence: "retail_message_bytecode",
					Blocks: []cdccontext.SourceBlock{{Position: 0, Role: "selection_arm", Japanese: "controlled JP", English: "controlled EN"}},
				}},
			}},
		}},
	}
	document := buildContextReviewDocument(result, "PSP_GAME")
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, wanted := range []string{`"schema_version":1`, `"view":"scene_review"`, `"id":"cdc/pa/cdc/do/example.cdc"`, `"record_id":1350035`, `"japanese":"JP"`, `"english":"EN"`, `"record_variants"`, `"japanese":"controlled JP"`, `"english":"controlled EN"`} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("compact JSON does not contain %s: %s", wanted, text)
		}
	}
	for _, unwanted := range []string{`"references"`, `"actors"`, `"raw":"C12:20+0"`, `"offset"`, `"source_controls"`, `"evidence_status"`, `"content_sha256"`, `retail_message_bytecode`, `<value:$20>`, `\u003cvalue:$20\u003e`} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("compact JSON contains verbose field %s: %s", unwanted, text)
		}
	}
}

func TestContextHelpAndSceneFirstOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runContext("../..", []string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit code = %d; stderr: %s", code, stderr.String())
	}
	for _, wanted := range []string{"--list-scenes", "--bank NNN", "--record ID", "--scene ID", "--verbose", "--format text|json"} {
		if !strings.Contains(stdout.String(), wanted) {
			t.Fatalf("help does not contain %q:\n%s", wanted, stdout.String())
		}
	}
	stdout.Reset()
	if code := runContext("../..", []string{"--game-dir", "PSP_GAME", "--help"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "--scene ID") {
		t.Fatalf("combined help exit code = %d; stdout: %s; stderr: %s", code, stdout.String(), stderr.String())
	}

	options, err := parseContextOptions([]string{"--game-dir", "PSP_GAME", "--scene", "bank/034", "--verbose", "--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if options.selectBy.Scene != "bank/034" || !options.verbose || options.format != "json" {
		t.Fatalf("parsed scene-first options = %#v", options)
	}
	if _, err := parseContextOptions([]string{"--game-dir", "PSP_GAME", "--record", "340008", "--scene", "bank/034"}); err == nil {
		t.Fatal("record and scene selectors were accepted together")
	}
	list, err := parseContextOptions([]string{"--game-dir", "PSP_GAME", "--list-scenes", "--format", "json"})
	if err != nil || !list.selectBy.ListScenes || list.format != "json" {
		t.Fatalf("parsed global scene catalogue options = %#v, %v", list, err)
	}
	if _, err := parseContextOptions([]string{"--game-dir", "PSP_GAME", "--list-scenes", "--bank", "3"}); err == nil {
		t.Fatal("global scene catalogue and bank selectors were accepted together")
	}
}
