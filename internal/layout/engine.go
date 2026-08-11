// SPDX-License-Identifier: GPL-3.0-or-later

// Package layout produces and validates build-local message layouts from
// authenticated retail records, contributor English, font metrics, and the
// runtime consumer map.
package layout

import (
	"fmt"
	"math"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/cp932"
	"github.com/HK47196/zill/internal/message"
)

const lineBreak = "<line-break>"

var controlTag = regexp.MustCompile(`<(?:\$[0-9A-F]{2}|color:[befho]|end|equal|greater-equal|if|less-equal|less|line-break|not-equal|select|value:\$[0-9A-F]{2})>`)
var valueTag = regexp.MustCompile(`<value:\$[0-9A-F]{2}>`)
var printfConversion = regexp.MustCompile(`%(?:[1-9][0-9]*)?[su]`)
var sourceAnchor = regexp.MustCompile(`\{\{[A-Z][A-Z0-9_]*_[1-9][0-9]*\}\}`)
var strongBreak = regexp.MustCompile(`[.!?…]["')\]]*$`)
var mediumBreak = regexp.MustCompile(`[,;:]["')\]]*$`)

type Engine struct {
	consumers         consumersFile
	glyphs            map[uint16]glyph
	categories        []categoryRange
	playerNameAdvance int
}

type Warning struct {
	Code      string
	MessageID int
}
type Result struct {
	Layouts  map[int]string
	Warnings []Warning
}

type reflowedItem struct {
	layout   string
	warnings []Warning
	err      error
}

// CheckGlyphs verifies that every translated contributor string can be
// rendered by the installed font without requiring retail message banks.
func (e *Engine) CheckGlyphs(project *corpus.Project) error {
	if project == nil {
		return fmt.Errorf("layout: nil project")
	}
	for _, item := range project.Items {
		if item.Translation.State != corpus.Translated {
			continue
		}
		if _, err := e.measure(item.Translation.Text, item.Record.ID); err != nil {
			return err
		}
	}
	return nil
}

// Reflow preserves explicitly authored breaks, derives layouts for unbroken
// translations, and performs all release-blocking validation.
func (e *Engine) Reflow(project *corpus.Project) (Result, error) {
	if project == nil {
		return Result{}, fmt.Errorf("layout: nil project")
	}
	result := Result{Layouts: make(map[int]string)}
	items := make(map[int]corpus.Item, len(project.Items))
	translated := make([]int, 0, len(project.Items))
	duplicateErrors := make([]error, len(project.Items))
	for index, item := range project.Items {
		id := item.Record.ID
		if _, exists := items[id]; exists {
			duplicateErrors[index] = fmt.Errorf("layout: duplicate message %d", id)
			continue
		}
		items[id] = item
		if item.Translation.State == corpus.Translated {
			translated = append(translated, index)
		}
	}
	reflowed := make([]reflowedItem, len(project.Items))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(translated)) {
		workers.Go(func() {
			for index := range jobs {
				reflowed[index] = e.reflowItem(project.Items[index])
			}
		})
	}
	for _, index := range translated {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	for index, projectItem := range project.Items {
		if duplicateErrors[index] != nil {
			return Result{}, duplicateErrors[index]
		}
		if projectItem.Translation.State != corpus.Translated {
			continue
		}
		item := reflowed[index]
		if item.err != nil {
			return Result{}, item.err
		}
		result.Layouts[projectItem.Record.ID] = item.layout
		result.Warnings = append(result.Warnings, item.warnings...)
	}
	if err := e.Validate(project, result.Layouts); err != nil {
		return Result{}, err
	}
	sort.Slice(result.Warnings, func(i, j int) bool {
		if result.Warnings[i].Code == result.Warnings[j].Code {
			return result.Warnings[i].MessageID < result.Warnings[j].MessageID
		}
		return result.Warnings[i].Code < result.Warnings[j].Code
	})
	return result, nil
}

func (e *Engine) reflowItem(item corpus.Item) (result reflowedItem) {
	id := item.Record.ID
	semantic := normalize(item.Translation.Text)
	projection, err := message.Project(item.Record)
	if err != nil {
		result.err = err
		return result
	}
	authored := strings.Contains(semantic, lineBreak)
	limit := e.advanceLimit(id)
	if authored {
		if _, err := projection.Materialize(semantic, true); err != nil {
			result.err = fmt.Errorf("message %d authored layout: %w", id, err)
			return result
		}
		result.layout = semantic
	} else {
		if _, err := projection.Materialize(semantic, false); err != nil {
			result.err = fmt.Errorf("message %d semantic: %w", id, err)
			return result
		}
		if e.itemDescription(id) {
			result.layout = semantic
		} else {
			result.layout, result.err = e.sourceAware(projection, semantic, limit, id)
		}
		if result.err != nil {
			return result
		}
		if result.layout == "" {
			result.layout = semantic
		}
		if e.has(e.consumers.C22IDs, id) {
			result.layout = e.tightenC22(id, semantic, result.layout, limit)
		}
	}
	if _, err := projection.Materialize(result.layout, true); err != nil {
		result.err = fmt.Errorf("message %d layout: %w", id, err)
		return result
	}
	width, err := e.maxProjectedWidth(projection, result.layout, id)
	if err != nil {
		result.err = err
		return result
	}
	if e.category(id, "character-profile") {
		if width > limit {
			result.err = fmt.Errorf("message %d: profile line is %d units (maximum %d)", id, width, limit)
			return result
		}
		if maxFragmentLines(projection, result.layout) > profileMaxLines {
			result.err = fmt.Errorf("message %d: profile exceeds %d lines", id, profileMaxLines)
			return result
		}
	} else if width > limit {
		code := "line_exceeds_authoring_ceiling"
		if e.itemDescription(id) {
			code = "item_description_single_line_overflow"
		}
		result.warnings = append(result.warnings, Warning{code, id})
	}
	if e.category(id, "chronicle-entry") && maxFragmentLines(projection, result.layout) > chronicleMaxLines {
		result.warnings = append(result.warnings, Warning{"chronicle_vertical_overflow", id})
	}
	if valueTag.MatchString(semantic) || (formatSignatureID(id) && printfConversion.MatchString(visible(semantic))) {
		result.warnings = append(result.warnings, Warning{"runtime_substitution_unbounded", id})
	}
	if e.has(e.consumers.GuildClientIDs, id) {
		if width, err := e.measure(semantic, id); err != nil {
			result.err = err
			return result
		} else if width > 104 {
			result.warnings = append(result.warnings, Warning{"guild_job_client_overflow", id})
		}
	}
	return result
}

func normalize(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", lineBreak)
}

func (e *Engine) advanceLimit(id int) int {
	switch {
	case e.category(id, "character-profile"):
		return profileAdvance
	case e.category(id, "character-creation-prompt"):
		return characterCreationPromptAdvance
	case e.category(id, "chronicle-entry"):
		return chronicleAdvance
	case e.equipmentFeedback(id):
		return equipmentFeedbackAdvance
	case e.category(id, "system-help"):
		return systemHelpAdvance
	case e.category(id, "objective-advice"):
		return objectiveAdviceAdvance
	case e.has(e.consumers.C5PortraitIDs, id):
		return c5PortraitAdvance
	case e.has(e.consumers.C5IDs, id) || e.narrowText(id):
		return c5Advance
	default:
		return defaultAdvance
	}
}

func (e *Engine) narrowText(id int) bool {
	for _, r := range e.categories {
		if id < r.First {
			break
		}
		if id <= r.Last {
			return r.Basis == "verified" && (r.Category == "dialogue" || r.Category == "in-world-guidance")
		}
	}
	return false
}
func (e *Engine) category(id int, wanted ...string) bool {
	for _, r := range e.categories {
		if id < r.First {
			break
		}
		if id <= r.Last {
			for _, w := range wanted {
				if r.Category == w {
					return true
				}
			}
			return false
		}
	}
	return false
}
func (e *Engine) itemDescription(id int) bool {
	return e.category(id, "equipment-description", "item-effect-description", "quest-item-description")
}
func (e *Engine) equipmentFeedback(id int) bool { return e.category(id, "equipment-feedback") }
func (e *Engine) has(ids []int, id int) bool {
	i := sort.SearchInts(ids, id)
	return i < len(ids) && ids[i] == id
}

func (e *Engine) sourceAware(p *message.Projection, semantic string, limit, id int) (string, error) {
	fragments, err := p.SplitSemantic(semantic)
	if err != nil {
		return "", err
	}
	result, cursor := semantic, 0
	for i, fragment := range fragments {
		source := p.Fragments[i].SourceLayout
		flow, err := e.preferred(fragment, source, limit, id, e.has(e.consumers.C5IDs, id) || e.has(e.consumers.SinglePageC5IDs, id))
		if err != nil {
			return "", err
		}
		if flow == "" && fragment != "" {
			return "", nil
		}
		at := strings.Index(result[cursor:], fragment)
		if at < 0 {
			return "", fmt.Errorf("message %d: cannot locate projected fragment", id)
		}
		at += cursor
		result = result[:at] + flow + result[at+len(fragment):]
		cursor = at + len(flow)
	}
	return result, nil
}

func splitParts(s string) []string {
	var out []string
	for len(s) > 0 {
		if loc := controlTag.FindStringIndex(s); loc != nil && loc[0] == 0 {
			out = append(out, s[:loc[1]])
			s = s[loc[1]:]
			continue
		}
		end := len(s)
		if loc := controlTag.FindStringIndex(s); loc != nil {
			end = loc[0]
		}
		plain := s[:end]
		for len(plain) > 0 {
			r, w := utf8First(plain)
			space := unicode.IsSpace(r)
			n := w
			for n < len(plain) {
				rr, ww := utf8First(plain[n:])
				if unicode.IsSpace(rr) != space {
					break
				}
				n += ww
			}
			out = append(out, plain[:n])
			plain = plain[n:]
		}
		s = s[end:]
	}
	return out
}
func utf8First(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

func (e *Engine) greedy(s string, limit, id int) (string, error) {
	parts := splitParts(s)
	var out []string
	start := 0
	last := -1
	for _, part := range parts {
		out = append(out, part)
		i := len(out) - 1
		if part == lineBreak || part == "<end>" {
			w, err := e.measure(strings.Join(out[start:i], ""), id)
			if err != nil {
				return "", err
			}
			if w > limit {
				if last < start {
					return "", nil
				}
				out[last] = lineBreak
				start = last + 1
				w, err = e.measure(strings.Join(out[start:i], ""), id)
				if err != nil || w > limit {
					return "", err
				}
			}
			start = i + 1
			last = -1
			continue
		}
		if strings.TrimSpace(part) == "" {
			last = i
			continue
		}
		w, err := e.measure(strings.Join(out[start:], ""), id)
		if err != nil {
			return "", err
		}
		if w > limit {
			if last < start {
				return "", nil
			}
			out[last] = lineBreak
			start = last + 1
			last = -1
			w, err = e.measure(strings.Join(out[start:], ""), id)
			if err != nil || w > limit {
				return "", err
			}
		}
	}
	w, err := e.measure(strings.Join(out[start:], ""), id)
	if err != nil {
		return "", err
	}
	if w > limit {
		if last < start {
			return "", nil
		}
		out[last] = lineBreak
		start = last + 1
		w, err = e.measure(strings.Join(out[start:], ""), id)
		if err != nil || w > limit {
			return "", err
		}
	}
	return strings.Join(out, ""), nil
}

func (e *Engine) preferred(s, source string, limit, id int, c5 bool) (string, error) {
	parts := splitParts(s)
	for _, part := range parts {
		if part == lineBreak || part == "<end>" {
			return e.greedy(s, limit, id)
		}
	}
	var chunks, separators []string
	var current, pending strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			pending.WriteString(part)
			continue
		}
		if pending.Len() > 0 && current.Len() > 0 {
			chunks = append(chunks, current.String())
			separators = append(separators, pending.String())
			current.Reset()
			pending.Reset()
		} else if pending.Len() > 0 {
			current.WriteString(pending.String())
			pending.Reset()
		}
		current.WriteString(part)
	}
	current.WriteString(pending.String())
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	if len(chunks) == 0 {
		return s, nil
	}
	greedy, err := e.greedy(s, limit, id)
	if err != nil || greedy == "" {
		return greedy, err
	}
	lines := strings.Count(greedy, lineBreak) + 1
	if c5 && lines > c5LinesPerPage && lines%c5LinesPerPage == 1 && lines < c5LinesPerPage*c5MaxPages {
		lines++
	}
	if lines > len(chunks) {
		lines = strings.Count(greedy, lineBreak) + 1
	}
	chunkAdvances := make([]int, len(chunks))
	for i, chunk := range chunks {
		advance, err := e.measure(chunk, id)
		if err != nil {
			return "", err
		}
		chunkAdvances[i] = advance
	}
	separatorAdvances := make([]int, len(separators))
	for i, separator := range separators {
		advance, err := e.measure(separator, id)
		if err != nil {
			return "", err
		}
		separatorAdvances[i] = advance
	}
	visibleCounts := make([]int, len(chunks))
	visiblePrefix := make([]int, len(chunks)+1)
	for i, chunk := range chunks {
		visibleCounts[i] = nonspaceRunes(visible(chunk))
		visiblePrefix[i+1] = visiblePrefix[i] + visibleCounts[i]
	}
	totalVisible := visiblePrefix[len(chunks)]
	sourceRatios := sourceBreakRatios(source)
	breakPenalty := func(end int) int {
		if end == len(chunks) || len(sourceRatios) == 0 || totalVisible == 0 {
			return 0
		}
		ratio := float64(visiblePrefix[end]) / float64(totalVisible)
		distance := math.Inf(1)
		for _, preferred := range sourceRatios {
			distance = min(distance, math.Abs(ratio-preferred))
		}
		return int(math.RoundToEven(distance * 1000))
	}
	breakQuality := func(chunk string) int {
		v := strings.TrimRightFunc(visible(chunk), unicode.IsSpace)
		if strongBreak.MatchString(v) {
			return 0
		}
		if mediumBreak.MatchString(v) {
			return 1
		}
		return 2
	}
	type state struct {
		cost   int
		breaks []int
	}
	states := map[int]state{0: {}}
	for line := 0; line < lines; line++ {
		next := map[int]state{}
		remain := lines - line - 1
		for start, st := range states {
			advance := 0
			for end := start + 1; end <= len(chunks); end++ {
				if end > start+1 {
					advance += separatorAdvances[end-2]
				}
				advance += chunkAdvances[end-1]
				if advance > limit {
					break
				}
				if visiblePrefix[end] <= visiblePrefix[start] {
					continue
				}
				if len(chunks)-end < remain || (end == len(chunks)) != (remain == 0) {
					continue
				}
				isBreak := end < len(chunks)
				cost := st.cost + (limit-advance)*(limit-advance)
				if isBreak {
					cost += breakQuality(chunks[end-1]) * limit * limit / 4
					cost += breakPenalty(end) * limit * limit / 2000
				}
				b := append([]int(nil), st.breaks...)
				if isBreak {
					b = append(b, end)
				}
				old, ok := next[end]
				if !ok || cost < old.cost || cost == old.cost && lexLess(b, old.breaks) {
					next[end] = state{cost, b}
				}
			}
		}
		states = next
	}
	best, ok := states[len(chunks)]
	if !ok {
		return greedy, nil
	}
	set := map[int]bool{}
	for _, b := range best.breaks {
		set[b] = true
	}
	var b strings.Builder
	for i, chunk := range chunks {
		if i > 0 {
			if set[i] {
				b.WriteString(lineBreak)
			} else {
				b.WriteString(separators[i-1])
			}
		}
		b.WriteString(chunk)
	}
	return b.String(), nil
}

func nonspaceRunes(s string) int {
	count := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			count++
		}
	}
	return count
}
func sourceBreakRatios(source string) []float64 {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	for len(lines) > 1 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 1 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	weights := make([]int, len(lines))
	total := 0
	for i, line := range lines {
		weights[i] = nonspaceRunes(sourceAnchor.ReplaceAllString(line, ""))
		total += weights[i]
	}
	cumulative := 0
	var ratios []float64
	for _, weight := range weights[:max(0, len(weights)-1)] {
		cumulative += weight
		if total > 0 && cumulative > 0 && cumulative < total {
			ratio := float64(cumulative) / float64(total)
			if len(ratios) == 0 || ratio != ratios[len(ratios)-1] {
				ratios = append(ratios, ratio)
			}
		}
	}
	return ratios
}
func lexLess(a, b []int) bool {
	for i := 0; i < min(len(a), len(b)); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func visible(s string) string { return controlTag.ReplaceAllString(normalize(s), "") }

func (e *Engine) measure(s string, id int) (int, error) {
	reserved := strings.Count(s, "<value:$28>") * e.playerNameAdvance
	plain := visible(s)
	total := 0
	for i, r := range plain {
		key := uint16(r)
		if r > unicode.MaxASCII {
			encoded, err := cp932.Encode(string(r))
			if err != nil {
				return 0, fmt.Errorf("message %d character %q at %d: %w", id, r, i, err)
			}
			if len(encoded) < 1 || len(encoded) > 2 {
				return 0, fmt.Errorf("message %d character %q has invalid CP932 width", id, r)
			}
			key = uint16(encoded[0])
			if len(encoded) == 2 {
				key |= uint16(encoded[1]) << 8
			}
		}
		g, ok := e.glyphs[key]
		if !ok {
			return 0, fmt.Errorf("message %d character %q has no installed-font glyph (%#04x)", id, r, key)
		}
		total += g.Advance
	}
	return total + reserved, nil
}

func (e *Engine) maxProjectedWidth(p *message.Projection, text string, id int) (int, error) {
	parts, err := p.SplitSemantic(text)
	if err != nil {
		return 0, err
	}
	maximum := 0
	for _, part := range parts {
		for _, page := range strings.Split(part, "<end>") {
			for _, line := range strings.Split(page, lineBreak) {
				w, err := e.measure(line, id)
				if err != nil {
					return 0, err
				}
				maximum = max(maximum, w)
			}
		}
	}
	return maximum, nil
}
func maxFragmentLines(p *message.Projection, text string) int {
	parts, err := p.SplitSemantic(text)
	if err != nil {
		return 0
	}
	m := 0
	for _, part := range parts {
		for _, page := range strings.Split(part, "<end>") {
			m = max(m, strings.Count(page, lineBreak)+1)
		}
	}
	return m
}
func formatSignatureID(id int) bool {
	switch id {
	case 20006, 170006, 170007, 170008, 170009, 1070022, 1070023:
		return true
	}
	return false
}
