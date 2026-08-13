// SPDX-License-Identifier: GPL-3.0-or-later

// Package message safely projects source message bytecode into editable text
// fragments and compiles translated records back into runtime message banks.
package message

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/cp932"
)

const lineBreak = "<line-break>"

var valueTag = regexp.MustCompile(`<value:\$([0-9A-F]{2})>`)
var reservedMarkup = regexp.MustCompile(`[<>]`)
var reservedAnchor = regexp.MustCompile(`\{\{[^{}]*\}\}`)
var printfConversion = regexp.MustCompile(`%(?:[1-9][0-9]*)?[su]`)

var formatSignatureIDs = map[int]bool{
	20006: true, 170006: true, 170007: true, 170008: true,
	170009: true, 1070022: true, 1070023: true,
}

var pureMovable = map[byte]bool{0x24: true, 0x25: true, 0x28: true, 0x2b: true}
var callerMovable = map[byte]bool{0x15: true, 0x16: true, 0x17: true, 0x1a: true, 0x1b: true}

var semanticJoin = map[string]bool{
	"if": true, "select": true, "expression": true, "flow_control": true,
	"separator": true, "call": true, "jump": true, "renderer_control": true,
	"unknown_control": true, "backspace": true, "tab": true, "color": true,
	"undecodable_data": true,
}

// Fragment describes one replaceable source-derived formatting region.
type Fragment struct {
	Key             string
	Source          string
	SourceLayout    string
	Anchors         []Anchor
	Substitutions   []byte
	FormatSignature []string
}

// Anchor names one source substitution that may move, but only within its
// original fragment.
type Anchor struct {
	Name   string
	Opcode byte
}

type projectionNode struct {
	fixed       bool
	kind        string
	display     string
	raw         []byte
	fragment    int
	kanaControl [][]byte
}

// Projection is an immutable binding between a source record and its editable
// semantic fragments.
type Projection struct {
	RecordID  int
	Fragments []Fragment
	nodes     []projectionNode
}

// Project derives the editable semantic regions and locked controls from a
// parsed retail source record.
func Project(record corpus.Record) (*Projection, error) {
	if len(record.Tokens) == 0 {
		return nil, fmt.Errorf("message %d: source has no tokens", record.ID)
	}
	p := &Projection{RecordID: record.ID}
	anchorCounts := make(map[string]int)
	fixedBreaks := fixedLineBreaks(record.Tokens)
	var region []corpus.Token
	flush := func() error {
		if len(region) == 0 {
			return nil
		}
		meaningful := false
		for _, token := range region {
			meaningful = meaningful || token.Kind == "text" || movableSubstitution(token)
		}
		if !meaningful {
			for _, token := range region {
				p.nodes = append(p.nodes, projectionNode{fixed: true, kind: token.Kind, raw: bytes.Clone(token.Raw)})
			}
			region = nil
			return nil
		}
		var raw []byte
		var substitutions []byte
		var kana [][]byte
		for _, token := range region {
			raw = append(raw, token.Raw...)
			if movableSubstitution(token) {
				substitutions = append(substitutions, token.Raw[1])
			}
			if token.Kind == "kana_mode" {
				kana = append(kana, bytes.Clone(token.Raw))
			}
		}
		visible, err := displayRaw(raw)
		if err != nil {
			return fmt.Errorf("message %d: display fragment: %w", record.ID, err)
		}
		sourceLayout := strings.ReplaceAll(visible, lineBreak, "\n")
		source := strings.ReplaceAll(visible, lineBreak, "")
		fragment := Fragment{Key: fragmentKey(p), Source: source, SourceLayout: sourceLayout, Substitutions: substitutions}
		for _, opcode := range substitutions {
			base := anchorBase(opcode)
			anchorCounts[base]++
			anchor := Anchor{Name: fmt.Sprintf("%s_%d", base, anchorCounts[base]), Opcode: opcode}
			fragment.Anchors = append(fragment.Anchors, anchor)
			tag := fmt.Sprintf("<value:$%02X>", opcode)
			marker := "{{" + anchor.Name + "}}"
			fragment.Source = strings.Replace(fragment.Source, tag, marker, 1)
			fragment.SourceLayout = strings.Replace(fragment.SourceLayout, tag, marker, 1)
		}
		if formatSignatureIDs[record.ID] {
			fragment.FormatSignature = printfConversion.FindAllString(source, -1)
		}
		p.Fragments = append(p.Fragments, fragment)
		p.nodes = append(p.nodes, projectionNode{fragment: len(p.Fragments) - 1, kanaControl: kana})
		region = nil
		return nil
	}
	for index, token := range record.Tokens {
		if token.Kind == "text" || movableSubstitution(token) || token.Kind == "kana_mode" || token.Kind == "line_break" && !fixedBreaks[index] {
			region = append(region, token)
			continue
		}
		if err := flush(); err != nil {
			return nil, err
		}
		display := ""
		if token.Kind != "suffix" && token.Kind != "archive_padding" && token.Kind != "kana_mode" {
			var err error
			display, err = displayRaw(token.Raw)
			if err != nil {
				return nil, fmt.Errorf("message %d: display %s: %w", record.ID, token.Kind, err)
			}
		}
		p.nodes = append(p.nodes, projectionNode{fixed: true, kind: token.Kind, display: display, raw: bytes.Clone(token.Raw)})
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(p.Fragments) == 0 {
		return nil, fmt.Errorf("message %d: source has no translatable fragments", record.ID)
	}
	return p, nil
}

func fragmentKey(p *Projection) string {
	base := "body"
	for index := len(p.nodes) - 1; index >= 0; index-- {
		n := p.nodes[index]
		if !n.fixed {
			break
		}
		if n.kind == "expression" {
			if value, ok := simpleCase(n.raw); ok {
				base = fmt.Sprintf("case_%03d", value)
			} else {
				base = "fallback"
			}
			break
		}
		if n.kind == "if" || n.kind == "select" {
			base = "fallback"
			break
		}
		if n.kind == "block_terminator" || n.kind == "separator" {
			break
		}
	}
	count := 0
	for _, f := range p.Fragments {
		if f.Key == base || strings.HasPrefix(f.Key, base+"_") {
			count++
		}
	}
	if count == 0 {
		return base
	}
	return fmt.Sprintf("%s_%02d", base, count+1)
}

func simpleCase(raw []byte) (int, bool) {
	if len(raw) < 5 || raw[0] != 2 || raw[2] != 4 || raw[3] != '=' {
		return 0, false
	}
	digits := raw[4:]
	if len(digits) > 0 && digits[0] == '%' {
		digits = digits[1:]
	}
	if len(digits) == 0 {
		return 0, false
	}
	value := 0
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		value = value*10 + int(digit-'0')
	}
	return value, true
}

func anchorBase(opcode byte) string {
	return map[byte]string{
		0x15: "STRING_ARG_1", 0x16: "STRING_ARG_2", 0x17: "STRING_ARG_3",
		0x1a: "INTEGER_ARG_1", 0x1b: "INTEGER_ARG_2", 0x24: "YEAR",
		0x25: "MONTH", 0x28: "PLAYER_NAME", 0x2b: "PLAYER_TITLE",
	}[opcode]
}

func fixedLineBreaks(tokens []corpus.Token) map[int]bool {
	fixed := make(map[int]bool)
	for index, token := range tokens {
		if token.Kind != "line_break" {
			continue
		}
		for _, step := range []int{-1, 1} {
			for cursor := index + step; cursor >= 0 && cursor < len(tokens); cursor += step {
				kind := tokens[cursor].Kind
				if kind == "line_break" || kind == "kana_mode" {
					continue
				}
				if semanticJoin[kind] {
					fixed[index] = true
				}
				break
			}
		}
	}
	return fixed
}

func movableSubstitution(token corpus.Token) bool {
	if token.Kind != "substitution" || len(token.Raw) != 2 || token.Raw[0] != 2 {
		return false
	}
	return pureMovable[token.Raw[1]] || callerMovable[token.Raw[1]]
}

func displayRaw(raw []byte) (string, error) {
	data := make([]byte, 4+len(raw))
	data[0], data[2] = 1, 4
	copy(data[4:], raw)
	bank, err := corpus.ParseBank("msgsec000.dat", data)
	if err != nil {
		return "", err
	}
	return bank.Records[0].Display, nil
}

// SplitSemantic validates fixed source controls and splits canonical annotated
// text into source-declared fragments.
func (p *Projection) SplitSemantic(text string) ([]string, error) {
	if text == "" {
		return nil, fmt.Errorf("message %d: canonical text must be nonempty", p.RecordID)
	}
	values := make([]string, len(p.Fragments))
	cursor := 0
	for index, node := range p.nodes {
		if node.fixed {
			if node.display != "" && !strings.HasPrefix(text[cursor:], node.display) {
				return nil, fmt.Errorf("message %d: canonical text changes fixed %s control", p.RecordID, node.kind)
			}
			cursor += len(node.display)
			continue
		}
		next := ""
		for _, following := range p.nodes[index+1:] {
			if !following.fixed {
				break
			}
			next += following.display
		}
		end := len(text)
		if next != "" {
			relative := strings.Index(text[cursor:], next)
			if relative < 0 {
				return nil, fmt.Errorf("message %d: canonical text is missing a fixed control", p.RecordID)
			}
			end = cursor + relative
		}
		values[node.fragment] = text[cursor:end]
		cursor = end
	}
	if cursor != len(text) {
		return nil, fmt.Errorf("message %d: canonical text has trailing material outside its projection", p.RecordID)
	}
	for index, value := range values {
		if err := p.validateFragment(index, value); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (p *Projection) validateFragment(index int, value string) error {
	f := p.Fragments[index]
	found := valueTag.FindAllStringSubmatch(value, -1)
	available := make(map[byte]int, len(f.Substitutions))
	for _, opcode := range f.Substitutions {
		available[opcode]++
	}
	for _, match := range found {
		var opcode byte
		_, _ = fmt.Sscanf(match[1], "%02X", &opcode)
		if available[opcode] == 0 {
			return fmt.Errorf("message %d fragment %s changes runtime substitutions", p.RecordID, f.Key)
		}
		available[opcode]--
	}
	if formatSignatureIDs[p.RecordID] {
		got := printfConversion.FindAllString(value, -1)
		if strings.Count(value, "%") != len(got) || strings.Join(got, "\x00") != strings.Join(f.FormatSignature, "\x00") {
			return fmt.Errorf("message %d fragment %s changes runtime format signature", p.RecordID, f.Key)
		}
	}
	plain := valueTag.ReplaceAllString(value, "")
	plain = strings.ReplaceAll(plain, lineBreak, "")
	if reservedMarkup.MatchString(plain) || reservedAnchor.MatchString(plain) {
		return fmt.Errorf("message %d fragment %s contains reserved markup", p.RecordID, f.Key)
	}
	return validateText(p.RecordID, f.Key, plain)
}

func validateText(id int, key, text string) error {
	for _, r := range text {
		if unicode.IsControl(r) {
			return fmt.Errorf("message %d fragment %s contains a raw control character", id, key)
		}
		if r >= 0xff61 && r <= 0xff9f {
			return fmt.Errorf("message %d fragment %s contains half-width kana", id, key)
		}
	}
	encoded, err := cp932.Encode(text)
	if err != nil {
		return fmt.Errorf("message %d fragment %s is not encodable as CP932: %w", id, key, err)
	}
	decoded, err := cp932.Decode(encoded)
	if err != nil || decoded != text {
		return fmt.Errorf("message %d fragment %s does not round-trip through CP932", id, key)
	}
	for index := 0; index < len(encoded); index++ {
		if encoded[index] >= 0x81 && encoded[index] <= 0x9f || encoded[index] >= 0xe0 && encoded[index] <= 0xfc {
			index++
		} else if encoded[index] >= 0xa1 && encoded[index] <= 0xdf {
			return fmt.Errorf("message %d fragment %s contains half-width kana", id, key)
		}
	}
	return nil
}

// Materialize lowers canonical annotated text while preserving all source-owned
// controls. Layout breaks are accepted only when layout is true.
func (p *Projection) Materialize(text string, layout bool) ([]byte, error) {
	values, err := p.SplitSemantic(text)
	if err != nil {
		return nil, err
	}
	var output []byte
	for _, node := range p.nodes {
		if node.fixed {
			output = append(output, node.raw...)
			continue
		}
		value := values[node.fragment]
		matches := regexp.MustCompile(`(<value:\$[0-9A-F]{2}>|<line-break>)`).FindAllStringIndex(value, -1)
		cursor := 0
		for _, match := range matches {
			encoded, err := cp932.Encode(value[cursor:match[0]])
			if err != nil {
				return nil, err
			}
			output = append(output, encoded...)
			piece := value[match[0]:match[1]]
			if piece == lineBreak {
				if !layout {
					return nil, fmt.Errorf("message %d: semantic text contains a layout break", p.RecordID)
				}
				output = append(output, 10)
			} else {
				var opcode byte
				_, _ = fmt.Sscanf(piece, "<value:$%02X>", &opcode)
				output = append(output, 2, opcode)
			}
			cursor = match[1]
		}
		encoded, err := cp932.Encode(value[cursor:])
		if err != nil {
			return nil, err
		}
		output = append(output, encoded...)
		for _, control := range node.kanaControl {
			output = append(output, control...)
		}
	}
	return output, nil
}
