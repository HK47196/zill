// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/cdccontext"
	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/gamefmt/paa"
)

const contextUsage = "zill context --game-dir PATH (--bank NNN | --record ID) [--format text|review|json]"

type contextOptions struct {
	gameDir  string
	format   string
	selectBy cdccontext.Selector
}

func runContext(root string, args []string, stdout, stderr io.Writer) int {
	options, err := parseContextOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "zill: context: %v\n", err)
		fmt.Fprintf(stderr, "zill: usage: %s\n", contextUsage)
		return 2
	}
	project, _, err := corpus.LoadProject(root)
	if err != nil {
		fmt.Fprintf(stderr, "zill: context: %v\n", err)
		return 1
	}
	if options.selectBy.Record >= 0 {
		if _, exists := project.Find(options.selectBy.Record); !exists {
			fmt.Fprintf(stderr, "zill: context: record %d does not exist\n", options.selectBy.Record)
			return 1
		}
	}
	terms, err := loadTerminology(root)
	if err != nil {
		fmt.Fprintf(stderr, "zill: context: %v\n", err)
		return 1
	}
	usrdir := filepath.Join(options.gameDir, "USRDIR")
	archives := make([]cdccontext.Archive, 0, 2)
	for _, name := range []string{"pa", "pami"} {
		pair, openErr := paa.Open(filepath.Join(usrdir, name+".bin"), filepath.Join(usrdir, name+".arc"))
		if openErr != nil {
			for _, archive := range archives {
				_ = archive.Pair.Close()
			}
			fmt.Fprintf(stderr, "zill: context: %v\n", openErr)
			return 1
		}
		archives = append(archives, cdccontext.Archive{Name: name, Pair: pair})
	}
	result, buildErr := cdccontext.Build(project, terms, archives, options.selectBy)
	var closeErr error
	for _, archive := range archives {
		if err := archive.Pair.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if buildErr != nil {
		fmt.Fprintf(stderr, "zill: context: %v\n", buildErr)
		return 1
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "zill: context: close PAA archive: %v\n", closeErr)
		return 1
	}
	if options.format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintf(stderr, "zill: context: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	if options.format == "review" {
		writeReviewText(stdout, result)
		return 0
	}
	writeContextText(stdout, result)
	return 0
}

func parseContextOptions(args []string) (contextOptions, error) {
	options := contextOptions{format: "text", selectBy: cdccontext.Selector{Bank: -1, Record: -1}}
	gameDirSet, bankSet, recordSet, formatSet := false, false, false, false
	for index := 0; index < len(args); index++ {
		argument := args[index]
		name, value, hasEquals := strings.Cut(argument, "=")
		var err error
		nextValue := func() (string, error) {
			if hasEquals {
				if value == "" {
					return "", fmt.Errorf("%s requires a value", name)
				}
				return value, nil
			}
			if index+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			index++
			if args[index] == "" {
				return "", fmt.Errorf("%s requires a value", name)
			}
			return args[index], nil
		}
		switch name {
		case "--game-dir":
			if gameDirSet {
				return contextOptions{}, fmt.Errorf("--game-dir may be specified only once")
			}
			gameDirSet = true
			options.gameDir, err = nextValue()
		case "--bank":
			if bankSet {
				return contextOptions{}, fmt.Errorf("--bank may be specified only once")
			}
			bankSet = true
			var raw string
			raw, err = nextValue()
			if err == nil {
				options.selectBy.Bank, err = parseContextInteger("bank", raw, 278)
			}
		case "--record":
			if recordSet {
				return contextOptions{}, fmt.Errorf("--record may be specified only once")
			}
			recordSet = true
			var raw string
			raw, err = nextValue()
			if err == nil {
				options.selectBy.Record, err = parseContextInteger("record", raw, 2_789_999)
			}
		case "--format":
			if formatSet {
				return contextOptions{}, fmt.Errorf("--format may be specified only once")
			}
			formatSet = true
			options.format, err = nextValue()
			if err == nil && options.format != "text" && options.format != "review" && options.format != "json" {
				err = fmt.Errorf("unsupported format %q", options.format)
			}
		default:
			return contextOptions{}, fmt.Errorf("unknown argument %q", argument)
		}
		if err != nil {
			return contextOptions{}, err
		}
	}
	if !gameDirSet {
		return contextOptions{}, fmt.Errorf("--game-dir is required")
	}
	if bankSet == recordSet {
		return contextOptions{}, fmt.Errorf("set exactly one of --bank or --record")
	}
	if options.format == "review" && options.selectBy.Record < 0 {
		return contextOptions{}, fmt.Errorf("--format review requires --record")
	}
	return options, nil
}

func writeReviewText(output io.Writer, result cdccontext.Result) {
	fmt.Fprintf(output, "Review target: record %d\nPackets: %d\n", result.Selector.Record, len(result.ReviewPackets))
	for _, packet := range result.ReviewPackets {
		fmt.Fprintf(output, "\nScene: %s\n", packet.SceneMember)
		if packet.EmbeddedMember != "" {
			fmt.Fprintf(output, "  Embedded resource: %s\n", packet.EmbeddedMember)
		}
		fmt.Fprintf(output, "  Archive: %s\n", packet.SourceArchive)
		fmt.Fprintf(output, "  Source: %s\n", packet.SourceKind)
		fmt.Fprintf(output, "  Ordering: %s\n", packet.Ordering)
		fmt.Fprintf(output, "  Evidence: %s\n", packet.EvidenceStatus)
		fmt.Fprintf(output, "  Target occurrence: position=%d path=%s\n", packet.OccurrencePosition, contextPath(packet.Path))
		for _, reference := range packet.References {
			fmt.Fprintf(output, "  Cross-program reference: %s execution=%s resolution=%s", reference.Opcode, reference.ExecutionStatus, reference.ResolutionStatus)
			if reference.ScenarioSlot != nil {
				fmt.Fprintf(output, " slot=%d candidates=%d/%d", *reference.ScenarioSlot, reference.CandidateGroupsFound, reference.CandidateGroupsExpected)
			}
			if reference.Resource != nil {
				fmt.Fprintf(output, " key=%s", reference.Resource.LogicalKey)
			}
			fmt.Fprintln(output)
		}
		if packet.ReferencesOmitted > 0 {
			fmt.Fprintf(output, "  Cross-program references omitted from bounded review: %d\n", packet.ReferencesOmitted)
		}
		for _, entry := range packet.Context {
			fmt.Fprintf(output, "\n  Review role: %s\n", entry.Role)
			writeContextEntries(output, []cdccontext.Entry{entry.Entry})
		}
		for _, entry := range packet.AlternateArms {
			fmt.Fprintf(output, "\n  Review role: %s\n", entry.Role)
			writeContextEntries(output, []cdccontext.Entry{entry.Entry})
		}
		if packet.AlternateArmsOmitted > 0 {
			fmt.Fprintf(output, "\n  Alternate-arm entries omitted from bounded review: %d\n", packet.AlternateArmsOmitted)
		}
	}
}

func parseContextInteger(kind, value string, maximum int) (int, error) {
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 || number > maximum {
		return 0, fmt.Errorf("invalid %s %q", kind, value)
	}
	return number, nil
}

func writeContextText(output io.Writer, result cdccontext.Result) {
	query := fmt.Sprintf("bank %03d", result.Selector.Bank)
	if result.Selector.Record >= 0 {
		query = fmt.Sprintf("record %d", result.Selector.Record)
	}
	fmt.Fprintf(output, "Query: %s\nScenes: %d\n", query, len(result.Scenes))
	for _, scene := range result.Scenes {
		fmt.Fprintf(output, "\nScene: %s\n", scene.Member)
		if scene.EmbeddedMember != "" {
			fmt.Fprintf(output, "  Embedded resource: %s\n", scene.EmbeddedMember)
		}
		fmt.Fprintf(output, "  Archive: %s\n", scene.SourceArchive)
		fmt.Fprintf(output, "  Source: %s\n", scene.SourceKind)
		fmt.Fprintf(output, "  Ordering: %s\n", scene.Ordering)
		fmt.Fprintf(output, "  Evidence: %s\n", scene.EvidenceStatus)
		if scene.FirstRecordMessageID != nil {
			fmt.Fprintf(output, "  First record (%d): %s", *scene.FirstRecordMessageID, scene.FirstRecordJapanese)
			if scene.FirstRecordEnglish != "" {
				fmt.Fprintf(output, " / %s", scene.FirstRecordEnglish)
			}
			fmt.Fprintln(output)
		}
		for _, evidence := range scene.SourceEvidence {
			fmt.Fprintf(output, "  Source evidence: %s status=%s confidence=%s runtime=%s\n", evidence.Kind, evidence.Status, evidence.Confidence, evidence.RuntimeStatus)
			if evidence.MarkerLabel != "" || len(evidence.MarkerMessageIDs) > 0 {
				fmt.Fprintf(output, "    Marker: event=%d label=%s records=%v\n", evidence.EventNumber, evidence.MarkerLabel, evidence.MarkerMessageIDs)
			}
			for _, candidate := range evidence.Candidates {
				fmt.Fprintf(output, "    Authoring candidate (%d): %s", candidate.MessageID, candidate.Japanese)
				if candidate.English != "" {
					fmt.Fprintf(output, " / %s", candidate.English)
				}
				fmt.Fprintf(output, " label_match=%t", candidate.LabelMatch)
				fmt.Fprintln(output)
			}
			fmt.Fprintf(output, "    Basis: %s\n", evidence.Basis)
			if evidence.SourceLocator != "" {
				fmt.Fprintf(output, "    Source locator: %s\n", evidence.SourceLocator)
			}
		}
		if scene.SourceKind == "message_bank" {
			fmt.Fprintln(output, "  Limitations: record-local controls and source-authoring candidates do not establish scene chronology, speakers, actor presence, or runtime reachability.")
		} else if scene.SourceKind == "ambient_interaction" {
			fmt.Fprintln(output, "  Limitations: room-authored entity records and the executable interaction mapping do not establish global dialogue chronology or simultaneous runtime presence.")
		}
		writeContextEntries(output, scene.Entries)
		if len(scene.References) > 0 {
			fmt.Fprintln(output, "  Static references (execution remains conditional):")
			for _, reference := range scene.References {
				fmt.Fprintf(output, "    %s @%d path=%s execution=%s resolution=%s raw=%s\n", reference.Opcode, reference.Offset, contextPath(reference.Path), reference.ExecutionStatus, reference.ResolutionStatus, reference.Raw)
				for _, candidate := range reference.ScenarioCandidates {
					fmt.Fprintf(output, "      Candidate: group=%s slot=%d archive=%s member=%s confidence=%s\n", candidate.Group, candidate.Slot, candidate.SourceArchive, candidate.Member, candidate.Confidence)
				}
			}
		}
	}
}

func writeContextEntries(output io.Writer, entries []cdccontext.Entry) {
	for _, entry := range entries {
		target := ""
		if entry.Selected {
			target = " target=true"
		}
		if entry.OffsetBasis == "message_bank_byte_offset" {
			fmt.Fprintf(output, "  [%d] %s %d @%d offset=%s reachability=%s%s\n", entry.Position, entry.Kind, entry.MessageID, entry.Offset, entry.OffsetBasis, entry.Reachability, target)
		} else if entry.Offset >= 0 {
			fmt.Fprintf(output, "  [%d] %s %d @%d offset=%s path=%s reachability=%s%s\n", entry.Position, entry.Kind, entry.MessageID, entry.Offset, entry.OffsetBasis, contextPath(entry.Path), entry.Reachability, target)
		} else {
			fmt.Fprintf(output, "  [%d] %s %d reachability=%s%s\n", entry.Position, entry.Kind, entry.MessageID, entry.Reachability, target)
		}
		if entry.Guard != "" {
			fmt.Fprintf(output, "    Enclosing blocks: %s\n", entry.Guard)
		}
		for _, condition := range entry.Conditions {
			fmt.Fprintf(output, "    Condition: %s kind=%s status=%s", condition.Raw, condition.Kind, condition.Status)
			if condition.Comparator != "" {
				fmt.Fprintf(output, " comparator=%s", condition.Comparator)
			}
			if condition.Polarity != "" {
				fmt.Fprintf(output, " polarity=%s", condition.Polarity)
			}
			fmt.Fprintln(output)
		}
		for controlIndex, control := range entry.SourceControls {
			fmt.Fprintf(output, "    Record-local control %d: %s evidence=%s", controlIndex, control.Kind, control.Evidence)
			if control.Selector != "" {
				fmt.Fprintf(output, " selector=%s", control.Selector)
			}
			if control.ExpectedBlocks != nil {
				fmt.Fprintf(output, " expected_blocks=%d", *control.ExpectedBlocks)
			}
			fmt.Fprintln(output)
			for _, block := range control.Blocks {
				fmt.Fprintf(output, "      Block %d: role=%s", block.Position, block.Role)
				if block.Condition != "" {
					fmt.Fprintf(output, " condition=%s", block.Condition)
				}
				fmt.Fprintln(output)
				fmt.Fprintf(output, "        Japanese: %s\n", block.Japanese)
				fmt.Fprintf(output, "        English: %s\n", block.English)
			}
		}
		if entry.EntityAssociationHandleRaw != nil {
			fmt.Fprintf(output, "    Association: handle=%d", *entry.EntityAssociationHandleRaw)
			if entry.DisplayMode != nil {
				fmt.Fprintf(output, " mode=%d", *entry.DisplayMode)
			}
			fmt.Fprintf(output, " resolution=%s", entry.AssociationResolution)
			if entry.AssociationNameRecordID != nil {
				fmt.Fprintf(output, " name_record=%d", *entry.AssociationNameRecordID)
			}
			if entry.AssociatedLabelMessageID != nil {
				fmt.Fprintf(output, " label_message=%d", *entry.AssociatedLabelMessageID)
			}
			fmt.Fprintln(output)
			if entry.DisplayMode != nil {
				fmt.Fprintf(output, "    Display requests: portrait=%t name_label=%t forced_state_three=%t portrait_status=%s\n", boolValue(entry.PortraitRequested), boolValue(entry.NameLabelRequested), boolValue(entry.ForcedStateThree), entry.PortraitStatus)
			}
			if entry.AssociatedLabelJapanese != "" || entry.AssociatedLabelEnglish != "" {
				fmt.Fprintf(output, "    Associated label: %s / %s\n", entry.AssociatedLabelJapanese, entry.AssociatedLabelEnglish)
			}
			fmt.Fprintf(output, "    Speaker status: %s", entry.SpeakerStatus)
			if entry.SpeakerEnglish != "" || entry.SpeakerJapanese != "" {
				fmt.Fprintf(output, " (%s / %s)", entry.SpeakerJapanese, entry.SpeakerEnglish)
			}
			fmt.Fprintln(output)
		}
		if entry.AmbientInteraction != nil {
			interaction := entry.AmbientInteraction
			fmt.Fprintf(output, "    Ambient interaction: status=%s runtime=%s", interaction.Status, interaction.RuntimeStatus)
			if interaction.RoomMember != "" {
				fmt.Fprintf(output, " room=%s", interaction.RoomMember)
			}
			if interaction.RoomResource != "" {
				fmt.Fprintf(output, " resource=%s", interaction.RoomResource)
			}
			if interaction.EntitySlot != nil {
				fmt.Fprintf(output, " slot=%d", *interaction.EntitySlot)
			}
			fmt.Fprintln(output)
			fmt.Fprintf(output, "      Source locator: %s\n", interaction.SourceLocator)
		}
		if len(entry.Actors) > 0 {
			parts := make([]string, len(entry.Actors))
			for index, actor := range entry.Actors {
				label := actor.AssociatedLabelEnglish
				if label == "" {
					label = actor.AssociatedLabelJapanese
				}
				if label == "" {
					label = actor.AssociationLabelResolution
				}
				parts[index] = fmt.Sprintf("%d=%s[%s;%s]", actor.Handle, label, actor.Presence, actor.PresenceBasis)
			}
			fmt.Fprintf(output, "    Actor lifecycle: %s\n", strings.Join(parts, ", "))
			for _, actor := range entry.Actors {
				if actor.Position != nil {
					fmt.Fprintf(output, "      Actor %d position: component_2=%d component_3=%d source=%s status=%s\n", actor.Handle, actor.Position.Component2, actor.Position.Component3, actor.Position.Source, actor.Position.Status)
				}
				if actor.Action != nil {
					fmt.Fprintf(output, "      Actor %d action: id=%d source=%s status=%s flag=%s", actor.Handle, actor.Action.ActionIDRaw, actor.Action.Source, actor.Action.Status, actor.Action.C5AssociationBehaviorFlag)
					if actor.Action.ModifierRaw != nil {
						fmt.Fprintf(output, " modifier=%d", *actor.Action.ModifierRaw)
					}
					fmt.Fprintln(output)
				}
				if actor.Relation != nil {
					fmt.Fprintf(output, "      Actor %d relation: mode_or_value=%d source=%s status=%s\n", actor.Handle, actor.Relation.ModeOrValueRaw, actor.Relation.Source, actor.Relation.Status)
				}
			}
		}
		for _, candidate := range entry.PossibleAddressees {
			fmt.Fprintf(output, "    Possible addressee: handle=%d label=%s confidence=%s evidence=%s\n", candidate.Handle, candidate.Label, candidate.Confidence, candidate.Evidence)
		}
		fmt.Fprintf(output, "    Japanese: %s\n", entry.Japanese)
		fmt.Fprintf(output, "    English: %s\n", entry.English)
		for _, term := range entry.Terminology {
			fmt.Fprintf(output, "    Authority: %s: %s → %s\n", term.Kind, term.Japanese, term.English)
		}
		if entry.AuthoringMetadata != nil {
			fmt.Fprintf(output, "    Authoring metadata: table=%s status=%s runtime=%s raw=%s\n", entry.AuthoringMetadata.TableKind, entry.AuthoringMetadata.Status, entry.AuthoringMetadata.RuntimeStatus, entry.AuthoringMetadata.RawLabel)
		}
		for _, evidence := range entry.ConsumerEvidence {
			fmt.Fprintf(output, "    Executable consumer: disposition=%s role=%s category=%s confidence=%s runtime=%s\n", evidence.Disposition, evidence.Role, evidence.Category, evidence.Confidence, evidence.RuntimeStatus)
		}
		for _, relationship := range entry.Relationships {
			fmt.Fprintf(output, "    Executable relationship: %s → %d status=%s runtime=%s\n", relationship.Kind, relationship.TargetMessage, relationship.Status, relationship.RuntimeStatus)
			fmt.Fprintf(output, "      Japanese: %s\n", relationship.TargetJapanese)
			fmt.Fprintf(output, "      English: %s\n", relationship.TargetEnglish)
		}
	}
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func contextPath(path []int) string {
	if len(path) == 0 {
		return "root"
	}
	parts := make([]string, len(path)+1)
	parts[0] = "root"
	for index, component := range path {
		parts[index+1] = strconv.Itoa(component)
	}
	return strings.Join(parts, "/")
}
