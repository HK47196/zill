// SPDX-License-Identifier: GPL-3.0-or-later

package cdc_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/HK47196/zill/internal/gamefmt/cdc"
)

func TestParsePreservesCommandsAndOffsetsThroughConditionalBlocks(t *testing.T) {
	payload := []byte("A16:{C0:0+1468+F{C76:858R}}E0000")
	program, err := cdc.Parse("cdc/wd/example.cdc", payload)
	if err != nil {
		t.Fatal(err)
	}
	if program.MaximumNesting != 2 {
		t.Fatalf("maximum nesting = %d, want 2", program.MaximumNesting)
	}
	if len(program.Commands) != 3 {
		t.Fatalf("commands = %d, want 3", len(program.Commands))
	}
	command := program.Commands[2]
	if command.Name != "C76" || command.Raw != "C76:858" || command.Offset != strings.Index(string(payload), "C76:858") {
		t.Fatalf("C76 command = %+v", command)
	}
	if argument, ok := command.IntegerArgument(); !ok || argument != 858 {
		t.Fatalf("C76 integer argument = %d, %t; want 858, true", argument, ok)
	}
	resource, ok := command.C76Resource()
	if !ok || resource.LookupID != 1776 || resource.LogicalKey != "cdcDo/ID0858" {
		t.Fatalf("C76 resource = %+v, %t; want lookup 1776 and cdcDo/ID0858", resource, ok)
	}
}

func TestParsePreservesOrderedStructuralElementsWithoutChangingFlattenedCommands(t *testing.T) {
	payload := []byte("L1C0:0+1+F{C5:3+1+1350035;{R}C76:858}RE")
	program, err := cdc.Parse("cdc/do/example.cdc", payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Commands) != 3 {
		t.Fatalf("flattened commands = %d, want 3", len(program.Commands))
	}
	if got := []string{program.Commands[0].Name, program.Commands[1].Name, program.Commands[2].Name}; strings.Join(got, ",") != "C0,C5,C76" {
		t.Fatalf("flattened command order = %v, want C0, C5, C76", got)
	}

	elements := program.Elements
	if len(elements) != 5 {
		t.Fatalf("root elements = %d, want 5", len(elements))
	}
	if elements[0].Kind != cdc.LabelElement || elements[0].Label != "1" || elements[0].Raw != "L1" || elements[0].Offset != 0 {
		t.Fatalf("label element = %+v, want L1 at offset 0", elements[0])
	}
	if elements[1].Kind != cdc.CommandElement || elements[1].Command.Name != program.Commands[0].Name || elements[1].Command.Offset != program.Commands[0].Offset {
		t.Fatalf("first command element = %+v, want flattened C0", elements[1])
	}
	branch := elements[2]
	if branch.Kind != cdc.BlockElement || branch.Raw != "{" || branch.Offset != strings.Index(string(payload), "{") || branch.Block.CloseRaw != "}" || branch.Block.CloseOffset != strings.LastIndex(string(payload), "}") {
		t.Fatalf("outer block = %+v, want matched source braces", branch)
	}
	if len(branch.Block.Elements) != 3 {
		t.Fatalf("outer block elements = %d, want 3", len(branch.Block.Elements))
	}
	if branch.Block.Elements[0].Command.Name != "C5" || branch.Block.Elements[0].Raw != "C5:3+1+1350035;" || branch.Block.Elements[0].Offset != strings.Index(string(payload), "C5:") {
		t.Fatalf("nested command = %+v, want C5 source token", branch.Block.Elements[0])
	}
	nested := branch.Block.Elements[1]
	if nested.Kind != cdc.BlockElement || len(nested.Block.Elements) != 1 || nested.Block.Elements[0].Kind != cdc.ReturnElement || nested.Block.Elements[0].Raw != "R" {
		t.Fatalf("nested return block = %+v, want {R}", nested)
	}
	if branch.Block.Elements[2].Command.Name != "C76" {
		t.Fatalf("last outer block element = %+v, want C76", branch.Block.Elements[2])
	}
	if elements[3].Kind != cdc.ReturnElement || elements[3].Raw != "R" {
		t.Fatalf("root return = %+v, want R", elements[3])
	}
	// E is the final root element after the root return.
	if elements[4].Kind != cdc.EndElement || elements[4].Raw != "E" || elements[4].Offset != len(payload)-1 {
		t.Fatalf("final terminator = %+v, want E at final offset", elements)
	}
}

func TestC76ResourceHonorsTheVerifiedDoGroupBoundaries(t *testing.T) {
	tests := []struct {
		command cdc.Command
		wantID  int
		wantKey string
		wantOK  bool
	}{
		{command: cdc.Command{Name: "C76", Arguments: []string{"1"}}, wantID: 919, wantKey: "cdcDo/ID0001", wantOK: true},
		{command: cdc.Command{Name: "C76", Arguments: []string{"1200"}}, wantID: 2118, wantKey: "cdcDo/ID1200", wantOK: true},
		{command: cdc.Command{Name: "C76", Arguments: []string{"0"}}, wantOK: false},
		{command: cdc.Command{Name: "C76", Arguments: []string{"1201"}}, wantOK: false},
		{command: cdc.Command{Name: "C75", Arguments: []string{"858"}}, wantOK: false},
	}
	for _, test := range tests {
		resource, ok := test.command.C76Resource()
		if ok != test.wantOK || resource.LookupID != test.wantID || resource.LogicalKey != test.wantKey {
			t.Errorf("%+v C76 resource = %+v, %t; want ID %d, key %q, %t", test.command, resource, ok, test.wantID, test.wantKey, test.wantOK)
		}
	}
}

func TestScenarioSlotHonorsTheConditionalLoaderContract(t *testing.T) {
	tests := []struct {
		command cdc.Command
		want    int
		wantOK  bool
	}{
		{command: cdc.Command{Name: "C12", Arguments: []string{"1", "0"}}, want: 1, wantOK: true},
		{command: cdc.Command{Name: "C13", Arguments: []string{"914", "0"}}, want: 914, wantOK: true},
		{command: cdc.Command{Name: "C14", Arguments: []string{"412"}}, want: 412, wantOK: true},
		{command: cdc.Command{Name: "C12", Arguments: []string{"0", "0"}}},
		{command: cdc.Command{Name: "C13", Arguments: []string{"915", "0"}}},
		{command: cdc.Command{Name: "C14", Arguments: []string{"1000"}}},
		{command: cdc.Command{Name: "C12", Arguments: []string{"slot", "0"}}},
		{command: cdc.Command{Name: "C12"}},
		{command: cdc.Command{Name: "C76", Arguments: []string{"412"}}},
	}
	for _, test := range tests {
		slot, ok := test.command.ScenarioSlot()
		if slot != test.want || ok != test.wantOK {
			t.Errorf("%+v scenario slot = %d, %t; want %d, %t", test.command, slot, ok, test.want, test.wantOK)
		}
	}
}

func TestScenarioSlotTableIndexPreservesBoundedDynamicReferences(t *testing.T) {
	tests := []struct {
		command cdc.Command
		want    int
		wantOK  bool
	}{
		{command: cdc.Command{Name: "C14", Arguments: []string{"1000"}}, want: 0, wantOK: true},
		{command: cdc.Command{Name: "C14", Arguments: []string{"1009"}}, want: 9, wantOK: true},
		{command: cdc.Command{Name: "C14", Arguments: []string{"999"}}},
		{command: cdc.Command{Name: "C14", Arguments: []string{"1010"}}},
		{command: cdc.Command{Name: "C12", Arguments: []string{"1000", "0"}}},
	}
	for _, test := range tests {
		index, ok := test.command.ScenarioSlotTableIndex()
		if index != test.want || ok != test.wantOK {
			t.Errorf("%+v scenario slot table index = %d, %t; want %d, %t", test.command, index, ok, test.want, test.wantOK)
		}
	}
}

func TestParseDoesNotPromoteEmbeddedRuntimeTextToACommand(t *testing.T) {
	program, err := cdc.Parse("cdc/do/example.cdc", []byte("C7:328+127+C5:3+328+1330623;E"))
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Commands) != 1 || program.Commands[0].Name != "C7" {
		t.Fatalf("commands = %+v, want one C7 command", program.Commands)
	}
}

func TestParseRecognizesOnlyTheDocumentedPlaceholder(t *testing.T) {
	if _, err := cdc.Parse("cdc/do/dummy.cdc", []byte("dummy.cdc")); !errors.Is(err, cdc.ErrPlaceholder) {
		t.Fatalf("documented placeholder error = %v", err)
	}
	if _, err := cdc.Parse("cdc/do/other.cdc", []byte("dummy.cdc")); err == nil || errors.Is(err, cdc.ErrPlaceholder) {
		t.Fatalf("ordinary malformed member error = %v", err)
	}
}

func TestParseRejectsMalformedPrograms(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    string
	}{
		{name: "non ASCII", payload: []byte{'C', '1', ':', 0xff, 'E'}, want: "non-ASCII"},
		{name: "missing terminator", payload: []byte("C1:0"), want: "missing final E"},
		{name: "unclosed block", payload: []byte("C0:0+1+F{E"), want: "unclosed block"},
		{name: "dangling label", payload: []byte("C69:2E"), want: "has no L2"},
		{name: "C opcode outside dispatch", payload: []byte("C207:E"), want: "outside the C dispatch table"},
		{name: "A opcode outside dispatch", payload: []byte("A26:E"), want: "outside the A dispatch table"},
		{name: "A command semicolon", payload: []byte("A0:0;E"), want: "has a semicolon"},
		{name: "C76 extra argument", payload: []byte("C76:858+999E"), want: "requires one integer argument"},
		{name: "C76 semicolon", payload: []byte("C76:858;E"), want: "requires one integer argument"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := cdc.Parse("cdc/do/example.cdc", test.payload); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse error = %v, want text %q", err, test.want)
			}
		})
	}
}
