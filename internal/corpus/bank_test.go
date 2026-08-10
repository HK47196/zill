// SPDX-License-Identifier: GPL-3.0-or-later

package corpus

import (
	"bytes"
	"testing"
)

func TestParseBankProducesStableDisplayRecords(t *testing.T) {
	data := []byte{1, 0, 4, 0, 0x8f, 0xc1, 5, 5, 5, 0}
	before := bytes.Clone(data)

	bank, err := ParseBank("msgsec002.dat", data)
	if err != nil {
		t.Fatalf("ParseBank: %v", err)
	}
	if bank.Section != 2 || len(bank.Records) != 1 {
		t.Fatalf("bank section/count = %d/%d, want 2/1", bank.Section, len(bank.Records))
	}
	if got := bank.Records[0]; got.ID != 20000 || got.Display != "消<end>" {
		t.Fatalf("first record = ID %d, %q", got.ID, got.Display)
	}
	if !bytes.Equal(data, before) {
		t.Fatal("ParseBank mutated its input")
	}
}

func TestParseBankRejectsOffsetInsideTable(t *testing.T) {
	data := []byte{1, 0, 3, 0}
	if _, err := ParseBank("msgsec000.dat", data); err == nil {
		t.Fatal("ParseBank accepted an offset inside its table")
	}
}
