// SPDX-License-Identifier: GPL-3.0-or-later

package message

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var inlineIfPrefix = regexp.MustCompile(`^<if>(<value:\$[0-9A-F]{2}>(?:<(?:equal|not-equal|less|greater|less-equal|greater-equal)>%?[0-9]+)?)`)
var inlineSelectPrefix = regexp.MustCompile(`^<select>(<value:\$[0-9A-F]{2}>)(%[0-9]+)?`)

// InlineControl is record-local retail message control represented without
// evaluating game state or inferring cross-record flow.
type InlineControl struct {
	Kind           string
	Selector       string
	ExpectedBlocks *int
	Blocks         []InlineBlock
}

// InlineBlock is one source-declared, end-terminated output block.
type InlineBlock struct {
	Position  int
	Role      string
	Condition string
	Text      string
}

// ParseInlineControls parses ordered record-local conditional and selection
// groups from canonical annotated message text. Ordinary substitutions outside
// a control prefix are not interpreted.
func ParseInlineControls(text string) ([]InlineControl, error) {
	if !strings.HasPrefix(text, "<if>") && !strings.HasPrefix(text, "<select>") {
		return nil, nil
	}
	segments, err := splitInlineBlocks(text)
	if err != nil {
		return nil, err
	}
	var controls []InlineControl
	for position := 0; position < len(segments); {
		switch {
		case strings.HasPrefix(segments[position], "<select>"):
			control, consumed, err := parseInlineSelection(segments[position:], position)
			if err != nil {
				return nil, err
			}
			controls = append(controls, control)
			position += consumed
		case strings.HasPrefix(segments[position], "<if>"):
			end := position + 1
			for end < len(segments) && !strings.HasPrefix(segments[end], "<select>") {
				end++
			}
			control, err := parseInlineConditional(segments[position:end], position)
			if err != nil {
				return nil, err
			}
			controls = append(controls, control)
			position = end
		default:
			return nil, fmt.Errorf("inline control has an unowned block at position %d", position)
		}
	}
	return controls, nil
}

func parseInlineSelection(segments []string, position int) (InlineControl, int, error) {
	match := inlineSelectPrefix.FindStringSubmatch(segments[0])
	if match == nil {
		return InlineControl{}, 0, fmt.Errorf("malformed inline selection prefix")
	}
	control := InlineControl{Kind: "selection", Selector: match[1] + match[2]}
	count := len(segments)
	if match[2] != "" {
		count, _ = strconv.Atoi(strings.TrimPrefix(match[2], "%"))
		control.ExpectedBlocks = &count
		if count > len(segments) {
			return InlineControl{}, 0, fmt.Errorf("inline selection declares %d blocks but contains %d", count, len(segments))
		}
	}
	control.Blocks = make([]InlineBlock, count)
	for index := range count {
		text := segments[index]
		if index == 0 {
			text = strings.TrimPrefix(text, match[0])
		}
		control.Blocks[index] = InlineBlock{Position: position + index, Role: "selection_arm", Text: text}
	}
	return control, count, nil
}

func parseInlineConditional(segments []string, position int) (InlineControl, error) {
	control := InlineControl{Kind: "conditional", Blocks: make([]InlineBlock, 0, len(segments))}
	for index, segment := range segments {
		block := InlineBlock{Position: position + index, Role: "fallback", Text: segment}
		if strings.HasPrefix(segment, "<if>") {
			match := inlineIfPrefix.FindStringSubmatch(segment)
			if match == nil {
				return InlineControl{}, fmt.Errorf("malformed inline conditional prefix")
			}
			block.Role = "condition"
			block.Condition = match[1]
			block.Text = strings.TrimPrefix(segment, match[0])
		} else if hasLaterInlineCondition(segments[index+1:]) {
			block.Role = "unconditioned"
		}
		control.Blocks = append(control.Blocks, block)
	}
	return control, nil
}

func hasLaterInlineCondition(segments []string) bool {
	for _, segment := range segments {
		if strings.HasPrefix(segment, "<if>") {
			return true
		}
	}
	return false
}

func splitInlineBlocks(text string) ([]string, error) {
	var blocks []string
	for text != "" {
		block, rest, ok := strings.Cut(text, "<end>")
		if !ok {
			return nil, fmt.Errorf("inline control is missing <end>")
		}
		blocks = append(blocks, block)
		text = rest
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("inline control has no output blocks")
	}
	return blocks, nil
}
