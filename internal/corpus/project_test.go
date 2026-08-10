// SPDX-License-Identifier: GPL-3.0-or-later

package corpus

import (
	"bytes"
	"testing"
)

func TestTrackedProjectPassesContributorFoundationChecks(t *testing.T) {
	_, _, err := LoadProject("../..")
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
}

func TestBindBanksAuthenticatesBeforeMutatingProject(t *testing.T) {
	project, _, err := LoadProject("../..")
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	banks := make([]Bank, 279)
	for _, item := range project.Items {
		section := item.Record.ID / 10_000
		banks[section].Section = section
		banks[section].Records = append(banks[section].Records, Record{
			ID: item.Record.ID, Index: item.Record.Index, Display: item.Record.Display, Raw: []byte{1},
		})
	}
	banks[278].Records[len(banks[278].Records)-1].Display = "different"
	if err := BindBanks(project, banks); err == nil {
		t.Fatal("BindBanks accepted mismatched retail Japanese")
	}
	if len(project.Items[0].Record.Raw) != 0 {
		t.Fatal("BindBanks mutated project after a failed authentication")
	}
	banks[278].Records[len(banks[278].Records)-1].Display = project.Items[len(project.Items)-1].Translation.Japanese
	if err := BindBanks(project, banks); err != nil {
		t.Fatalf("BindBanks: %v", err)
	}
	if !bytes.Equal(project.Items[0].Record.Raw, []byte{1}) {
		t.Fatal("BindBanks did not replace placeholders with retail records")
	}
}
