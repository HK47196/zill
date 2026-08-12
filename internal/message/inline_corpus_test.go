// SPDX-License-Identifier: GPL-3.0-or-later

package message_test

import (
	"testing"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/message"
)

func TestInlineControlAcceptsCanonicalMessageCorpus(t *testing.T) {
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range project.Items {
		source, err := message.ParseInlineControls(item.Translation.Japanese)
		if err != nil {
			t.Fatalf("message %d Japanese: %v", item.Translation.ID, err)
		}
		if source == nil || item.Translation.Text == "" {
			continue
		}
		translated, err := message.ParseInlineControls(item.Translation.Text)
		if err != nil {
			t.Fatalf("message %d English: %v", item.Translation.ID, err)
		}
		if len(source) != len(translated) {
			t.Fatalf("message %d English control differs from Japanese source structure", item.Translation.ID)
		}
		for controlIndex := range source {
			if source[controlIndex].Kind != translated[controlIndex].Kind || source[controlIndex].Selector != translated[controlIndex].Selector || len(source[controlIndex].Blocks) != len(translated[controlIndex].Blocks) {
				t.Fatalf("message %d English control %d differs from Japanese source structure", item.Translation.ID, controlIndex)
			}
			for blockIndex := range source[controlIndex].Blocks {
				if source[controlIndex].Blocks[blockIndex].Role != translated[controlIndex].Blocks[blockIndex].Role || source[controlIndex].Blocks[blockIndex].Condition != translated[controlIndex].Blocks[blockIndex].Condition {
					t.Fatalf("message %d English control %d block %d differs from Japanese source structure", item.Translation.ID, controlIndex, blockIndex)
				}
				if err := message.ValidateInlineBlock(item.Translation.ID, source[controlIndex].Blocks[blockIndex].Text, translated[controlIndex].Blocks[blockIndex].Text); err != nil {
					t.Fatalf("message %d English control %d block %d: %v", item.Translation.ID, controlIndex, blockIndex, err)
				}
			}
		}
		rendered, err := message.RenderInlineControls(translated)
		if err != nil {
			t.Fatalf("message %d English render: %v", item.Translation.ID, err)
		}
		if rendered != item.Translation.Text {
			t.Fatalf("message %d English inline controls did not round trip", item.Translation.ID)
		}
	}
}
