// SPDX-License-Identifier: GPL-3.0-or-later

package corpus

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTrackedProjectPassesContributorFoundationChecks(t *testing.T) {
	_, _, err := LoadProject("../..")
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
}

func TestLoadProjectAcceptsMessageComments(t *testing.T) {
	sourceDir := filepath.Join("..", "..", "translations", "messages")
	root := t.TempDir()
	targetDir := filepath.Join(root, "translations", "messages")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		source := filepath.Join(sourceDir, entry.Name())
		target := filepath.Join(targetDir, entry.Name())
		if entry.Name() != "msgsec000.toml" {
			absolute, err := filepath.Abs(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(absolute, target); err != nil {
				t.Fatal(err)
			}
			continue
		}
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		withComment := bytes.Replace(data, []byte("\n[\"0\"]\n"), []byte("\n# Verified buffer note.\n[\"0\"]\n"), 1)
		if bytes.Equal(withComment, data) {
			t.Fatal("failed to add test comment")
		}
		if err := os.WriteFile(target, withComment, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, _, err := LoadProject(root); err != nil {
		t.Fatalf("LoadProject rejected a message comment: %v", err)
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
