// SPDX-License-Identifier: GPL-3.0-or-later

package fixeddata

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/HK47196/zill/internal/corpus"
)

func TestAuthoritativeFixedDataParsesWithCompleteCoverage(t *testing.T) {
	tests := []struct {
		path  string
		parse func([]byte) error
	}{
		{"../../release/strings/eboot.toml", func(data []byte) error {
			_, err := ParseEBOOT(data)
			return err
		}},
		{"../../release/strings/equipment.toml", func(data []byte) error {
			translations, err := ParseEquipment(data)
			if err == nil && len(translations) != 132 {
				t.Fatalf("equipment has %d names, want 132", len(translations))
			}
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			data, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.parse(data); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestApplyFixedDataRejectsUnauthenticatedSourcesWithoutOutput(t *testing.T) {
	eboot, err := ParseEBOOT(mustRead(t, "../../release/strings/eboot.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if output, err := ApplyEBOOT([]byte("not the retail ELF"), eboot); err == nil || output != nil {
		t.Fatalf("ApplyEBOOT returned output %x, error %v", output, err)
	}
	equipment, err := ParseEquipment(mustRead(t, "../../release/strings/equipment.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if output, err := ApplyEquipment([]byte("not retail bindata"), equipment); err == nil || output != nil {
		t.Fatalf("ApplyEquipment returned output %x, error %v", output, err)
	}
}

func TestParseEBOOTRejectsReplacementBeyondFixedFieldCapacity(t *testing.T) {
	authority := mustRead(t, "../../release/strings/eboot.toml")
	authority = bytes.Replace(authority, []byte(`replacement = "Age Tea"`), []byte(`replacement = "This replacement cannot fit"`), 1)
	if _, err := ParseEBOOT(authority); err == nil {
		t.Fatal("ParseEBOOT accepted a replacement beyond its source field capacity")
	}
}

func TestTerminologySearchIsStableAndRetainsSpellingVariant(t *testing.T) {
	terms, err := ParseTerminology(
		mustRead(t, "../../translations/terminology/names.toml"),
		mustRead(t, "../../translations/terminology/glossary.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	first := terms.Search("Aqyurius")
	second := terms.Search("Aqyurius")
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical terminology searches are not stable")
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].Term.Key >= first[i].Term.Key {
			t.Fatalf("search results are not in stable key order: %#v", first)
		}
	}
	variants := terms.Search("アキュリュ－ス")
	if len(variants) != 1 || variants[0].Term.Key != "name_0930" || variants[0].Term.English != "Aqyurius" {
		t.Fatalf("Aqyurius spelling variant missing from search: %#v", variants)
	}
}

func TestTerminologyRejectsExactScopeTranslationMismatch(t *testing.T) {
	terms := Terminology{Names: []Term{{Key: "name_test", Japanese: "名前", English: "Name", Scope: "source_records", SourceIDs: []int{7}}}}
	project := &corpus.Project{Items: []corpus.Item{{
		Record:      corpus.Record{ID: 7, Display: "名前<end>"},
		Translation: corpus.Translation{ID: 7, State: corpus.Translated, Text: "Wrong<end>"},
	}}}
	if err := terms.Validate(project); err == nil {
		t.Fatal("Validate accepted an exact-scope name with the wrong translation")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
