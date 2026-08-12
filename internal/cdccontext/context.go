// SPDX-License-Identifier: GPL-3.0-or-later

// Package cdccontext derives static translation context from CDC programs and
// message-bank storage.
package cdccontext

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/fixeddata"
	"github.com/HK47196/zill/internal/gamefmt/cdc"
	"github.com/HK47196/zill/internal/gamefmt/paa"
	"github.com/HK47196/zill/internal/message"
)

// Selector selects scenes by exactly one message bank or record.
type Selector struct {
	// Bank and Record use a negative value for "not selected". Exactly one must
	// be non-negative; this makes bank 000 and record 0 representable.
	Bank   int `json:"bank"`
	Record int `json:"record"`
}

// Archive is one named retail PAA source available to context recovery.
type Archive struct {
	Name string
	Pair *paa.Pair
}

// Result is the complete static context for the selected scenes.
type Result struct {
	Selector                     Selector                      `json:"selector"`
	Scenes                       []Scene                       `json:"scenes"`
	ScenarioFamilies             []ScenarioFamily              `json:"scenario_families,omitempty"`
	RoomMessageBankRegistrations []RoomMessageBankRegistration `json:"room_message_bank_registrations,omitempty"`
	ReviewPackets                []ReviewPacket                `json:"review_packets,omitempty"`
}

// Scene is one complete static context unit. CDC programs provide control-flow
// scenes, room packages provide authored ambient-interaction groups, and a
// message bank provides the lossless storage-order unit.
type Scene struct {
	Member               string           `json:"member"`
	EmbeddedMember       string           `json:"embedded_member,omitempty"`
	SourceArchive        string           `json:"source_archive"`
	SourceKind           string           `json:"source_kind"`
	Ordering             string           `json:"ordering"`
	EvidenceStatus       string           `json:"evidence_status"`
	Scenario             *ScenarioScene   `json:"scenario,omitempty"`
	SourceEvidence       []SourceEvidence `json:"source_evidence,omitempty"`
	FirstRecordMessageID *int             `json:"first_record_message_id,omitempty"`
	FirstRecordJapanese  string           `json:"first_record_japanese,omitempty"`
	FirstRecordEnglish   string           `json:"first_record_english,omitempty"`
	Entries              []Entry          `json:"entries"`
	References           []Reference      `json:"references"`
}

// Entry is one authored message occurrence with its joined static flow state.
type Entry struct {
	Kind                       string               `json:"kind"`
	MessageID                  int                  `json:"message_id"`
	Offset                     int                  `json:"offset"`
	OffsetBasis                string               `json:"offset_basis"`
	Position                   int                  `json:"position"`
	Selected                   bool                 `json:"selected"`
	Reachability               string               `json:"reachability"`
	Depth                      int                  `json:"depth"`
	Path                       []int                `json:"path"`
	Guard                      string               `json:"guard"`
	Conditions                 []Condition          `json:"conditions,omitempty"`
	Raw                        string               `json:"raw"`
	Japanese                   string               `json:"japanese"`
	English                    string               `json:"english"`
	State                      corpus.State         `json:"state"`
	Terminology                []TerminologyEntry   `json:"terminology"`
	DisplayMode                *int                 `json:"display_mode,omitempty"`
	EntityAssociationHandleRaw *int                 `json:"entity_association_handle_raw,omitempty"`
	AssociationNameRecordID    *int                 `json:"association_name_record_id,omitempty"`
	AssociatedLabelMessageID   *int                 `json:"associated_label_message_id,omitempty"`
	AssociatedLabelJapanese    string               `json:"associated_label_japanese,omitempty"`
	AssociatedLabelEnglish     string               `json:"associated_label_english,omitempty"`
	AssociationResolution      string               `json:"association_resolution,omitempty"`
	PortraitRequested          *bool                `json:"portrait_requested,omitempty"`
	NameLabelRequested         *bool                `json:"name_label_requested,omitempty"`
	ForcedStateThree           *bool                `json:"forced_state_three,omitempty"`
	PortraitStatus             string               `json:"portrait_status,omitempty"`
	SpeakerStatus              string               `json:"speaker_status,omitempty"`
	SpeakerJapanese            string               `json:"speaker_japanese,omitempty"`
	SpeakerEnglish             string               `json:"speaker_english,omitempty"`
	SpeakerSource              string               `json:"speaker_source,omitempty"`
	SourceControls             []SourceControl      `json:"source_controls,omitempty"`
	Relationships              []Relationship       `json:"relationships,omitempty"`
	ConsumerEvidence           []ConsumerEvidence   `json:"consumer_evidence,omitempty"`
	AuthoringMetadata          *AuthoringMetadata   `json:"authoring_metadata,omitempty"`
	AmbientInteraction         *AmbientInteraction  `json:"ambient_interaction,omitempty"`
	PossibleAddressees         []AddresseeCandidate `json:"possible_addressees,omitempty"`
	Actors                     []Actor              `json:"actors"`
}

// AddresseeCandidate is ranked static evidence, never a confirmed addressee.
type AddresseeCandidate struct {
	Handle     int    `json:"handle"`
	Label      string `json:"label,omitempty"`
	Confidence string `json:"confidence"`
	Status     string `json:"status"`
	Evidence   string `json:"evidence"`
}

// Condition is verified control behavior for one enclosing CDC block. Raw
// selectors remain numeric when their gameplay meanings are unresolved.
type Condition struct {
	Opcode             string `json:"opcode"`
	Raw                string `json:"raw"`
	Kind               string `json:"kind"`
	Status             string `json:"status"`
	SelectorRaw        *int   `json:"selector_raw,omitempty"`
	Operand1Raw        *int   `json:"operand_1_raw,omitempty"`
	Operand2Raw        *int   `json:"operand_2_raw,omitempty"`
	EntitySelectorRaw  *int   `json:"entity_selector_raw,omitempty"`
	ComparisonValue    *int   `json:"comparison_value,omitempty"`
	ComparatorRaw      string `json:"comparator_raw,omitempty"`
	Comparator         string `json:"comparator,omitempty"`
	PolarityRaw        string `json:"polarity_raw,omitempty"`
	Polarity           string `json:"polarity,omitempty"`
	PredicateFamilyRaw string `json:"predicate_family_raw,omitempty"`
	PredicateIndexRaw  *int   `json:"predicate_index_raw,omitempty"`
	SelectedIndex      *int   `json:"selected_index,omitempty"`
	SlotRaw            *int   `json:"slot_raw,omitempty"`
	BaseMessageID      *int   `json:"base_message_id,omitempty"`
	OptionCount        *int   `json:"option_count,omitempty"`
	Basis              string `json:"basis"`
}

// TerminologyEntry is one applicable authority in a stable JSON shape.
type TerminologyEntry struct {
	Kind      string `json:"kind"`
	Japanese  string `json:"japanese"`
	English   string `json:"english"`
	Scope     string `json:"scope"`
	SourceIDs []int  `json:"source_ids,omitempty"`
}

// Actor is the abstract lifecycle state for one observed handle.
type Actor struct {
	Handle                     int            `json:"handle"`
	Presence                   string         `json:"presence"`
	PresenceBasis              string         `json:"presence_basis"`
	AssociationNameRecordID    *int           `json:"association_name_record_id,omitempty"`
	AssociatedLabelMessageID   *int           `json:"associated_label_message_id,omitempty"`
	AssociatedLabelJapanese    string         `json:"associated_label_japanese,omitempty"`
	AssociatedLabelEnglish     string         `json:"associated_label_english,omitempty"`
	AssociationLabelResolution string         `json:"association_label_resolution"`
	Position                   *ActorPosition `json:"position,omitempty"`
	Action                     *ActorAction   `json:"action,omitempty"`
	Relation                   *ActorRelation `json:"relation,omitempty"`
}

// ActorPosition is the last path-stable coordinate-like pair supplied by C2
// or C6. Axis meanings remain unresolved.
type ActorPosition struct {
	Component2 int    `json:"component_2"`
	Component3 int    `json:"component_3"`
	Source     string `json:"source"`
	Status     string `json:"status"`
}

// ActorAction is the last path-stable per-entity action supplied by C7/C17.
type ActorAction struct {
	ActionIDRaw               int    `json:"action_id_raw"`
	ModifierRaw               *int   `json:"modifier_raw,omitempty"`
	OptionO                   bool   `json:"option_o"`
	C5AssociationBehaviorFlag string `json:"c5_association_behavior_flag"`
	Source                    string `json:"source"`
	Status                    string `json:"status"`
}

// ActorRelation is the last path-stable opaque C18 mode/value.
type ActorRelation struct {
	ModeOrValueRaw int    `json:"mode_or_value_raw"`
	Source         string `json:"source"`
	Status         string `json:"status"`
}

// Reference is a raw static cross-program reference. Resolution is deliberately
// left to callers because C12/C13/C14 are runtime-state dependent.
type Reference struct {
	Opcode                string                 `json:"opcode"`
	Offset                int                    `json:"offset"`
	Path                  []int                  `json:"path"`
	Guard                 string                 `json:"guard"`
	Raw                   string                 `json:"raw"`
	Arguments             []string               `json:"arguments"`
	ExecutionStatus       string                 `json:"execution_status"`
	ResolutionStatus      string                 `json:"resolution_status"`
	Scenario              *ScenarioReference     `json:"scenario,omitempty"`
	ScenarioRoomTable     *ScenarioRoomTable     `json:"scenario_room_table,omitempty"`
	Resource              *cdc.ResourceReference `json:"resource,omitempty"`
	ResourceAuthoringName string                 `json:"resource_authoring_name,omitempty"`
}

// ScenarioReference is one logical scenario-family edge. Runtime state chooses
// a physical group and therefore one content variant within the family.
type ScenarioReference struct {
	Slot   int    `json:"slot"`
	Status string `json:"status"`
}

// ScenarioRoomTable summarizes the possible logical slots supplied by one
// current-room C14 table selector. The current room remains runtime-dependent.
type ScenarioRoomTable struct {
	TableIndex    int    `json:"table_index"`
	SelectorValue int    `json:"selector_value"`
	PossibleSlots []int  `json:"possible_slots"`
	TargetCount   int    `json:"target_count"`
	RoomCount     int    `json:"room_count"`
	Status        string `json:"status"`
}

// ScenarioScene identifies the family and exact-byte content variant rendered
// by one canonical CDC Scene.
type ScenarioScene struct {
	Slot                  int      `json:"slot"`
	ContentSHA256         string   `json:"content_sha256"`
	EquivalentGroups      []string `json:"equivalent_groups"`
	EquivalentMemberCount int      `json:"equivalent_member_count"`
}

// ScenarioFamily is the complete static metadata for one logical slot.
type ScenarioFamily struct {
	Slot        int                      `json:"slot"`
	Status      string                   `json:"status"`
	Basis       string                   `json:"basis"`
	Relevance   []string                 `json:"relevance"`
	Variants    []ScenarioContentVariant `json:"variants"`
	Incoming    []ScenarioIncomingEdge   `json:"incoming,omitempty"`
	Roots       []ScenarioRoot           `json:"roots,omitempty"`
	RoomTargets []ScenarioRoomTarget     `json:"room_targets,omitempty"`
}

// ScenarioContentVariant groups physical members whose stored CDC bytes are
// exactly equal.
type ScenarioContentVariant struct {
	ContentSHA256   string                   `json:"content_sha256"`
	CanonicalMember string                   `json:"canonical_member"`
	Members         []ScenarioPhysicalMember `json:"members"`
}

// ScenarioPhysicalMember joins a logical RBB resource to its physical PAA
// member. AuthoringName is source metadata, not a player-facing scene title.
type ScenarioPhysicalMember struct {
	Group         string `json:"group"`
	LogicalKey    string `json:"logical_key"`
	AuthoringName string `json:"authoring_name"`
	SourceArchive string `json:"source_archive"`
	Member        string `json:"member"`
	ArchiveIndex  int    `json:"archive_index"`
}

// ScenarioIncomingEdge is a compact source locator for one static CDC edge.
type ScenarioIncomingEdge struct {
	SourceMember       string `json:"source_member"`
	SourceArchive      string `json:"source_archive"`
	SourceScenarioSlot *int   `json:"source_scenario_slot,omitempty"`
	Path               []int  `json:"path"`
	Guard              string `json:"guard"`
	Opcode             string `json:"opcode"`
	Offset             int    `json:"offset"`
	ExecutionStatus    string `json:"execution_status"`
}

// ScenarioRoot is independently verified executable entry evidence.
type ScenarioRoot struct {
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Confidence    string `json:"confidence"`
	SourceLocator string `json:"source_locator"`
}

// ScenarioRoomTarget is one current-room IMD selector that can supply a slot
// to C14. It is authored room metadata, not proof that the room is active.
type ScenarioRoomTarget struct {
	SourceArchive    string `json:"source_archive"`
	RoomArchiveIndex int    `json:"room_archive_index"`
	RoomMember       string `json:"room_member"`
	EmbeddedMember   string `json:"embedded_member"`
	SelectorIndex    int    `json:"selector_index"`
	Status           string `json:"status"`
}

// RoomMessageBankRegistration is an exact RBB registration making a message
// bank available under a room key. It does not prove a record is displayed.
type RoomMessageBankRegistration struct {
	RoomLogicalKey    string `json:"room_logical_key"`
	RoomAuthoringName string `json:"room_authoring_name"`
	RoomSourceArchive string `json:"room_source_archive"`
	RoomMember        string `json:"room_member"`
	RoomArchiveIndex  int    `json:"room_archive_index"`
	Bank              int    `json:"bank"`
	BankLogicalKey    string `json:"bank_logical_key"`
	BankSourceArchive string `json:"bank_source_archive"`
	BankMember        string `json:"bank_member"`
	BankArchiveIndex  int    `json:"bank_archive_index"`
	Status            string `json:"status"`
	RuntimeStatus     string `json:"runtime_status"`
}

type locatedMember struct {
	archive Archive
	member  paa.Member
}

// Build derives an in-memory retail index and returns context matching selector.
// Commands that issue multiple queries should reuse BuildFromRetailIndex.
func Build(project *corpus.Project, terms fixeddata.Terminology, archives []Archive, selector Selector) (Result, error) {
	index, err := BuildRetailIndex(archives)
	if err != nil {
		return Result{}, err
	}
	return BuildFromRetailIndex(project, terms, index, selector)
}

func markSelected(scene *Scene, selector Selector) {
	for index := range scene.Entries {
		scene.Entries[index].Selected = selectorMatchesMessage(selector, scene.Entries[index].MessageID)
	}
}

func buildRetailScene(archive, member string, p cdc.Program, bindata []byte, catalog scenarioCatalog) (Scene, error) {
	s := Scene{
		Member:         member,
		SourceArchive:  archive,
		SourceKind:     "cdc_program",
		Ordering:       "source_order_with_static_control_flow",
		EvidenceStatus: "static_consumer_reference",
		Entries:        make([]Entry, 0),
		References:     make([]Reference, 0),
	}
	s.Scenario = catalog.scene(member)
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
			entries, err := retailConsumer(bindata, node.command, node.offset, node.path, node.guard, node.depth, pos, flow)
			if err != nil {
				return Scene{}, fmt.Errorf("cdc context: %s: %w", member, err)
			}
			pos += len(entries)
			s.Entries = append(s.Entries, entries...)
		case "C12", "C13", "C14", "C76":
			s.References = append(s.References, reference(node.command, node.offset, node.path, node.guard, catalog))
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

func retailConsumer(data []byte, c cdc.Command, offset int, path []int, guard string, depth, pos int, flow abstractFlow) ([]Entry, error) {
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
		e := Entry{Kind: kind, MessageID: id, Offset: offset, OffsetBasis: "cdc_program_byte_offset", Position: pos + i, Reachability: flow.reachability(), Path: append([]int{}, path...), Guard: guard, Conditions: conditionSemantics(guard), Depth: depth, Raw: c.Raw, Actors: retailActorList(data, flow.actors)}
		if kind == "dialogue_association" {
			e.DisplayMode = intPointer(mode)
			e.EntityAssociationHandleRaw = intPointer(handle)
			portraitRequested := mode&1 != 0
			nameRequested := mode&2 != 0
			forcedStateThree := mode&4 != 0
			e.PortraitRequested = boolPointer(portraitRequested)
			e.NameLabelRequested = boolPointer(nameRequested)
			e.ForcedStateThree = boolPointer(forcedStateThree)
			if portraitRequested {
				e.PortraitStatus = "requested_availability_unresolved"
			} else {
				e.PortraitStatus = "not_requested"
			}
			a := resolveRetailAssociation(data, handle)
			e.AssociationNameRecordID = a.nameRecordID
			e.AssociatedLabelMessageID = a.labelMessageID
			e.AssociationResolution = a.resolution
			e.SpeakerStatus = a.speakerStatus
		}
		r = append(r, e)
	}
	return r, nil
}

func possibleAddressees(actors []Actor, associationHandle int) []AddresseeCandidate {
	result := make([]AddresseeCandidate, 0)
	for _, actor := range actors {
		if actor.Handle == associationHandle || actor.Presence != "present" {
			continue
		}
		label := actor.AssociatedLabelEnglish
		if label == "" {
			label = actor.AssociatedLabelJapanese
		}
		if label == "" {
			label = actor.AssociationLabelResolution
		}
		result = append(result, AddresseeCandidate{
			Handle: actor.Handle, Label: label, Confidence: "low",
			Status: "possible", Evidence: "path_local_present_actor_other_than_c5_association",
		})
	}
	return result
}

func conditionSemantics(guard string) []Condition {
	if guard == "" {
		return nil
	}
	parts := strings.Split(guard, " > ")
	result := make([]Condition, 0, len(parts))
	for _, raw := range parts {
		program, err := cdc.Parse("guard", []byte(raw+"E"))
		if err != nil || len(program.Commands) != 1 {
			result = append(result, Condition{Raw: raw, Kind: "unresolved", Status: "unresolved", Basis: "raw_cdc_guard"})
			continue
		}
		result = append(result, conditionForCommand(program.Commands[0]))
	}
	return result
}

func conditionForCommand(command cdc.Command) Condition {
	condition := Condition{Opcode: command.Name, Raw: command.Raw, Kind: "unresolved", Status: "unresolved", Basis: "raw_cdc_guard"}
	argument := func(index int) (*int, bool) {
		if index >= len(command.Arguments) {
			return nil, false
		}
		value, err := strconv.Atoi(command.Arguments[index])
		if err != nil {
			return nil, false
		}
		return intPointer(value), true
	}
	polarity := func(raw string) {
		condition.PolarityRaw = raw
		switch raw {
		case "F", "H":
			condition.Polarity = "positive"
		case "O", "N":
			condition.Polarity = "negated"
		}
	}
	comparator := func(raw string) {
		condition.ComparatorRaw = raw
		switch raw {
		case "Q":
			condition.Comparator = "equal"
		case "U":
			condition.Comparator = "greater_or_equal"
		case "W":
			condition.Comparator = "less_or_equal"
		}
	}
	switch command.Name {
	case "C0":
		if len(command.Arguments) == 3 {
			condition.SelectorRaw, _ = argument(0)
			condition.ComparisonValue, _ = argument(1)
			polarity(command.Arguments[2])
			condition.Kind, condition.Status, condition.Basis = "runtime_predicate", "verified_control_behavior", "executable_handler"
		}
	case "C1":
		if len(command.Arguments) == 3 {
			condition.SelectorRaw, _ = argument(0)
			condition.ComparisonValue, _ = argument(1)
			condition.PolarityRaw = command.Arguments[2]
			condition.Kind, condition.Status, condition.Basis = "control_action_scope", "verified_nonpredicate", "executable_handler"
		}
	case "C21":
		if len(command.Arguments) == 1 {
			condition.SelectedIndex, _ = argument(0)
			condition.Kind, condition.Status, condition.Basis = "choice_result_equals", "verified_control_behavior", "executable_handler"
		}
	case "C20":
		if len(command.Arguments) >= 2 {
			condition.BaseMessageID, _ = argument(0)
			condition.OptionCount, _ = argument(1)
			condition.Kind, condition.Status, condition.Basis = "choice_context", "verified_control_behavior", "executable_handler"
		}
	case "C32":
		if len(command.Arguments) == 4 {
			condition.Operand1Raw, _ = argument(0)
			condition.Operand2Raw, _ = argument(1)
			condition.ComparisonValue, _ = argument(2)
			comparator(command.Arguments[3])
			condition.Kind, condition.Status, condition.Basis = "runtime_compare", "verified_block_skip_behavior", "executable_handler"
		}
	case "C36":
		if len(command.Arguments) == 3 {
			condition.Operand1Raw, _ = argument(0)
			condition.ComparisonValue, _ = argument(1)
			comparator(command.Arguments[2])
			condition.Kind, condition.Status, condition.Basis = "runtime_existence_or_compare", "verified_block_skip_behavior", "executable_handler"
		}
	case "C45":
		if len(command.Arguments) == 3 {
			condition.SelectorRaw, _ = argument(0)
			condition.ComparisonValue, _ = argument(1)
			comparator(command.Arguments[2])
			condition.Kind, condition.Status, condition.Basis = "runtime_compare", "verified_block_skip_behavior", "executable_handler"
		}
	case "C58":
		if len(command.Arguments) == 3 && len(command.Arguments[1]) >= 2 {
			condition.EntitySelectorRaw, _ = argument(0)
			condition.PredicateFamilyRaw = command.Arguments[1][:1]
			value, err := strconv.Atoi(command.Arguments[1][1:])
			if err == nil {
				condition.PredicateIndexRaw = intPointer(value)
			}
			polarity(command.Arguments[2])
			condition.Kind, condition.Status, condition.Basis = "entity_predicate", "verified_control_behavior", "executable_handler"
		}
	case "C75":
		if len(command.Arguments) == 2 {
			condition.SlotRaw, _ = argument(0)
			polarity(command.Arguments[1])
			condition.Kind, condition.Status, condition.Basis = "slot_predicate", "verified_block_skip_behavior", "executable_handler"
		}
	case "C80":
		if len(command.Arguments) == 3 {
			condition.EntitySelectorRaw, _ = argument(0)
			condition.Operand2Raw, _ = argument(1)
			condition.ComparatorRaw = command.Arguments[2]
			condition.Kind, condition.Status, condition.Basis = "entity_subselector_condition", "verified_block_skip_behavior", "executable_handler"
		}
	}
	return condition
}

func sourceControls(japanese, english string) ([]SourceControl, error) {
	source, err := message.ParseInlineControls(japanese)
	if err != nil || len(source) == 0 {
		return nil, err
	}
	translated, err := message.ParseInlineControls(english)
	if err != nil {
		return nil, fmt.Errorf("English inline control: %w", err)
	}
	result := make([]SourceControl, len(source))
	if len(translated) == 0 && english == "" {
		for index, control := range source {
			result[index] = joinedSourceControl(control, nil)
		}
		return result, nil
	}
	if len(source) != len(translated) {
		return nil, fmt.Errorf("English inline control differs from Japanese source structure")
	}
	for index, control := range source {
		translatedControl := translated[index]
		if control.Kind != translatedControl.Kind || control.Selector != translatedControl.Selector || len(control.Blocks) != len(translatedControl.Blocks) {
			return nil, fmt.Errorf("English inline control differs from Japanese source structure")
		}
		for blockIndex, block := range control.Blocks {
			translatedBlock := translatedControl.Blocks[blockIndex]
			if block.Role != translatedBlock.Role || block.Condition != translatedBlock.Condition {
				return nil, fmt.Errorf("English inline control differs from Japanese source structure")
			}
		}
		result[index] = joinedSourceControl(control, &translatedControl)
	}
	return result, nil
}

func joinedSourceControl(source message.InlineControl, translated *message.InlineControl) SourceControl {
	result := SourceControl{Kind: source.Kind, Selector: source.Selector, Evidence: "retail_message_bytecode", Blocks: make([]SourceBlock, len(source.Blocks))}
	if source.ExpectedBlocks != nil {
		result.ExpectedBlocks = intPointer(*source.ExpectedBlocks)
	}
	for index, block := range source.Blocks {
		result.Blocks[index] = SourceBlock{Position: block.Position, Role: block.Role, Condition: block.Condition, Japanese: block.Text}
		if translated != nil {
			result.Blocks[index].English = translated.Blocks[index].Text
		}
	}
	return result
}

func intPointer(value int) *int {
	return &value
}

func boolPointer(value bool) *bool {
	return &value
}

func applicableTerms(entries []fixeddata.SearchEntry) []TerminologyEntry {
	result := make([]TerminologyEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, TerminologyEntry{
			Kind:      entry.Kind,
			Japanese:  entry.Term.Japanese,
			English:   entry.Term.English,
			Scope:     entry.Term.Scope,
			SourceIDs: append([]int(nil), entry.Term.SourceIDs...),
		})
	}
	return result
}
func retailActorList(data []byte, actors actorState) []Actor {
	r := make([]Actor, 0, len(actors))
	for h, fact := range actors {
		a := resolveRetailAssociation(data, h)
		r = append(r, Actor{
			Handle:                     h,
			Presence:                   fact.presence,
			PresenceBasis:              fact.basis,
			AssociationNameRecordID:    a.nameRecordID,
			AssociatedLabelMessageID:   a.labelMessageID,
			AssociationLabelResolution: a.resolution,
			Position:                   actorPosition(fact),
			Action:                     actorAction(fact),
			Relation:                   actorRelation(fact),
		})
	}
	sort.Slice(r, func(i, j int) bool { return r[i].Handle < r[j].Handle })
	return r
}

func actorPosition(fact actorFact) *ActorPosition {
	if !fact.positionKnown {
		return nil
	}
	return &ActorPosition{Component2: fact.positionComponent2, Component3: fact.positionComponent3, Source: fact.positionSource, Status: "coordinate_like_pair"}
}

func actorAction(fact actorFact) *ActorAction {
	if !fact.actionKnown {
		return nil
	}
	action := &ActorAction{ActionIDRaw: fact.actionID, OptionO: fact.actionOptionO, C5AssociationBehaviorFlag: fact.actionAssociationFlag, Source: fact.actionSource, Status: "per_entity_action"}
	if fact.actionModifierKnown {
		action.ModifierRaw = intPointer(fact.actionModifier)
	}
	return action
}

func actorRelation(fact actorFact) *ActorRelation {
	if !fact.relationKnown {
		return nil
	}
	return &ActorRelation{ModeOrValueRaw: fact.relationValue, Source: fact.relationSource, Status: "opaque_per_entity_state"}
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

func resolveRetailAssociation(data []byte, h int) association {
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
	result := unresolvedAssociation("mapped_label_message")
	result.nameRecordID = intPointer(id)
	result.labelMessageID = intPointer(msg)
	return result
}

func hydrateAssociation(project *corpus.Project, retail association) association {
	if retail.labelMessageID == nil {
		return retail
	}
	result := retail
	item, ok := project.Find(*retail.labelMessageID)
	if !ok {
		result.resolution = "unmapped_handle"
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
func reference(c cdc.Command, offset int, path []int, guard string, catalog scenarioCatalog) Reference {
	r := Reference{Opcode: c.Name, Offset: offset, Path: append([]int{}, path...), Guard: guard, Raw: c.Raw, Arguments: append([]string{}, c.Arguments...), ExecutionStatus: "runtime_dependent", ResolutionStatus: "unresolved"}
	if n, ok := c.ScenarioSlot(); ok {
		status := "catalog_unavailable"
		if _, found := catalog.families[n]; found {
			status = "group_runtime_dependent"
		}
		r.Scenario = &ScenarioReference{Slot: n, Status: status}
		r.ResolutionStatus = status
		if c.Name == "C14" {
			r.ExecutionStatus = "direct_request"
		}
	}
	if index, ok := c.ScenarioSlotTableIndex(); ok {
		r.ScenarioRoomTable = &ScenarioRoomTable{
			TableIndex: index, SelectorValue: 1000 + index,
			Status: "room_runtime_dependent",
		}
		r.ResolutionStatus = "room_runtime_dependent"
	}
	if x, ok := c.C76Resource(); ok {
		r.Resource = &x
		r.ExecutionStatus = "direct_request"
		r.ResolutionStatus = "logical_key_only"
		if name, ok := catalog.resourceNames[x.LogicalKey]; ok {
			r.ResourceAuthoringName = name
			r.ResolutionStatus = "logical_key_with_authoring_name"
		}
	}
	return r
}
