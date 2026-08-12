// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/HK47196/zill/internal/corpus"
	"golang.org/x/text/unicode/norm"
)

// SourceEvidence is a relationship explicitly suggested by authored message
// records, kept separate from executable consumer evidence.
type SourceEvidence struct {
	Kind             string            `json:"kind"`
	Status           string            `json:"status"`
	Confidence       string            `json:"confidence"`
	RuntimeStatus    string            `json:"runtime_status"`
	EventNumber      int               `json:"event_number,omitempty"`
	MarkerLabel      string            `json:"marker_label,omitempty"`
	MarkerMessageIDs []int             `json:"marker_message_ids,omitempty"`
	Candidates       []SourceCandidate `json:"candidates,omitempty"`
	Basis            string            `json:"basis"`
	SourceLocator    string            `json:"source_locator,omitempty"`
}

// SourceCandidate is a same-index row from a discovered event-authoring table.
type SourceCandidate struct {
	MessageID  int    `json:"message_id"`
	Japanese   string `json:"japanese"`
	English    string `json:"english"`
	LabelMatch bool   `json:"label_match"`
}

// SourceControl is record-local retail message control. It does not establish
// cross-record chronology or runtime reachability.
type SourceControl struct {
	Kind           string        `json:"kind"`
	Selector       string        `json:"selector,omitempty"`
	ExpectedBlocks *int          `json:"expected_blocks,omitempty"`
	Evidence       string        `json:"evidence"`
	Blocks         []SourceBlock `json:"blocks"`
}

// SourceBlock is one end-terminated output block within record-local control.
type SourceBlock struct {
	Position  int    `json:"position"`
	Role      string `json:"role"`
	Condition string `json:"condition,omitempty"`
	Japanese  string `json:"japanese"`
	English   string `json:"english"`
}

type reserveMarker struct {
	event int
	label string
	ids   []int
}

func sourceEvidence(project *corpus.Project, bank int) []SourceEvidence {
	markers := make(map[string]*reserveMarker)
	for _, item := range project.Items {
		if item.Translation.ID/10_000 != bank {
			continue
		}
		event, label, ok := parseScenarioReserveMarker(item.Translation.Japanese)
		if !ok {
			continue
		}
		key := strconv.Itoa(event) + "\x00" + label
		marker := markers[key]
		if marker == nil {
			marker = &reserveMarker{event: event, label: label}
			markers[key] = marker
		}
		marker.ids = append(marker.ids, item.Translation.ID)
	}

	keys := make([]string, 0, len(markers))
	for key := range markers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := bankSourceEvidence(bank)
	for _, key := range keys {
		marker := markers[key]
		evidence := SourceEvidence{
			Kind:             "scenario_reserve_marker",
			Status:           "source_authoring_hint",
			Confidence:       "low",
			RuntimeStatus:    "unresolved",
			EventNumber:      marker.event,
			MarkerLabel:      marker.label,
			MarkerMessageIDs: append([]int(nil), marker.ids...),
			Candidates:       make([]SourceCandidate, 0, 1),
			Basis:            "reserve_marker",
		}
		// Static analysis identifies msgsec194 as the event-title authoring
		// table. The reserve marker's explicit event number selects its row;
		// neither the candidate nor a text match proves a runtime consumer edge.
		const eventTitleTableBase = 1_940_000
		if candidate, ok := project.Find(eventTitleTableBase + marker.event); ok {
			labelMatch := normalizeAuthoringLabel(marker.label) == normalizeAuthoringLabel(candidate.Translation.Japanese)
			evidence.Candidates = append(evidence.Candidates, SourceCandidate{
				MessageID:  candidate.Translation.ID,
				Japanese:   candidate.Translation.Japanese,
				English:    candidate.Translation.Text,
				LabelMatch: labelMatch,
			})
			evidence.Status = "source_authoring_candidate"
			evidence.Basis = "reserve_marker_event_number"
			if labelMatch {
				evidence.Status = "source_authoring_match"
				evidence.Confidence = "high"
				evidence.Basis = "reserve_marker_event_number_and_title"
			}
		}
		result = append(result, evidence)
	}
	return result
}

func bankSourceEvidence(bank int) []SourceEvidence {
	switch bank {
	case 159:
		return []SourceEvidence{{
			Kind: "dynamic_bank_route", Status: "verified_executable_bank_route", Confidence: "high",
			RuntimeStatus: "record_selector_unresolved", Basis: "state_412_router_and_factory",
			SourceLocator: "ULJM05410-1.03 EBOOT 0xb6698/0x1780a8",
		}}
	case 194, 195, 196, 197:
		return []SourceEvidence{{
			Kind: "source_authoring_table", Status: "no_resolved_static_consumer_reference", Confidence: "high",
			RuntimeStatus: "unresolved", Basis: "retained_event_authoring_table_and_exhaustive_consumer_audit",
			SourceLocator: "ULJM05410-1.03 formatter and CDC consumer audit",
		}}
	default:
		return make([]SourceEvidence, 0)
	}
}

func parseScenarioReserveMarker(text string) (int, string, bool) {
	if !strings.HasPrefix(text, "予備メッセージ") || !strings.HasSuffix(text, "<end>") {
		return 0, "", false
	}
	parts := strings.Split(strings.TrimSuffix(text, "<end>"), "<line-break>")
	if len(parts) != 2 {
		return 0, "", false
	}
	runes := []rune(parts[1])
	if len(runes) < 3 {
		return 0, "", false
	}
	tens, ok := decimalRune(runes[0])
	if !ok {
		return 0, "", false
	}
	ones, ok := decimalRune(runes[1])
	if !ok {
		return 0, "", false
	}
	if _, thirdIsDigit := decimalRune(runes[2]); thirdIsDigit {
		return 0, "", false
	}
	label := strings.TrimSpace(string(runes[2:]))
	if label == "" {
		return 0, "", false
	}
	return tens*10 + ones, label, true
}

func decimalRune(value rune) (int, bool) {
	switch {
	case value >= '0' && value <= '9':
		return int(value - '0'), true
	case value >= '０' && value <= '９':
		return int(value - '０'), true
	default:
		return 0, false
	}
}

func normalizeAuthoringLabel(value string) string {
	value = strings.TrimSuffix(value, "<end>")
	value = strings.ToLower(norm.NFKC.String(value))
	return strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) || unicode.IsPunct(character) || unicode.IsSymbol(character) {
			return -1
		}
		return character
	}, value)
}
