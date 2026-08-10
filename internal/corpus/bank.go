// SPDX-License-Identifier: GPL-3.0-or-later

// Package corpus reads contributor message data and authenticated retail banks.
package corpus

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/cp932"
	"golang.org/x/text/unicode/norm"
)

var bankName = regexp.MustCompile(`^msgsec([0-9]{3})\.dat$`)

var archivePadding = []byte("ZillO'll ")

// Token is one lossless editable-text or source-owned control span.
type Token struct {
	Kind string
	Raw  []byte
	Text string
}

// Record is one native message-bank record.
type Record struct {
	ID                 int
	Index              int
	Offset             int
	Raw                []byte
	DisplaySize        int
	HasBlockTerminator bool
	Display            string
	Tokens             []Token
}

// Bank is one retail-format msgsecNNN.dat file.
type Bank struct {
	Name    string
	Section int
	Records []Record
}

// ReadBank reads and validates a native message bank without modifying it.
func ReadBank(path string) (Bank, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Bank{}, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseBank(filepath.Base(path), data)
}

// ParseBank validates and parses a retail uint16-offset message bank.
func ParseBank(name string, data []byte) (Bank, error) {
	match := bankName.FindStringSubmatch(name)
	if match == nil {
		return Bank{}, fmt.Errorf("%s: expected filename msgsecNNN.dat", name)
	}
	section, err := strconv.Atoi(match[1])
	if err != nil {
		return Bank{}, fmt.Errorf("%s: invalid section: %w", name, err)
	}
	if len(data) < 2 {
		return Bank{}, fmt.Errorf("%s: file is too small for a message count", name)
	}

	count := int(binary.LittleEndian.Uint16(data[:2]))
	tableEnd := 2 + count*2
	if tableEnd > len(data) {
		return Bank{}, fmt.Errorf("%s: message offset table extends past end of file", name)
	}
	offsets := make([]int, count)
	previous := tableEnd
	for index := range count {
		offset := int(binary.LittleEndian.Uint16(data[2+index*2:]))
		if offset < tableEnd || offset < previous || offset > len(data) {
			return Bank{}, fmt.Errorf(
				"%s: invalid offset %#x for message %d", name, offset, index,
			)
		}
		offsets[index] = offset
		previous = offset
	}

	bank := Bank{Name: name, Section: section, Records: make([]Record, count)}
	for index, start := range offsets {
		end := len(data)
		if index+1 < len(offsets) {
			end = offsets[index+1]
		}
		raw := bytes.Clone(data[start:end])
		displayEnd := len(raw)
		if nul := bytes.IndexByte(raw, 0); nul >= 0 {
			displayEnd = nul
		} else if padding := archivePaddingOffset(raw); padding >= 0 {
			displayEnd = padding
		}
		displayRaw := raw[:displayEnd]
		bank.Records[index] = Record{
			ID:                 section*10_000 + index,
			Index:              index,
			Offset:             start,
			Raw:                raw,
			DisplaySize:        displayEnd,
			HasBlockTerminator: bytes.Contains(displayRaw, []byte{5, 5, 5}),
			Display:            displayText(displayRaw),
			Tokens:             tokenize(raw),
		}
	}
	return bank, nil
}

func archivePaddingOffset(data []byte) int {
	terminator := bytes.LastIndex(data, []byte{5, 5, 5})
	if terminator < 0 {
		return -1
	}
	offset := terminator + 3
	tail := data[offset:]
	if len(tail) == 0 {
		return -1
	}
	for index, value := range tail {
		if value != archivePadding[index%len(archivePadding)] {
			return -1
		}
	}
	return offset
}

func tokenize(data []byte) []Token {
	var tokens []Token
	var text []byte
	paddingOffset := -1
	if !bytes.Contains(data, []byte{0}) {
		paddingOffset = archivePaddingOffset(data)
	}

	flushText := func() {
		if len(text) == 0 {
			return
		}
		raw := bytes.Clone(text)
		decoded, err := cp932.Decode(raw)
		if err != nil {
			tokens = append(tokens, Token{Kind: "undecodable_data", Raw: raw})
		} else {
			tokens = append(tokens, Token{Kind: "text", Raw: raw, Text: decoded})
		}
		text = text[:0]
	}
	appendLocked := func(kind string, start, end int) {
		flushText()
		tokens = append(tokens, Token{Kind: kind, Raw: bytes.Clone(data[start:end])})
	}

	for index := 0; index < len(data); {
		if index == paddingOffset {
			appendLocked("archive_padding", index, len(data))
			break
		}
		value := data[index]
		if value == 0 {
			appendLocked("suffix", index, len(data))
			break
		}
		if index+3 < len(data) && value == 1 && (data[index+1] == 'C' || data[index+1] == 'J') {
			kind := "call"
			if data[index+1] == 'J' {
				kind = "jump"
			}
			appendLocked(kind, index, index+4)
			index += 4
			continue
		}
		if index+1 < len(data) && value == 1 && (data[index+1] == 'I' || data[index+1] == 'S') {
			kind := "if"
			tier := "boolean"
			if data[index+1] == 'S' {
				kind = "select"
				tier = "arithmetic"
			}
			appendLocked(kind, index, index+2)
			index += 2
			end := expressionEnd(data, index, tier)
			if end > index {
				appendLocked("expression", index, end)
				index = end
			}
			continue
		}
		if value == 1 {
			end := min(index+2, len(data))
			appendLocked("flow_control", index, end)
			index = end
			continue
		}
		if value == 2 && index+1 < len(data) {
			opcode := data[index+1]
			appendLocked("substitution", index, index+2)
			index += 2
			if opcode == 0x1f || opcode == 0x20 {
				end := expressionEnd(data, index, "arithmetic")
				if end > index {
					appendLocked("expression", index, end)
					index = end
				}
			}
			continue
		}
		if (value == 3 || value == 4 || value == 6) && index+1 < len(data) {
			appendLocked("expression_operator", index, index+2)
			index += 2
			continue
		}
		if index+1 < len(data) && value == 0x1b && (data[index+1] == 'K' || data[index+1] == 'H' || data[index+1] == 'k') {
			appendLocked("kana_mode", index, index+2)
			index += 2
			continue
		}
		if index+2 < len(data) && value == 0x1b && data[index+1] == 'C' {
			appendLocked("color", index, index+3)
			index += 3
			continue
		}
		if value == 0x1b {
			end := index + 1
			if index+1 < len(data) {
				end = index + 2
				if 0x43 <= data[index+1] && data[index+1] <= 0x6b && index+2 < len(data) {
					end = index + 3
				}
			}
			appendLocked("renderer_control", index, end)
			index = end
			continue
		}
		if index+2 < len(data) && bytes.Equal(data[index:index+3], []byte{5, 5, 5}) {
			appendLocked("block_terminator", index, index+3)
			index += 3
			continue
		}
		if value == 5 {
			appendLocked("separator", index, index+1)
			index++
			continue
		}
		if value == 8 || value == 9 || value == 10 {
			kind := map[byte]string{8: "backspace", 9: "tab", 10: "line_break"}[value]
			appendLocked(kind, index, index+1)
			index++
			continue
		}
		if value < 0x20 || value == 0x7f {
			appendLocked("unknown_control", index, index+1)
			index++
			continue
		}
		if isDoubleByteLead(value) && index+1 < len(data) {
			text = append(text, data[index:index+2]...)
			index += 2
			continue
		}
		text = append(text, value)
		index++
	}
	flushText()
	return tokens
}

func expressionEnd(data []byte, index int, tier string) int {
	var atomEnd func(int) int
	var arithmeticEnd func(int) int
	atomEnd = func(position int) int {
		if position >= len(data) {
			return position
		}
		if data[position] == 2 && position+1 < len(data) {
			position += 2
			if data[position-1] == 0x1f || data[position-1] == 0x20 {
				return arithmeticEnd(position)
			}
			return position
		}
		if data[position] == '%' {
			position++
		}
		start := position
		for position < len(data) && '0' <= data[position] && data[position] <= '9' {
			position++
		}
		if position > start {
			return position
		}
		return start
	}
	arithmeticEnd = func(position int) int {
		position = atomEnd(position)
		for position+1 < len(data) && data[position] == 3 {
			next := atomEnd(position + 2)
			if next == position+2 {
				break
			}
			position = next
		}
		return position
	}
	comparisonEnd := func(position int) int {
		position = arithmeticEnd(position)
		if position+1 < len(data) && data[position] == 4 {
			next := arithmeticEnd(position + 2)
			if next > position+2 {
				position = next
			}
		}
		return position
	}

	position := comparisonEnd(index)
	if tier == "boolean" {
		for position+1 < len(data) && data[position] == 6 {
			next := comparisonEnd(position + 2)
			if next == position+2 {
				break
			}
			position = next
		}
	}
	return position
}

func displayText(data []byte) string {
	var output strings.Builder
	var text []byte
	kanaMode := "hiragana"
	flush := func() {
		if len(text) == 0 {
			return
		}
		decoded, err := cp932.Decode(text)
		if err != nil {
			for _, value := range text {
				fmt.Fprintf(&output, "\\x%02x", value)
			}
		} else {
			output.WriteString(decoded)
		}
		text = text[:0]
	}

	for index := 0; index < len(data); {
		value := data[index]
		if index+1 < len(data) && value == 1 && (data[index+1] == 'I' || data[index+1] == 'S') {
			flush()
			if data[index+1] == 'I' {
				output.WriteString("<if>")
			} else {
				output.WriteString("<select>")
			}
			index += 2
			continue
		}
		if index+3 < len(data) && value == 1 && (data[index+1] == 'C' || data[index+1] == 'J') {
			flush()
			command := "call"
			if data[index+1] == 'J' {
				command = "jump"
			}
			target := binary.LittleEndian.Uint16(data[index+2:])
			fmt.Fprintf(&output, "<%s:%d>", command, target)
			index += 4
			continue
		}
		if value == 2 && index+1 < len(data) {
			flush()
			fmt.Fprintf(&output, "<value:$%02X>", data[index+1])
			index += 2
			continue
		}
		if (value == 3 || value == 4 || value == 6) && index+1 < len(data) {
			flush()
			if name := operatorName(value, data[index+1]); name != "" {
				fmt.Fprintf(&output, "<%s>", name)
			} else {
				fmt.Fprintf(&output, "<operator:$%02X:$%02X>", value, data[index+1])
			}
			index += 2
			continue
		}
		if index+1 < len(data) && value == 0x1b && (data[index+1] == 'K' || data[index+1] == 'H' || data[index+1] == 'k') {
			flush()
			kanaMode = map[byte]string{'K': "katakana", 'H': "hiragana", 'k': "halfwidth"}[data[index+1]]
			index += 2
			continue
		}
		if index+2 < len(data) && value == 0x1b && data[index+1] == 'C' {
			flush()
			fmt.Fprintf(&output, "<color:%c>", data[index+2])
			index += 3
			continue
		}
		if value == 0x1b && index+1 < len(data) {
			flush()
			command := data[index+1]
			if 0x43 <= command && command <= 0x6b && index+2 < len(data) {
				fmt.Fprintf(&output, "<discard:%c:$%02X>", command, data[index+2])
				index += 3
			} else {
				fmt.Fprintf(&output, "<escape:$%02X>", command)
				index += 2
			}
			continue
		}
		if index+2 < len(data) && bytes.Equal(data[index:index+3], []byte{5, 5, 5}) {
			flush()
			output.WriteString("<end>")
			index += 3
			continue
		}
		if value == 5 {
			flush()
			output.WriteString("<separator>")
			index++
			continue
		}
		if value == 8 || value == 9 || value == 10 {
			flush()
			output.WriteString(map[byte]string{8: "<backspace>", 9: "<tab>", 10: "<line-break>"}[value])
			index++
			continue
		}
		if isDoubleByteLead(value) && index+1 < len(data) {
			text = append(text, data[index:index+2]...)
			index += 2
			continue
		}
		if 0xa6 <= value && value <= 0xdf {
			flush()
			end := index + 1
			if kanaMode != "halfwidth" && end < len(data) && (data[end] == 0xde || data[end] == 0xdf) {
				end++
			}
			kana, err := cp932.Decode(data[index:end])
			if err != nil {
				fmt.Fprintf(&output, "\\x%02x", value)
				index++
				continue
			}
			if kanaMode != "halfwidth" {
				kana = norm.NFKC.String(kana)
				if kanaMode == "hiragana" {
					converted := []rune(kana)
					for position, character := range converted {
						if 'ァ' <= character && character <= 'ヶ' {
							converted[position] = character - 0x60
						}
					}
					kana = string(converted)
				}
			}
			output.WriteString(kana)
			index = end
			continue
		}
		if value < 0x20 || value == 0x7f {
			flush()
			fmt.Fprintf(&output, "<$%02X>", value)
			index++
			continue
		}
		text = append(text, value)
		index++
	}
	flush()
	return output.String()
}

func isDoubleByteLead(value byte) bool {
	return 0x81 <= value && value <= 0x9f || 0xe0 <= value && value <= 0xfc
}

func operatorName(tier, operator byte) string {
	operators := map[byte]map[byte]string{
		3: {'+': "add", '-': "subtract", '*': "multiply", '/': "divide", '%': "modulo"},
		4: {'=': "equal", '!': "not-equal", '<': "less", '>': "greater", '{': "less-equal", '}': "greater-equal"},
		6: {'&': "and", '|': "or"},
	}
	return operators[tier][operator]
}
