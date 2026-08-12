// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext

import (
	"strings"

	"github.com/HK47196/zill/internal/corpus"
)

// Relationship is a statically verified executable association between two
// message records. It does not establish playthrough order or reachability.
type Relationship struct {
	Kind           string `json:"kind"`
	SourceMessage  int    `json:"source_message_id"`
	TargetMessage  int    `json:"target_message_id"`
	TargetJapanese string `json:"target_japanese"`
	TargetEnglish  string `json:"target_english"`
	Status         string `json:"status"`
	Confidence     string `json:"confidence"`
	RuntimeStatus  string `json:"runtime_status"`
	Basis          string `json:"basis"`
	SourceLocator  string `json:"source_locator"`
}

// AuthoringMetadata is a record-local role from a retained source-authoring
// table. It deliberately supplies no runtime edge or cross-table join.
type AuthoringMetadata struct {
	TableKind     string `json:"table_kind"`
	RawLabel      string `json:"raw_label,omitempty"`
	Status        string `json:"status"`
	RuntimeStatus string `json:"runtime_status"`
	Basis         string `json:"basis"`
}

// ConsumerEvidence is a bounded executable disposition for a message record
// outside the CDC consumer commands.
type ConsumerEvidence struct {
	Disposition   string `json:"disposition"`
	Role          string `json:"role,omitempty"`
	Category      string `json:"category,omitempty"`
	Variant       string `json:"variant,omitempty"`
	Confidence    string `json:"confidence"`
	RuntimeStatus string `json:"runtime_status"`
	SourceLocator string `json:"source_locator"`
}

type consumerEvidenceRule struct {
	firstRecord   int
	lastRecord    int
	disposition   string
	role          string
	category      string
	variant       string
	sourceLocator string
}

var executableConsumerRules = []consumerEvidenceRule{
	{452, 581, "verified_consumer", "location_label", "location-route-label", "bounded_location_route_label", "ULJM05410_EBOOT.BIN#0x45238;bounds=1..130"},
	{1065, 1135, "verified_consumer", "ability_command_name", "ability-command-name", "ability_action_name", "ULJM05410_EBOOT.BIN#0x454b0;bounds=1..71"},
	{1136, 1136, "verified_non_display", "", "", "rejected_selector_zero", "ULJM05410_EBOOT.BIN#0x454b0,0x45558,0x1944e4"},
	{1137, 1206, "verified_consumer", "item_name", "item-name", "inventory_item_name", "ULJM05410_EBOOT.BIN#0x45558,0x1944e4;bounds=1..70"},
	{1207, 1225, "verified_consumer", "item_name", "item-name", "inventory_item_name_high_selector", "ULJM05410_EBOOT.BIN#0x45558;selectors=100..118"},
	{1227, 1238, "verified_consumer", "quest_item_name", "quest-item-name", "quest_item_name_low_selector", "ULJM05410_EBOOT.BIN#0x45558;selectors=120..131"},
	{1239, 1338, "verified_consumer", "quest_item_name", "quest-item-name", "quest_item_name_high_selector", "ULJM05410_EBOOT.BIN#0x45558;selectors=142..241"},
	{1339, 1448, "verified_consumer", "equipment_name", "equipment-name", "equipment_name", "ULJM05410_EBOOT.BIN#0x456f4,0x2f63c,0x2f750"},
	{1449, 1470, "verified_consumer", "equipment_name", "mixed-use", "equipment_name", "ULJM05410_EBOOT.BIN#0x456f4,0x2f63c,0x2f750"},
	{1471, 1532, "verified_consumer", "name_label", "entity-and-role-label", "constructed_entity_label", "ULJM05410_EBOOT.BIN#0xe1090,0xe1698,0x45d04,0x44fb0"},
	{1546, 1553, "verified_consumer", "ui_label", "general-ui-menu", "bounded_attribute_label", "ULJM05410_EBOOT.BIN#0x17c8e8,0x28568,0x667a4"},
	{1565, 1569, "verified_consumer", "quest_topic_label", "quest-topic-label", "quest_kind_label", "ULJM05410_EBOOT.BIN#0xe1d9c,0x3b890,0xe2ecc"},
	{1588, 1588, "verified_consumer", "ui_message", "general-ui-menu", "bounded_identifier_message", "ULJM05410_EBOOT.BIN#0x18b18,0x18e6c,0x18eb8,0x18ff8"},
}

type relationshipRule struct {
	sourceBank    int
	firstRecord   int
	lastRecord    int
	selectorBase  int
	targetBase    int
	targetStride  int
	targetOffsets []int
	kind          string
	sourceLocator string
}

var executableRelationshipRules = []relationshipRule{
	{sourceBank: 0, firstRecord: 467, lastRecord: 508, selectorBase: 451, targetBase: 160016, targetStride: 2, targetOffsets: []int{0, 1}, kind: "same_selector_companion", sourceLocator: "ULJM05410_EBOOT.BIN#0x2586ac,0x4d42c,0x4c3c8,0x4d7b4;targets=0x4a26c,0x4a234"},
	{sourceBank: 0, firstRecord: 1065, lastRecord: 1135, selectorBase: 1064, targetBase: 1650070, targetStride: 1, targetOffsets: []int{0}, kind: "ability_description", sourceLocator: "ULJM05410_EBOOT.BIN#0x45504;dispatcher=0x194b44@0x194b70"},
	{sourceBank: 0, firstRecord: 1137, lastRecord: 1206, selectorBase: 1136, targetBase: 1659999, targetStride: 1, targetOffsets: []int{0}, kind: "item_effect_description", sourceLocator: "ULJM05410_EBOOT.BIN#0x4562c;dispatcher=0x194b44@0x194c24"},
	{sourceBank: 0, firstRecord: 1207, lastRecord: 1225, selectorBase: 1107, targetBase: 1659970, targetStride: 1, targetOffsets: []int{0}, kind: "item_effect_description", sourceLocator: "ULJM05410_EBOOT.BIN#0x4562c;selector-domain=100..131"},
	{sourceBank: 0, firstRecord: 1227, lastRecord: 1238, selectorBase: 1107, targetBase: 1659970, targetStride: 1, targetOffsets: []int{0}, kind: "quest_item_description", sourceLocator: "ULJM05410_EBOOT.BIN#0x4562c;selector-domain=100..131"},
	{sourceBank: 0, firstRecord: 1239, lastRecord: 1338, selectorBase: 1097, targetBase: 1659960, targetStride: 1, targetOffsets: []int{0}, kind: "quest_item_description", sourceLocator: "ULJM05410_EBOOT.BIN#0x4562c;selector-domain=142..241"},
}

func executableConsumerEvidence(messageID int) []ConsumerEvidence {
	if messageID/10_000 != 0 {
		return nil
	}
	record := messageID % 10_000
	result := make([]ConsumerEvidence, 0, 2)
	for _, rule := range executableConsumerRules {
		if record < rule.firstRecord || record > rule.lastRecord {
			continue
		}
		runtimeStatus := "selection_runtime_dependent"
		if rule.disposition == "verified_non_display" {
			runtimeStatus = "not_displayed_by_bounded_resolvers"
		}
		result = append(result, ConsumerEvidence{
			Disposition: rule.disposition, Role: rule.role, Category: rule.category,
			Variant: rule.variant, Confidence: "high", RuntimeStatus: runtimeStatus,
			SourceLocator: rule.sourceLocator,
		})
		if record >= 1449 && record <= 1470 {
			result = append(result, ConsumerEvidence{
				Disposition: "verified_consumer", Role: "name_label", Category: "mixed-use",
				Variant: "constructed_entity_label", Confidence: "high",
				RuntimeStatus: "selection_runtime_dependent",
				SourceLocator: "ULJM05410_EBOOT.BIN#0xe1090,0xe1698,0x45d04,0x44fb0;constructed-object-id=1..22",
			})
		}
		return result
	}
	return nil
}

func executableRelationships(project *corpus.Project, sourceID int) []Relationship {
	bank, record := sourceID/10_000, sourceID%10_000
	result := make([]Relationship, 0)
	for _, rule := range executableRelationshipRules {
		if bank != rule.sourceBank || record < rule.firstRecord || record > rule.lastRecord {
			continue
		}
		selector := record - rule.selectorBase
		for _, offset := range rule.targetOffsets {
			targetID := rule.targetBase + selector*rule.targetStride + offset
			target, ok := project.Find(targetID)
			if !ok {
				continue
			}
			result = append(result, Relationship{
				Kind: rule.kind, SourceMessage: sourceID, TargetMessage: targetID,
				TargetJapanese: target.Translation.Japanese, TargetEnglish: target.Translation.Text,
				Status: "verified_executable_formula", Confidence: "high",
				RuntimeStatus: "selection_runtime_dependent",
				Basis:         "bounded_formatter_selector_formula",
				SourceLocator: rule.sourceLocator,
			})
		}
	}
	return result
}

func authoringMetadata(messageID int, japanese string) *AuthoringMetadata {
	tableKinds := map[int]string{
		194: "event_title",
		195: "event_selector_label",
		196: "event_selector_label",
		197: "event_state_label",
	}
	kind, ok := tableKinds[messageID/10_000]
	if !ok {
		return nil
	}
	return &AuthoringMetadata{
		TableKind: kind, RawLabel: strings.TrimSpace(strings.TrimSuffix(japanese, "<end>")),
		Status: "record_local_authoring_metadata", RuntimeStatus: "unresolved",
		Basis: "retained_event_authoring_table_schema",
	}
}
