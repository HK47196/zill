// SPDX-License-Identifier: GPL-3.0-or-later

// Package rbb reads the retail resource-catalog format used by res/res.rbb.
package rbb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	headerSize     = 16
	nullIndex      = ^uint32(0)
	scenarioSlots  = 914
	doResources    = 1184
	resourceHeader = 12
)

// Room is one room catalog entry. Resources preserves the catalog's authored
// child order and contains names such as ID0020_par and msgsec003_dat.
type Room struct {
	ID        int
	Resources []string
}

// Catalog is the validated portion of the RBB hierarchy used to name CDC
// resources and room-local resources. Names are retained byte-for-byte; a
// name may contain CP932 bytes and is not assumed to be UTF-8.
type Catalog struct {
	scenarios map[string][]string
	do        []string
	rooms     map[int]Room
}

// Parse validates an RBB catalog and extracts its CDC and room hierarchies.
// It fails closed: a catalog which has an invalid header, out-of-bounds index,
// malformed record, duplicate ID, or incomplete expected CDC hierarchy is
// rejected rather than partially returned.
func Parse(data []byte) (*Catalog, error) {
	offsets, indexLimit, err := validateHeader(data)
	if err != nil {
		return nil, err
	}
	records, err := parseRecords(data, offsets, indexLimit)
	if err != nil {
		return nil, err
	}

	catalog := &Catalog{
		scenarios: make(map[string][]string, 13),
		rooms:     make(map[int]Room),
	}
	for _, group := range scenarioGroups() {
		entries, err := exactResourceChildren(records, group, scenarioSlots)
		if err != nil {
			return nil, fmt.Errorf("RBB scenario group %q: %w", group, err)
		}
		catalog.scenarios[group] = entries
	}
	do, err := exactResourceChildren(records, "cdcDo", doResources)
	if err != nil {
		return nil, fmt.Errorf("RBB Do group: %w", err)
	}
	catalog.do = do

	rooms, err := roomChildren(records)
	if err != nil {
		return nil, err
	}
	catalog.rooms = rooms
	return catalog, nil
}

// Scenario returns the authored resource name for one exact scenario group
// and slot. It returns false for an invalid group or a slot outside 1..914.
func (c *Catalog) Scenario(group string, slot int) (string, bool) {
	entries, ok := c.scenarios[group]
	if !ok || slot < 1 || slot > len(entries) {
		return "", false
	}
	return entries[slot-1], true
}

// Do returns the authored resource name for one cdcDo ID. It returns false
// for an ID outside the validated 1..1184 hierarchy.
func (c *Catalog) Do(id int) (string, bool) {
	if id < 1 || id > len(c.do) {
		return "", false
	}
	return c.do[id-1], true
}

// Room returns a copy of a room's authored child resource names, in catalog
// order. It returns false when the catalog contains no such room.
func (c *Catalog) Room(id int) (Room, bool) {
	room, ok := c.rooms[id]
	if !ok {
		return Room{}, false
	}
	room.Resources = append([]string(nil), room.Resources...)
	return room, true
}

// Rooms returns every room entry in ascending room-ID order. Returned resource
// slices are independent of the catalog.
func (c *Catalog) Rooms() []Room {
	ids := make([]int, 0, len(c.rooms))
	for id := range c.rooms {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	rooms := make([]Room, 0, len(ids))
	for _, id := range ids {
		room, _ := c.Room(id)
		rooms = append(rooms, room)
	}
	return rooms
}

type record struct {
	offset int
	end    int
	dir    bool
	name   string
}

func validateHeader(data []byte) ([]uint32, uint32, error) {
	if len(data) < headerSize || !bytes.Equal(data[:4], []byte("RBB ")) {
		return nil, 0, errors.New("not an RBB catalog")
	}
	sectionOffset := binary.LittleEndian.Uint32(data[4:8])
	count := binary.LittleEndian.Uint32(data[8:12])
	tableOffset := uint64(binary.LittleEndian.Uint32(data[12:16]))
	if sectionOffset != headerSize || count <= 1 {
		return nil, 0, errors.New("invalid RBB header")
	}
	tableEnd := tableOffset + uint64(count)*4
	if tableEnd > uint64(len(data)) {
		return nil, 0, errors.New("RBB index table extends past end of file")
	}
	offsets := make([]uint32, count)
	for index := range offsets {
		offsets[index] = binary.LittleEndian.Uint32(data[tableOffset+uint64(index)*4:])
	}
	if uint64(offsets[0])+16 != tableEnd || uint64(offsets[1]) != tableEnd {
		return nil, 0, errors.New("RBB index-table sentinel is invalid")
	}
	previous := uint64(offsets[1])
	for index := 2; index < len(offsets); index++ {
		offset := uint64(offsets[index])
		if offset < uint64(tableEnd) || offset >= uint64(len(data)) || offset <= previous {
			return nil, 0, fmt.Errorf("RBB index-table entry %d is outside order or bounds", index)
		}
		previous = offset
	}
	return offsets, count, nil
}

func parseRecords(data []byte, offsets []uint32, indexLimit uint32) ([]record, error) {
	position := int(offsets[1])
	records := make([]record, 0, len(offsets))
	for index := 0; index < len(offsets); index++ {
		record, err := parseRecord(data, position, indexLimit)
		if err != nil {
			return nil, fmt.Errorf("RBB descriptor %d: %w", index, err)
		}
		records = append(records, record)
		position = record.end
	}
	if position != len(data) {
		return nil, errors.New("RBB descriptor stream does not end at end of file")
	}
	return records, nil
}

func parseRecord(data []byte, offset int, indexLimit uint32) (record, error) {
	if offset+resourceHeader > len(data) {
		return record{}, errors.New("truncated header")
	}
	kind, length := data[offset], int(data[offset+1])
	if length == 0 || kind > 1 {
		return record{}, errors.New("invalid record kind or name length")
	}
	first := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
	second := binary.LittleEndian.Uint32(data[offset+8 : offset+12])
	if kind == 0 {
		if data[offset+2] != 0 || data[offset+3] != 0 || first >= indexLimit {
			return record{}, errors.New("invalid directory indexes")
		}
	} else if binary.LittleEndian.Uint16(data[offset+2:offset+4]) >= uint16(indexLimit) || first != nullIndex {
		return record{}, errors.New("invalid resource indexes")
	}
	if second != nullIndex && second >= indexLimit {
		return record{}, errors.New("invalid next-record index")
	}
	nameStart := offset + resourceHeader
	nameEnd := nameStart + length
	if nameEnd > len(data) || data[nameEnd-1] == 0 {
		return record{}, errors.New("truncated name")
	}
	end := aligned(nameEnd, 4)
	if end > len(data) || !allZero(data[nameEnd:end]) {
		return record{}, errors.New("invalid record extent or padding")
	}
	return record{offset: offset, end: end, dir: kind == 0, name: string(data[nameStart:nameEnd])}, nil
}

func exactResourceChildren(records []record, parent string, expected int) ([]string, error) {
	candidates := indexes(records, func(record record) bool { return record.dir && record.name == parent })
	if len(candidates) == 0 {
		return nil, errors.New("missing hierarchy")
	}
	var found []string
	for _, candidate := range candidates {
		entries, ok := numberedResourceChildren(records, candidate, expected)
		if !ok {
			continue
		}
		if found != nil {
			return nil, errors.New("duplicate complete hierarchy")
		}
		found = entries
	}
	if found == nil {
		return nil, fmt.Errorf("expected ID0001 through ID%04d with one resource each", expected)
	}
	return found, nil
}

func numberedResourceChildren(records []record, parent, expected int) ([]string, bool) {
	position := parent + 1
	entries := make([]string, 0, expected)
	for id := 1; id <= expected; id++ {
		if position >= len(records) || !records[position].dir || records[position].name != fmt.Sprintf("ID%04d", id) {
			return nil, false
		}
		position++
		if position >= len(records) || records[position].dir {
			return nil, false
		}
		entries = append(entries, records[position].name)
		position++
	}
	if position < len(records) && !records[position].dir {
		return nil, false
	}
	return entries, true
}

func roomChildren(records []record) (map[int]Room, error) {
	candidates := indexes(records, func(record record) bool { return record.dir && record.name == "room" })
	if len(candidates) == 0 {
		return nil, errors.New("RBB room hierarchy is missing")
	}
	var found map[int]Room
	for _, candidate := range candidates {
		rooms, ok := numberedRooms(records, candidate)
		if !ok {
			continue
		}
		if found != nil {
			return nil, errors.New("duplicate complete room hierarchy")
		}
		found = rooms
	}
	if found == nil {
		return nil, errors.New("RBB room hierarchy has invalid child shape")
	}
	return found, nil
}

func numberedRooms(records []record, parent int) (map[int]Room, bool) {
	position := parent + 1
	rooms := make(map[int]Room)
	previousID := 0
	for position < len(records) {
		if !records[position].dir {
			break
		}
		id, ok := resourceID(records[position].name)
		if !ok {
			if strings.HasPrefix(records[position].name, "ID") {
				return nil, false
			}
			break
		}
		if id <= previousID {
			return nil, false
		}
		position++
		var names []string
		for position < len(records) && !records[position].dir {
			names = append(names, records[position].name)
			position++
		}
		if len(names) == 0 {
			return nil, false
		}
		rooms[id] = Room{ID: id, Resources: names}
		previousID = id
	}
	return rooms, len(rooms) > 0
}

func resourceID(name string) (int, bool) {
	if len(name) != 6 || name[:2] != "ID" {
		return 0, false
	}
	id, err := strconv.Atoi(name[2:])
	return id, err == nil
}

func indexes(records []record, match func(record) bool) []int {
	var result []int
	for index, record := range records {
		if match(record) {
			result = append(result, index)
		}
	}
	return result
}

func scenarioGroups() []string {
	groups := make([]string, 0, 13)
	for id := 1; id <= 6; id++ {
		groups = append(groups, fmt.Sprintf("cdc%02d", id))
	}
	for id := 1; id <= 7; id++ {
		groups = append(groups, "cdcV"+strconv.FormatInt(int64(id), 10))
	}
	return groups
}

func aligned(value, alignment int) int {
	return (value + alignment - 1) &^ (alignment - 1)
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}
