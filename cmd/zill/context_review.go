// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/HK47196/zill/internal/cdccontext"
	"github.com/HK47196/zill/internal/corpus"
)

const contextReviewSchemaVersion = 1

const (
	storageNeighborVariantLimit = 2
	storageTargetVariantLimit   = 12
	storageNeighborTextLimit    = 400
)

type contextReviewDocument struct {
	SchemaVersion int                    `json:"schema_version"`
	View          string                 `json:"view"`
	Query         contextReviewQuery     `json:"query"`
	Record        *contextRecordReview   `json:"record,omitempty"`
	Scenes        []contextSceneSummary  `json:"scenes,omitempty"`
	Scene         *contextSceneReview    `json:"scene,omitempty"`
	Storage       *contextStorageSummary `json:"storage,omitempty"`
}

type contextReviewQuery struct {
	ListScenes bool   `json:"list_scenes,omitempty"`
	Bank       *int   `json:"bank,omitempty"`
	Record     *int   `json:"record,omitempty"`
	Scene      string `json:"scene,omitempty"`
}

type contextRecordReview struct {
	MessageID   int          `json:"message_id"`
	Editable    string       `json:"editable"`
	State       corpus.State `json:"state"`
	Japanese    string       `json:"japanese"`
	English     string       `json:"english"`
	StorageOnly bool         `json:"storage_only"`
}

type contextSceneSummary struct {
	ID                string                  `json:"id"`
	Aliases           []string                `json:"aliases,omitempty"`
	Kind              string                  `json:"kind"`
	MatchKind         string                  `json:"match_kind"`
	SourceArchive     string                  `json:"source_archive"`
	Member            string                  `json:"member"`
	EmbeddedMember    string                  `json:"embedded_member,omitempty"`
	Ordering          string                  `json:"ordering"`
	RuntimeChronology string                  `json:"runtime_chronology"`
	RecordIDs         []int                   `json:"record_ids"`
	MatchingRecordIDs []int                   `json:"matching_record_ids,omitempty"`
	OtherRecordIDs    []int                   `json:"other_record_ids,omitempty"`
	OccurrenceCount   int                     `json:"occurrence_count"`
	Participants      []contextParticipant    `json:"participants,omitempty"`
	Translation       contextTranslationCount `json:"translation"`
	Scenario          *contextScenarioSummary `json:"scenario,omitempty"`
	OpenCommand       string                  `json:"open_command"`
}

type contextScenarioSummary struct {
	Slot             int      `json:"slot"`
	EquivalentGroups []string `json:"equivalent_groups"`
}

type contextParticipant struct {
	Japanese string `json:"japanese,omitempty"`
	English  string `json:"english,omitempty"`
	Status   string `json:"status"`
	Basis    string `json:"basis,omitempty"`
}

type contextTranslationCount struct {
	Translated   int `json:"translated"`
	Todo         int `json:"todo"`
	KeepJapanese int `json:"keep_japanese"`
}

type contextStorageSummary struct {
	ID                   string               `json:"id"`
	SourceArchive        string               `json:"source_archive"`
	Member               string               `json:"member"`
	Ordering             string               `json:"ordering"`
	RecordCount          int                  `json:"record_count"`
	StorageOnlyRecordIDs []int                `json:"storage_only_record_ids,omitempty"`
	NeighborRecords      []contextReviewEntry `json:"neighbor_records,omitempty"`
	OpenCommand          string               `json:"open_command"`
}

type contextSceneReview struct {
	Summary     contextSceneSummary  `json:"summary"`
	Entries     []contextReviewEntry `json:"entries"`
	Terminology []contextReviewTerm  `json:"terminology,omitempty"`
	Limitations []string             `json:"limitations"`
}

type contextReviewEntry struct {
	Position       int                      `json:"position"`
	RecordID       int                      `json:"record_id"`
	Editable       string                   `json:"editable"`
	Kind           string                   `json:"kind"`
	State          corpus.State             `json:"state"`
	Speaker        contextParticipant       `json:"speaker"`
	Branches       []contextReviewCondition `json:"branches,omitempty"`
	RecordVariants []contextReviewControl   `json:"record_variants,omitempty"`
	Japanese       string                   `json:"japanese,omitempty"`
	English        string                   `json:"english,omitempty"`
	Terminology    []contextReviewTerm      `json:"terminology,omitempty"`
}

type contextReviewControl struct {
	Kind            string                 `json:"kind"`
	Variants        []contextReviewVariant `json:"variants"`
	VariantsOmitted int                    `json:"variants_omitted,omitempty"`
}

type contextReviewVariant struct {
	Position int    `json:"position"`
	Role     string `json:"role"`
	Japanese string `json:"japanese"`
	English  string `json:"english"`
}

type contextReviewCondition struct {
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Description   string `json:"description"`
	BaseMessageID *int   `json:"base_message_id,omitempty"`
	OptionCount   *int   `json:"option_count,omitempty"`
	SelectedIndex *int   `json:"selected_index,omitempty"`
}

type contextReviewTerm struct {
	Kind     string `json:"kind"`
	Japanese string `json:"japanese"`
	English  string `json:"english"`
}

func buildContextReviewDocument(result cdccontext.Result, gameDir string) contextReviewDocument {
	document := contextReviewDocument{SchemaVersion: contextReviewSchemaVersion}
	switch {
	case result.Selector.ListScenes:
		document.View = "scene_catalogue"
		document.Query.ListScenes = true
		document.Scenes, _ = summarizeResultScenes(result, gameDir)
	case result.Selector.Scene != "":
		document.View = "scene_review"
		document.Query.Scene = result.Selector.Scene
		if len(result.Scenes) == 1 {
			review := buildSceneReview(result.Scenes[0], result.Selector, gameDir)
			document.Scene = &review
		}
	case result.Selector.Record >= 0:
		document.View = "record_scene_index"
		document.Query.Record = intPointer(result.Selector.Record)
		document.Scenes, document.Storage = summarizeResultScenes(result, gameDir)
		if entry, ok := selectedRecordEntry(result); ok {
			document.Record = &contextRecordReview{
				MessageID: entry.MessageID, Editable: editablePath(entry.MessageID), State: entry.State,
				Japanese: entry.Japanese, English: entry.English, StorageOnly: len(document.Scenes) == 0,
			}
		}
		if len(document.Scenes) == 0 && document.Storage != nil {
			document.Storage.NeighborRecords = storageNeighbors(result.Scenes, result.Selector.Record)
		}
	default:
		document.View = "bank_scene_index"
		document.Query.Bank = intPointer(result.Selector.Bank)
		document.Scenes, document.Storage = summarizeResultScenes(result, gameDir)
	}
	return document
}

func summarizeResultScenes(result cdccontext.Result, gameDir string) ([]contextSceneSummary, *contextStorageSummary) {
	recovered := make([]contextSceneSummary, 0, len(result.Scenes))
	var storageScene *cdccontext.Scene
	covered := make(map[int]bool)
	for index := range result.Scenes {
		scene := &result.Scenes[index]
		if scene.SourceKind == "message_bank" {
			storageScene = scene
			continue
		}
		summary := summarizeScene(*scene, result.Selector, gameDir)
		recovered = append(recovered, summary)
		for _, id := range summary.MatchingRecordIDs {
			covered[id] = true
		}
	}
	if storageScene == nil {
		return recovered, nil
	}
	storageOnly := make([]int, 0)
	for _, entry := range storageScene.Entries {
		if selectorMatchesReview(result.Selector, entry.MessageID) && !covered[entry.MessageID] {
			storageOnly = append(storageOnly, entry.MessageID)
		}
	}
	storage := &contextStorageSummary{
		ID: storageScene.ID, SourceArchive: storageScene.SourceArchive, Member: storageScene.Member,
		Ordering: storageScene.Ordering, RecordCount: len(storageScene.Entries),
		StorageOnlyRecordIDs: uniqueSortedIDs(storageOnly),
		OpenCommand:          sceneOpenCommand(gameDir, storageScene.ID),
	}
	return recovered, storage
}

func summarizeScene(scene cdccontext.Scene, selector cdccontext.Selector, gameDir string) contextSceneSummary {
	records := make([]int, 0, len(scene.Entries))
	matching := make([]int, 0, len(scene.Entries))
	other := make([]int, 0, len(scene.Entries))
	translation := contextTranslationCount{}
	countedTranslation := make(map[int]bool)
	for _, entry := range scene.Entries {
		records = append(records, entry.MessageID)
		if selectorMatchesReview(selector, entry.MessageID) || selector.Scene != "" {
			matching = append(matching, entry.MessageID)
		} else {
			other = append(other, entry.MessageID)
		}
		if !countedTranslation[entry.MessageID] {
			switch entry.State {
			case corpus.Translated:
				translation.Translated++
			case corpus.Todo:
				translation.Todo++
			case corpus.KeepJapanese:
				translation.KeepJapanese++
			}
			countedTranslation[entry.MessageID] = true
		}
	}
	summary := contextSceneSummary{
		ID: scene.ID, Aliases: append([]string(nil), scene.Aliases...), Kind: reviewSceneKind(scene.SourceKind),
		MatchKind: reviewMatchKind(scene.SourceKind), SourceArchive: scene.SourceArchive,
		Member: scene.Member, EmbeddedMember: scene.EmbeddedMember, Ordering: scene.Ordering,
		RuntimeChronology: runtimeChronology(scene.SourceKind),
		RecordIDs:         uniqueSortedIDs(records), MatchingRecordIDs: uniqueSortedIDs(matching),
		OtherRecordIDs: uniqueSortedIDs(other), OccurrenceCount: len(scene.Entries),
		Participants: sceneParticipants(scene), Translation: translation,
		OpenCommand: sceneOpenCommand(gameDir, scene.ID),
	}
	if scene.Scenario != nil {
		summary.Scenario = &contextScenarioSummary{
			Slot: scene.Scenario.Slot, EquivalentGroups: append([]string(nil), scene.Scenario.EquivalentGroups...),
		}
	}
	return summary
}

func buildSceneReview(scene cdccontext.Scene, selector cdccontext.Selector, gameDir string) contextSceneReview {
	review := contextSceneReview{
		Summary: summarizeScene(scene, selector, gameDir), Entries: make([]contextReviewEntry, 0, len(scene.Entries)),
		Limitations: sceneReviewLimitations(scene),
	}
	termSeen := make(map[string]bool)
	for _, entry := range scene.Entries {
		reviewEntry := compactReviewEntry(entry)
		review.Entries = append(review.Entries, reviewEntry)
		for _, term := range reviewEntry.Terminology {
			key := term.Kind + "\x00" + term.Japanese + "\x00" + term.English
			if !termSeen[key] {
				review.Terminology = append(review.Terminology, term)
				termSeen[key] = true
			}
		}
	}
	return review
}

func compactReviewEntry(entry cdccontext.Entry) contextReviewEntry {
	result := contextReviewEntry{
		Position: entry.Position, RecordID: entry.MessageID, Editable: editablePath(entry.MessageID),
		Kind: reviewEntryKind(entry.Kind), State: entry.State,
		Speaker: contextParticipant{
			Japanese: entry.SpeakerJapanese, English: entry.SpeakerEnglish,
			Status: speakerStatus(entry), Basis: entry.SpeakerSource,
		},
	}
	if len(entry.SourceControls) == 0 {
		result.Japanese = entry.Japanese
		result.English = entry.English
	} else {
		for _, control := range entry.SourceControls {
			compact := contextReviewControl{Kind: control.Kind, Variants: make([]contextReviewVariant, 0, len(control.Blocks))}
			for _, block := range control.Blocks {
				compact.Variants = append(compact.Variants, contextReviewVariant{
					Position: block.Position, Role: block.Role, Japanese: block.Japanese, English: block.English,
				})
			}
			result.RecordVariants = append(result.RecordVariants, compact)
		}
	}
	for _, condition := range entry.Conditions {
		if condition.Status == "unresolved" && condition.SelectedIndex == nil {
			continue
		}
		result.Branches = append(result.Branches, contextReviewCondition{
			Kind: condition.Kind, Status: condition.Status, Description: reviewConditionDescription(condition),
			BaseMessageID: condition.BaseMessageID, OptionCount: condition.OptionCount,
			SelectedIndex: condition.SelectedIndex,
		})
	}
	for _, term := range entry.Terminology {
		result.Terminology = append(result.Terminology, contextReviewTerm{Kind: term.Kind, Japanese: term.Japanese, English: term.English})
	}
	return result
}

func writeContextReviewText(output io.Writer, result cdccontext.Result, gameDir string) {
	document := buildContextReviewDocument(result, gameDir)
	switch document.View {
	case "scene_catalogue":
		writeGlobalSceneCatalogue(output, document)
	case "bank_scene_index":
		writeBankSceneIndex(output, document)
	case "record_scene_index":
		writeRecordSceneIndex(output, document)
	case "scene_review":
		if document.Scene != nil {
			writeSceneReview(output, *document.Scene)
		}
	}
}

func writeGlobalSceneCatalogue(output io.Writer, document contextReviewDocument) {
	fmt.Fprintf(output, "Recovered scenes: %d\n", len(document.Scenes))
	fmt.Fprintln(output, "Scene IDs (first column; pass one to --scene):")
	for _, scene := range document.Scenes {
		fmt.Fprintf(output, "%s\t%s\trecords=%s", scene.ID, scene.Kind, formatIDRanges(scene.RecordIDs))
		if scene.Scenario != nil {
			fmt.Fprintf(output, "\tgroups=%s", strings.Join(scene.Scenario.EquivalentGroups, ","))
		}
		if labels := participantLabels(scene.Participants, 3); labels != "" {
			fmt.Fprintf(output, "\tspeakers=%s", labels)
		}
		fmt.Fprintln(output)
	}
}

func writeBankSceneIndex(output io.Writer, document contextReviewDocument) {
	bank := *document.Query.Bank
	fmt.Fprintf(output, "Bank %03d\n", bank)
	if document.Storage != nil {
		fmt.Fprintf(output, "Records: %d\n", document.Storage.RecordCount)
	}
	fmt.Fprintf(output, "Recovered scenes: %d\n", len(document.Scenes))
	writeSceneSummaryList(output, document.Scenes)
	if document.Storage != nil {
		fmt.Fprintf(output, "\nStorage-only records: %s\n", formatIDRanges(document.Storage.StorageOnlyRecordIDs))
		fmt.Fprintf(output, "Storage container: %s (%s; not verified chronology)\n", document.Storage.ID, document.Storage.Member)
		fmt.Fprintf(output, "Open storage: %s\n", document.Storage.OpenCommand)
	}
}

func writeRecordSceneIndex(output io.Writer, document contextReviewDocument) {
	recordID := *document.Query.Record
	fmt.Fprintf(output, "Record %d\n", recordID)
	if document.Record != nil {
		fmt.Fprintf(output, "State: %s\nEditable: %s\n", document.Record.State, document.Record.Editable)
		fmt.Fprintf(output, "Japanese: %s\nEnglish: %s\n", snippet(document.Record.Japanese, 280), snippet(document.Record.English, 280))
	}
	fmt.Fprintf(output, "\nRecovered scenes: %d\n", len(document.Scenes))
	writeSceneSummaryList(output, document.Scenes)
	if len(document.Scenes) == 0 && document.Storage != nil {
		fmt.Fprintln(output, "\nNo recovered scene references this record.")
		fmt.Fprintln(output, "Source-bank neighbors follow in storage order only; they are not verified chronology.")
		for _, entry := range document.Storage.NeighborRecords {
			marker := " "
			if entry.RecordID == recordID {
				marker = ">"
			}
			if len(entry.RecordVariants) == 0 {
				fmt.Fprintf(output, "%s %d [%s] JP: %s\n  EN: %s\n", marker, entry.RecordID, entry.State, snippet(entry.Japanese, storageNeighborTextLimit), snippet(entry.English, storageNeighborTextLimit))
				continue
			}
			fmt.Fprintf(output, "%s %d [%s] record variants:\n", marker, entry.RecordID, entry.State)
			for _, control := range entry.RecordVariants {
				for _, variant := range control.Variants {
					fmt.Fprintf(output, "    Variant %d (%s) JP: %s\n      EN: %s\n", variant.Position+1, strings.ReplaceAll(variant.Role, "_", " "), snippet(variant.Japanese, storageNeighborTextLimit), snippet(variant.English, storageNeighborTextLimit))
				}
				if control.VariantsOmitted > 0 {
					fmt.Fprintf(output, "    Additional variants omitted: %d\n", control.VariantsOmitted)
				}
			}
		}
		fmt.Fprintf(output, "Open storage: %s\n", document.Storage.OpenCommand)
	}
}

func writeSceneSummaryList(output io.Writer, scenes []contextSceneSummary) {
	for index, scene := range scenes {
		fmt.Fprintf(output, "\n%d. %s\n", index+1, scene.ID)
		fmt.Fprintf(output, "   Kind: %s (%s)\n", scene.Kind, scene.MatchKind)
		fmt.Fprintf(output, "   Records: %s", formatIDRanges(scene.MatchingRecordIDs))
		if len(scene.OtherRecordIDs) > 0 {
			fmt.Fprintf(output, " · other scene records: %s", formatIDRanges(scene.OtherRecordIDs))
		}
		fmt.Fprintln(output)
		if labels := participantLabels(scene.Participants, 4); labels != "" {
			fmt.Fprintf(output, "   Speakers: %s\n", labels)
		}
		if scene.Scenario != nil {
			fmt.Fprintf(output, "   Scenario groups with identical content: %s\n", strings.Join(scene.Scenario.EquivalentGroups, ", "))
		}
		fmt.Fprintf(output, "   Translation: %d translated, %d todo, %d keep-Japanese\n", scene.Translation.Translated, scene.Translation.Todo, scene.Translation.KeepJapanese)
		fmt.Fprintf(output, "   Open: %s\n", scene.OpenCommand)
	}
}

func writeSceneReview(output io.Writer, review contextSceneReview) {
	scene := review.Summary
	fmt.Fprintf(output, "Scene: %s\n", scene.ID)
	fmt.Fprintf(output, "Kind: %s\nSource: %s:%s", scene.Kind, scene.SourceArchive, scene.Member)
	if scene.EmbeddedMember != "" {
		fmt.Fprintf(output, "#%s", scene.EmbeddedMember)
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "Ordering: %s\nRuntime chronology: %s\n", scene.Ordering, scene.RuntimeChronology)
	if scene.Scenario != nil {
		fmt.Fprintf(output, "Equivalent scenario groups: %s\n", strings.Join(scene.Scenario.EquivalentGroups, ", "))
	}
	if len(scene.Aliases) > 0 {
		const aliasLimit = 8
		aliases := scene.Aliases
		if len(aliases) > aliasLimit {
			aliases = aliases[:aliasLimit]
		}
		fmt.Fprintf(output, "Aliases: %s", strings.Join(aliases, ", "))
		if len(scene.Aliases) > aliasLimit {
			fmt.Fprintf(output, " (+%d more; JSON retains all)", len(scene.Aliases)-aliasLimit)
		}
		fmt.Fprintln(output)
	}
	if len(scene.Participants) == 0 {
		fmt.Fprintln(output, "Participants: unknown")
	} else {
		fmt.Fprintln(output, "Participants:")
		for _, participant := range scene.Participants {
			fmt.Fprintf(output, "- %s — %s\n", participantDisplay(participant), participant.Status)
		}
	}
	fmt.Fprintf(output, "Translation: %d translated, %d todo, %d keep-Japanese\n", scene.Translation.Translated, scene.Translation.Todo, scene.Translation.KeepJapanese)

	fmt.Fprintln(output, "\nDialogue:")
	for _, entry := range review.Entries {
		fmt.Fprintf(output, "\n[%d] %s %d · %s\n", entry.Position, entry.Kind, entry.RecordID, entry.State)
		fmt.Fprintf(output, "Speaker: %s", participantDisplay(entry.Speaker))
		if entry.Speaker.Status != "unknown" {
			fmt.Fprintf(output, " — %s", entry.Speaker.Status)
		}
		fmt.Fprintln(output)
		fmt.Fprintf(output, "Editable: %s\n", entry.Editable)
		for _, branch := range entry.Branches {
			fmt.Fprintf(output, "Branch: %s\n", branch.Description)
		}
		if len(entry.RecordVariants) == 0 {
			fmt.Fprintf(output, "JP: %s\nEN: %s\n", entry.Japanese, entry.English)
		} else {
			for _, control := range entry.RecordVariants {
				fmt.Fprintf(output, "Record variants: %s\n", control.Kind)
				for _, variant := range control.Variants {
					fmt.Fprintf(output, "  Variant %d: %s", variant.Position+1, strings.ReplaceAll(variant.Role, "_", " "))
					fmt.Fprintf(output, "\n    JP: %s\n    EN: %s\n", variant.Japanese, variant.English)
				}
			}
		}
	}
	if len(review.Terminology) > 0 {
		fmt.Fprintln(output, "\nTerminology:")
		for _, term := range review.Terminology {
			fmt.Fprintf(output, "- %s: %s → %s\n", term.Kind, term.Japanese, term.English)
		}
	}
	fmt.Fprintln(output, "\nContext boundaries:")
	for _, limitation := range review.Limitations {
		fmt.Fprintf(output, "- %s\n", limitation)
	}
}

func selectedRecordEntry(result cdccontext.Result) (cdccontext.Entry, bool) {
	for _, scene := range result.Scenes {
		for _, entry := range scene.Entries {
			if entry.MessageID == result.Selector.Record {
				return entry, true
			}
		}
	}
	return cdccontext.Entry{}, false
}

func storageNeighbors(scenes []cdccontext.Scene, recordID int) []contextReviewEntry {
	for _, scene := range scenes {
		if scene.SourceKind != "message_bank" {
			continue
		}
		for index, entry := range scene.Entries {
			if entry.MessageID != recordID {
				continue
			}
			first := max(0, index-3)
			last := min(len(scene.Entries), index+4)
			result := make([]contextReviewEntry, 0, last-first)
			for _, neighbor := range scene.Entries[first:last] {
				compact := compactReviewEntry(neighbor)
				limit := storageNeighborVariantLimit
				if neighbor.MessageID == recordID {
					limit = storageTargetVariantLimit
				}
				for controlIndex := range compact.RecordVariants {
					control := &compact.RecordVariants[controlIndex]
					if len(control.Variants) > limit {
						control.VariantsOmitted = len(control.Variants) - limit
						control.Variants = control.Variants[:limit]
					}
				}
				result = append(result, compact)
			}
			return result
		}
	}
	return nil
}

func sceneParticipants(scene cdccontext.Scene) []contextParticipant {
	result := make([]contextParticipant, 0)
	seen := make(map[string]bool)
	for _, entry := range scene.Entries {
		if entry.SpeakerEnglish == "" && entry.SpeakerJapanese == "" {
			continue
		}
		participant := contextParticipant{
			Japanese: entry.SpeakerJapanese, English: entry.SpeakerEnglish,
			Status: speakerStatus(entry), Basis: entry.SpeakerSource,
		}
		key := participant.Japanese + "\x00" + participant.English + "\x00" + participant.Status
		if !seen[key] {
			result = append(result, participant)
			seen[key] = true
		}
	}
	return result
}

func speakerStatus(entry cdccontext.Entry) string {
	if entry.SpeakerStatus == "" {
		return "unknown"
	}
	return entry.SpeakerStatus
}

func participantDisplay(participant contextParticipant) string {
	if participant.English != "" && participant.Japanese != "" && participant.English != participant.Japanese {
		return participant.English + " / " + participant.Japanese
	}
	if participant.English != "" {
		return participant.English
	}
	if participant.Japanese != "" {
		return participant.Japanese
	}
	return "unknown"
}

func participantLabels(participants []contextParticipant, limit int) string {
	if len(participants) == 0 {
		return ""
	}
	count := min(len(participants), limit)
	labels := make([]string, count)
	for index := range labels {
		labels[index] = participantDisplay(participants[index])
	}
	result := strings.Join(labels, ", ")
	if len(participants) > limit {
		result += fmt.Sprintf(" (+%d more)", len(participants)-limit)
	}
	return result
}

func sceneReviewLimitations(scene cdccontext.Scene) []string {
	result := []string{"Source ordering is preserved; absolute gameplay chronology and runtime branch choices are not established."}
	for _, participant := range sceneParticipants(scene) {
		if strings.HasPrefix(participant.Status, "inferred_") {
			result = append(result, "Speaker identities marked as inferred come from static association labels, not confirmed vocal-speaker data.")
			break
		}
	}
	if scene.SourceKind == "ambient_interaction" {
		result = append(result, "Room-authored interaction targets do not establish simultaneous presence or global dialogue order.")
	}
	if scene.SourceKind == "message_bank" {
		result = append(result, "This is a storage container, not a recovered dialogue scene; record order is not verified chronology.")
	}
	return result
}

func reviewConditionDescription(condition cdccontext.Condition) string {
	if condition.SelectedIndex != nil {
		option := *condition.SelectedIndex + 1
		if condition.OptionCount != nil {
			return fmt.Sprintf("choice option %d of %d", option, *condition.OptionCount)
		}
		return fmt.Sprintf("choice option %d", option)
	}
	if condition.Status == "unresolved" {
		return "conditional context unresolved"
	}
	description := strings.ReplaceAll(condition.Kind, "_", " ")
	if condition.Comparator != "" {
		description += " using " + strings.ReplaceAll(condition.Comparator, "_", " ")
	}
	if condition.Polarity == "negated" {
		description += " (negated)"
	}
	return description
}

func reviewSceneKind(kind string) string {
	switch kind {
	case "cdc_program":
		return "CDC dialogue scene"
	case "ambient_interaction":
		return "ambient interaction group"
	case "message_bank":
		return "message-bank storage"
	default:
		return strings.ReplaceAll(kind, "_", " ")
	}
}

func reviewMatchKind(kind string) string {
	switch kind {
	case "cdc_program":
		return "direct_cdc_consumer"
	case "ambient_interaction":
		return "verified_ambient_interaction"
	case "message_bank":
		return "storage_fallback"
	default:
		return kind
	}
}

func reviewEntryKind(kind string) string {
	switch kind {
	case "dialogue_association", "ambient_dialogue":
		return "Dialogue"
	case "selection_option":
		return "Choice option"
	case "notification":
		return "Notification"
	case "cinematic_text":
		return "Cinematic text"
	case "prompt":
		return "Prompt"
	case "bank_record":
		return "Storage record"
	default:
		return strings.ReplaceAll(kind, "_", " ")
	}
}

func runtimeChronology(sourceKind string) string {
	if sourceKind == "message_bank" {
		return "not established (storage order only)"
	}
	return "not established"
}

func selectorMatchesReview(selector cdccontext.Selector, messageID int) bool {
	switch {
	case selector.Record >= 0:
		return selector.Record == messageID
	case selector.Bank >= 0:
		return selector.Bank == messageID/10_000
	default:
		return true
	}
}

func uniqueSortedIDs(ids []int) []int {
	seen := make(map[int]bool, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	sort.Ints(result)
	return result
}

func formatIDRanges(ids []int) string {
	ids = uniqueSortedIDs(ids)
	if len(ids) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(ids))
	for first := 0; first < len(ids); {
		last := first
		for last+1 < len(ids) && ids[last+1] == ids[last]+1 {
			last++
		}
		if last-first >= 2 {
			parts = append(parts, fmt.Sprintf("%d–%d", ids[first], ids[last]))
		} else {
			for index := first; index <= last; index++ {
				parts = append(parts, strconv.Itoa(ids[index]))
			}
		}
		first = last + 1
	}
	return strings.Join(parts, ", ")
}

func sceneOpenCommand(gameDir, sceneID string) string {
	return fmt.Sprintf("./zill context --game-dir %s --scene %s", shellQuote(gameDir), shellQuote(sceneID))
}

func shellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_./:@%+=,-#", r))
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func intPointer(value int) *int {
	return &value
}
