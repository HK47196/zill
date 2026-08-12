// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/fixeddata"
	"github.com/HK47196/zill/internal/gamefmt/cdc"
	"github.com/HK47196/zill/internal/gamefmt/rbb"
)

// RetailIndex is the immutable retail-derived half of static context. It
// deliberately contains no contributor English or terminology results.
type RetailIndex struct {
	Bindata                      []byte
	Scenes                       []Scene
	MessageScenes                map[int][]int
	BankScenes                   map[int][]int
	SceneLookup                  map[string][]int
	StorageBanks                 map[int]RetailBank
	ScenarioFamilies             map[int]ScenarioFamily
	RoomMessageBankRegistrations map[int][]RoomMessageBankRegistration
}

// RetailBank is one exact immutable message-bank payload and its archive
// provenance. It is parsed only when a query needs storage-order context.
type RetailBank struct {
	SourceArchive string
	Member        string
	Payload       []byte
}

// BuildRetailIndex performs the archive, CDC control-flow, RBB, and room-IMD
// analysis that is invariant across translation edits.
func BuildRetailIndex(archives []Archive) (*RetailIndex, error) {
	if len(archives) == 0 {
		return nil, fmt.Errorf("cdc context: archives are required")
	}
	var bindata []byte
	var rbbData []byte
	var programs []locatedMember
	var rooms []locatedMember
	var allMembers []locatedMember
	bankMembers := make(map[int]locatedMember)
	seenArchives := make(map[string]bool, len(archives))
	for _, archive := range archives {
		if archive.Name == "" || archive.Pair == nil {
			return nil, fmt.Errorf("cdc context: archive name and pair are required")
		}
		if seenArchives[archive.Name] {
			return nil, fmt.Errorf("cdc context: duplicate archive name %q", archive.Name)
		}
		seenArchives[archive.Name] = true
		for _, member := range archive.Pair.Members() {
			located := locatedMember{archive: archive, member: member}
			allMembers = append(allMembers, located)
			switch member.Name {
			case "data/bindata.dat":
				if bindata != nil {
					return nil, fmt.Errorf("cdc context: duplicate data/bindata.dat")
				}
				payload, err := archive.Pair.Payload(member.Index)
				if err != nil {
					return nil, err
				}
				bindata = payload
			case "res/res.rbb":
				if rbbData != nil {
					return nil, fmt.Errorf("cdc context: duplicate res/res.rbb")
				}
				payload, err := archive.Pair.Payload(member.Index)
				if err != nil {
					return nil, err
				}
				rbbData = payload
			}
			if strings.HasPrefix(member.Name, "cdc/") && strings.HasSuffix(member.Name, ".cdc") {
				programs = append(programs, located)
			}
			if strings.HasPrefix(member.Name, "room/id") && strings.HasSuffix(member.Name, ".par") {
				rooms = append(rooms, located)
			}
			if section, ok := messageBankSection(member.Name); ok {
				if _, duplicate := bankMembers[section]; duplicate {
					return nil, fmt.Errorf("cdc context: duplicate %s", member.Name)
				}
				bankMembers[section] = located
			}
		}
	}
	if bindata == nil {
		return nil, fmt.Errorf("cdc context: missing data/bindata.dat")
	}
	if len(programs) == 0 {
		return nil, fmt.Errorf("cdc context: archive contains no cdc/*.cdc members")
	}

	catalog := emptyScenarioCatalog()
	var resources *rbb.Catalog
	if rbbData != nil {
		parsed, err := rbb.Parse(rbbData)
		if err != nil {
			return nil, fmt.Errorf("cdc context: res/res.rbb: %w", err)
		}
		resources = parsed
		catalog, err = buildScenarioCatalog(resources, programs)
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(programs, func(i, j int) bool {
		if programs[i].archive.Name != programs[j].archive.Name {
			return programs[i].archive.Name < programs[j].archive.Name
		}
		return programs[i].member.Name < programs[j].member.Name
	})
	scenes := make([]Scene, 0, len(programs))
	for _, located := range programs {
		member := located.member
		if !catalog.shouldParse(member.Name) {
			continue
		}
		payload, err := located.archive.Pair.Payload(member.Index)
		if err != nil {
			return nil, err
		}
		program, err := cdc.Parse(member.Name, payload)
		if errors.Is(err, cdc.ErrPlaceholder) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cdc context: %s: %w", member.Name, err)
		}
		scene, err := buildRetailScene(located.archive.Name, member.Name, program, bindata, catalog)
		if err != nil {
			return nil, err
		}
		scenes = append(scenes, scene)
	}

	ambientScenes, roomTargets, err := scanRetailRooms(bindata, rooms)
	if err != nil {
		return nil, err
	}
	if resources != nil {
		attachScenarioRoomTables(scenes, roomTargets)
		attachScenarioGraph(&catalog, scenes, roomTargets)
	}
	scenes = append(scenes, ambientScenes...)

	storage := make(map[int]RetailBank, len(bankMembers))
	for section, located := range bankMembers {
		payload, err := located.archive.Pair.Payload(located.member.Index)
		if err != nil {
			return nil, err
		}
		storage[section] = RetailBank{SourceArchive: located.archive.Name, Member: located.member.Name, Payload: payload}
	}

	registrations := make(map[int][]RoomMessageBankRegistration)
	if resources != nil {
		registrations, err = roomMessageBankRegistrations(resources, allMembers)
		if err != nil {
			return nil, err
		}
	}
	index := &RetailIndex{
		Bindata: append([]byte(nil), bindata...), Scenes: scenes, MessageScenes: make(map[int][]int), BankScenes: make(map[int][]int), SceneLookup: make(map[string][]int),
		StorageBanks: storage, ScenarioFamilies: catalog.families,
		RoomMessageBankRegistrations: registrations,
	}
	index.buildReverseIndexes(catalog)
	if err := index.validate(); err != nil {
		return nil, err
	}
	return index, nil
}

func messageBankSection(name string) (int, bool) {
	const prefix = "message/msgsec"
	const suffix = ".dat"
	if len(name) != len(prefix)+3+len(suffix) || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return 0, false
	}
	section, err := strconv.Atoi(name[len(prefix) : len(prefix)+3])
	return section, err == nil && section >= 0 && section < 279
}

func buildRetailBankScene(bindata []byte, archive, member string, bank corpus.Bank) Scene {
	scene := Scene{
		ID: fmt.Sprintf("bank/%03d", bank.Section), Aliases: []string{archive + ":" + member},
		Member: member, SourceArchive: archive, SourceKind: "message_bank",
		Ordering: "storage_order_only", EvidenceStatus: "retail_storage_source",
		Entries: make([]Entry, 0, len(bank.Records)), References: make([]Reference, 0),
	}
	for _, record := range bank.Records {
		entry := Entry{
			Kind: "bank_record", MessageID: record.ID, Offset: record.Offset,
			OffsetBasis: "message_bank_byte_offset", Position: record.Index,
			Reachability: "unresolved", Path: make([]int, 0), Actors: make([]Actor, 0),
		}
		if handle, ok := ambientHandleForMessage(record.ID); ok {
			addRetailAmbientAssociation(bindata, &entry, AmbientInteraction{
				EntityHandle: handle, Status: "verified_executable_mapping",
				RuntimeStatus: "interaction_target_runtime_dependent", SourceLocator: ambientSourceLocator,
			})
		}
		scene.Entries = append(scene.Entries, entry)
	}
	if len(scene.Entries) > 0 {
		id := scene.Entries[0].MessageID
		scene.FirstRecordMessageID = &id
	}
	return scene
}

func (index *RetailIndex) buildReverseIndexes(catalog scenarioCatalog) {
	index.MessageScenes = make(map[int][]int)
	index.BankScenes = make(map[int][]int)
	index.SceneLookup = make(map[string][]int)
	rawAliasCandidates := make(map[string][]int)
	for sceneIndex, scene := range index.Scenes {
		assignSceneIdentity(&scene, catalog)
		index.Scenes[sceneIndex] = scene
		index.SceneLookup[scene.ID] = append(index.SceneLookup[scene.ID], sceneIndex)
		for _, alias := range scene.Aliases {
			index.SceneLookup[alias] = append(index.SceneLookup[alias], sceneIndex)
		}
		for _, alias := range sceneRawAliases(scene, catalog) {
			rawAliasCandidates[alias] = append(rawAliasCandidates[alias], sceneIndex)
		}
		banks := make(map[int]bool)
		messages := make(map[int]bool)
		for _, entry := range scene.Entries {
			if !messages[entry.MessageID] {
				index.MessageScenes[entry.MessageID] = append(index.MessageScenes[entry.MessageID], sceneIndex)
				messages[entry.MessageID] = true
			}
			bank := entry.MessageID / 10_000
			if !banks[bank] {
				index.BankScenes[bank] = append(index.BankScenes[bank], sceneIndex)
				banks[bank] = true
			}
		}
	}
	for alias, indexes := range rawAliasCandidates {
		if len(indexes) != 1 {
			// Keep collisions resolvable as deliberately ambiguous rather than
			// misreporting a known physical member as an unknown scene.
			index.SceneLookup[alias] = append(index.SceneLookup[alias], indexes...)
			continue
		}
		sceneIndex := indexes[0]
		index.Scenes[sceneIndex].Aliases = append(index.Scenes[sceneIndex].Aliases, alias)
		index.SceneLookup[alias] = append(index.SceneLookup[alias], sceneIndex)
	}
	for sceneIndex := range index.Scenes {
		sort.Strings(index.Scenes[sceneIndex].Aliases)
	}
}

func assignSceneIdentity(scene *Scene, catalog scenarioCatalog) {
	aliases := make([]string, 0)
	if scene.Scenario != nil {
		scene.ID = fmt.Sprintf("scenario/%d/%s", scene.Scenario.Slot, scene.Scenario.ContentSHA256[:12])
		family := catalog.families[scene.Scenario.Slot]
		for _, variant := range family.Variants {
			if variant.ContentSHA256 != scene.Scenario.ContentSHA256 {
				continue
			}
			for _, member := range variant.Members {
				aliases = append(aliases, member.SourceArchive+":"+member.Member)
			}
			break
		}
	} else if scene.SourceKind == "ambient_interaction" {
		scene.ID = "ambient/" + scene.SourceArchive + "/" + scene.Member + "#" + scene.EmbeddedMember
		aliases = append(aliases, scene.SourceArchive+":"+scene.Member+"#"+scene.EmbeddedMember)
	} else {
		scene.ID = "cdc/" + scene.SourceArchive + "/" + scene.Member
		aliases = append(aliases, scene.SourceArchive+":"+scene.Member)
	}
	sort.Strings(aliases)
	scene.Aliases = aliases[:0]
	for _, alias := range aliases {
		if alias != scene.ID && (len(scene.Aliases) == 0 || scene.Aliases[len(scene.Aliases)-1] != alias) {
			scene.Aliases = append(scene.Aliases, alias)
		}
	}
}

func sceneRawAliases(scene Scene, catalog scenarioCatalog) []string {
	if scene.Scenario != nil {
		family := catalog.families[scene.Scenario.Slot]
		aliases := make([]string, 0)
		for _, variant := range family.Variants {
			if variant.ContentSHA256 != scene.Scenario.ContentSHA256 {
				continue
			}
			for _, member := range variant.Members {
				aliases = append(aliases, member.Member)
			}
			return aliases
		}
		return nil
	}
	if scene.SourceKind == "ambient_interaction" {
		return []string{scene.Member + "#" + scene.EmbeddedMember}
	}
	return []string{scene.Member}
}

func (index *RetailIndex) validate() error {
	if index == nil || len(index.Bindata) == 0 || index.MessageScenes == nil || index.BankScenes == nil || index.SceneLookup == nil || index.StorageBanks == nil || index.ScenarioFamilies == nil || index.RoomMessageBankRegistrations == nil {
		return fmt.Errorf("cdc context: invalid retail index")
	}
	check := func(key int, indexes []int, byBank bool) error {
		prior := -1
		for _, sceneIndex := range indexes {
			if sceneIndex <= prior || sceneIndex < 0 || sceneIndex >= len(index.Scenes) {
				return fmt.Errorf("cdc context: invalid retail reverse index")
			}
			prior = sceneIndex
			found := false
			for _, entry := range index.Scenes[sceneIndex].Entries {
				found = found || (!byBank && entry.MessageID == key) || (byBank && entry.MessageID/10_000 == key)
			}
			if !found {
				return fmt.Errorf("cdc context: inconsistent retail reverse index")
			}
		}
		return nil
	}
	for id, indexes := range index.MessageScenes {
		if err := check(id, indexes, false); err != nil {
			return err
		}
	}
	for bank, indexes := range index.BankScenes {
		if bank < 0 || bank >= 279 {
			return fmt.Errorf("cdc context: invalid retail bank index")
		}
		if err := check(bank, indexes, true); err != nil {
			return err
		}
	}
	for bank, source := range index.StorageBanks {
		if bank < 0 || bank >= 279 || source.SourceArchive == "" || source.Member != fmt.Sprintf("message/msgsec%03d.dat", bank) {
			return fmt.Errorf("cdc context: invalid retail storage bank")
		}
	}
	for key, indexes := range index.SceneLookup {
		if key == "" || len(indexes) == 0 {
			return fmt.Errorf("cdc context: invalid retail scene lookup")
		}
		for _, sceneIndex := range indexes {
			if sceneIndex < 0 || sceneIndex >= len(index.Scenes) {
				return fmt.Errorf("cdc context: invalid retail scene lookup")
			}
		}
	}
	return nil
}

// BuildFromRetailIndex filters immutable retail facts before joining current
// contributor English and terminology.
func BuildFromRetailIndex(project *corpus.Project, terms fixeddata.Terminology, index *RetailIndex, selector Selector) (Result, error) {
	if project == nil || index == nil {
		return Result{}, fmt.Errorf("cdc context: project and retail index are required")
	}
	if err := index.validate(); err != nil {
		return Result{}, err
	}
	set := 0
	if selector.Bank >= 0 {
		set++
	}
	if selector.Record >= 0 {
		set++
	}
	if selector.Scene != "" {
		set++
	}
	if selector.ListScenes {
		set++
	}
	if set != 1 {
		return Result{}, fmt.Errorf("cdc context: set exactly one of bank, record, scene, or list scenes")
	}
	if selector.Bank >= 279 {
		return Result{}, fmt.Errorf("cdc context: invalid bank %d", selector.Bank)
	}
	if selector.Record >= 0 && selector.Record/10_000 >= 279 {
		return Result{}, fmt.Errorf("cdc context: invalid record %d", selector.Record)
	}
	bank := selector.Bank
	indexes := index.BankScenes[selector.Bank]
	if selector.ListScenes {
		indexes = make([]int, 0, len(index.Scenes))
		for sceneIndex := range index.Scenes {
			if len(index.Scenes[sceneIndex].Entries) > 0 {
				indexes = append(indexes, sceneIndex)
			}
		}
	}
	if selector.Record >= 0 {
		bank = selector.Record / 10_000
		indexes = index.MessageScenes[selector.Record]
	}
	if selector.Scene != "" {
		if bank, ok := storageSceneBank(selector.Scene); ok {
			return buildStorageSceneResult(project, terms, index, selector, bank)
		}
		for bank, source := range index.StorageBanks {
			if selector.Scene == source.SourceArchive+":"+source.Member {
				return buildStorageSceneResult(project, terms, index, selector, bank)
			}
		}
		matches, ok := index.SceneLookup[selector.Scene]
		if !ok {
			return Result{}, fmt.Errorf("cdc context: unknown scene %q", selector.Scene)
		}
		if len(matches) != 1 {
			return Result{}, fmt.Errorf("cdc context: ambiguous scene alias %q", selector.Scene)
		}
		indexes = matches
		if len(index.Scenes[matches[0]].Entries) > 0 {
			bank = index.Scenes[matches[0]].Entries[0].MessageID / 10_000
		}
	}
	result := Result{Selector: selector, Scenes: make([]Scene, 0, len(indexes)+1)}
	for _, sceneIndex := range indexes {
		scene := cloneRetailScene(index.Scenes[sceneIndex])
		if err := hydrateScene(project, terms, &scene); err != nil {
			return Result{}, err
		}
		markSelected(&scene, selector)
		result.Scenes = append(result.Scenes, scene)
	}
	if len(result.Scenes) > 0 && !selector.ListScenes {
		evidence := sourceEvidence(project, bank)
		for sceneIndex := range result.Scenes {
			result.Scenes[sceneIndex].SourceEvidence = append([]SourceEvidence(nil), evidence...)
		}
	}
	catalog := emptyScenarioCatalog()
	catalog.families = index.ScenarioFamilies
	result.ScenarioFamilies = scenarioFamiliesForScenes(catalog, result.Scenes)
	result.RoomMessageBankRegistrations = append([]RoomMessageBankRegistration(nil), index.RoomMessageBankRegistrations[bank]...)

	if !selector.ListScenes && selector.Scene == "" && (selector.Bank >= 0 || len(result.Scenes) == 0) {
		source, ok := index.StorageBanks[bank]
		if !ok {
			return Result{}, fmt.Errorf("cdc context: archive is missing message/msgsec%03d.dat", bank)
		}
		retailBank, err := corpus.ParseBank(fmt.Sprintf("msgsec%03d.dat", bank), source.Payload)
		if err != nil {
			return Result{}, fmt.Errorf("cdc context: %s: %w", source.Member, err)
		}
		scene := buildRetailBankScene(index.Bindata, source.SourceArchive, source.Member, retailBank)
		if err := hydrateScene(project, terms, &scene); err != nil {
			return Result{}, err
		}
		markSelected(&scene, selector)
		if selector.Bank >= 0 {
			result.Scenes = append([]Scene{scene}, result.Scenes...)
		} else {
			result.Scenes = append(result.Scenes, scene)
		}
	}
	return result, nil
}

func storageSceneBank(value string) (int, bool) {
	if len(value) != len("bank/000") || !strings.HasPrefix(value, "bank/") {
		return 0, false
	}
	bank, err := strconv.Atoi(value[len("bank/"):])
	return bank, err == nil && bank >= 0 && bank < 279
}

func buildStorageSceneResult(project *corpus.Project, terms fixeddata.Terminology, index *RetailIndex, selector Selector, bank int) (Result, error) {
	source, ok := index.StorageBanks[bank]
	if !ok {
		return Result{}, fmt.Errorf("cdc context: unknown scene %q", selector.Scene)
	}
	retailBank, err := corpus.ParseBank(fmt.Sprintf("msgsec%03d.dat", bank), source.Payload)
	if err != nil {
		return Result{}, fmt.Errorf("cdc context: %s: %w", source.Member, err)
	}
	scene := buildRetailBankScene(index.Bindata, source.SourceArchive, source.Member, retailBank)
	if err := hydrateScene(project, terms, &scene); err != nil {
		return Result{}, err
	}
	result := Result{Selector: selector, Scenes: []Scene{scene}}
	return result, nil
}

func cloneRetailScene(source Scene) Scene {
	result := source
	result.Aliases = append([]string(nil), source.Aliases...)
	result.Entries = append([]Entry(nil), source.Entries...)
	result.References = append([]Reference(nil), source.References...)
	result.SourceEvidence = nil
	for index := range result.Entries {
		result.Entries[index].Path = append([]int(nil), source.Entries[index].Path...)
		result.Entries[index].Conditions = append([]Condition(nil), source.Entries[index].Conditions...)
		result.Entries[index].Actors = append([]Actor(nil), source.Entries[index].Actors...)
	}
	return result
}

func hydrateScene(project *corpus.Project, terms fixeddata.Terminology, scene *Scene) error {
	for index := range scene.Entries {
		entry := &scene.Entries[index]
		item, ok := project.Find(entry.MessageID)
		if !ok {
			return fmt.Errorf("cdc context: %s contains unknown record %d", scene.Member, entry.MessageID)
		}
		controls, err := sourceControls(item.Translation.Japanese, item.Translation.Text)
		if err != nil {
			return fmt.Errorf("cdc context: message %d: %w", entry.MessageID, err)
		}
		entry.Japanese = item.Translation.Japanese
		entry.English = item.Translation.Text
		entry.State = item.Translation.State
		entry.Terminology = applicableTerms(terms.Applicable(item))
		entry.SourceControls = controls
		entry.Relationships = executableRelationships(project, entry.MessageID)
		entry.ConsumerEvidence = allConsumerEvidence(entry.MessageID)
		entry.AuthoringMetadata = authoringMetadata(entry.MessageID, item.Translation.Japanese)
		hydrateEntryAssociations(project, entry)
	}
	for referenceIndex := range scene.References {
		reference := &scene.References[referenceIndex]
		if reference.ScenarioRoomTable != nil {
			table := *reference.ScenarioRoomTable
			table.PossibleSlots = append([]int(nil), table.PossibleSlots...)
			reference.ScenarioRoomTable = &table
		}
	}
	if len(scene.Entries) > 0 && scene.SourceKind == "message_bank" {
		id := scene.Entries[0].MessageID
		scene.FirstRecordMessageID = &id
		scene.FirstRecordJapanese = scene.Entries[0].Japanese
		scene.FirstRecordEnglish = scene.Entries[0].English
		scene.SourceEvidence = sourceEvidence(project, id/10_000)
	}
	return nil
}

func hydrateEntryAssociations(project *corpus.Project, entry *Entry) {
	if entry.EntityAssociationHandleRaw != nil {
		retail := association{
			nameRecordID: entry.AssociationNameRecordID, labelMessageID: entry.AssociatedLabelMessageID,
			resolution: entry.AssociationResolution, speakerStatus: entry.SpeakerStatus,
		}
		resolved := hydrateAssociation(project, retail)
		entry.AssociationResolution = resolved.resolution
		entry.AssociatedLabelJapanese = resolved.labelJapanese
		entry.AssociatedLabelEnglish = resolved.labelEnglish
		if entry.AmbientInteraction != nil && (resolved.labelJapanese != "" || resolved.labelEnglish != "") {
			entry.SpeakerStatus = "inferred_from_verified_interaction_target"
			entry.SpeakerJapanese = resolved.labelJapanese
			entry.SpeakerEnglish = resolved.labelEnglish
			entry.SpeakerSource = "ambient_interaction_target_label"
		} else {
			entry.SpeakerStatus = resolved.speakerStatus
			entry.SpeakerJapanese = resolved.labelJapanese
			entry.SpeakerEnglish = resolved.labelEnglish
			if resolved.speakerStatus == "inferred_from_associated_label" {
				entry.SpeakerSource = "c5_associated_label"
			}
		}
	}
	for index := range entry.Actors {
		actor := &entry.Actors[index]
		resolved := hydrateAssociation(project, association{
			nameRecordID: actor.AssociationNameRecordID, labelMessageID: actor.AssociatedLabelMessageID,
			resolution: actor.AssociationLabelResolution, speakerStatus: "unresolved",
		})
		actor.AssociationLabelResolution = resolved.resolution
		actor.AssociatedLabelJapanese = resolved.labelJapanese
		actor.AssociatedLabelEnglish = resolved.labelEnglish
	}
	if entry.EntityAssociationHandleRaw != nil {
		entry.PossibleAddressees = possibleAddressees(entry.Actors, *entry.EntityAssociationHandleRaw)
	}
}
