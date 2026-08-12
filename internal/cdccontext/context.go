// SPDX-License-Identifier: GPL-3.0-or-later

// Package cdccontext derives static translation context from CDC programs and
// message-bank storage.
package cdccontext

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/fixeddata"
	"github.com/HK47196/zill/internal/gamefmt/cdc"
	"github.com/HK47196/zill/internal/gamefmt/paa"
)

// Selector selects scenes by exactly one message bank or record.
type Selector struct {
	// Bank and Record use a negative value for "not selected". Exactly one must
	// be non-negative; this makes bank 000 and record 0 representable.
	Bank   int `json:"bank"`
	Record int `json:"record"`
}

// Result is the complete static context for the selected scenes.
type Result struct {
	Selector Selector `json:"selector"`
	Scenes   []Scene  `json:"scenes"`
}

// Scene is one complete static context unit. CDC programs provide control-flow
// scenes; a message bank provides the lossless storage-order unit when no
// static consumer references the query.
type Scene struct {
	Member               string      `json:"member"`
	SourceKind           string      `json:"source_kind"`
	Ordering             string      `json:"ordering"`
	EvidenceStatus       string      `json:"evidence_status"`
	FirstRecordMessageID *int        `json:"first_record_message_id,omitempty"`
	FirstRecordJapanese  string      `json:"first_record_japanese,omitempty"`
	FirstRecordEnglish   string      `json:"first_record_english,omitempty"`
	Entries              []Entry     `json:"entries"`
	References           []Reference `json:"references"`
}

// Entry is one authored message occurrence with its joined static flow state.
type Entry struct {
	Kind                       string             `json:"kind"`
	MessageID                  int                `json:"message_id"`
	Offset                     int                `json:"offset"`
	OffsetBasis                string             `json:"offset_basis"`
	Position                   int                `json:"position"`
	Selected                   bool               `json:"selected"`
	Reachability               string             `json:"reachability"`
	Depth                      int                `json:"depth"`
	Path                       []int              `json:"path"`
	Guard                      string             `json:"guard"`
	Raw                        string             `json:"raw"`
	Japanese                   string             `json:"japanese"`
	English                    string             `json:"english"`
	State                      corpus.State       `json:"state"`
	Terminology                []TerminologyEntry `json:"terminology"`
	DisplayMode                *int               `json:"display_mode,omitempty"`
	EntityAssociationHandleRaw *int               `json:"entity_association_handle_raw,omitempty"`
	AssociationNameRecordID    *int               `json:"association_name_record_id,omitempty"`
	AssociatedLabelMessageID   *int               `json:"associated_label_message_id,omitempty"`
	AssociatedLabelJapanese    string             `json:"associated_label_japanese,omitempty"`
	AssociatedLabelEnglish     string             `json:"associated_label_english,omitempty"`
	AssociationResolution      string             `json:"association_resolution,omitempty"`
	SpeakerStatus              string             `json:"speaker_status,omitempty"`
	SpeakerJapanese            string             `json:"speaker_japanese,omitempty"`
	SpeakerEnglish             string             `json:"speaker_english,omitempty"`
	SpeakerSource              string             `json:"speaker_source,omitempty"`
	Actors                     []Actor            `json:"actors"`
}

// TerminologyEntry is one applicable authority in a stable JSON shape.
type TerminologyEntry struct {
	Kind      string `json:"kind"`
	Key       string `json:"key"`
	Japanese  string `json:"japanese"`
	English   string `json:"english"`
	Scope     string `json:"scope"`
	SourceIDs []int  `json:"source_ids,omitempty"`
}

// Actor is the abstract lifecycle state for one observed handle.
type Actor struct {
	Handle                     int    `json:"handle"`
	Presence                   string `json:"presence"`
	PresenceBasis              string `json:"presence_basis"`
	AssociationNameRecordID    *int   `json:"association_name_record_id,omitempty"`
	AssociatedLabelMessageID   *int   `json:"associated_label_message_id,omitempty"`
	AssociatedLabelJapanese    string `json:"associated_label_japanese,omitempty"`
	AssociatedLabelEnglish     string `json:"associated_label_english,omitempty"`
	AssociationLabelResolution string `json:"association_label_resolution"`
}

// Reference is a raw static cross-program reference. Resolution is deliberately
// left to callers because C12/C13/C14 are runtime-state dependent.
type Reference struct {
	Opcode       string                 `json:"opcode"`
	Offset       int                    `json:"offset"`
	Path         []int                  `json:"path"`
	Guard        string                 `json:"guard"`
	Raw          string                 `json:"raw"`
	Arguments    []string               `json:"arguments"`
	ScenarioSlot *int                   `json:"scenario_slot,omitempty"`
	Resource     *cdc.ResourceReference `json:"resource,omitempty"`
}

// Build scans the supplied PAA archive and returns complete static context units
// matching selector.
func Build(project *corpus.Project, terms fixeddata.Terminology, pair *paa.Pair, selector Selector) (Result, error) {
	if project == nil || pair == nil {
		return Result{}, fmt.Errorf("cdc context: project and pair are required")
	}
	if (selector.Bank >= 0) == (selector.Record >= 0) {
		return Result{}, fmt.Errorf("cdc context: set exactly one of bank or record")
	}
	if selector.Bank >= 279 {
		return Result{}, fmt.Errorf("cdc context: invalid bank %d", selector.Bank)
	}
	if selector.Record >= 0 && selector.Record/10000 >= 279 {
		return Result{}, fmt.Errorf("cdc context: invalid record %d", selector.Record)
	}
	bank := selector.Bank
	if selector.Record >= 0 {
		bank = selector.Record / 10_000
	}
	bankMemberName := fmt.Sprintf("message/msgsec%03d.dat", bank)
	bankMemberIndex := -1
	var bindata []byte
	var members []paa.Member
	for _, m := range pair.Members() {
		if m.Name == "data/bindata.dat" {
			if bindata != nil {
				return Result{}, fmt.Errorf("cdc context: duplicate data/bindata.dat")
			}
			b, e := pair.Payload(m.Index)
			if e != nil {
				return Result{}, e
			}
			bindata = b
		}
		if len(m.Name) >= 8 && m.Name[:4] == "cdc/" && len(m.Name) >= 4 && m.Name[len(m.Name)-4:] == ".cdc" {
			members = append(members, m)
		}
		if m.Name == bankMemberName {
			if bankMemberIndex >= 0 {
				return Result{}, fmt.Errorf("cdc context: duplicate %s", bankMemberName)
			}
			bankMemberIndex = m.Index
		}
	}
	if bindata == nil {
		return Result{}, fmt.Errorf("cdc context: missing data/bindata.dat")
	}
	if len(members) == 0 {
		return Result{}, fmt.Errorf("cdc context: archive contains no cdc/*.cdc members")
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
	result := Result{Selector: selector, Scenes: make([]Scene, 0)}
	for _, m := range members {
		payload, err := pair.Payload(m.Index)
		if err != nil {
			return Result{}, err
		}
		program, err := cdc.Parse(m.Name, payload)
		if errors.Is(err, cdc.ErrPlaceholder) {
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("cdc context: %s: %w", m.Name, err)
		}
		scene, err := buildScene(project, terms, m.Name, program, bindata)
		if err != nil {
			return Result{}, err
		}
		selected := false
		for _, e := range scene.Entries {
			if selector.Record >= 0 && e.MessageID == selector.Record {
				selected = true
			}
			if selector.Bank >= 0 && e.MessageID/10000 == selector.Bank {
				selected = true
			}
		}
		if selected {
			markSelectedRecord(&scene, selector)
			result.Scenes = append(result.Scenes, scene)
		}
	}
	if len(result.Scenes) == 0 {
		if bankMemberIndex < 0 {
			return Result{}, fmt.Errorf("cdc context: archive is missing %s", bankMemberName)
		}
		payload, err := pair.Payload(bankMemberIndex)
		if err != nil {
			return Result{}, err
		}
		retailBank, err := corpus.ParseBank(fmt.Sprintf("msgsec%03d.dat", bank), payload)
		if err != nil {
			return Result{}, fmt.Errorf("cdc context: %s: %w", bankMemberName, err)
		}
		scene, err := buildBankScene(project, terms, bankMemberName, retailBank)
		if err != nil {
			return Result{}, err
		}
		markSelectedRecord(&scene, selector)
		result.Scenes = append(result.Scenes, scene)
	}
	return result, nil
}

func buildBankScene(project *corpus.Project, terms fixeddata.Terminology, member string, bank corpus.Bank) (Scene, error) {
	scene := Scene{
		Member:         member,
		SourceKind:     "message_bank",
		Ordering:       "storage_order_only",
		EvidenceStatus: "no_resolved_static_consumer_reference",
		Entries:        make([]Entry, 0, len(bank.Records)),
		References:     make([]Reference, 0),
	}
	expected := 0
	for _, item := range project.Items {
		if item.Translation.ID/10_000 == bank.Section {
			expected++
		}
	}
	if len(bank.Records) != expected {
		return Scene{}, fmt.Errorf("cdc context: %s has %d retail records for %d contributor records", member, len(bank.Records), expected)
	}
	for _, record := range bank.Records {
		item, ok := project.Find(record.ID)
		if !ok {
			return Scene{}, fmt.Errorf("cdc context: %s contains unknown record %d", member, record.ID)
		}
		scene.Entries = append(scene.Entries, Entry{
			Kind:         "bank_record",
			MessageID:    record.ID,
			Offset:       record.Offset,
			OffsetBasis:  "message_bank_byte_offset",
			Position:     record.Index,
			Reachability: "unresolved",
			Path:         make([]int, 0),
			Japanese:     item.Translation.Japanese,
			English:      item.Translation.Text,
			State:        item.Translation.State,
			Terminology:  applicableTerms(terms.Applicable(item)),
			Actors:       make([]Actor, 0),
		})
	}
	if len(scene.Entries) > 0 {
		label := scene.Entries[0]
		scene.FirstRecordMessageID = &label.MessageID
		scene.FirstRecordJapanese = label.Japanese
		scene.FirstRecordEnglish = label.English
	}
	return scene, nil
}

func markSelectedRecord(scene *Scene, selector Selector) {
	if selector.Record < 0 {
		return
	}
	for index := range scene.Entries {
		scene.Entries[index].Selected = scene.Entries[index].MessageID == selector.Record
	}
}

func buildScene(project *corpus.Project, terms fixeddata.Terminology, member string, p cdc.Program, bindata []byte) (Scene, error) {
	s := Scene{
		Member:         member,
		SourceKind:     "cdc_program",
		Ordering:       "source_order_with_static_control_flow",
		EvidenceStatus: "static_consumer_reference",
		Entries:        make([]Entry, 0),
		References:     make([]Reference, 0),
	}
	graph, err := compileFlow(p)
	if err != nil {
		return Scene{}, fmt.Errorf("cdc context: %s: %w", member, err)
	}
	analysis := analyzeFlow(graph)
	pos := 0
	for _, nodeIndex := range sourceOrderedNodes(graph) {
		node := graph.nodes[nodeIndex]
		if node.kind != flowCommand {
			continue
		}
		flow := analysis.byNode[nodeIndex]
		switch node.command.Name {
		case "C5", "C20", "C22", "C23":
			entries, err := consumer(project, terms, bindata, node.command, node.offset, node.path, node.guard, node.depth, pos, flow)
			if err != nil {
				return Scene{}, fmt.Errorf("cdc context: %s: %w", member, err)
			}
			pos += len(entries)
			s.Entries = append(s.Entries, entries...)
		case "C12", "C13", "C14", "C76":
			s.References = append(s.References, reference(node.command, node.offset, node.path, node.guard))
		}
	}
	return s, nil
}

func rawCommand(c cdc.Command) string {
	if c.Raw != "" {
		return c.Raw
	}
	return c.Name + ":" + strings.Join(c.Arguments, "+")
}

func firstInt(c cdc.Command) (int, bool) {
	if len(c.Arguments) < 1 {
		return 0, false
	}
	n, e := strconv.Atoi(c.Arguments[0])
	return n, e == nil
}
func ints(c cdc.Command, wantMin, wantMax int) ([]int, error) {
	if len(c.Arguments) < wantMin || len(c.Arguments) > wantMax {
		return nil, fmt.Errorf("%s@%d: malformed %s", c.Name, c.Offset, c.Name)
	}
	r := make([]int, len(c.Arguments))
	for i, a := range c.Arguments {
		n, e := strconv.Atoi(a)
		if e != nil {
			return nil, fmt.Errorf("%s@%d: malformed %s", c.Name, c.Offset, c.Name)
		}
		r[i] = n
	}
	return r, nil
}

func consumer(project *corpus.Project, terms fixeddata.Terminology, data []byte, c cdc.Command, offset int, path []int, guard string, depth, pos int, flow abstractFlow) ([]Entry, error) {
	var ids []int
	kind := ""
	mode, handle := 0, 0
	switch c.Name {
	case "C5":
		if !c.Semicolon {
			return nil, fmt.Errorf("C5@%d: malformed C5", offset)
		}
		a, e := ints(c, 3, 7)
		if e != nil {
			return nil, e
		}
		mode, handle = a[0], a[1]
		ids = a[2:]
		kind = "dialogue_association"
	case "C20":
		if c.Semicolon {
			return nil, fmt.Errorf("C20@%d: malformed C20", offset)
		}
		a, e := ints(c, 2, 3)
		if e != nil {
			return nil, e
		}
		if a[1] < 1 || a[1] > 37 {
			return nil, fmt.Errorf("C20@%d: malformed C20", offset)
		}
		kind = "selection_option"
		for i := 0; i < a[1]; i++ {
			ids = append(ids, a[0]+i)
		}
	case "C22":
		if !c.Semicolon {
			return nil, fmt.Errorf("C22@%d: malformed C22", offset)
		}
		if len(c.Arguments) == 1 {
			n, e := strconv.Atoi(c.Arguments[0])
			if e != nil {
				return nil, fmt.Errorf("C22@%d: malformed C22", offset)
			}
			ids = []int{n}
			kind = "notification"
		} else if len(c.Arguments) == 2 && c.Arguments[0] == "T" {
			n, e := strconv.Atoi(c.Arguments[1])
			if e != nil {
				return nil, fmt.Errorf("C22@%d: malformed C22", offset)
			}
			ids = []int{n}
			kind = "cinematic_text"
		} else {
			return nil, fmt.Errorf("C22@%d: malformed C22", offset)
		}
	case "C23":
		if c.Semicolon || len(c.Arguments) != 2 || c.Arguments[1] != "Y" {
			return nil, fmt.Errorf("C23@%d: malformed C23", offset)
		}
		n, e := strconv.Atoi(c.Arguments[0])
		if e != nil {
			return nil, fmt.Errorf("C23@%d: malformed C23", offset)
		}
		ids = []int{n}
		kind = "confirmation_prompt"
	}
	r := make([]Entry, 0, len(ids))
	for i, id := range ids {
		item, ok := project.Find(id)
		if !ok {
			return nil, fmt.Errorf("%s@%d: message ID %d not in project", c.Name, offset, id)
		}
		e := Entry{Kind: kind, MessageID: id, Offset: offset, OffsetBasis: "cdc_program_byte_offset", Position: pos + i, Reachability: flow.reachability(), Path: append([]int{}, path...), Guard: guard, Depth: depth, Raw: c.Raw, Japanese: item.Translation.Japanese, English: item.Translation.Text, State: item.Translation.State, Terminology: applicableTerms(terms.Applicable(item)), Actors: actorList(project, data, flow.actors)}
		if kind == "dialogue_association" {
			e.DisplayMode = intPointer(mode)
			e.EntityAssociationHandleRaw = intPointer(handle)
			a := resolve(project, data, handle)
			e.AssociationNameRecordID = a.nameRecordID
			e.AssociatedLabelMessageID = a.labelMessageID
			e.AssociatedLabelJapanese = a.labelJapanese
			e.AssociatedLabelEnglish = a.labelEnglish
			e.AssociationResolution = a.resolution
			e.SpeakerStatus = a.speakerStatus
			e.SpeakerJapanese = a.labelJapanese
			e.SpeakerEnglish = a.labelEnglish
			if a.speakerStatus == "inferred_from_associated_label" {
				e.SpeakerSource = "c5_associated_label"
			}
		}
		r = append(r, e)
	}
	return r, nil
}

func intPointer(value int) *int {
	return &value
}

func applicableTerms(entries []fixeddata.SearchEntry) []TerminologyEntry {
	result := make([]TerminologyEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, TerminologyEntry{
			Kind:      entry.Kind,
			Key:       entry.Term.Key,
			Japanese:  entry.Term.Japanese,
			English:   entry.Term.English,
			Scope:     entry.Term.Scope,
			SourceIDs: append([]int(nil), entry.Term.SourceIDs...),
		})
	}
	return result
}
func actorList(project *corpus.Project, data []byte, actors actorState) []Actor {
	r := make([]Actor, 0, len(actors))
	for h, fact := range actors {
		a := resolve(project, data, h)
		r = append(r, Actor{
			Handle:                     h,
			Presence:                   fact.presence,
			PresenceBasis:              fact.basis,
			AssociationNameRecordID:    a.nameRecordID,
			AssociatedLabelMessageID:   a.labelMessageID,
			AssociatedLabelJapanese:    a.labelJapanese,
			AssociatedLabelEnglish:     a.labelEnglish,
			AssociationLabelResolution: a.resolution,
		})
	}
	sort.Slice(r, func(i, j int) bool { return r[i].Handle < r[j].Handle })
	return r
}

type association struct {
	nameRecordID   *int
	labelMessageID *int
	labelJapanese  string
	labelEnglish   string
	resolution     string
	speakerStatus  string
}

func unresolvedAssociation(resolution string) association {
	return association{resolution: resolution, speakerStatus: "unresolved"}
}

func resolve(project *corpus.Project, data []byte, h int) association {
	if h == 1 {
		result := unresolvedAssociation("runtime_player_name")
		result.nameRecordID = intPointer(1980)
		return result
	}
	if h == 9999 {
		return unresolvedAssociation("dynamic_context")
	}
	var id int
	switch {
	case h >= 2 && h <= 27:
		id = h + 139
	case h >= 28 && h <= 99:
		id = h + 144
	case h >= 100 && h <= 155:
		return unresolvedAssociation("runtime_dependent")
	case h >= 156 && h <= 169:
		id = h + 144
	case h == 170:
		id = 15
	case h >= 171 && h <= 189:
		id = h + 144
	case h >= 190 && h <= 194:
		id = h - 23
	case h >= 195 && h <= 199:
		id = h + 144
	case h >= 200 && h <= 299:
		o := 0x2800 + (h-200)*10
		if o+4 > len(data) {
			return unresolvedAssociation("unmapped_handle")
		}
		id = int(binary.LittleEndian.Uint16(data[o+2:]))
	case h >= 300 && h <= 369:
		id = h - 291
	case h >= 370 && h <= 399:
		id = h - 306
	case h >= 400 && h <= 449:
		o := 0x3000 + (h-400)*8
		if o+8 > len(data) {
			return unresolvedAssociation("unmapped_handle")
		}
		id = int(binary.LittleEndian.Uint16(data[o+6:]))
	case h >= 450 && h <= 499:
		id = h - 356
	case h >= 500 && h <= 651:
		id = h - 270
	case h >= 652 && h <= 1068:
		id = h - 618
	case h >= 1069 && h <= 1516:
		id = h + 463
	default:
		return unresolvedAssociation("unmapped_handle")
	}
	if id == 0 {
		result := unresolvedAssociation("name_record_zero")
		result.nameRecordID = intPointer(0)
		return result
	}
	var msg int
	switch {
	case id >= 1 && id <= 1531:
		msg = id - 1
	case id >= 1532 && id <= 1979:
		msg = 1670000 + id - 1532
	default:
		result := unresolvedAssociation("unmapped_handle")
		result.nameRecordID = intPointer(id)
		return result
	}
	result := unresolvedAssociation("unmapped_handle")
	result.nameRecordID = intPointer(id)
	result.labelMessageID = intPointer(msg)
	item, ok := project.Find(msg)
	if !ok {
		return result
	}
	result.labelJapanese = labelText(item.Translation.Japanese)
	result.labelEnglish = labelText(item.Translation.Text)
	if result.labelJapanese == "" {
		result.resolution = "blank_label"
		return result
	}
	result.resolution = "resolved_label_only"
	result.speakerStatus = "inferred_from_associated_label"
	return result
}

func labelText(value string) string {
	return strings.TrimSpace(strings.TrimSuffix(value, "<end>"))
}
func reference(c cdc.Command, offset int, path []int, guard string) Reference {
	r := Reference{Opcode: c.Name, Offset: offset, Path: append([]int{}, path...), Guard: guard, Raw: c.Raw, Arguments: append([]string{}, c.Arguments...)}
	if n, ok := c.ScenarioSlot(); ok {
		r.ScenarioSlot = &n
	}
	if x, ok := c.C76Resource(); ok {
		r.Resource = &x
	}
	return r
}
