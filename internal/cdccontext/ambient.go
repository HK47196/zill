// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/fixeddata"
)

const (
	roomEntityTableOffset   = 0xc0
	roomEntityRecordSize    = 0x1a
	roomEntityRecordCount   = 8
	roomScenarioTableOffset = 0x21e
	roomScenarioRecordSize  = 10
	roomScenarioRecordCount = 10
	maxRoomPARSize          = 16 << 20
)

// AmbientInteraction is the verified ordinary-talk association for one
// message. A room occurrence proves authored placement data, not runtime
// presence or global dialogue order.
type AmbientInteraction struct {
	EntityHandle  int    `json:"entity_handle"`
	Status        string `json:"status"`
	RuntimeStatus string `json:"runtime_status"`
	SourceLocator string `json:"source_locator"`
	RoomMember    string `json:"room_member,omitempty"`
	RoomResource  string `json:"room_resource,omitempty"`
	EntitySlot    *int   `json:"entity_slot,omitempty"`
	EntityOffset  *int   `json:"entity_offset,omitempty"`
}

type ambientRange struct {
	firstHandle  int
	lastHandle   int
	firstMessage int
}

// These ranges are the complete bounded piecewise mapping in the supported
// ULJM05410 1.03 ordinary-interaction handler at 0x18b18. They are executable
// dispatch rules, not bank-specific text heuristics.
var ambientRanges = []ambientRange{
	{1069, 1101, 30000},
	{1102, 1121, 40000},
	{1122, 1132, 1580000},
	{1133, 1146, 50000},
	{1147, 1194, 60000},
	{1195, 1220, 70000},
	{1221, 1236, 80000},
	{1237, 1247, 90000},
	{1248, 1259, 100000},
	{1260, 1289, 110000},
	{1290, 1294, 120000},
	{1295, 1304, 130000},
	{1305, 1320, 140000},
	{1321, 1338, 150000},
	{1339, 1350, 680000},
	{1361, 1370, 1600000},
}

const ambientSourceLocator = "ULJM05410-1.03 EBOOT 0xb9f60,0x18eb8,0x18b18,0x18e6c,0x8f564,0x11e3b4"

func ambientMessageForHandle(handle int) (int, bool) {
	if handle == 529 {
		return 1290013, true
	}
	if handle >= 1351 && handle <= 1360 {
		return 1588, true
	}
	for _, rule := range ambientRanges {
		if handle >= rule.firstHandle && handle <= rule.lastHandle {
			return rule.firstMessage + handle - rule.firstHandle, true
		}
	}
	return 0, false
}

func ambientHandleForMessage(messageID int) (int, bool) {
	if messageID == 1290013 {
		return 529, true
	}
	for _, rule := range ambientRanges {
		lastMessage := rule.firstMessage + rule.lastHandle - rule.firstHandle
		if messageID >= rule.firstMessage && messageID <= lastMessage {
			return rule.firstHandle + messageID - rule.firstMessage, true
		}
	}
	return 0, false
}

func ambientConsumerEvidence(messageID int) []ConsumerEvidence {
	if _, ok := ambientHandleForMessage(messageID); !ok && messageID != 1588 {
		return nil
	}
	return []ConsumerEvidence{{
		Disposition:   "verified_consumer",
		Role:          "ambient_npc_dialogue",
		Category:      "ordinary-interaction-dialogue",
		Variant:       "entity_handle_to_message",
		Confidence:    "high",
		RuntimeStatus: "interaction_target_runtime_dependent",
		SourceLocator: ambientSourceLocator,
	}}
}

func allConsumerEvidence(messageID int) []ConsumerEvidence {
	result := executableConsumerEvidence(messageID)
	return append(result, ambientConsumerEvidence(messageID)...)
}

func addAmbientAssociation(project *corpus.Project, bindata []byte, entry *Entry, interaction AmbientInteraction) {
	handle := interaction.EntityHandle
	entry.AmbientInteraction = &interaction
	entry.EntityAssociationHandleRaw = intPointer(handle)
	association := resolve(project, bindata, handle)
	entry.AssociationNameRecordID = association.nameRecordID
	entry.AssociatedLabelMessageID = association.labelMessageID
	entry.AssociatedLabelJapanese = association.labelJapanese
	entry.AssociatedLabelEnglish = association.labelEnglish
	entry.AssociationResolution = association.resolution
	if association.labelJapanese != "" || association.labelEnglish != "" {
		entry.SpeakerStatus = "inferred_from_verified_interaction_target"
		entry.SpeakerJapanese = association.labelJapanese
		entry.SpeakerEnglish = association.labelEnglish
		entry.SpeakerSource = "ambient_interaction_target_label"
	}
}

func buildAmbientScenes(project *corpus.Project, terms fixeddata.Terminology, bindata []byte, rooms []locatedMember, selector Selector) ([]Scene, error) {
	if !selectorMayMatchAmbient(selector) {
		return nil, nil
	}
	sort.Slice(rooms, func(i, j int) bool {
		if rooms[i].archive.Name != rooms[j].archive.Name {
			return rooms[i].archive.Name < rooms[j].archive.Name
		}
		return rooms[i].member.Name < rooms[j].member.Name
	})
	result := make([]Scene, 0)
	for _, room := range rooms {
		payload, err := room.archive.Pair.Payload(room.member.Index)
		if err != nil {
			return nil, err
		}
		resources, err := roomIMDResources(room.member.Name, payload)
		if err != nil {
			return nil, fmt.Errorf("cdc context: %s: %w", room.member.Name, err)
		}
		for _, resource := range resources {
			scene := Scene{
				Member: room.member.Name, EmbeddedMember: resource.name,
				SourceArchive: room.archive.Name, SourceKind: "ambient_interaction",
				Ordering: "room_entity_table_order", EvidenceStatus: "verified_executable_interaction_mapping",
				Entries: make([]Entry, 0), References: make([]Reference, 0),
			}
			selected := false
			for slot := 0; slot < roomEntityRecordCount; slot++ {
				offset := roomEntityTableOffset + slot*roomEntityRecordSize
				handle := int(binary.LittleEndian.Uint16(resource.data[offset:]))
				messageID, ok := ambientMessageForHandle(handle)
				if !ok {
					continue
				}
				matches := selectorMatchesMessage(selector, messageID)
				selected = selected || matches
				item, ok := project.Find(messageID)
				if !ok {
					return nil, fmt.Errorf("cdc context: ambient handle %d maps to unknown message %d", handle, messageID)
				}
				controls, err := sourceControls(item.Translation.Japanese, item.Translation.Text)
				if err != nil {
					return nil, fmt.Errorf("cdc context: message %d: %w", messageID, err)
				}
				entry := Entry{
					Kind: "ambient_dialogue", MessageID: messageID,
					Offset: offset, OffsetBasis: "room_imd_entity_record_offset", Position: slot,
					Selected: matches, Reachability: "runtime_dependent", Path: make([]int, 0),
					Raw:      fmt.Sprintf("entity_handle=%d", handle),
					Japanese: item.Translation.Japanese, English: item.Translation.Text, State: item.Translation.State,
					Terminology: applicableTerms(terms.Applicable(item)), SourceControls: controls,
					Relationships: executableRelationships(project, messageID), ConsumerEvidence: allConsumerEvidence(messageID),
					AuthoringMetadata: authoringMetadata(messageID, item.Translation.Japanese), Actors: make([]Actor, 0),
				}
				interaction := AmbientInteraction{
					EntityHandle: handle, Status: "verified_executable_mapping",
					RuntimeStatus: "interaction_target_runtime_dependent", SourceLocator: ambientSourceLocator,
					RoomMember: room.member.Name, RoomResource: resource.name,
					EntitySlot: intPointer(slot), EntityOffset: intPointer(offset),
				}
				addAmbientAssociation(project, bindata, &entry, interaction)
				scene.Entries = append(scene.Entries, entry)
			}
			if selected {
				result = append(result, scene)
			}
		}
	}
	return result, nil
}

func selectorMayMatchAmbient(selector Selector) bool {
	if selector.Record >= 0 {
		if selector.Record == 1588 {
			return true
		}
		_, ok := ambientHandleForMessage(selector.Record)
		return ok
	}
	if selector.Bank == 0 || selector.Bank == 129 {
		return true
	}
	for _, rule := range ambientRanges {
		if rule.firstMessage/10_000 == selector.Bank {
			return true
		}
	}
	return false
}

func selectorMatchesMessage(selector Selector, messageID int) bool {
	if selector.Record >= 0 {
		return selector.Record == messageID
	}
	return selector.Bank == messageID/10_000
}

type roomResource struct {
	name string
	data []byte
}

func roomIMDResources(source string, payload []byte) ([]roomResource, error) {
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("open gzip room package: %w", err)
	}
	decompressed, readErr := io.ReadAll(io.LimitReader(reader, maxRoomPARSize+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("decompress room package: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close room package: %w", closeErr)
	}
	if len(decompressed) > maxRoomPARSize {
		return nil, fmt.Errorf("decompressed room package exceeds %d bytes", maxRoomPARSize)
	}
	children, err := parseRoomPAR(decompressed)
	if err != nil {
		return nil, err
	}
	resources := make([]roomResource, 0, 1)
	for _, child := range children {
		if !strings.HasSuffix(strings.ToLower(child.name), ".imd") {
			continue
		}
		data := decompressed[child.start:child.end]
		minimum := roomEntityTableOffset + roomEntityRecordCount*roomEntityRecordSize
		if len(data) < minimum {
			return nil, fmt.Errorf("%s: %s is too small for the room entity table", source, child.name)
		}
		resources = append(resources, roomResource{name: child.name, data: data})
	}
	return resources, nil
}

func scenarioRoomTargets(rooms []locatedMember) (map[int][]ScenarioRoomTarget, error) {
	result := make(map[int][]ScenarioRoomTarget)
	sort.Slice(rooms, func(i, j int) bool {
		if rooms[i].archive.Name != rooms[j].archive.Name {
			return rooms[i].archive.Name < rooms[j].archive.Name
		}
		return rooms[i].member.Name < rooms[j].member.Name
	})
	for _, room := range rooms {
		payload, err := room.archive.Pair.Payload(room.member.Index)
		if err != nil {
			return nil, err
		}
		resources, err := roomIMDResources(room.member.Name, payload)
		if err != nil {
			return nil, fmt.Errorf("cdc context: %s: %w", room.member.Name, err)
		}
		for _, resource := range resources {
			minimum := roomScenarioTableOffset + (roomScenarioRecordCount-1)*roomScenarioRecordSize + 2
			if len(resource.data) < minimum {
				return nil, fmt.Errorf("cdc context: %s: %s is too small for the scenario-slot table", room.member.Name, resource.name)
			}
			for index := 0; index < roomScenarioRecordCount; index++ {
				offset := roomScenarioTableOffset + index*roomScenarioRecordSize
				slot := int(int16(binary.LittleEndian.Uint16(resource.data[offset:])))
				if slot < 1 || slot > 914 {
					continue
				}
				result[slot] = append(result[slot], ScenarioRoomTarget{
					SourceArchive: room.archive.Name, RoomArchiveIndex: room.member.Index,
					RoomMember: room.member.Name, EmbeddedMember: resource.name,
					SelectorIndex: 1000 + index, Status: "verified_room_imd_slot",
				})
			}
		}
	}
	return result, nil
}

type roomPARChild struct {
	name       string
	start, end int
}

func parseRoomPAR(data []byte) ([]roomPARChild, error) {
	if len(data) < 16 || !bytes.Equal(data[:4], []byte{'P', 'A', 'R', 0}) {
		return nil, errors.New("room package is not a PAR container")
	}
	count := int(binary.LittleEndian.Uint32(data[8:12]))
	if count > (len(data)-16)/4 {
		return nil, errors.New("truncated PAR offset table")
	}
	namesBase := (16 + count*4 + 15) &^ 15
	if count > (len(data)-namesBase)/32 {
		return nil, errors.New("truncated PAR name table")
	}
	children := make([]roomPARChild, count)
	previous := namesBase + count*32
	for index := 0; index < count; index++ {
		start := int(binary.LittleEndian.Uint32(data[16+index*4:]))
		if start < previous || start >= len(data) {
			return nil, fmt.Errorf("PAR child %d has an invalid offset", index)
		}
		end := len(data)
		if index+1 < count {
			end = int(binary.LittleEndian.Uint32(data[16+(index+1)*4:]))
			if end <= start || end > len(data) {
				return nil, fmt.Errorf("PAR child %d has an invalid end offset", index)
			}
		}
		rawName := data[namesBase+index*32 : namesBase+(index+1)*32]
		if nul := bytes.IndexByte(rawName, 0); nul >= 0 {
			rawName = rawName[:nul]
		}
		if len(rawName) == 0 {
			return nil, fmt.Errorf("PAR child %d has an empty name", index)
		}
		children[index] = roomPARChild{name: string(rawName), start: start, end: end}
		previous = end
	}
	return children, nil
}
