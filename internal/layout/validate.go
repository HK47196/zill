// SPDX-License-Identifier: GPL-3.0-or-later

package layout

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/cp932"
	"github.com/HK47196/zill/internal/message"
)

// Validate enforces every fixed-storage contract for the supported game.
// layouts must contain an effective layout for every translated record.
func (e *Engine) Validate(project *corpus.Project, layouts map[int]string) error {
	if project == nil {
		return fmt.Errorf("layout: nil project")
	}
	effective := make(map[int]string, len(project.Items))
	translated := make(map[int]bool)
	items := make(map[int]corpus.Item, len(project.Items))
	for _, item := range project.Items {
		id := item.Record.ID
		items[id] = item
		if item.Translation.State == corpus.Translated {
			text, ok := layouts[id]
			if !ok || text == "" {
				return fmt.Errorf("message %d: translated record has no effective layout", id)
			}
			effective[id] = text
			translated[id] = true
		} else {
			effective[id] = item.Record.Display
		}
	}
	var failures []string
	checkFixed := func(label string, ids []int, bufferCapacityBytes int) {
		for _, id := range ids {
			if !translated[id] {
				continue
			}
			size, err := expandedBytes(effective[id], id)
			if err != nil {
				failures = append(failures, err.Error())
				continue
			}
			if size >= bufferCapacityBytes {
				failures = append(failures, fmt.Sprintf("%s message %d uses %d bytes (maximum %d)", label, id, size, bufferCapacityBytes-1))
			}
		}
	}
	checkFixed("bounded label", e.consumers.BoundedLabelIDs, boundedLabelBufferCapacityBytes)
	checkFixed("guild client", e.consumers.GuildClientIDs, guildClientBufferCapacityBytes)
	checkFixed("guild region", e.consumers.GuildRegionIDs, guildRegionBufferCapacityBytes)
	if translated[trapID] {
		size, err := expandedBytes(effective[trapID], trapID)
		if err != nil {
			failures = append(failures, err.Error())
		} else {
			size += len(valueTag.FindAllString(effective[trapID], -1)) * trapValueMaxBytes
			if size >= trapBufferCapacityBytes {
				failures = append(failures, fmt.Sprintf("trap message %d uses up to %d bytes (maximum %d)", trapID, size, trapBufferCapacityBytes-1))
			}
		}
	}
	for id := range translated {
		if e.category(id, "character-creation-choice") {
			size, err := expandedBytes(effective[id], id)
			if err != nil {
				failures = append(failures, err.Error())
			} else if size >= characterCreationChoiceCapacityBytes {
				failures = append(failures, fmt.Sprintf("character-creation choice message %d uses %d bytes (maximum %d)", id, size, characterCreationChoiceCapacityBytes-1))
			}
		}
		if e.equipmentFeedback(id) {
			size, err := expandedBytes(effective[id], id)
			if err != nil {
				failures = append(failures, err.Error())
			} else if size >= equipmentFeedbackBufferCapacityBytes {
				failures = append(failures, fmt.Sprintf("equipment feedback message %d uses %d bytes (maximum %d)", id, size, equipmentFeedbackBufferCapacityBytes-1))
			}
		}
		if e.category(id, "chronicle-entry") {
			size, err := expandedBytes(effective[id], id)
			if err != nil {
				failures = append(failures, err.Error())
				continue
			}
			size += strings.Count(effective[id], "<value:$28>") * playerNameMaxEncodedBytes
			if size > chronicleEntryMaxPayloadBytes {
				failures = append(failures, fmt.Sprintf("chronicle entry message %d uses up to %d bytes (maximum %d)", id, size, chronicleEntryMaxPayloadBytes))
			}
		}
	}
	for _, group := range e.consumers.C20Groups {
		changed := false
		total := 0
		missing := false
		for _, id := range group.IDs {
			changed = changed || translated[id]
			text, ok := effective[id]
			if !ok {
				missing = true
				break
			}
			size, err := minimumBytes(text, id)
			if err != nil {
				failures = append(failures, err.Error())
				missing = true
				break
			}
			total += size + 1
		}
		if changed && missing {
			failures = append(failures, fmt.Sprintf("C20 group starting at %d lacks a message", group.IDs[0]))
		} else if changed && total >= c20GroupBufferCapacityBytes {
			failures = append(failures, fmt.Sprintf("C20 group starting at %d uses %d bytes (maximum %d)", group.IDs[0], total, c20GroupBufferCapacityBytes-1))
		}
	}
	for _, id := range e.consumers.C22IDs {
		if !translated[id] {
			continue
		}
		if why, err := e.c22Violation(id, effective[id]); err != nil {
			failures = append(failures, err.Error())
		} else if why != "" {
			failures = append(failures, "C22 "+why)
		}
	}
	c5ids := append(append([]int(nil), e.consumers.C5IDs...), e.consumers.SinglePageC5IDs...)
	sort.Ints(c5ids)
	for _, id := range c5ids {
		if !translated[id] {
			continue
		}
		item, ok := items[id]
		if !ok {
			failures = append(failures, fmt.Sprintf("C5 message %d lacks source", id))
			continue
		}
		if why, err := e.c5Violation(item, effective[id]); err != nil {
			failures = append(failures, err.Error())
		} else if why != "" {
			failures = append(failures, "C5 "+why)
		}
	}
	e.validatePostings(effective, translated, &failures)
	if len(failures) > 0 {
		return fmt.Errorf("consumer storage validation failed:\n- %s", strings.Join(failures, "\n- "))
	}
	return nil
}

func expandedBytes(text string, id int) (int, error) {
	total := 0
	for _, part := range splitParts(normalize(text)) {
		switch {
		case part == lineBreak:
			total++
		case strings.HasPrefix(part, "<color:"):
			total += 3
		case controlTag.MatchString(part) && controlTag.FindString(part) == part:
			continue
		default:
			b, err := cp932.Encode(part)
			if err != nil {
				return 0, fmt.Errorf("message %d storage: %w", id, err)
			}
			total += len(b)
		}
	}
	return total, nil
}
func minimumBytes(text string, id int) (int, error) {
	if formatSignatureID(id) {
		text = printfConversion.ReplaceAllString(text, "")
	}
	return expandedBytes(text, id)
}

func (e *Engine) c22Violation(id int, text string) (string, error) {
	runtime := strings.Split(normalize(text), "<end>")[0]
	lines := strings.Split(runtime, lineBreak)
	total, err := minimumBytes(runtime, id)
	if err != nil {
		return "", err
	}
	var violations []string
	if (len(lines)+3)/4 > c22MaxPages {
		violations = append(violations, fmt.Sprintf("message %d has %d pages (maximum %d)", id, (len(lines)+3)/4, c22MaxPages))
	}
	if total >= c22TotalBufferCapacityBytes {
		violations = append(violations, fmt.Sprintf("message %d uses %d total bytes (maximum %d)", id, total, c22TotalBufferCapacityBytes-1))
	}
	for i, line := range lines {
		size, err := minimumBytes(line, id)
		if err != nil {
			return "", err
		}
		if size > c22MaxLineBytes {
			violations = append(violations, fmt.Sprintf("message %d line %d uses %d bytes (maximum %d)", id, i+1, size, c22MaxLineBytes))
		}
	}
	for p := 0; p < len(lines); p += 4 {
		end := min(p+4, len(lines))
		size, err := minimumBytes(strings.Join(lines[p:end], lineBreak), id)
		if err != nil {
			return "", err
		}
		if size >= c22PageBufferCapacityBytes {
			violations = append(violations, fmt.Sprintf("message %d page %d uses %d bytes (maximum %d)", id, p/4+1, size, c22PageBufferCapacityBytes-1))
		}
	}
	return strings.Join(violations, ", "), nil
}

func (e *Engine) tightenC22(id int, semantic, layout string, limit int) string {
	if why, _ := e.c22Violation(id, layout); why == "" {
		return layout
	}
	for candidate := limit - 1; candidate > 0; candidate-- {
		flow, err := e.greedy(semantic, candidate, id)
		if err != nil || flow == "" {
			continue
		}
		if why, _ := e.c22Violation(id, flow); why == "" {
			return flow
		}
	}
	return layout
}

func (e *Engine) validatePostings(effective map[int]string, translated map[int]bool, failures *[]string) {
	maxima := map[string]int{}
	for role, ids := range e.postingCandidateIDs() {
		for _, id := range ids {
			if text, ok := effective[id]; ok {
				if size, err := expandedBytes(text, id); err == nil && size > maxima[role] {
					maxima[role] = size
				}
			}
		}
	}
	integer := map[string]bool{}
	for _, role := range e.consumers.PostingCandidates.IntegerRoles {
		integer[role] = true
	}
	for _, p := range e.consumers.Postings {
		if !translated[p.ID] {
			continue
		}
		text, ok := effective[p.ID]
		if !ok {
			*failures = append(*failures, fmt.Sprintf("guild posting %d lacks text", p.ID))
			continue
		}
		size, err := expandedBytes(text, p.ID)
		if err != nil {
			*failures = append(*failures, err.Error())
			continue
		}
		for tag, role := range p.Bindings {
			count := strings.Count(text, tag)
			if integer[role] {
				size += count * guildPostingIntegerMaxBytes
			} else {
				size += count * maxima[role]
			}
		}
		if size >= guildPostingBufferCapacityBytes {
			*failures = append(*failures, fmt.Sprintf("guild posting message %d uses %d bytes (maximum %d)", p.ID, size, guildPostingBufferCapacityBytes-1))
		}
	}
}

type c5Leaf struct {
	data    []byte
	dynamic bool
}

func (e *Engine) c5Violation(item corpus.Item, text string) (string, error) {
	p, err := message.Project(item.Record)
	if err != nil {
		return "", err
	}
	raw, err := p.Materialize(text, true)
	if err != nil {
		return "", err
	}
	bankData := make([]byte, 4+len(raw))
	binary.LittleEndian.PutUint16(bankData, 1)
	binary.LittleEndian.PutUint16(bankData[2:], 4)
	copy(bankData[4:], raw)
	bank, err := corpus.ParseBank("msgsec000.dat", bankData)
	if err != nil {
		return "", fmt.Errorf("message %d C5 lowering: %w", item.Record.ID, err)
	}
	leaves, err := walkC5(bank.Records[0].Tokens, 0, nil, false)
	if err != nil {
		return "", fmt.Errorf("message %d C5 analysis: %w", item.Record.ID, err)
	}
	var violations []string
	maxPages := c5MaxPages
	if e.has(e.consumers.SinglePageC5IDs, item.Record.ID) {
		maxPages = 1
	}
	for branch, leaf := range leaves {
		pages := []int{0}
		breaks := 0
		for _, b := range leaf.data {
			if b == 10 {
				breaks++
				if breaks == c5LinesPerPage {
					pages = append(pages, 0)
					breaks = 0
					continue
				}
			}
			pages[len(pages)-1]++
		}
		if len(pages) > maxPages {
			violations = append(violations, fmt.Sprintf("message %d branch %d has %d pages (maximum %d)", item.Record.ID, branch+1, len(pages), maxPages))
		}
		for page, size := range pages {
			if size >= c5PageBufferCapacityBytes {
				violations = append(violations, fmt.Sprintf("message %d branch %d page %d uses %d bytes (maximum %d)", item.Record.ID, branch+1, page+1, size, c5PageBufferCapacityBytes-1))
			}
		}
	}
	return strings.Join(violations, ", "), nil
}

func walkC5(tokens []corpus.Token, index int, prefix []byte, dynamic bool) ([]c5Leaf, error) {
	out := append([]byte(nil), prefix...)
	nextTerm := func(start int) (int, error) {
		for i := start; i < len(tokens); i++ {
			if tokens[i].Kind == "block_terminator" {
				return i, nil
			}
		}
		return 0, fmt.Errorf("control flow has no block terminator")
	}
	for index < len(tokens) {
		t := tokens[index]
		switch t.Kind {
		case "if":
			term, err := nextTerm(index + 2)
			if err != nil {
				return nil, err
			}
			yes, err := walkC5(tokens, index+2, out, dynamic)
			if err != nil {
				return nil, err
			}
			no, err := walkC5(tokens, term+1, out, dynamic)
			return append(yes, no...), err
		case "select":
			if index+1 >= len(tokens) || tokens[index+1].Kind != "expression" {
				return nil, fmt.Errorf("select lacks expression")
			}
			arms := 0
			sink := false
			expr := tokens[index+1].Raw
			if len(expr) >= 4 && expr[0] == 2 && expr[1] == 0x20 && expr[2] == '%' {
				fmt.Sscanf(string(expr[3:]), "%d", &arms)
			} else if len(expr) == 2 && expr[0] == 2 && expr[1] == 0x33 {
				arms = 8
				sink = true
			} else {
				return nil, fmt.Errorf("unsupported select expression")
			}
			cursor := index + 2
			var leaves []c5Leaf
			for range arms {
				arm, err := walkC5(tokens, cursor, out, dynamic)
				if err != nil {
					return nil, err
				}
				leaves = append(leaves, arm...)
				term, err := nextTerm(cursor)
				if err != nil {
					return nil, err
				}
				cursor = term + 1
			}
			if sink {
				rest, err := walkC5(tokens, cursor, out, dynamic)
				if err != nil {
					return nil, err
				}
				leaves = append(leaves, rest...)
			}
			return leaves, nil
		case "block_terminator", "archive_padding", "suffix":
			return []c5Leaf{{out, dynamic}}, nil
		case "text", "backspace", "tab", "line_break", "color", "undecodable_data":
			out = append(out, t.Raw...)
		case "substitution":
			dynamic = true
		case "unknown_control":
			if len(t.Raw) == 1 && t.Raw[0] == 0x7f {
				out = append(out, t.Raw...)
			}
		case "call", "jump":
			return nil, fmt.Errorf("unsupported %s", t.Kind)
		}
		index++
	}
	return []c5Leaf{{out, dynamic}}, nil
}
