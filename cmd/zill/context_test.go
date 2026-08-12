// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HK47196/zill/internal/cdccontext"
)

func TestContextRequiresOneValidSelector(t *testing.T) {
	tests := map[string][]string{
		"missing game directory": {"--bank", "135"},
		"missing selector":       {"--game-dir", "PSP_GAME"},
		"two selectors":          {"--game-dir", "PSP_GAME", "--bank", "135", "--record", "1350035"},
		"invalid bank":           {"--game-dir", "PSP_GAME", "--bank", "279"},
		"invalid format":         {"--game-dir", "PSP_GAME", "--bank", "135", "--format", "yaml"},
		"bank review":            {"--game-dir", "PSP_GAME", "--bank", "135", "--format", "review"},
	}
	for name, arguments := range tests {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runContext("../..", arguments, &stdout, &stderr); code != 2 {
				t.Fatalf("exit code = %d, want 2; stderr: %s", code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("invalid invocation wrote stdout: %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), contextUsage) {
				t.Fatalf("stderr does not contain usage: %q", stderr.String())
			}
		})
	}
}

func TestContextTextReportsUnreferencedBankAsAScene(t *testing.T) {
	labelID := 340000
	result := cdccontext.Result{
		Selector: cdccontext.Selector{Bank: -1, Record: 340008},
		Scenes: []cdccontext.Scene{{
			Member:               "message/msgsec034.dat",
			SourceArchive:        "pami",
			SourceKind:           "message_bank",
			Ordering:             "storage_order_only",
			EvidenceStatus:       "retail_storage_source",
			FirstRecordMessageID: &labelID,
			FirstRecordJapanese:  "旅立ち０７メッセージ<end>",
			SourceEvidence: []cdccontext.SourceEvidence{{
				Kind:             "scenario_reserve_marker",
				Status:           "source_authoring_candidate",
				Confidence:       "low",
				RuntimeStatus:    "unresolved",
				EventNumber:      7,
				MarkerLabel:      "バーニン親子鷹",
				MarkerMessageIDs: []int{340062, 340063},
				Candidates: []cdccontext.SourceCandidate{{
					MessageID: 1940007,
					Japanese:  "Ｂｕｒｎｉｎ’！　親子鷹<end>",
				}},
				Basis: "reserve_marker_event_number",
			}},
			Entries: []cdccontext.Entry{{
				Kind:         "bank_record",
				MessageID:    340008,
				Offset:       415,
				OffsetBasis:  "message_bank_byte_offset",
				Selected:     true,
				Reachability: "unresolved",
				Japanese:     "そう言えばっ！<end>",
				English:      "Oh, that reminds me!<end>",
			}, {
				Kind:         "bank_record",
				MessageID:    340017,
				Offset:       700,
				OffsetBasis:  "message_bank_byte_offset",
				Reachability: "unresolved",
				Japanese:     "<if><value:$29><equal>%0息子<end>娘<end>",
				English:      "<if><value:$29><equal>%0son<end>daughter<end>",
				SourceControls: []cdccontext.SourceControl{{
					Kind:     "conditional",
					Evidence: "retail_message_bytecode",
					Blocks: []cdccontext.SourceBlock{
						{Position: 0, Role: "condition", Condition: "<value:$29><equal>%0", Japanese: "息子", English: "son"},
						{Position: 1, Role: "fallback", Japanese: "娘", English: "daughter"},
					},
				}},
			}},
		}},
	}
	var output bytes.Buffer
	writeContextText(&output, result)

	text := output.String()
	for _, wanted := range []string{
		"Query: record 340008",
		"Scenes: 1",
		"Scene: message/msgsec034.dat",
		"Archive: pami",
		"Source: message_bank",
		"Ordering: storage_order_only",
		"Evidence: retail_storage_source",
		"First record (340000): 旅立ち０７メッセージ<end>",
		"Source evidence: scenario_reserve_marker status=source_authoring_candidate confidence=low runtime=unresolved",
		"Marker: event=7 label=バーニン親子鷹 records=[340062 340063]",
		"Authoring candidate (1940007): Ｂｕｒｎｉｎ’！　親子鷹<end> label_match=false",
		"Limitations: record-local controls and source-authoring candidates do not establish scene chronology, speakers, actor presence, or runtime reachability.",
		"bank_record 340008 @415 offset=message_bank_byte_offset reachability=unresolved target=true",
		"Record-local control 0: conditional evidence=retail_message_bytecode",
		"Block 0: role=condition condition=<value:$29><equal>%0",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("output does not contain %q:\n%s", wanted, text)
		}
	}
	if strings.Contains(text, "No CDC scenes reference") {
		t.Fatalf("output still reports zero context scenes:\n%s", text)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Scenes []struct {
			SourceKind     string `json:"source_kind"`
			Ordering       string `json:"ordering"`
			EvidenceStatus string `json:"evidence_status"`
		} `json:"scenes"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Scenes) != 1 || document.Scenes[0].SourceKind != "message_bank" || document.Scenes[0].Ordering != "storage_order_only" || document.Scenes[0].EvidenceStatus != "retail_storage_source" {
		t.Fatalf("JSON scenes = %s", encoded)
	}
	if bytes.Contains(encoded, []byte(`"bank_context"`)) || bytes.Contains(encoded, []byte(`"unreferenced"`)) {
		t.Fatalf("JSON retained superseded fallback fields: %s", encoded)
	}
}

func TestContextTextQualifiesAmbientInteractionEvidence(t *testing.T) {
	handle, slot, offset := 1097, 2, 0xf4
	result := cdccontext.Result{
		Selector: cdccontext.Selector{Bank: -1, Record: 30028},
		Scenes: []cdccontext.Scene{{
			Member: "room/id0025.par", EmbeddedMember: "ancthrbr.imd",
			SourceArchive: "pami", SourceKind: "ambient_interaction",
			Ordering: "room_entity_table_order", EvidenceStatus: "verified_executable_interaction_mapping",
			Entries: []cdccontext.Entry{{
				Kind: "ambient_dialogue", MessageID: 30028, Offset: offset,
				OffsetBasis: "room_imd_entity_record_offset", Position: slot, Selected: true,
				Reachability: "runtime_dependent", EntityAssociationHandleRaw: &handle,
				AssociatedLabelJapanese: "船員", AssociatedLabelEnglish: "Sailor",
				AssociationResolution: "associated_label", SpeakerStatus: "inferred_from_verified_interaction_target",
				AmbientInteraction: &cdccontext.AmbientInteraction{
					EntityHandle: handle, Status: "verified_executable_mapping",
					RuntimeStatus: "interaction_target_runtime_dependent", SourceLocator: "retail executable",
					RoomMember: "room/id0025.par", RoomResource: "ancthrbr.imd", EntitySlot: &slot, EntityOffset: &offset,
				},
				Japanese: "港の話<end>", English: "Harbor talk.<end>",
			}},
		}},
	}
	var output bytes.Buffer
	writeContextText(&output, result)
	text := output.String()
	for _, wanted := range []string{
		"Scene: room/id0025.par",
		"Embedded resource: ancthrbr.imd",
		"Source: ambient_interaction",
		"Limitations: room-authored entity records and the executable interaction mapping do not establish global dialogue chronology or simultaneous runtime presence.",
		"ambient_dialogue 30028 @244 offset=room_imd_entity_record_offset",
		"Association: handle=1097 resolution=associated_label",
		"Speaker status: inferred_from_verified_interaction_target",
		"Ambient interaction: status=verified_executable_mapping runtime=interaction_target_runtime_dependent room=room/id0025.par resource=ancthrbr.imd slot=2",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("output does not contain %q:\n%s", wanted, text)
		}
	}
	if strings.Contains(text, "Display requests:") {
		t.Fatalf("ambient association was rendered as a C5 display request:\n%s", text)
	}
}

func TestContextTextRendersLogicalScenarioFamiliesInsteadOfFlatCandidates(t *testing.T) {
	sourceSlot := 7
	result := cdccontext.Result{
		Selector: cdccontext.Selector{Bank: -1, Record: 30021},
		Scenes: []cdccontext.Scene{{
			Member: "cdc/01/source01.cdc", SourceArchive: "pa", SourceKind: "cdc_program",
			Ordering: "source_order_with_static_control_flow", EvidenceStatus: "static_consumer_reference",
			References: []cdccontext.Reference{{
				Opcode: "C14", Offset: 44, Raw: "C14:1000", ExecutionStatus: "runtime_dependent",
				ResolutionStatus: "room_runtime_dependent",
				ScenarioRoomTable: &cdccontext.ScenarioRoomTable{
					TableIndex: 0, SelectorValue: 1000, PossibleSlots: []int{20, 21},
					TargetCount: 853, RoomCount: 853, Status: "current_room_runtime_dependent",
				},
			}},
		}},
		RoomMessageBankRegistrations: []cdccontext.RoomMessageBankRegistration{{
			RoomLogicalKey: "room/ID0020", RoomSourceArchive: "pami", RoomMember: "room/id0020.par", RoomArchiveIndex: 20, Bank: 3,
			BankLogicalKey: "msgsec003_dat", BankSourceArchive: "pami", BankMember: "message/msgsec003.dat", BankArchiveIndex: 21,
			Status: "verified_rbb_room_registration", RuntimeStatus: "current_room_runtime_dependent",
		}},
		ScenarioFamilies: []cdccontext.ScenarioFamily{{
			Slot: 20, Status: "group_runtime_dependent", Basis: "verified_rbb_logical_resource_catalog", Relevance: []string{"selected_scene"},
			Incoming: []cdccontext.ScenarioIncomingEdge{{
				SourceMember: "cdc/01/source01.cdc", SourceArchive: "pa", SourceScenarioSlot: &sourceSlot,
				Path: []int{1, 3}, Guard: "A7:1090", Opcode: "C12", Offset: 12,
				ExecutionStatus: "runtime_dependent",
			}},
			Roots: []cdccontext.ScenarioRoot{{
				Kind: "direct_executable_request", Status: "verified_executable_root",
				Confidence: "high", SourceLocator: "retail executable",
			}},
			RoomTargets: []cdccontext.ScenarioRoomTarget{{
				SourceArchive: "pami", RoomArchiveIndex: 20, RoomMember: "room/id0020.par", EmbeddedMember: "anctsrni.imd",
				SelectorIndex: 1000, Status: "verified_room_imd_slot",
			}},
			Variants: []cdccontext.ScenarioContentVariant{{
				ContentSHA256: "abc123", CanonicalMember: "cdc/01/ancsri01.cdc",
				Members: []cdccontext.ScenarioPhysicalMember{{
					Group: "01", LogicalKey: "cdc01/ID0020", AuthoringName: "AncSrI01_cdc",
					SourceArchive: "pa", Member: "cdc/01/ancsri01.cdc", ArchiveIndex: 339,
				}, {
					Group: "v7", LogicalKey: "cdcV7/ID0020", AuthoringName: "AncSrIV7_cdc",
					SourceArchive: "pa", Member: "cdc/v7/ancsriv7.cdc", ArchiveIndex: 11307,
				}},
			}},
		}},
	}
	var output bytes.Buffer
	writeContextText(&output, result)
	text := output.String()
	for _, wanted := range []string{
		"Room-local bank registrations: 1 (availability only; runtime room remains dependent)",
		"room/ID0020 -> msgsec003_dat room=room/id0020.par@pami#20 bank_member=message/msgsec003.dat@pami#21",
		"Slot 20: status=group_runtime_dependent relevance=selected_scene variants=1 incoming_edges=1 roots=1 room_targets=1",
		"Incoming: source=cdc/01/source01.cdc@pa source_scenario_slot=7 opcode=C12 offset=12 path=root/1/3 guard=A7:1090 execution=runtime_dependent",
		"Room target: room=room/id0020.par@pami#20 resource=anctsrni.imd selector=1000 status=verified_room_imd_slot",
		"Variant 0: canonical=cdc/01/ancsri01.cdc content_sha256=abc123 groups=01,v7",
		"01 -> cdc01/ID0020 authoring_name=AncSrI01_cdc physical=cdc/01/ancsri01.cdc archive=pa index=339",
		"Room scenario table: selector=1000 table_index=0 status=current_room_runtime_dependent possible_slots=2 authored_targets=853 rooms=853",
	} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("output does not contain %q:\n%s", wanted, text)
		}
	}
	if strings.Contains(text, "Candidate:") {
		t.Fatalf("output retained superseded flat scenario candidates:\n%s", text)
	}
	result.ReviewPackets = []cdccontext.ReviewPacket{{
		SceneMember: "cdc/01/source01.cdc", SourceArchive: "pa", SourceKind: "cdc_program",
		Ordering: "source_order_with_static_control_flow", EvidenceStatus: "static_consumer_reference",
		TargetMessageID: 30021, References: result.Scenes[0].References,
	}}
	output.Reset()
	writeReviewText(&output, result)
	if !strings.Contains(output.String(), "Cross-program reference: C14 execution=runtime_dependent resolution=room_runtime_dependent room_table_selector=1000 possible_slots=2 authored_targets=853 rooms=853") {
		t.Fatalf("review output omits room-table context:\n%s", output.String())
	}
}
