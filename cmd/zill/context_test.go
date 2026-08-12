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
			SourceKind:           "message_bank",
			Ordering:             "storage_order_only",
			EvidenceStatus:       "no_resolved_static_consumer_reference",
			FirstRecordMessageID: &labelID,
			FirstRecordJapanese:  "旅立ち０７メッセージ<end>",
			Entries: []cdccontext.Entry{{
				Kind:         "bank_record",
				MessageID:    340008,
				Offset:       415,
				OffsetBasis:  "message_bank_byte_offset",
				Selected:     true,
				Reachability: "unresolved",
				Japanese:     "そう言えばっ！<end>",
				English:      "Oh, that reminds me!<end>",
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
		"Source: message_bank",
		"Ordering: storage_order_only",
		"Evidence: no_resolved_static_consumer_reference",
		"First record (340000): 旅立ち０７メッセージ<end>",
		"Limitations: no resolved static consumer identifies branch topology, speakers, actor presence, or runtime reachability.",
		"bank_record 340008 @415 offset=message_bank_byte_offset reachability=unresolved target=true",
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
	if len(document.Scenes) != 1 || document.Scenes[0].SourceKind != "message_bank" || document.Scenes[0].Ordering != "storage_order_only" || document.Scenes[0].EvidenceStatus != "no_resolved_static_consumer_reference" {
		t.Fatalf("JSON scenes = %s", encoded)
	}
	if bytes.Contains(encoded, []byte(`"bank_context"`)) || bytes.Contains(encoded, []byte(`"unreferenced"`)) {
		t.Fatalf("JSON retained superseded fallback fields: %s", encoded)
	}
}
