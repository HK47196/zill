// SPDX-License-Identifier: GPL-3.0-or-later

// Package cdc parses the ASCII event programs stored as CDC archive members.
package cdc

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/gamefmt/paa"
)

const (
	dummyName           = "cdc/do/dummy.cdc"
	dummyData           = "dummy.cdc"
	scenarioSlotMinimum = 1
	scenarioSlotMaximum = 0x392
	c76LookupOffset     = 0x396
	c76DoLookupMinimum  = 0x397
	c76DoLookupMaximum  = 0x846
)

var (
	// ErrPlaceholder identifies the one documented non-program CDC member.
	ErrPlaceholder = errors.New("CDC placeholder")

	tokenPattern          = regexp.MustCompile(`^(?:[AC][0-9]+:|L[0-9]+|[{}RE])`)
	commandNamePattern    = regexp.MustCompile(`^[AC][0-9]+$`)
	integerPattern        = regexp.MustCompile(`^-?[0-9]+$`)
	decimalPattern        = regexp.MustCompile(`^-?[0-9]+\.[0-9]+$`)
	symbolPattern         = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
	runtimeOverlapPattern = regexp.MustCompile(`^(?:(?:C7:-?[0-9]+\+-?[0-9]+)|(?:C17:-?[0-9]+\+-?[0-9]+\+[FO]))(?:\+-?[0-9]+)?\+C[0-9]+:`)
)

// Command is one parsed A or C command. Offset is its byte offset in the
// unpadded ASCII program.
type Command struct {
	Name      string
	Arguments []string
	Offset    int
	Raw       string
	Semicolon bool
}

// ElementKind identifies one token in a CDC program's structural sequence.
// Elements retain the program order; Block elements own their nested Elements.
type ElementKind uint8

const (
	CommandElement ElementKind = iota
	BlockElement
	LabelElement
	ReturnElement
	EndElement
)

// Element is one ordered structural token in a Program or Block. Offset and
// Raw identify the original unpadded ASCII token. Command is populated for a
// CommandElement, Block for a BlockElement, and Label for a LabelElement.
type Element struct {
	Kind    ElementKind
	Offset  int
	Raw     string
	Command Command
	Block   Block
	Label   string
}

// Block is a matched {...} region. Its Elements are in source order between
// the braces, allowing callers to assign paths without flattening branches.
type Block struct {
	Offset      int
	Raw         string
	Elements    []Element
	CloseOffset int
	CloseRaw    string
}

// ScenarioSlot returns a statically direct scenario-slot reference encoded by
// C12, C13, or C14. The executable only passes slots 1 through 914 to the
// parser loader. C12/C13 loads are runtime-state dependent, and C14 values at
// or above 1000 take a separate dynamic mapping path that this method does not
// resolve.
func (c Command) ScenarioSlot() (int, bool) {
	if (c.Name != "C12" && c.Name != "C13" && c.Name != "C14") || len(c.Arguments) == 0 || !integerPattern.MatchString(c.Arguments[0]) {
		return 0, false
	}
	slot, err := strconv.Atoi(c.Arguments[0])
	if err != nil || slot < scenarioSlotMinimum || slot > scenarioSlotMaximum {
		return 0, false
	}
	return slot, true
}

// ScenarioSlotTableIndex returns the bounded runtime-table index encoded by a
// C14 value from 1000 through 1009. The selected record is populated from the
// current room's IMD data and supplies a slot subsequently guarded to 1
// through 914.
func (c Command) ScenarioSlotTableIndex() (int, bool) {
	if c.Name != "C14" || len(c.Arguments) == 0 || !integerPattern.MatchString(c.Arguments[0]) {
		return 0, false
	}
	value, err := strconv.Atoi(c.Arguments[0])
	if err != nil || value < 1000 || value > 1009 {
		return 0, false
	}
	return value - 1000, true
}

// IntegerArgument returns the command's sole signed decimal argument.
func (c Command) IntegerArgument() (int, bool) {
	if len(c.Arguments) != 1 || !integerPattern.MatchString(c.Arguments[0]) {
		return 0, false
	}
	value, err := strconv.Atoi(c.Arguments[0])
	return value, err == nil
}

// ResourceReference is a verified logical resource-manager request. LookupID
// is the numeric cache/load ID; LogicalKey is the formatted resource key.
type ResourceReference struct {
	LookupID   int    `json:"lookup_id"`
	LogicalKey string `json:"logical_key"`
}

// C76Resource returns the verified Do-group resource request made by a C76
// command. It returns false when the command or argument is outside that
// resource range.
func (c Command) C76Resource() (ResourceReference, bool) {
	if c.Name != "C76" {
		return ResourceReference{}, false
	}
	argument, ok := c.IntegerArgument()
	if !ok {
		return ResourceReference{}, false
	}
	lookupID := argument + c76LookupOffset
	if lookupID < c76DoLookupMinimum || lookupID > c76DoLookupMaximum {
		return ResourceReference{}, false
	}
	return ResourceReference{
		LookupID:   lookupID,
		LogicalKey: fmt.Sprintf("cdcDo/ID%04d", argument),
	}, true
}

// Program is a lexically and structurally validated CDC program. Parse does
// not validate every handler-specific argument shape.
type Program struct {
	Commands       []Command
	Elements       []Element
	MaximumNesting int
}

type elementBuilder struct {
	kind    ElementKind
	offset  int
	raw     string
	command Command
	label   string
	block   *blockBuilder
}

type blockBuilder struct {
	offset      int
	raw         string
	elements    []elementBuilder
	closeOffset int
	closeRaw    string
}

func (b *blockBuilder) append(element elementBuilder) {
	b.elements = append(b.elements, element)
}

func structuralElements(elements []elementBuilder) []Element {
	result := make([]Element, 0, len(elements))
	for _, element := range elements {
		converted := Element{
			Kind:    element.kind,
			Offset:  element.offset,
			Raw:     element.raw,
			Command: element.command,
			Label:   element.label,
		}
		if element.block != nil {
			converted.Block = Block{
				Offset:      element.block.offset,
				Raw:         element.block.raw,
				Elements:    structuralElements(element.block.elements),
				CloseOffset: element.block.closeOffset,
				CloseRaw:    element.block.closeRaw,
			}
		}
		result = append(result, converted)
	}
	return result
}

// Parse validates and parses one exact stored CDC payload. Literal ASCII '0'
// archive padding is removed before parsing.
func Parse(name string, payload []byte) (Program, error) {
	if name == dummyName && bytes.Equal(payload, []byte(dummyData)) {
		return Program{}, ErrPlaceholder
	}
	for offset, value := range payload {
		if value > 0x7f {
			return Program{}, fmt.Errorf("non-ASCII byte %#x at offset %d", value, offset)
		}
	}
	text := strings.TrimRight(string(payload), "0")
	if !strings.HasSuffix(text, "E") {
		return Program{}, errors.New("missing final E terminator")
	}
	if count := strings.Count(text, "E"); count != 1 {
		return Program{}, fmt.Errorf("expected one final E, found %d", count)
	}

	program := Program{}
	root := &blockBuilder{}
	current := root
	var blocks []*blockBuilder
	labels := make(map[string]struct{})
	type target struct {
		command string
		label   string
		offset  int
	}
	var targets []target
	depth := 0
	position := 0
	for position < len(text) {
		token := tokenAt(text, position)
		if token == "" {
			end := position + 1
			for end < len(text) && tokenAt(text, end) == "" {
				end++
			}
			return Program{}, fmt.Errorf("unknown text at offset %d: %q", position, text[position:end])
		}
		start := position
		position += len(token)

		if token[0] == 'A' || token[0] == 'C' {
			name := strings.TrimSuffix(token, ":")
			if err := validateOpcodeName(name); err != nil {
				return Program{}, fmt.Errorf("offset %d: %w", start, err)
			}
			end := position
			recoveryEnd := end
			if match := runtimeOverlapPattern.FindString(text[start:]); match != "" {
				commandEnd := strings.LastIndex(match, "+C") + 2
				end = start + commandEnd
				recoveryEnd = end
				for recoveryEnd < len(text) && tokenAt(text, recoveryEnd) == "" {
					recoveryEnd++
				}
			} else {
				for end < len(text) && tokenAt(text, end) == "" {
					end++
				}
				recoveryEnd = end
			}

			body := text[position:end]
			semicolon := strings.HasSuffix(body, ";")
			core := strings.TrimSuffix(body, ";")
			if strings.Contains(core, ";") {
				return Program{}, fmt.Errorf("misplaced semicolon in %s at offset %d", name, start)
			}
			arguments := []string(nil)
			if core != "" {
				arguments = strings.Split(core, "+")
			}
			for _, argument := range arguments {
				if !validArgument(argument) {
					return Program{}, fmt.Errorf("%s at offset %d has invalid argument %q", name, start, argument)
				}
			}
			if name[0] == 'A' && semicolon {
				return Program{}, fmt.Errorf("A command %s at offset %d has a semicolon", name, start)
			}
			if name == "C76" && (semicolon || len(arguments) != 1 || !integerPattern.MatchString(arguments[0])) {
				return Program{}, fmt.Errorf("C76 at offset %d requires one integer argument without a semicolon", start)
			}
			command := Command{
				Name:      name,
				Arguments: append([]string(nil), arguments...),
				Offset:    start,
				Raw:       text[start:end],
				Semicolon: semicolon,
			}
			program.Commands = append(program.Commands, command)
			current.append(elementBuilder{kind: CommandElement, offset: start, raw: command.Raw, command: command})
			if name == "C69" || name == "C70" {
				if len(arguments) != 1 || !integerPattern.MatchString(arguments[0]) {
					return Program{}, fmt.Errorf("%s at offset %d has invalid label target %q", name, start, core)
				}
				targets = append(targets, target{command: name, label: arguments[0], offset: start})
			}
			position = recoveryEnd
			continue
		}

		switch token[0] {
		case '{':
			depth++
			if depth > program.MaximumNesting {
				program.MaximumNesting = depth
			}
			block := &blockBuilder{offset: start, raw: token}
			current.append(elementBuilder{kind: BlockElement, offset: start, raw: token, block: block})
			blocks = append(blocks, current)
			current = block
		case '}':
			depth--
			if depth < 0 {
				return Program{}, fmt.Errorf("unmatched } at offset %d", start)
			}
			current.closeOffset = start
			current.closeRaw = token
			current = blocks[len(blocks)-1]
			blocks = blocks[:len(blocks)-1]
		case 'L':
			labels[token[1:]] = struct{}{}
			current.append(elementBuilder{kind: LabelElement, offset: start, raw: token, label: token[1:]})
		case 'R':
			current.append(elementBuilder{kind: ReturnElement, offset: start, raw: token})
		case 'E':
			if position != len(text) {
				return Program{}, fmt.Errorf("E before end of program at offset %d", start)
			}
			current.append(elementBuilder{kind: EndElement, offset: start, raw: token})
		}
	}
	if depth != 0 {
		return Program{}, fmt.Errorf("%d unclosed block(s)", depth)
	}
	for _, target := range targets {
		if _, ok := labels[target.label]; !ok {
			return Program{}, fmt.Errorf("%s:%s at offset %d has no L%s", target.command, target.label, target.offset, target.label)
		}
	}
	program.Elements = structuralElements(root.elements)
	return program, nil
}

func tokenAt(text string, position int) string {
	match := tokenPattern.FindString(text[position:])
	return match
}

func validArgument(argument string) bool {
	return argument == "" || integerPattern.MatchString(argument) || decimalPattern.MatchString(argument) || symbolPattern.MatchString(argument)
}

func validateOpcodeName(opcode string) error {
	if !commandNamePattern.MatchString(opcode) {
		return fmt.Errorf("invalid CDC opcode %q", opcode)
	}
	index, err := strconv.Atoi(opcode[1:])
	if err != nil {
		return fmt.Errorf("invalid CDC opcode %q", opcode)
	}
	limit := 207
	if opcode[0] == 'A' {
		limit = 26
	}
	if index >= limit {
		return fmt.Errorf("CDC opcode %s is outside the %c dispatch table", opcode, opcode[0])
	}
	return nil
}

// Occurrence identifies one command in one CDC archive member.
type Occurrence struct {
	Member  string
	Command Command
}

// Report summarizes a validated CDC corpus and occurrences of one requested
// opcode.
type Report struct {
	ArchiveMembers int
	Programs       int
	Placeholders   []string
	CommandCount   int
	OpcodeCounts   map[string]int
	Occurrences    []Occurrence
	members        map[string]struct{}
}

// HasMember reports whether the CDC corpus contains an exact archive member
// name.
func (r Report) HasMember(name string) bool {
	_, ok := r.members[name]
	return ok
}

// Audit validates every CDC member in pair and records occurrences of opcode.
func Audit(pair *paa.Pair, opcode string) (Report, error) {
	if err := validateOpcodeName(opcode); err != nil {
		return Report{}, err
	}
	report := Report{OpcodeCounts: make(map[string]int), members: make(map[string]struct{})}
	var members []paa.Member
	for _, member := range pair.Members() {
		if !strings.HasPrefix(member.Name, "cdc/") || !strings.HasSuffix(member.Name, ".cdc") {
			continue
		}
		members = append(members, member)
		report.members[member.Name] = struct{}{}
	}
	report.ArchiveMembers = len(members)
	if report.ArchiveMembers == 0 {
		return Report{}, errors.New("archive contains no cdc/*.cdc members")
	}
	for _, member := range members {
		payload, err := pair.Payload(member.Index)
		if err != nil {
			return Report{}, err
		}
		program, err := Parse(member.Name, payload)
		if errors.Is(err, ErrPlaceholder) {
			report.Placeholders = append(report.Placeholders, member.Name)
			continue
		}
		if err != nil {
			return Report{}, fmt.Errorf("%s: %w", member.Name, err)
		}
		report.Programs++
		for _, command := range program.Commands {
			report.CommandCount++
			report.OpcodeCounts[command.Name]++
			if command.Name == opcode {
				report.Occurrences = append(report.Occurrences, Occurrence{Member: member.Name, Command: command})
			}
		}
	}
	return report, nil
}
