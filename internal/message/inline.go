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
var inlineAnnotatedTag = regexp.MustCompile(`<[^<>]+>`)

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

// ValidateInlineStructure verifies that translated controls preserve the
// immutable source control topology while allowing only block payloads to
// differ.
func ValidateInlineStructure(source, translated []InlineControl) error {
	if len(source) != len(translated) {
		return fmt.Errorf("inline control count differs from source")
	}
	for controlIndex := range source {
		left, right := source[controlIndex], translated[controlIndex]
		if left.Kind != right.Kind || left.Selector != right.Selector || len(left.Blocks) != len(right.Blocks) {
			return fmt.Errorf("inline control %d differs from source structure", controlIndex)
		}
		if (left.ExpectedBlocks == nil) != (right.ExpectedBlocks == nil) || left.ExpectedBlocks != nil && *left.ExpectedBlocks != *right.ExpectedBlocks {
			return fmt.Errorf("inline control %d differs from source block count", controlIndex)
		}
		for blockIndex := range left.Blocks {
			if left.Blocks[blockIndex].Role != right.Blocks[blockIndex].Role || left.Blocks[blockIndex].Condition != right.Blocks[blockIndex].Condition {
				return fmt.Errorf("inline control %d block %d differs from source structure", controlIndex, blockIndex)
			}
		}
	}
	return nil
}

// RenderInlineControls reconstructs canonical annotated text from parsed
// controls. It owns all control syntax; callers provide only block payloads.
func RenderInlineControls(controls []InlineControl) (string, error) {
	if len(controls) == 0 {
		return "", fmt.Errorf("inline controls are required")
	}
	var output strings.Builder
	for controlIndex, control := range controls {
		if len(control.Blocks) == 0 {
			return "", fmt.Errorf("inline control %d has no blocks", controlIndex)
		}
		switch control.Kind {
		case "selection":
			if control.Selector == "" {
				return "", fmt.Errorf("inline selection %d has no selector", controlIndex)
			}
			for blockIndex, block := range control.Blocks {
				if block.Role != "selection_arm" || block.Condition != "" {
					return "", fmt.Errorf("inline selection %d block %d has invalid structure", controlIndex, blockIndex)
				}
				if blockIndex == 0 {
					output.WriteString("<select>")
					output.WriteString(control.Selector)
				}
				output.WriteString(block.Text)
				output.WriteString("<end>")
			}
		case "conditional":
			for blockIndex, block := range control.Blocks {
				switch block.Role {
				case "condition":
					if block.Condition == "" {
						return "", fmt.Errorf("inline conditional %d block %d has no condition", controlIndex, blockIndex)
					}
					output.WriteString("<if>")
					output.WriteString(block.Condition)
				case "unconditioned", "fallback":
					if block.Condition != "" {
						return "", fmt.Errorf("inline conditional %d block %d has an unexpected condition", controlIndex, blockIndex)
					}
				default:
					return "", fmt.Errorf("inline conditional %d block %d has invalid role %q", controlIndex, blockIndex, block.Role)
				}
				output.WriteString(block.Text)
				output.WriteString("<end>")
			}
		default:
			return "", fmt.Errorf("inline control %d has unsupported kind %q", controlIndex, control.Kind)
		}
	}
	return output.String(), nil
}

// ValidateInlineBlock verifies one translated payload without accepting
// source-owned control syntax or changes to runtime substitutions.
func ValidateInlineBlock(recordID int, source, translated string) error {
	sourceValues := valueTag.FindAllString(source, -1)
	translatedValues := valueTag.FindAllString(translated, -1)
	if len(sourceValues) != len(translatedValues) {
		return fmt.Errorf("message %d inline block changes runtime substitutions", recordID)
	}
	counts := make(map[string]int, len(sourceValues))
	for _, value := range sourceValues {
		counts[value]++
	}
	for _, value := range translatedValues {
		counts[value]--
	}
	for _, count := range counts {
		if count != 0 {
			return fmt.Errorf("message %d inline block changes runtime substitutions", recordID)
		}
	}
	fixedTags := func(text string) []string {
		result := make([]string, 0)
		for _, tag := range inlineAnnotatedTag.FindAllString(text, -1) {
			if tag != lineBreak && !valueTag.MatchString(tag) {
				result = append(result, tag)
			}
		}
		return result
	}
	if strings.Join(fixedTags(source), "\x00") != strings.Join(fixedTags(translated), "\x00") {
		return fmt.Errorf("message %d inline block changes fixed annotated controls", recordID)
	}
	if formatSignatureIDs[recordID] {
		sourceSignature := printfConversion.FindAllString(source, -1)
		translatedSignature := printfConversion.FindAllString(translated, -1)
		if strings.Count(translated, "%") != len(translatedSignature) || strings.Join(sourceSignature, "\x00") != strings.Join(translatedSignature, "\x00") {
			return fmt.Errorf("message %d inline block changes runtime format signature", recordID)
		}
	}
	plain := inlineAnnotatedTag.ReplaceAllString(translated, "")
	if reservedMarkup.MatchString(plain) || reservedAnchor.MatchString(plain) {
		return fmt.Errorf("message %d inline block contains reserved markup", recordID)
	}
	return validateText(recordID, "inline_block", plain)
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
