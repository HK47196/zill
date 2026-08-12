// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext

import "slices"

const reviewNeighborCount = 3

const (
	reviewAlternateLimit = 12
	reviewReferenceLimit = 12
)

// ReviewPacket is a bounded target-centred projection of one complete Scene.
// Scene remains the lossless source of truth.
type ReviewPacket struct {
	SceneMember          string           `json:"scene_member"`
	SourceArchive        string           `json:"source_archive"`
	SourceKind           string           `json:"source_kind"`
	Ordering             string           `json:"ordering"`
	EvidenceStatus       string           `json:"evidence_status"`
	TargetMessageID      int              `json:"target_message_id"`
	OccurrencePosition   int              `json:"occurrence_position"`
	Path                 []int            `json:"path"`
	Context              []ReviewEntry    `json:"context"`
	AlternateArms        []ReviewEntry    `json:"alternate_arms"`
	AlternateArmsOmitted int              `json:"alternate_arms_omitted"`
	SourceEvidence       []SourceEvidence `json:"source_evidence,omitempty"`
	References           []Reference      `json:"references,omitempty"`
	ReferencesOmitted    int              `json:"references_omitted"`
}

// ReviewEntry assigns a review role to one lossless authored occurrence.
type ReviewEntry struct {
	Role  string `json:"role"`
	Entry Entry  `json:"entry"`
}

func buildReviewPackets(result Result) []ReviewPacket {
	if result.Selector.Record < 0 {
		return nil
	}
	packets := make([]ReviewPacket, 0)
	for _, scene := range result.Scenes {
		for targetIndex, target := range scene.Entries {
			if target.MessageID != result.Selector.Record {
				continue
			}
			packet := ReviewPacket{
				SceneMember: scene.Member, SourceArchive: scene.SourceArchive,
				SourceKind: scene.SourceKind, Ordering: scene.Ordering,
				EvidenceStatus: scene.EvidenceStatus, TargetMessageID: target.MessageID,
				OccurrencePosition: target.Position, Path: append([]int(nil), target.Path...),
				Context:        make([]ReviewEntry, 0, reviewNeighborCount*2+1),
				AlternateArms:  make([]ReviewEntry, 0),
				SourceEvidence: append([]SourceEvidence(nil), scene.SourceEvidence...),
			}
			before := reviewNeighbors(scene.Entries, targetIndex, -1, target.Path, scene.SourceKind)
			slices.Reverse(before)
			packet.Context = append(packet.Context, before...)
			packet.Context = append(packet.Context, ReviewEntry{Role: "target", Entry: target})
			packet.Context = append(packet.Context, reviewNeighbors(scene.Entries, targetIndex, 1, target.Path, scene.SourceKind)...)
			packet.AlternateArms, packet.AlternateArmsOmitted = reviewAlternates(scene.Entries, targetIndex)
			packet.References, packet.ReferencesOmitted = reviewReferences(scene.References, target.Path)
			packets = append(packets, packet)
		}
	}
	return packets
}

func reviewNeighbors(entries []Entry, targetIndex, step int, path []int, sourceKind string) []ReviewEntry {
	result := make([]ReviewEntry, 0, reviewNeighborCount)
	role := "same_path_after"
	if step < 0 {
		role = "same_path_before"
	}
	if sourceKind == "message_bank" {
		role = "storage_neighbor_after"
		if step < 0 {
			role = "storage_neighbor_before"
		}
	}
	for index := targetIndex + step; index >= 0 && index < len(entries) && len(result) < reviewNeighborCount; index += step {
		if slices.Equal(entries[index].Path, path) {
			result = append(result, ReviewEntry{Role: role, Entry: entries[index]})
		}
	}
	return result
}

func reviewAlternates(entries []Entry, targetIndex int) ([]ReviewEntry, int) {
	base, count, selected, scope, ok := choiceContext(entries[targetIndex])
	if !ok {
		return nil, 0
	}
	result := make([]ReviewEntry, 0)
	omitted := 0
	for index, entry := range entries {
		if index == targetIndex {
			continue
		}
		otherBase, otherCount, otherSelected, otherScope, otherOK := choiceContext(entry)
		if !otherOK || otherBase != base || otherCount != count || otherSelected == selected || !slices.Equal(otherScope, scope) {
			continue
		}
		if len(result) == reviewAlternateLimit {
			omitted++
			continue
		}
		result = append(result, ReviewEntry{Role: "alternate_choice_arm", Entry: entry})
	}
	return result, omitted
}

func choiceContext(entry Entry) (base, count, selected int, scope []int, ok bool) {
	selected = -2
	choiceDepth := -1
	for index, condition := range entry.Conditions {
		switch condition.Kind {
		case "choice_context":
			if condition.BaseMessageID == nil || condition.OptionCount == nil {
				return 0, 0, 0, nil, false
			}
			base, count = *condition.BaseMessageID, *condition.OptionCount
			choiceDepth = index + 1
		case "choice_result_equals":
			if condition.SelectedIndex != nil {
				selected = *condition.SelectedIndex
			}
		}
	}
	if choiceDepth <= 0 || choiceDepth > len(entry.Path) {
		return 0, 0, 0, nil, false
	}
	scope = append([]int(nil), entry.Path[:choiceDepth]...)
	return base, count, selected, scope, base > 0 && count > 0 && selected >= 0
}

func reviewReferences(references []Reference, targetPath []int) ([]Reference, int) {
	result := make([]Reference, 0, min(len(references), reviewReferenceLimit))
	omitted := 0
	for _, reference := range references {
		if !pathCompatible(reference.Path, targetPath) {
			continue
		}
		if len(result) == reviewReferenceLimit {
			omitted++
			continue
		}
		result = append(result, reference)
	}
	return result, omitted
}

func pathCompatible(left, right []int) bool {
	shared := min(len(left), len(right))
	return slices.Equal(left[:shared], right[:shared])
}
