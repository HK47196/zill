// SPDX-License-Identifier: GPL-3.0-or-later

package layout_test

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/layout"
	"github.com/HK47196/zill/internal/message"
)

func releaseInputs(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return read("../../release/layout/consumer-map.toml"), read("../../release/font/metrics.toml"), read("../../release/layout/categories.toml")
}

func TestContributorCorpusUsesInstalledGlyphs(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.CheckGlyphs(project); err != nil {
		t.Fatal(err)
	}
}

func TestSystemHelpReflowUsesNarrowTextBox(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	source := "To open treasure chests, you must be able to use the Detector skill. Harbor Gremory's Soul."
	record := corpus.Record{
		ID: 1070080, Index: 80, Display: source + "<end>", HasBlockTerminator: true,
		Tokens: []corpus.Token{
			{Kind: "text", Raw: []byte(source), Text: source},
			{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
		},
	}
	project := &corpus.Project{Items: []corpus.Item{{
		Record: record,
		Translation: corpus.Translation{
			ID: 1070080, Japanese: record.Display, State: corpus.Translated, Text: source + "<end>",
		},
	}}}
	result, err := engine.Reflow(project)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Layouts[1070080]; !strings.Contains(got, "<line-break>") {
		t.Errorf("system-help layout did not fit the narrow text box: %q", got)
	}
}

func TestGuildPostingAndCommentaryReflowFitsTheirPanels(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		id            int
		text          string
		tokens        []corpus.Token
		minimumBreaks int
	}{
		{
			name: "commentary",
			id:   240018,
			text: "<value:$15>: Please deliver it safely, and get it there as soon as you can.<end>",
			tokens: []corpus.Token{
				{Kind: "substitution", Raw: []byte{2, 0x15}},
				{Kind: "text", Raw: []byte(": Please deliver it safely, and get it there as soon as you can."), Text: ": Please deliver it safely, and get it there as soon as you can."},
				{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
			},
			minimumBreaks: 1,
		},
		{
			name: "posting",
			id:   240019,
			text: "Seeking a trustworthy adventurer to transport the following important cargo to <value:$16>: <value:$15>. Deadline: within <value:$1B> days. Reward upon success: <value:$1A> Gea.<end>",
			tokens: []corpus.Token{
				{Kind: "text", Raw: []byte("Seeking a trustworthy adventurer to transport the following important cargo to "), Text: "Seeking a trustworthy adventurer to transport the following important cargo to "},
				{Kind: "substitution", Raw: []byte{2, 0x16}},
				{Kind: "text", Raw: []byte(": "), Text: ": "},
				{Kind: "substitution", Raw: []byte{2, 0x15}},
				{Kind: "text", Raw: []byte(". Deadline: within "), Text: ". Deadline: within "},
				{Kind: "substitution", Raw: []byte{2, 0x1b}},
				{Kind: "text", Raw: []byte(" days. Reward upon success: "), Text: " days. Reward upon success: "},
				{Kind: "substitution", Raw: []byte{2, 0x1a}},
				{Kind: "text", Raw: []byte(" Gea."), Text: " Gea."},
				{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
			},
			minimumBreaks: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := corpus.Record{
				ID: test.id, Index: test.id % 10_000, Display: test.text,
				HasBlockTerminator: true, Tokens: test.tokens,
			}
			project := &corpus.Project{Items: []corpus.Item{{
				Record: record,
				Translation: corpus.Translation{
					ID: test.id, Japanese: test.text, State: corpus.Translated, Text: test.text,
				},
			}}}
			result, err := engine.Reflow(project)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(result.Layouts[test.id], "<line-break>"); got < test.minimumBreaks {
				t.Errorf("guild %s layout has %d line breaks, want at least %d: %q", test.name, got, test.minimumBreaks, result.Layouts[test.id])
			}
		})
	}
}

func TestObjectiveAdviceReflowFitsAdvicePanel(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	const beforeName = "We can't let Nemea become the God of Destruction! I swear I'll save him. You'll lend me your strength too, won't you, "
	const afterName = "? Come on, let's go to the Island of the Dark Gate!"
	for _, id := range []int{260046, 1080046, 1210046, 1220046, 1230046, 1240046, 1250046} {
		record := corpus.Record{
			ID: id, Index: id % 10_000, Display: beforeName + "<value:$28>" + afterName + "<end>", HasBlockTerminator: true,
			Tokens: []corpus.Token{
				{Kind: "text", Raw: []byte(beforeName), Text: beforeName},
				{Kind: "substitution", Raw: []byte{2, 0x28}},
				{Kind: "text", Raw: []byte(afterName), Text: afterName},
				{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
			},
		}
		project := &corpus.Project{Items: []corpus.Item{{
			Record: record,
			Translation: corpus.Translation{
				ID: id, Japanese: record.Display, State: corpus.Translated, Text: record.Display,
			},
		}}}
		result, err := engine.Reflow(project)
		if err != nil {
			t.Fatal(err)
		}
		if got := result.Layouts[id]; strings.Count(got, "<line-break>") < 3 {
			t.Errorf("objective-advice layout %d does not fit the captured advice panel: %q", id, got)
		}
	}
}

func TestPortraitDialogueReflowFitsPortraitBody(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	const id = 2390003
	for _, test := range []struct {
		name, text string
		breaks     int
	}{
		// This string is exactly 241 units in the installed font, so the
		// portrait body's 240-unit ceiling must wrap it.
		{"width boundary", "WWWWWWWWWWWW WWWWWWWWWWW.,il", 1},
		{"reported overflow", "That was close. ...? Never did I expect to encounter someone with the potential for Infinite Soul in a place like this...", 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := corpus.Record{
				ID: id, Index: 3, Display: test.text + "<end>", HasBlockTerminator: true,
				Tokens: []corpus.Token{
					{Kind: "text", Raw: []byte(test.text), Text: test.text},
					{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
				},
			}
			project := &corpus.Project{Items: []corpus.Item{{
				Record: record,
				Translation: corpus.Translation{
					ID: id, Japanese: record.Display, State: corpus.Translated, Text: record.Display,
				},
			}}}
			result, err := engine.Reflow(project)
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Layouts[id]; strings.Count(got, "<line-break>") != test.breaks {
				t.Errorf("portrait dialogue layout = %q, want %d line breaks", got, test.breaks)
			}
		})
	}
}

func TestCharacterCreationPromptReflowUsesPromptWidth(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	const text = "O Bearer of the Infinite Soul, answer me. Answer my question and show me your soul."
	record := corpus.Record{
		ID: 10007, Index: 7, Display: text + "<end>", HasBlockTerminator: true,
		Tokens: []corpus.Token{
			{Kind: "text", Raw: []byte(text), Text: text},
			{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
		},
	}
	project := &corpus.Project{Items: []corpus.Item{{
		Record: record,
		Translation: corpus.Translation{
			ID: 10007, Japanese: record.Display, State: corpus.Translated, Text: text + "<end>",
		},
	}}}
	result, err := engine.Reflow(project)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Layouts[10007]; !strings.Contains(got, "<line-break>") {
		t.Errorf("character-creation prompt did not fit its text box: %q", got)
	}
}

func TestChronicleReflowUsesHistoryPanelWidth(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	const id = 1090015
	text := "resolved to set out, train, and reclaim the Grail from the Goblin personally."
	record := corpus.Record{
		ID: id, Index: 15, Display: text + "<end>", HasBlockTerminator: true,
		Tokens: []corpus.Token{
			{Kind: "text", Raw: []byte(text), Text: text},
			{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
		},
	}
	project := &corpus.Project{Items: []corpus.Item{{
		Record: record,
		Translation: corpus.Translation{
			ID: id, Japanese: record.Display, State: corpus.Translated, Text: record.Display,
		},
	}}}
	result, err := engine.Reflow(project)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Layouts[id]; !strings.Contains(got, "<line-break>") {
		t.Errorf("chronicle layout did not fit the History panel: %q", got)
	}
}

func TestChronicleReflowWarnsWhenEntryExceedsTenLines(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	project := &corpus.Project{}
	for index, lines := range []int{10, 11} {
		id := 1090015 + index*2
		text := strings.TrimSuffix(strings.Repeat("line ", lines), " ")
		layout := strings.ReplaceAll(text, " ", "<line-break>") + "<end>"
		record := corpus.Record{
			ID: id, Index: 15 + index*2, Display: text + "<end>", HasBlockTerminator: true,
			Tokens: []corpus.Token{
				{Kind: "text", Raw: []byte(text), Text: text},
				{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
			},
		}
		project.Items = append(project.Items, corpus.Item{
			Record: record,
			Translation: corpus.Translation{
				ID: id, Japanese: record.Display, State: corpus.Translated, Text: layout,
			},
		})
	}
	result, err := engine.Reflow(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != (layout.Warning{Code: "chronicle_vertical_overflow", MessageID: 1090017}) {
		t.Fatalf("chronicle warnings = %#v, want one vertical-overflow warning for the 11-line entry", result.Warnings)
	}
}

func TestReflowProcessesEveryMessage(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	const count = 64
	project := &corpus.Project{Items: make([]corpus.Item, count)}
	want := make(map[int]string, count)
	for index := range count {
		id := 3_000_000 + index
		text := fmt.Sprintf("Message %d", index)
		layout := text + "<end>"
		record := corpus.Record{
			ID: id, Index: index, Display: layout, HasBlockTerminator: true,
			Tokens: []corpus.Token{
				{Kind: "text", Raw: []byte(text), Text: text},
				{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
			},
		}
		project.Items[index] = corpus.Item{
			Record: record,
			Translation: corpus.Translation{
				ID: id, Japanese: layout, State: corpus.Translated, Text: layout,
			},
		}
		want[id] = layout
	}
	result, err := engine.Reflow(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Layouts) != count {
		t.Fatalf("Reflow returned %d layouts, want %d", len(result.Layouts), count)
	}
	for id, layout := range want {
		if got := result.Layouts[id]; got != layout {
			t.Errorf("message %d layout = %q, want %q", id, got, layout)
		}
	}
}

func TestReflowReportsFirstMessageError(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	project := &corpus.Project{Items: []corpus.Item{
		{
			Record: corpus.Record{ID: 3_000_002},
			Translation: corpus.Translation{
				ID: 3_000_002, State: corpus.Translated, Text: "first",
			},
		},
		{
			Record: corpus.Record{ID: 3_000_001},
			Translation: corpus.Translation{
				ID: 3_000_001, State: corpus.Translated, Text: "second",
			},
		},
	}}
	_, err = engine.Reflow(project)
	if err == nil || !strings.Contains(err.Error(), "message 3000002: source has no tokens") {
		t.Fatalf("Reflow error = %v, want first message error", err)
	}
}

func TestAuthoredLineBreaksArePreservedThroughCompilation(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	record := corpus.Record{
		ID: 270066, Index: 66, Display: "amount=<value:$1A> units<end>", HasBlockTerminator: true,
		Tokens: []corpus.Token{
			{Kind: "text", Raw: []byte("amount=")},
			{Kind: "substitution", Raw: []byte{2, 0x1a}},
			{Kind: "text", Raw: []byte(" units")},
			{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
		},
	}
	authored := "<line-break>Bounty: <value:$1A> Gea received.<line-break> <end>"
	project := &corpus.Project{Items: []corpus.Item{{
		Record: record,
		Translation: corpus.Translation{
			ID: 270066, Japanese: record.Display, State: corpus.Translated, Text: authored,
		},
	}}}
	result, err := engine.Reflow(project)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Layouts[270066]; got != authored {
		t.Fatalf("authored layout = %q, want %q", got, authored)
	}
	project.Items[0].Layout = result.Layouts[270066]
	compiled, err := message.CompileBank(
		corpus.Bank{Name: "msgsec027.dat", Section: 27, Records: []corpus.Record{record}},
		project.Items,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{10}, []byte("Bounty: ")...)
	want = append(want, 2, 0x1a)
	want = append(want, []byte(" Gea received.")...)
	want = append(want, 10, ' ', 5, 5, 5)
	if got := compiled[8:]; !bytes.Equal(got, want) {
		t.Fatalf("compiled authored layout = % x, want % x", got, want)
	}
}

func TestChronicleEntryRejectsUnsafeExpandedPayload(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	const id = 1090015
	project := &corpus.Project{Items: []corpus.Item{{
		Record: corpus.Record{ID: id},
		Translation: corpus.Translation{
			ID: id, State: corpus.Translated,
		},
	}}}
	if err := engine.Validate(project, map[int]string{
		id: strings.Repeat("A", 748) + "<value:$28><end>",
	}); err != nil {
		t.Fatalf("Validate rejected the maximum safe chronicle payload: %v", err)
	}
	err = engine.Validate(project, map[int]string{
		id: strings.Repeat("A", 749) + "<value:$28><end>",
	})
	if err == nil || !strings.Contains(err.Error(), "chronicle entry message 1090015 uses up to 765 bytes (maximum 764)") {
		t.Fatalf("Validate error = %v, want chronicle payload overflow", err)
	}
}

func TestCharacterCreationChoiceRejectsMoreThanThirtyBytes(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	engine, err := layout.Load(consumers, metrics, categories)
	if err != nil {
		t.Fatal(err)
	}
	const id = 10013
	project := &corpus.Project{Items: []corpus.Item{{
		Record: corpus.Record{ID: id},
		Translation: corpus.Translation{
			ID: id, State: corpus.Translated,
		},
	}}}
	if err := engine.Validate(project, map[int]string{
		id: strings.Repeat("A", 30) + "<end>",
	}); err != nil {
		t.Fatalf("Validate rejected the maximum safe character-creation choice: %v", err)
	}
	err = engine.Validate(project, map[int]string{
		id: strings.Repeat("A", 31) + "<end>",
	})
	if err == nil || !strings.Contains(err.Error(), "character-creation choice message 10013 uses 31 bytes (maximum 30)") {
		t.Fatalf("Validate error = %v, want character-creation choice overflow", err)
	}
}

func TestLoadRejectsUnknownConsumerField(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	consumers = bytes.Replace(consumers, []byte("format = \"zill-message-consumers\""), []byte("format = \"zill-message-consumers\"\nunexpected = true"), 1)
	_, err := layout.Load(consumers, metrics, categories)
	if err == nil || !strings.Contains(err.Error(), "invalid TOML") {
		t.Fatalf("Load error = %v, want unknown-field rejection", err)
	}
}
