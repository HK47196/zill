// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/cdccontext"
	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/gamefmt/paa"
)

const contextUsage = "zill context --game-dir PATH (--list-scenes | --bank NNN | --record ID | --scene ID) [--format text|json] [--verbose]"

const contextHelp = `Usage: zill context --game-dir PATH (--list-scenes | --bank NNN | --record ID | --scene ID) [options]

Selectors:
  --list-scenes List every recovered CDC and ambient dialogue scene
  --bank NNN    List recovered scenes containing records from one message bank
  --record ID   Map one message record to every recovered scene containing it
  --scene ID    Render one complete scene for translation review

Options:
  --format text|json  Output format (default: text)
  --verbose           Emit the complete diagnostic projection
  -h, --help          Show this help

Bank and record queries print copyable --scene commands. A message-bank storage
unit is reported separately because storage order is not verified chronology.
`

type contextOptions struct {
	gameDir  string
	format   string
	verbose  bool
	selectBy cdccontext.Selector
}

func runContext(root string, args []string, stdout, stderr io.Writer) int {
	for _, argument := range args {
		if argument == "-h" || argument == "--help" {
			fmt.Fprint(stdout, contextHelp)
			return 0
		}
	}
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
	cacheDirectory := ""
	if userCache, cacheErr := os.UserCacheDir(); cacheErr == nil {
		cacheDirectory = filepath.Join(userCache, "zill", "context")
	}
	index, buildErr := cdccontext.LoadOrBuildRetailIndex(archives, cacheDirectory)
	var result cdccontext.Result
	if buildErr == nil {
		result, buildErr = cdccontext.BuildFromRetailIndex(project, terms, index, options.selectBy)
	}
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
		encoder.SetEscapeHTML(false)
		var document any = buildContextReviewDocument(result, options.gameDir)
		if options.verbose {
			document = result
		}
		if err := encoder.Encode(document); err != nil {
			fmt.Fprintf(stderr, "zill: context: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	if options.verbose {
		writeContextText(stdout, result)
	} else {
		writeContextReviewText(stdout, result, options.gameDir)
	}
	return 0
}

func parseContextOptions(args []string) (contextOptions, error) {
	options := contextOptions{format: "text", selectBy: cdccontext.Selector{Bank: -1, Record: -1}}
	gameDirSet, listScenesSet, bankSet, recordSet, sceneSet, formatSet, verboseSet := false, false, false, false, false, false, false
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
		case "--list-scenes":
			if hasEquals {
				return contextOptions{}, fmt.Errorf("--list-scenes does not take a value")
			}
			if listScenesSet {
				return contextOptions{}, fmt.Errorf("--list-scenes may be specified only once")
			}
			listScenesSet = true
			options.selectBy.ListScenes = true
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
		case "--scene":
			if sceneSet {
				return contextOptions{}, fmt.Errorf("--scene may be specified only once")
			}
			sceneSet = true
			options.selectBy.Scene, err = nextValue()
		case "--format":
			if formatSet {
				return contextOptions{}, fmt.Errorf("--format may be specified only once")
			}
			formatSet = true
			options.format, err = nextValue()
			if err == nil && options.format != "text" && options.format != "json" {
				err = fmt.Errorf("unsupported format %q", options.format)
			}
		case "--verbose":
			if hasEquals {
				return contextOptions{}, fmt.Errorf("--verbose does not take a value")
			}
			if verboseSet {
				return contextOptions{}, fmt.Errorf("--verbose may be specified only once")
			}
			verboseSet = true
			options.verbose = true
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
	selectorCount := 0
	for _, set := range []bool{listScenesSet, bankSet, recordSet, sceneSet} {
		if set {
			selectorCount++
		}
	}
	if selectorCount != 1 {
		return contextOptions{}, fmt.Errorf("set exactly one of --list-scenes, --bank, --record, or --scene")
	}
	return options, nil
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
	if result.Selector.ListScenes {
		query = "all recovered scenes"
	} else if result.Selector.Scene != "" {
		query = fmt.Sprintf("scene %s", result.Selector.Scene)
	} else if result.Selector.Record >= 0 {
		query = fmt.Sprintf("record %d", result.Selector.Record)
	}
	fmt.Fprintf(output, "Query: %s\nScenes: %d\n", query, len(result.Scenes))
	if len(result.RoomMessageBankRegistrations) > 0 {
		fmt.Fprintf(output, "Room-local bank registrations: %d (availability only; runtime room remains dependent)\n", len(result.RoomMessageBankRegistrations))
		for _, registration := range result.RoomMessageBankRegistrations {
			fmt.Fprintf(output, "  %s -> %s room=%s@%s#%d bank_member=%s@%s#%d status=%s runtime=%s\n", registration.RoomLogicalKey, registration.BankLogicalKey, registration.RoomMember, registration.RoomSourceArchive, registration.RoomArchiveIndex, registration.BankMember, registration.BankSourceArchive, registration.BankArchiveIndex, registration.Status, registration.RuntimeStatus)
		}
	}
	writeScenarioFamilies(output, result.ScenarioFamilies)
	for _, scene := range result.Scenes {
		fmt.Fprintf(output, "\nScene: %s\n", scene.Member)
		if scene.ID != "" {
			fmt.Fprintf(output, "  Scene ID: %s\n", scene.ID)
		}
		if scene.EmbeddedMember != "" {
			fmt.Fprintf(output, "  Embedded resource: %s\n", scene.EmbeddedMember)
		}
		fmt.Fprintf(output, "  Archive: %s\n", scene.SourceArchive)
		fmt.Fprintf(output, "  Source: %s\n", scene.SourceKind)
		fmt.Fprintf(output, "  Ordering: %s\n", scene.Ordering)
		fmt.Fprintf(output, "  Evidence: %s\n", scene.EvidenceStatus)
		if scene.Scenario != nil {
			fmt.Fprintf(output, "  Scenario family: slot=%d content_sha256=%s equivalent_groups=%s\n", scene.Scenario.Slot, scene.Scenario.ContentSHA256, strings.Join(scene.Scenario.EquivalentGroups, ","))
		}
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
				if reference.Scenario != nil {
					fmt.Fprintf(output, "      Scenario family: slot=%d status=%s\n", reference.Scenario.Slot, reference.Scenario.Status)
				}
				if table := reference.ScenarioRoomTable; table != nil {
					fmt.Fprintf(output, "      Room scenario table: selector=%d table_index=%d status=%s possible_slots=%d authored_targets=%d rooms=%d\n", table.SelectorValue, table.TableIndex, table.Status, len(table.PossibleSlots), table.TargetCount, table.RoomCount)
				}
				if reference.ResourceAuthoringName != "" {
					fmt.Fprintf(output, "      Authoring resource name: %s\n", reference.ResourceAuthoringName)
				}
			}
		}
	}
}

func writeScenarioFamilies(output io.Writer, families []cdccontext.ScenarioFamily) {
	const provenanceExampleLimit = 12
	if len(families) == 0 {
		return
	}
	roomOnly := 0
	for _, family := range families {
		if len(family.Relevance) == 1 && family.Relevance[0] == "room_table_possible" {
			roomOnly++
		}
	}
	fmt.Fprintf(output, "Scenario families: %d", len(families))
	if roomOnly > 0 {
		fmt.Fprintf(output, " (%d reachable only through current-room tables; full metadata retained in JSON)", roomOnly)
	}
	fmt.Fprintln(output)
	for _, family := range families {
		if len(family.Relevance) == 1 && family.Relevance[0] == "room_table_possible" {
			continue
		}
		fmt.Fprintf(output, "  Slot %d: status=%s relevance=%s variants=%d incoming_edges=%d roots=%d room_targets=%d basis=%s\n", family.Slot, family.Status, strings.Join(family.Relevance, ","), len(family.Variants), len(family.Incoming), len(family.Roots), len(family.RoomTargets), family.Basis)
		for _, root := range family.Roots {
			fmt.Fprintf(output, "    Root: kind=%s status=%s confidence=%s source=%s\n", root.Kind, root.Status, root.Confidence, root.SourceLocator)
		}
		for index, edge := range family.Incoming {
			if index == provenanceExampleLimit {
				fmt.Fprintf(output, "    Incoming edges omitted from text: %d (JSON retains all)\n", len(family.Incoming)-index)
				break
			}
			sourceFamily := ""
			if edge.SourceScenarioSlot != nil {
				sourceFamily = fmt.Sprintf(" source_scenario_slot=%d", *edge.SourceScenarioSlot)
			}
			fmt.Fprintf(output, "    Incoming: source=%s@%s%s opcode=%s offset=%d path=%s guard=%s execution=%s\n", edge.SourceMember, edge.SourceArchive, sourceFamily, edge.Opcode, edge.Offset, contextPath(edge.Path), edge.Guard, edge.ExecutionStatus)
		}
		for index, target := range family.RoomTargets {
			if index == provenanceExampleLimit {
				fmt.Fprintf(output, "    Room targets omitted from text: %d (JSON retains all)\n", len(family.RoomTargets)-index)
				break
			}
			fmt.Fprintf(output, "    Room target: room=%s@%s#%d resource=%s selector=%d status=%s\n", target.RoomMember, target.SourceArchive, target.RoomArchiveIndex, target.EmbeddedMember, target.SelectorIndex, target.Status)
		}
		for index, variant := range family.Variants {
			groups := make([]string, len(variant.Members))
			for memberIndex, member := range variant.Members {
				groups[memberIndex] = member.Group
			}
			fmt.Fprintf(output, "    Variant %d: canonical=%s content_sha256=%s groups=%s\n", index, variant.CanonicalMember, variant.ContentSHA256, strings.Join(groups, ","))
			for _, member := range variant.Members {
				fmt.Fprintf(output, "      %s -> %s authoring_name=%s physical=%s archive=%s index=%d\n", member.Group, member.LogicalKey, member.AuthoringName, member.Member, member.SourceArchive, member.ArchiveIndex)
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
