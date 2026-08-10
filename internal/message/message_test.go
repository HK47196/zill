// SPDX-License-Identifier: GPL-3.0-or-later

package message_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/message"
)

func TestProjectionMovesOnlySourceApprovedSubstitutionAndPreservesControls(t *testing.T) {
	record := corpus.Record{ID: 42, Tokens: []corpus.Token{
		{Kind: "text", Raw: []byte("name=")},
		{Kind: "substitution", Raw: []byte{2, 0x28}},
		{Kind: "text", Raw: []byte("!")},
		{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
	}}
	projection, err := message.Project(record)
	if err != nil {
		t.Fatal(err)
	}
	if got := projection.Fragments[0].Source; got != "name={{PLAYER_NAME_1}}!" {
		t.Fatalf("projected source = %q", got)
	}
	got, err := projection.Materialize("Hello, <value:$28>.<end>", false)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte("Hello, "), 2, 0x28)
	want = append(want, []byte(".")...)
	want = append(want, 5, 5, 5)
	if !bytes.Equal(got, want) {
		t.Fatalf("materialized bytes = % x, want % x", got, want)
	}

	if _, err := projection.Materialize("Hello.<end>", false); err == nil || !strings.Contains(err.Error(), "runtime substitutions") {
		t.Fatalf("removing a source substitution returned %v", err)
	}
	if _, err := projection.Materialize("Hello, <value:$15>.<end>", false); err == nil || !strings.Contains(err.Error(), "runtime substitutions") {
		t.Fatalf("changing a source substitution returned %v", err)
	}
}

func TestProjectionNamesSelectorCaseFromSourceExpression(t *testing.T) {
	record := corpus.Record{ID: 7, Tokens: []corpus.Token{
		{Kind: "select", Raw: []byte{1, 'S'}},
		{Kind: "expression", Raw: []byte{2, 0x01, 4, '=', '%', '2', '4'}},
		{Kind: "text", Raw: []byte("case")},
		{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
	}}
	projection, err := message.Project(record)
	if err != nil {
		t.Fatal(err)
	}
	if got := projection.Fragments[0].Key; got != "case_024" {
		t.Fatalf("fragment key = %q, want case_024", got)
	}
}

func TestProjectionRejectsUnsafeTextAndProtectsPrintfSignature(t *testing.T) {
	record := corpus.Record{ID: 20006, Tokens: []corpus.Token{
		{Kind: "text", Raw: []byte("%s")},
		{Kind: "block_terminator", Raw: []byte{5, 5, 5}},
	}}
	projection, err := message.Project(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"ordinary text<end>", "%s\n<end>", "%sｱ<end>"} {
		if _, err := projection.Materialize(text, false); err == nil {
			t.Errorf("unsafe canonical text %q was accepted", text)
		}
	}
	if _, err := projection.Materialize("%%s<end>", false); err == nil || !strings.Contains(err.Error(), "format signature") {
		t.Fatalf("extra percent returned %v", err)
	}
}

func TestCompileBankUsesWideOffsetsAndLowersUnfinishedStatesLosslessly(t *testing.T) {
	translatedSource := corpus.Record{ID: 10000, Raw: []byte("old\x00"), Tokens: []corpus.Token{
		{Kind: "text", Raw: []byte("old")}, {Kind: "suffix", Raw: []byte{0}},
	}}
	todoSource := corpus.Record{ID: 10001, Raw: []byte{0x82, 0xa0, 5, 5, 5}, Tokens: []corpus.Token{
		{Kind: "text", Raw: []byte{0x82, 0xa0}}, {Kind: "block_terminator", Raw: []byte{5, 5, 5}},
	}}
	bank := corpus.Bank{Name: "msgsec001.dat", Section: 1, Records: []corpus.Record{translatedSource, todoSource}}
	items := []corpus.Item{
		{Record: translatedSource, Translation: corpus.Translation{ID: 10000, State: corpus.Translated, Text: "new"}},
		{Record: todoSource, Translation: corpus.Translation{ID: 10001, State: corpus.Todo}},
	}
	compiled, err := message.CompileBank(bank, items)
	if err != nil {
		t.Fatal(err)
	}
	if count, reserved := binary.LittleEndian.Uint16(compiled), binary.LittleEndian.Uint16(compiled[2:]); count != 2 || reserved != 0 {
		t.Fatalf("wide header = count %d, reserved %d", count, reserved)
	}
	first := binary.LittleEndian.Uint32(compiled[4:])
	second := binary.LittleEndian.Uint32(compiled[8:])
	if first != 12 || second != 16 {
		t.Fatalf("absolute offsets = [%d %d], want [12 16]", first, second)
	}
	if got, want := compiled[first:second], []byte("new\x00"); !bytes.Equal(got, want) {
		t.Fatalf("translated record = % x, want % x", got, want)
	}
	if got := compiled[second:]; !bytes.Equal(got, todoSource.Raw) {
		t.Fatalf("todo record changed: % x", got)
	}
}

func TestCompileBankRejectsLayoutThatChangesTranslation(t *testing.T) {
	source := corpus.Record{ID: 10000, Raw: []byte("old\x00"), Tokens: []corpus.Token{
		{Kind: "text", Raw: []byte("old")}, {Kind: "suffix", Raw: []byte{0}},
	}}
	bank := corpus.Bank{Name: "msgsec001.dat", Section: 1, Records: []corpus.Record{source}}
	items := []corpus.Item{{
		Record:      source,
		Translation: corpus.Translation{ID: 10000, State: corpus.Translated, Text: "one two"},
		Layout:      "one<line-break>changed",
	}}
	if _, err := message.CompileBank(bank, items); err == nil || !strings.Contains(err.Error(), "layout changes semantic") {
		t.Fatalf("semantic-changing layout returned %v", err)
	}
	items[0].Layout = "one<line-break>two"
	if _, err := message.CompileBank(bank, items); err != nil {
		t.Fatalf("whitespace-reflow layout was rejected: %v", err)
	}
	items[0].Translation.Text = "one　two"
	items[0].Layout = "one two"
	if _, err := message.CompileBank(bank, items); err != nil {
		t.Fatalf("full-width whitespace normalization was rejected: %v", err)
	}
}
