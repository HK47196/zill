// SPDX-License-Identifier: GPL-3.0-or-later

package message

import (
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/HK47196/zill/internal/corpus"
)

// RuntimeBankCapacity returns the relocated runtime slot size for a section.
func RuntimeBankCapacity(section int) int {
	if section == 0 {
		return 0x9000
	}
	if section >= 165 && section <= 167 {
		return 0x4000
	}
	return 0x17000
}

// CompileBank lowers a complete bank's joined corpus items into the patched
// runtime's aligned absolute uint32-offset representation. Todo and
// keep_japanese records remain byte-identical to their retail source.
func CompileBank(bank corpus.Bank, items []corpus.Item) ([]byte, error) {
	if len(items) != len(bank.Records) {
		return nil, fmt.Errorf("%s: compilation has %d items for %d source records", bank.Name, len(items), len(bank.Records))
	}
	if len(items) > math.MaxUint16 {
		return nil, fmt.Errorf("%s: message count exceeds uint16", bank.Name)
	}
	records := make([][]byte, len(items))
	for index, item := range items {
		source := bank.Records[index]
		if item.Record.ID != source.ID || item.Translation.ID != source.ID {
			return nil, fmt.Errorf("%s: item %d does not match source ID %d", bank.Name, index, source.ID)
		}
		switch item.Translation.State {
		case corpus.Todo, corpus.KeepJapanese:
			records[index] = append([]byte(nil), source.Raw...)
		case corpus.Translated:
			projection, err := Project(source)
			if err != nil {
				return nil, err
			}
			authored := strings.Contains(item.Translation.Text, lineBreak)
			text, layout := item.Translation.Text, authored
			if item.Layout != "" {
				if authored {
					if item.Layout != item.Translation.Text {
						return nil, fmt.Errorf("%s: ID %d: generated layout changes explicitly authored line breaks", bank.Name, source.ID)
					}
				} else {
					if _, err := projection.Materialize(item.Translation.Text, false); err != nil {
						return nil, fmt.Errorf("%s: ID %d semantic text: %w", bank.Name, source.ID, err)
					}
					if !preservesSemantics(item.Translation.Text, item.Layout) {
						return nil, fmt.Errorf("%s: ID %d: layout changes semantic/control text; only complete whitespace spans may become line breaks", bank.Name, source.ID)
					}
				}
				text, layout = item.Layout, true
			}
			records[index], err = projection.Materialize(text, layout)
			if err != nil {
				return nil, fmt.Errorf("%s: ID %d: %w", bank.Name, source.ID, err)
			}
		default:
			return nil, fmt.Errorf("%s: ID %d: unsupported translation state %q", bank.Name, source.ID, item.Translation.State)
		}
	}
	tableEnd := uint64(4 + len(records)*4)
	position := tableEnd
	for _, record := range records {
		position += uint64(len(record))
	}
	if position > math.MaxUint32 {
		return nil, fmt.Errorf("%s: compiled bank is %d bytes; uint32 maximum is %d", bank.Name, position, uint64(math.MaxUint32))
	}
	if position > uint64(RuntimeBankCapacity(bank.Section)) {
		return nil, fmt.Errorf("%s: compiled bank is %d bytes; runtime slot capacity is %d (shorten translations by at least %d encoded bytes)", bank.Name, position, RuntimeBankCapacity(bank.Section), position-uint64(RuntimeBankCapacity(bank.Section)))
	}
	output := make([]byte, int(position))
	binary.LittleEndian.PutUint16(output, uint16(len(records)))
	offset := tableEnd
	for index, record := range records {
		binary.LittleEndian.PutUint32(output[4+index*4:], uint32(offset))
		copy(output[int(offset):], record)
		offset += uint64(len(record))
	}
	return output, nil
}

type semanticUnit struct{ kind, value string }

var annotatedControl = regexp.MustCompile(`<[^<>]+>`)

func semanticUnits(text string) []semanticUnit {
	var units []semanticUnit
	for len(text) > 0 {
		if location := annotatedControl.FindStringIndex(text); location != nil && location[0] == 0 {
			value := text[:location[1]]
			kind := "literal"
			if value == "<line-break>" {
				kind = "boundary"
			}
			units = append(units, semanticUnit{kind, value})
			text = text[location[1]:]
			continue
		}
		end := len(text)
		if location := annotatedControl.FindStringIndex(text); location != nil {
			end = location[0]
		}
		plain := text[:end]
		for len(plain) > 0 {
			first, _ := utf8.DecodeRuneInString(plain)
			space := unicode.IsSpace(first)
			cursor := 0
			for cursor < len(plain) {
				r, width := utf8.DecodeRuneInString(plain[cursor:])
				if unicode.IsSpace(r) != space {
					break
				}
				cursor += width
			}
			kind := "literal"
			if space {
				kind = "whitespace"
			}
			units = append(units, semanticUnit{kind, plain[:cursor]})
			plain = plain[cursor:]
		}
		text = text[end:]
	}
	return units
}

func preservesSemantics(semantic, layout string) bool {
	want, got := semanticUnits(semantic), semanticUnits(layout)
	if len(want) != len(got) {
		return false
	}
	for index := range want {
		if want[index] == got[index] {
			continue
		}
		if want[index].kind != "whitespace" || got[index].kind != "whitespace" && got[index] != (semanticUnit{"boundary", lineBreak}) {
			return false
		}
	}
	return true
}
