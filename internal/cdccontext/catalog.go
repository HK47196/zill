// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/cp932"
	"github.com/HK47196/zill/internal/gamefmt/rbb"
)

var roomMessageBankName = regexp.MustCompile(`^msgsec([0-9]{3})_dat$`)

var scenarioGroups = []struct {
	public string
	rbb    string
}{
	{"01", "cdc01"}, {"02", "cdc02"}, {"03", "cdc03"}, {"04", "cdc04"}, {"05", "cdc05"}, {"06", "cdc06"},
	{"v1", "cdcV1"}, {"v2", "cdcV2"}, {"v3", "cdcV3"}, {"v4", "cdcV4"}, {"v5", "cdcV5"}, {"v6", "cdcV6"}, {"v7", "cdcV7"},
}

type scenarioCatalog struct {
	families        map[int]ScenarioFamily
	scenesByMember  map[string]ScenarioScene
	representatives map[string]bool
	physicalMembers map[string]bool
	resourceNames   map[string]string
}

func emptyScenarioCatalog() scenarioCatalog {
	return scenarioCatalog{
		families: make(map[int]ScenarioFamily), scenesByMember: make(map[string]ScenarioScene),
		representatives: make(map[string]bool), physicalMembers: make(map[string]bool),
		resourceNames: make(map[string]string),
	}
}

func buildScenarioCatalog(catalog *rbb.Catalog, members []locatedMember) (scenarioCatalog, error) {
	result := emptyScenarioCatalog()
	byName := make(map[string]locatedMember, len(members))
	for _, member := range members {
		if _, duplicate := byName[member.member.Name]; duplicate {
			return scenarioCatalog{}, fmt.Errorf("cdc context: duplicate archive member %s", member.member.Name)
		}
		byName[member.member.Name] = member
	}
	for id := 1; id <= 1184; id++ {
		name, ok := catalog.Do(id)
		if !ok {
			return scenarioCatalog{}, fmt.Errorf("cdc context: RBB is missing cdcDo/ID%04d", id)
		}
		decoded, err := cp932.Decode([]byte(name))
		if err != nil {
			return scenarioCatalog{}, fmt.Errorf("cdc context: RBB cdcDo/ID%04d authoring name: %w", id, err)
		}
		result.resourceNames[fmt.Sprintf("cdcDo/ID%04d", id)] = decoded
	}
	for slot := 1; slot <= 914; slot++ {
		family := ScenarioFamily{
			Slot: slot, Status: "group_runtime_dependent", Basis: "verified_rbb_logical_resource_catalog",
			Variants: make([]ScenarioContentVariant, 0), Incoming: make([]ScenarioIncomingEdge, 0),
			Roots: make([]ScenarioRoot, 0), RoomTargets: make([]ScenarioRoomTarget, 0),
		}
		byPayload := make(map[string]int)
		for _, group := range scenarioGroups {
			authoring, ok := catalog.Scenario(group.rbb, slot)
			if !ok {
				return scenarioCatalog{}, fmt.Errorf("cdc context: RBB is missing %s/ID%04d", group.rbb, slot)
			}
			decoded, err := cp932.Decode([]byte(authoring))
			if err != nil {
				return scenarioCatalog{}, fmt.Errorf("cdc context: RBB %s/ID%04d authoring name: %w", group.rbb, slot, err)
			}
			stem := strings.TrimSuffix(strings.ToLower(decoded), "_cdc")
			physicalName := fmt.Sprintf("cdc/%s/%s.cdc", group.public, stem)
			located, ok := byName[physicalName]
			if !ok {
				return scenarioCatalog{}, fmt.Errorf("cdc context: RBB %s/ID%04d names missing physical member %s", group.rbb, slot, physicalName)
			}
			payload, err := located.archive.Pair.Payload(located.member.Index)
			if err != nil {
				return scenarioCatalog{}, err
			}
			member := ScenarioPhysicalMember{
				Group: group.public, LogicalKey: fmt.Sprintf("%s/ID%04d", group.rbb, slot),
				AuthoringName: decoded, SourceArchive: located.archive.Name,
				Member: physicalName, ArchiveIndex: located.member.Index,
			}
			key := string(payload)
			variantIndex, exists := byPayload[key]
			if !exists {
				digest := sha256.Sum256(payload)
				variantIndex = len(family.Variants)
				byPayload[key] = variantIndex
				family.Variants = append(family.Variants, ScenarioContentVariant{
					ContentSHA256: hex.EncodeToString(digest[:]), CanonicalMember: physicalName,
					Members: make([]ScenarioPhysicalMember, 0, len(scenarioGroups)),
				})
				result.representatives[physicalName] = true
			}
			family.Variants[variantIndex].Members = append(family.Variants[variantIndex].Members, member)
			result.physicalMembers[physicalName] = true
		}
		for _, variant := range family.Variants {
			groups := make([]string, len(variant.Members))
			for index, member := range variant.Members {
				groups[index] = member.Group
			}
			result.scenesByMember[variant.CanonicalMember] = ScenarioScene{
				Slot: slot, ContentSHA256: variant.ContentSHA256,
				EquivalentGroups: groups, EquivalentMemberCount: len(variant.Members),
			}
		}
		result.families[slot] = family
	}
	return result, nil
}

func roomMessageBankRegistrations(catalog *rbb.Catalog, members []locatedMember, bank int) ([]RoomMessageBankRegistration, error) {
	available := make(map[string][]locatedMember, len(members))
	for _, member := range members {
		if member.member.Name == "" {
			continue
		}
		available[member.member.Name] = append(available[member.member.Name], member)
	}
	result := make([]RoomMessageBankRegistration, 0)
	for _, room := range catalog.Rooms() {
		roomMember := fmt.Sprintf("room/id%04d.par", room.ID)
		roomMatches := available[roomMember]
		if len(roomMatches) != 1 {
			return nil, fmt.Errorf("cdc context: RBB room/ID%04d does not identify exactly one physical member %s", room.ID, roomMember)
		}
		locatedRoom := roomMatches[0]
		roomAuthoringName := fmt.Sprintf("ID%04d_par", room.ID)
		if !slices.Contains(room.Resources, roomAuthoringName) {
			return nil, fmt.Errorf("cdc context: RBB room/ID%04d is missing authored resource %s", room.ID, roomAuthoringName)
		}
		for _, resource := range room.Resources {
			match := roomMessageBankName.FindStringSubmatch(resource)
			if match == nil {
				continue
			}
			section, err := strconv.Atoi(match[1])
			if err != nil || section != bank {
				continue
			}
			bankMember := fmt.Sprintf("message/msgsec%03d.dat", section)
			bankMatches := available[bankMember]
			if len(bankMatches) != 1 {
				return nil, fmt.Errorf("cdc context: RBB room/ID%04d does not register exactly one physical member %s", room.ID, bankMember)
			}
			locatedBank := bankMatches[0]
			result = append(result, RoomMessageBankRegistration{
				RoomLogicalKey: fmt.Sprintf("room/ID%04d", room.ID), RoomAuthoringName: roomAuthoringName,
				RoomSourceArchive: locatedRoom.archive.Name, RoomMember: roomMember, RoomArchiveIndex: locatedRoom.member.Index,
				Bank: section, BankLogicalKey: resource, BankSourceArchive: locatedBank.archive.Name,
				BankMember: bankMember, BankArchiveIndex: locatedBank.member.Index,
				Status: "verified_rbb_room_registration", RuntimeStatus: "current_room_runtime_dependent",
			})
		}
	}
	return result, nil
}

func (catalog scenarioCatalog) shouldParse(member string) bool {
	if !catalog.physicalMembers[member] {
		return true
	}
	return catalog.representatives[member]
}

func (catalog scenarioCatalog) scene(member string) *ScenarioScene {
	metadata, ok := catalog.scenesByMember[member]
	if !ok {
		return nil
	}
	copy := metadata
	copy.EquivalentGroups = append([]string(nil), metadata.EquivalentGroups...)
	return &copy
}

func attachScenarioGraph(catalog *scenarioCatalog, scenes []Scene, roomTargets map[int][]ScenarioRoomTarget) {
	for slot, family := range catalog.families {
		family.Roots = scenarioRoots(slot)
		family.RoomTargets = append([]ScenarioRoomTarget(nil), roomTargets[slot]...)
		catalog.families[slot] = family
	}
	for _, scene := range scenes {
		for _, reference := range scene.References {
			if reference.Scenario == nil {
				continue
			}
			family, ok := catalog.families[reference.Scenario.Slot]
			if !ok {
				continue
			}
			edge := ScenarioIncomingEdge{
				SourceMember: scene.Member, SourceArchive: scene.SourceArchive, Opcode: reference.Opcode,
				Offset: reference.Offset, Path: append([]int(nil), reference.Path...),
				Guard: reference.Guard, ExecutionStatus: reference.ExecutionStatus,
			}
			if scene.Scenario != nil {
				edge.SourceScenarioSlot = intPointer(scene.Scenario.Slot)
			}
			family.Incoming = append(family.Incoming, edge)
			catalog.families[family.Slot] = family
		}
	}
}

func attachScenarioRoomTables(scenes []Scene, roomTargets map[int][]ScenarioRoomTarget) {
	summaries := make(map[int]ScenarioRoomTable, 10)
	for tableIndex := 0; tableIndex < 10; tableIndex++ {
		slots := make(map[int]bool)
		rooms := make(map[string]bool)
		targetCount := 0
		for slot, targets := range roomTargets {
			for _, target := range targets {
				if target.SelectorIndex != 1000+tableIndex {
					continue
				}
				slots[slot] = true
				rooms[target.RoomMember] = true
				targetCount++
			}
		}
		possibleSlots := make([]int, 0, len(slots))
		for slot := range slots {
			possibleSlots = append(possibleSlots, slot)
		}
		sort.Ints(possibleSlots)
		summaries[tableIndex] = ScenarioRoomTable{
			TableIndex: tableIndex, SelectorValue: 1000 + tableIndex,
			PossibleSlots: possibleSlots, TargetCount: targetCount, RoomCount: len(rooms),
			Status: "current_room_runtime_dependent",
		}
	}
	for sceneIndex := range scenes {
		for referenceIndex := range scenes[sceneIndex].References {
			reference := &scenes[sceneIndex].References[referenceIndex]
			if reference.ScenarioRoomTable == nil {
				continue
			}
			summary := summaries[reference.ScenarioRoomTable.TableIndex]
			summary.PossibleSlots = append([]int(nil), summary.PossibleSlots...)
			reference.ScenarioRoomTable = &summary
		}
	}
}

func scenarioRoots(slot int) []ScenarioRoot {
	result := make([]ScenarioRoot, 0, 2)
	if _, ok := directScenarioRootSlots[slot]; ok {
		result = append(result, ScenarioRoot{
			Kind: "direct_executable_request", Status: "verified_executable_root", Confidence: "high",
			SourceLocator: "ULJM05410-1.03 EBOOT direct 0xd0498 callers",
		})
	}
	if _, ok := boundedScenarioRootSlots[slot]; ok {
		result = append(result, ScenarioRoot{
			Kind: "bounded_executable_table", Status: "verified_executable_root", Confidence: "high",
			SourceLocator: "ULJM05410-1.03 EBOOT 0xbb940 indexed scenario table",
		})
	}
	return result
}

var directScenarioRootSlots = map[int]struct{}{
	42: {}, 65: {}, 67: {}, 93: {}, 94: {}, 117: {}, 118: {}, 229: {}, 524: {}, 540: {}, 775: {},
}

var boundedScenarioRootSlots = map[int]struct{}{
	3: {}, 4: {}, 31: {}, 32: {}, 33: {},
}

func scenarioFamiliesForScenes(catalog scenarioCatalog, scenes []Scene) []ScenarioFamily {
	wanted := make(map[int]map[string]bool)
	add := func(slot int, relevance string) {
		if wanted[slot] == nil {
			wanted[slot] = make(map[string]bool)
		}
		wanted[slot][relevance] = true
	}
	for _, scene := range scenes {
		if scene.Scenario != nil {
			add(scene.Scenario.Slot, "selected_scene")
		}
		for _, reference := range scene.References {
			if reference.Scenario != nil {
				add(reference.Scenario.Slot, "direct_reference")
			}
			if reference.ScenarioRoomTable != nil {
				for _, slot := range reference.ScenarioRoomTable.PossibleSlots {
					add(slot, "room_table_possible")
				}
			}
		}
	}
	slots := make([]int, 0, len(wanted))
	for slot := range wanted {
		slots = append(slots, slot)
	}
	sort.Ints(slots)
	result := make([]ScenarioFamily, 0, len(slots))
	for _, slot := range slots {
		if family, ok := catalog.families[slot]; ok {
			for _, relevance := range []string{"selected_scene", "direct_reference", "room_table_possible"} {
				if wanted[slot][relevance] {
					family.Relevance = append(family.Relevance, relevance)
				}
			}
			result = append(result, family)
		}
	}
	return result
}
