// SPDX-License-Identifier: GPL-3.0-or-later

package message

import "testing"

func TestParseInlineControlPreservesConditionalBlocks(t *testing.T) {
	controls, err := ParseInlineControls("<if><value:$29><equal>%0son <value:$28><end>daughter <value:$28><end>")
	if err != nil {
		t.Fatal(err)
	}
	if len(controls) != 1 || controls[0].Kind != "conditional" || len(controls[0].Blocks) != 2 || controls[0].Blocks[0].Condition != "<value:$29><equal>%0" || controls[0].Blocks[0].Text != "son <value:$28>" || controls[0].Blocks[1].Role != "fallback" || controls[0].Blocks[1].Text != "daughter <value:$28>" {
		t.Fatalf("conditional = %#v", controls)
	}
}

func TestParseInlineControlReportsCountedAndUncountedSelections(t *testing.T) {
	counted, err := ParseInlineControls("<select><value:$20>%2first<end>second<end>")
	if err != nil {
		t.Fatal(err)
	}
	if len(counted) != 1 || counted[0].Kind != "selection" || counted[0].Selector != "<value:$20>%2" || counted[0].ExpectedBlocks == nil || *counted[0].ExpectedBlocks != 2 || len(counted[0].Blocks) != 2 {
		t.Fatalf("counted selection = %#v", counted)
	}
	uncounted, err := ParseInlineControls("<select><value:$33>first<end>second<end>")
	if err != nil {
		t.Fatal(err)
	}
	if len(uncounted) != 1 || uncounted[0].ExpectedBlocks != nil || len(uncounted[0].Blocks) != 2 {
		t.Fatalf("uncounted selection = %#v", uncounted)
	}
}

func TestParseInlineControlDoesNotInterpretPayloadSubstitutions(t *testing.T) {
	controls, err := ParseInlineControls("Price: %20, <value:$28>.<end>")
	if err != nil || controls != nil {
		t.Fatalf("ordinary payload = %#v, %v", controls, err)
	}
}

func TestParseInlineControlRejectsMalformedBlockStructure(t *testing.T) {
	for _, text := range []string{
		"<if><value:$29><equal>%0missing end",
		"<select><value:$20>%2only one<end>",
	} {
		if controls, err := ParseInlineControls(text); err == nil || controls != nil {
			t.Fatalf("malformed control = %#v, %v", controls, err)
		}
	}
}

func TestParseInlineControlsPreservesMixedConditionalAndSelectionGroups(t *testing.T) {
	controls, err := ParseInlineControls("<if><value:$33><less-equal>2conditional<end><select><value:$20>%2first<end>second<end>")
	if err != nil {
		t.Fatal(err)
	}
	if len(controls) != 2 || controls[0].Kind != "conditional" || len(controls[0].Blocks) != 1 || controls[1].Kind != "selection" || controls[1].Selector != "<value:$20>%2" || len(controls[1].Blocks) != 2 || controls[1].Blocks[0].Position != 1 {
		t.Fatalf("mixed controls = %#v", controls)
	}
}

func TestInlineControlsRenderAfterOnePayloadChangeWithoutChangingStructure(t *testing.T) {
	source := "<if><value:$33><less-equal>2conditional<end><select><value:$20>%2first<end>second<end>"
	controls, err := ParseInlineControls(source)
	if err != nil {
		t.Fatal(err)
	}
	controls[1].Blocks[0].Text = "revised"
	rendered, err := RenderInlineControls(controls)
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "<if><value:$33><less-equal>2conditional<end><select><value:$20>%2revised<end>second<end>" {
		t.Fatalf("rendered controls = %q", rendered)
	}
	reparsed, err := ParseInlineControls(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateInlineStructure(controls, reparsed); err != nil {
		t.Fatal(err)
	}
}

func TestInlineBlockAllowsOmittedSubstitutionsButRejectsUnsafeContent(t *testing.T) {
	for _, translated := range []string{"bad<end>", "wrong <value:$29>", "repeated <value:$28> <value:$28> <value:$28>", "half-width ｱ"} {
		if err := ValidateInlineBlock(42, "source <value:$28> repeated <value:$28>", translated); err == nil {
			t.Fatalf("unsafe block %q was accepted", translated)
		}
	}
	for _, translated := range []string{"No substitution needed.", "Safe <value:$28><line-break>text."} {
		if err := ValidateInlineBlock(42, "source <value:$28> repeated <value:$28>", translated); err != nil {
			t.Fatalf("safe block %q was rejected: %v", translated, err)
		}
	}
}
