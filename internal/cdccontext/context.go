// SPDX-License-Identifier: GPL-3.0-or-later

// Package cdccontext derives static, branch-aware translation context from CDC.
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

// Result is the complete static context for the selected CDC scenes.
type Result struct {
	Selector     Selector `json:"selector"`
	Scenes       []Scene  `json:"scenes"`
	Unreferenced bool     `json:"unreferenced"`
}

// Scene is one CDC member, with every supported message consumer it contains.
type Scene struct {
	Member     string      `json:"member"`
	Entries    []Entry     `json:"entries"`
	References []Reference `json:"references"`
}

// Entry is a path-local supported message occurrence.
type Entry struct {
	Kind                       string             `json:"kind"`
	MessageID                  int                `json:"message_id"`
	Offset                     int                `json:"offset"`
	Position                   int                `json:"position"`
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

// Actor is the path-local abstract lifecycle state for one observed handle.
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

type state map[int]string

const unresolvedControlFlow = -1

func (s state) clone() state {
	r := make(state, len(s))
	for k, v := range s {
		r[k] = v
	}
	return r
}
func merge(a, b state) state {
	r := make(state)
	keys := map[int]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	for k := range keys {
		if a[k] == b[k] {
			r[k] = a[k]
		} else {
			r[k] = "unknown"
		}
	}
	return r
}

func setPresence(actors state, handle int, presence string) {
	if _, unresolved := actors[unresolvedControlFlow]; unresolved {
		presence = "unknown"
	}
	actors[handle] = presence
}

func markControlFlowUnresolved(actors state) {
	actors[unresolvedControlFlow] = "unknown"
	for handle := range actors {
		if handle != unresolvedControlFlow {
			actors[handle] = "unknown"
		}
	}
}

// Build scans the supplied PAA archive and returns full scenes matching selector.
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
			result.Scenes = append(result.Scenes, scene)
		}
	}
	result.Unreferenced = len(result.Scenes) == 0
	return result, nil
}

func buildScene(project *corpus.Project, terms fixeddata.Terminology, member string, p cdc.Program, bindata []byte) (Scene, error) {
	s := Scene{Member: member, Entries: make([]Entry, 0), References: make([]Reference, 0)}
	pos := 0
	_, err := walk(project, terms, bindata, &s, p.Elements, state{}, nil, "", 0, &pos)
	if err != nil {
		return Scene{}, fmt.Errorf("cdc context: %s: %w", member, err)
	}
	return s, nil
}

func walk(project *corpus.Project, terms fixeddata.Terminology, data []byte, scene *Scene, es []cdc.Element, actors state, path []int, guard string, depth int, pos *int) (state, error) {
	previous := ""
	for i, e := range es {
		switch e.Kind {
		case cdc.CommandElement:
			c := e.Command
			previous = rawCommand(c)
			switch c.Name {
			case "C2", "C6":
				h, ok := firstInt(c)
				if !ok {
					return nil, fmt.Errorf("%s@%d: malformed %s", c.Name, e.Offset, c.Name)
				}
				setPresence(actors, h, "present")
			case "C3":
				h, ok := firstInt(c)
				if !ok {
					return nil, fmt.Errorf("C3@%d: malformed C3", e.Offset)
				}
				setPresence(actors, h, "absent")
			case "C69", "C70", "C71":
				markControlFlowUnresolved(actors)
			case "C5", "C20", "C22", "C23":
				entries, err := consumer(project, terms, data, c, e.Offset, path, guard, depth, *pos, actors)
				if err != nil {
					return nil, err
				}
				*pos += len(entries)
				scene.Entries = append(scene.Entries, entries...)
			case "C12", "C13", "C14", "C76":
				scene.References = append(scene.References, reference(c, e.Offset, path, guard))
			}
		case cdc.BlockElement:
			childPath := append(append([]int(nil), path...), i)
			childGuard := guard
			if childGuard != "" {
				childGuard += " > "
			}
			if previous != "" {
				childGuard += previous
			} else {
				childGuard += e.Raw
			}
			out, err := walk(project, terms, data, scene, e.Block.Elements, actors.clone(), childPath, childGuard, depth+1, pos)
			if err != nil {
				return nil, err
			}
			actors = merge(actors, out)
		case cdc.LabelElement:
			previous = e.Raw
		case cdc.ReturnElement:
			markControlFlowUnresolved(actors)
		}
	}
	return actors, nil
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

func consumer(project *corpus.Project, terms fixeddata.Terminology, data []byte, c cdc.Command, offset int, path []int, guard string, depth, pos int, actors state) ([]Entry, error) {
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
		e := Entry{Kind: kind, MessageID: id, Offset: offset, Position: pos + i, Path: append([]int{}, path...), Guard: guard, Depth: depth, Raw: c.Raw, Japanese: item.Translation.Japanese, English: item.Translation.Text, State: item.Translation.State, Terminology: applicableTerms(terms.Applicable(item)), Actors: actorList(project, data, actors)}
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
func actorList(project *corpus.Project, data []byte, s state) []Actor {
	r := make([]Actor, 0, len(s))
	basis := "structural_lifecycle"
	if _, unresolved := s[unresolvedControlFlow]; unresolved {
		basis = "unresolved_control_flow"
	}
	for h, p := range s {
		if h == unresolvedControlFlow {
			continue
		}
		a := resolve(project, data, h)
		r = append(r, Actor{
			Handle:                     h,
			Presence:                   p,
			PresenceBasis:              basis,
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
