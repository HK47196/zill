// SPDX-License-Identifier: GPL-3.0-or-later

package rbb_test

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/HK47196/zill/internal/gamefmt/rbb"
)

func TestParseExposesCatalogHierarchyAndPreservesChildOrder(t *testing.T) {
	catalog, err := rbb.Parse(catalogFixture(t, false, false, false))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, ok := catalog.Scenario("cdc01", 20); !ok || got != "scenario_cdc01_0020" {
		t.Fatalf("Scenario(cdc01, 20) = %q, %t; want exact authored name", got, ok)
	}
	if _, ok := catalog.Scenario("cdc01", 915); ok {
		t.Fatal("Scenario accepted an out-of-range slot")
	}
	if got, ok := catalog.Do(1184); !ok || got != "do_1184" {
		t.Fatalf("Do(1184) = %q, %t; want exact authored name", got, ok)
	}
	room, ok := catalog.Room(2)
	if !ok {
		t.Fatal("Room(2) is missing")
	}
	want := []string{"ID0002_par", "msgsec003_dat", "msgsec149_dat"}
	if strings.Join(room.Resources, "|") != strings.Join(want, "|") {
		t.Fatalf("Room(2).Resources = %q, want %q", room.Resources, want)
	}
	room.Resources[0] = "changed"
	again, _ := catalog.Room(2)
	if again.Resources[0] != want[0] {
		t.Fatal("Room returned catalog-owned resource storage")
	}
	if room, ok := catalog.Room(1047); !ok || strings.Join(room.Resources, "|") != "ID1047_par" {
		t.Fatalf("Room(1047) = %#v, %t; want sparse catalog room", room, ok)
	}
}

func TestParseRejectsTruncatedAndStructurallyInvalidCatalogs(t *testing.T) {
	valid := catalogFixture(t, false, false, false)
	if _, err := rbb.Parse(valid[:len(valid)/2]); err == nil {
		t.Fatal("Parse accepted truncated catalog")
	}
	if _, err := rbb.Parse(catalogFixture(t, true, false, false)); err == nil {
		t.Fatal("Parse accepted duplicate scenario ID hierarchy")
	}
	if _, err := rbb.Parse(catalogFixture(t, false, true, false)); err == nil {
		t.Fatal("Parse accepted duplicate room ID hierarchy")
	}
	if _, err := rbb.Parse(catalogFixture(t, false, false, true)); err == nil {
		t.Fatal("Parse accepted a duplicate resource under the terminal scenario ID")
	}
	corruptOffset := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint32(corruptOffset[20:24], uint32(len(corruptOffset)))
	if _, err := rbb.Parse(corruptOffset); err == nil {
		t.Fatal("Parse accepted an out-of-bounds record offset")
	}
}

func catalogFixture(t *testing.T, duplicateScenarioID, duplicateRoomID, duplicateTerminalResource bool) []byte {
	t.Helper()
	var records [][]byte
	appendDirectory := func(name string) { records = append(records, encodedRecord(0, name)) }
	appendResource := func(name string) { records = append(records, encodedRecord(1, name)) }
	for _, group := range fixtureScenarioGroups() {
		appendDirectory(group)
		for id := 1; id <= 914; id++ {
			nameID := id
			if duplicateScenarioID && group == "cdc01" && id == 2 {
				nameID = 1
			}
			appendDirectory("ID" + four(nameID))
			appendResource("scenario_" + group + "_" + four(id))
			if duplicateTerminalResource && group == "cdc01" && id == 914 {
				appendResource("duplicate_terminal_resource")
			}
		}
	}
	appendDirectory("cdcDo")
	for id := 1; id <= 1184; id++ {
		appendDirectory("ID" + four(id))
		appendResource("do_" + four(id))
	}
	appendDirectory("room")
	appendDirectory("ID0001")
	appendResource("ID0001_par")
	roomTwoID := "ID0002"
	if duplicateRoomID {
		roomTwoID = "ID0001"
	}
	appendDirectory(roomTwoID)
	appendResource("ID0002_par")
	appendResource("msgsec003_dat")
	appendResource("msgsec149_dat")
	appendDirectory("ID1047")
	appendResource("ID1047_par")

	const tableOffset = 16
	data := make([]byte, tableOffset+len(records)*4)
	copy(data, "RBB ")
	binary.LittleEndian.PutUint32(data[4:8], 16)
	binary.LittleEndian.PutUint32(data[8:12], uint32(len(records)))
	binary.LittleEndian.PutUint32(data[12:16], tableOffset)
	binary.LittleEndian.PutUint32(data[tableOffset:], uint32(len(data)-16))
	binary.LittleEndian.PutUint32(data[tableOffset+4:], uint32(len(data)))
	for index, record := range records {
		data = append(data, record...)
		if index+2 < len(records) {
			binary.LittleEndian.PutUint32(data[tableOffset+(index+2)*4:], uint32(len(data)))
		}
	}
	return data
}

func encodedRecord(kind byte, name string) []byte {
	header := make([]byte, 12)
	header[0] = kind
	header[1] = byte(len(name))
	if kind == 0 {
		binary.LittleEndian.PutUint32(header[4:8], 1)
	} else {
		binary.LittleEndian.PutUint16(header[2:4], 1)
		binary.LittleEndian.PutUint32(header[4:8], 0xffffffff)
	}
	binary.LittleEndian.PutUint32(header[8:12], 0xffffffff)
	record := append(header, name...)
	for len(record)%4 != 0 {
		record = append(record, 0)
	}
	return record
}

func four(value int) string {
	return fmt.Sprintf("%04d", value)
}

func fixtureScenarioGroups() []string {
	groups := make([]string, 0, 13)
	for id := 1; id <= 6; id++ {
		groups = append(groups, fmt.Sprintf("cdc%02d", id))
	}
	for id := 1; id <= 7; id++ {
		groups = append(groups, fmt.Sprintf("cdcV%d", id))
	}
	return groups
}
