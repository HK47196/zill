// SPDX-License-Identifier: GPL-3.0-or-later

package layout_test

import (
	"bytes"
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

func TestLoadRejectsUnknownConsumerField(t *testing.T) {
	consumers, metrics, categories := releaseInputs(t)
	consumers = bytes.Replace(consumers, []byte("format = \"zill-message-consumers\""), []byte("format = \"zill-message-consumers\"\nunexpected = true"), 1)
	_, err := layout.Load(consumers, metrics, categories)
	if err == nil || !strings.Contains(err.Error(), "invalid TOML") {
		t.Fatalf("Load error = %v, want unknown-field rejection", err)
	}
}
