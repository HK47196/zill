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

const contextUsage = "zill context --game-dir PATH (--bank NNN | --record ID) [--format text|json]"

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
	pair, err := paa.Open(filepath.Join(usrdir, "pa.bin"), filepath.Join(usrdir, "pa.arc"))
	if err != nil {
		fmt.Fprintf(stderr, "zill: context: %v\n", err)
		return 1
	}
	result, buildErr := cdccontext.Build(project, terms, pair, options.selectBy)
	closeErr := pair.Close()
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
			if err == nil && options.format != "text" && options.format != "json" {
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
	if result.Selector.Record >= 0 {
		query = fmt.Sprintf("record %d", result.Selector.Record)
	}
	fmt.Fprintf(output, "Query: %s\nScenes: %d\n", query, len(result.Scenes))
	for _, scene := range result.Scenes {
		fmt.Fprintf(output, "\nScene: %s\n", scene.Member)
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
			fmt.Fprintf(output, "    Marker: event=%d label=%s records=%v\n", evidence.EventNumber, evidence.MarkerLabel, evidence.MarkerMessageIDs)
			for _, candidate := range evidence.Candidates {
				fmt.Fprintf(output, "    Authoring candidate (%d): %s", candidate.MessageID, candidate.Japanese)
				if candidate.English != "" {
					fmt.Fprintf(output, " / %s", candidate.English)
				}
				fmt.Fprintf(output, " label_match=%t", candidate.LabelMatch)
				fmt.Fprintln(output)
			}
			fmt.Fprintf(output, "    Basis: %s\n", evidence.Basis)
		}
		if scene.SourceKind == "message_bank" {
			fmt.Fprintln(output, "  Limitations: record-local controls and source-authoring candidates do not establish scene chronology, speakers, actor presence, or runtime reachability.")
		}
		writeContextEntries(output, scene.Entries)
		if len(scene.References) > 0 {
			fmt.Fprintln(output, "  Static references (execution remains conditional):")
			for _, reference := range scene.References {
				fmt.Fprintf(output, "    %s @%d path=%s raw=%s\n", reference.Opcode, reference.Offset, contextPath(reference.Path), reference.Raw)
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
			fmt.Fprintf(output, "    Association: handle=%d mode=%d resolution=%s", *entry.EntityAssociationHandleRaw, *entry.DisplayMode, entry.AssociationResolution)
			if entry.AssociationNameRecordID != nil {
				fmt.Fprintf(output, " name_record=%d", *entry.AssociationNameRecordID)
			}
			if entry.AssociatedLabelMessageID != nil {
				fmt.Fprintf(output, " label_message=%d", *entry.AssociatedLabelMessageID)
			}
			fmt.Fprintln(output)
			if entry.AssociatedLabelJapanese != "" || entry.AssociatedLabelEnglish != "" {
				fmt.Fprintf(output, "    Associated label: %s / %s\n", entry.AssociatedLabelJapanese, entry.AssociatedLabelEnglish)
			}
			fmt.Fprintf(output, "    Speaker status: %s", entry.SpeakerStatus)
			if entry.SpeakerEnglish != "" || entry.SpeakerJapanese != "" {
				fmt.Fprintf(output, " (%s / %s)", entry.SpeakerJapanese, entry.SpeakerEnglish)
			}
			fmt.Fprintln(output)
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
		}
		fmt.Fprintf(output, "    Japanese: %s\n", entry.Japanese)
		fmt.Fprintf(output, "    English: %s\n", entry.English)
		for _, term := range entry.Terminology {
			fmt.Fprintf(output, "    Authority: %s: %s → %s\n", term.Kind, term.Japanese, term.English)
		}
	}
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
