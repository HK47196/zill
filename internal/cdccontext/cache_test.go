// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/HK47196/zill/internal/cdccontext"
	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/fixeddata"
)

func TestRetailIndexCacheLoadsWithoutReadingClosedArchives(t *testing.T) {
	pair := openPair(t, retailCacheFixtureMembers())
	archives := oneArchive(pair)
	cacheDirectory := filepath.Join(t.TempDir(), "context-cache")

	want, err := cdccontext.BuildRetailIndex(archives)
	if err != nil {
		t.Fatal(err)
	}
	first, err := cdccontext.LoadOrBuildRetailIndex(archives, cacheDirectory)
	if err != nil {
		t.Fatal(err)
	}
	assertSameRetailIndexJSON(t, first, want, "first persisted load")
	if err := pair.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := cdccontext.LoadOrBuildRetailIndex(archives, cacheDirectory)
	if err != nil {
		t.Fatalf("load compatible cache after archive Close: %v", err)
	}
	if len(got.Scenes) != len(want.Scenes) || len(got.Scenes) != 1 || got.Scenes[0].Member != want.Scenes[0].Member || got.Scenes[0].Entries[0].MessageID != 1350035 {
		t.Fatalf("cached retail index does not preserve its scene: %#v", got.Scenes)
	}
	if len(got.MessageScenes[1350035]) != 1 || got.MessageScenes[1350035][0] != 0 {
		t.Fatalf("cached message lookup = %#v", got.MessageScenes)
	}
	assertSameRetailIndexJSON(t, got, want, "cache hit")
}

func assertSameRetailIndexJSON(t *testing.T, got, want *cdccontext.RetailIndex, label string) {
	t.Helper()
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("%s and direct retail index have different public representations", label)
	}
}

func TestRetailIndexCacheRebuildsCorruptEntry(t *testing.T) {
	pair := openPair(t, retailCacheFixtureMembers())
	defer pair.Close()
	archives := oneArchive(pair)
	cacheDirectory := filepath.Join(t.TempDir(), "context-cache")

	want, err := cdccontext.LoadOrBuildRetailIndex(archives, cacheDirectory)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(cacheDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("cache entries = %d, want 1", len(entries))
	}
	cachePath := filepath.Join(cacheDirectory, entries[0].Name())
	if err := os.WriteFile(cachePath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := cdccontext.LoadOrBuildRetailIndex(archives, cacheDirectory)
	if err != nil {
		t.Fatalf("rebuild corrupt cache: %v", err)
	}
	if len(got.Scenes) != len(want.Scenes) || len(got.Scenes) != 1 || got.Scenes[0].Member != want.Scenes[0].Member {
		t.Fatal("rebuilt retail index differs from the original index")
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "corrupt" {
		t.Fatal("corrupt cache entry was not replaced")
	}
}

func TestRetailIndexCacheUsesCurrentTranslationEnglish(t *testing.T) {
	pair := openPair(t, retailCacheFixtureMembers())
	defer pair.Close()
	cacheDirectory := filepath.Join(t.TempDir(), "context-cache")
	index, err := cdccontext.LoadOrBuildRetailIndex(oneArchive(pair), cacheDirectory)
	if err != nil {
		t.Fatal(err)
	}
	index, err = cdccontext.LoadOrBuildRetailIndex(oneArchive(pair), cacheDirectory)
	if err != nil {
		t.Fatal(err)
	}
	project, _, err := corpus.LoadProject("../..")
	if err != nil {
		t.Fatal(err)
	}
	const currentEnglish = "English changed after the retail cache was written."
	found := false
	for itemIndex := range project.Items {
		if project.Items[itemIndex].Record.ID == 1350035 {
			project.Items[itemIndex].Translation.Text = currentEnglish
			project.Items[itemIndex].Translation.State = corpus.Translated
			found = true
			break
		}
	}
	if !found {
		t.Fatal("fixture translation record 1350035 is absent")
	}

	result, err := cdccontext.BuildFromRetailIndex(project, fixeddata.Terminology{}, index, cdccontext.Selector{Bank: -1, Record: 1350035})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scenes) != 1 || len(result.Scenes[0].Entries) != 1 || result.Scenes[0].Entries[0].English != currentEnglish {
		t.Fatalf("cached context English = %#v, want current project English %q", result.Scenes, currentEnglish)
	}
}

func retailCacheFixtureMembers() []fixtureMember {
	return []fixtureMember{
		{name: "data/bindata.dat", payload: make([]byte, 0x4000)},
		{name: "cdc/do/cache.cdc", payload: []byte("C5:3+2+1350035;E")},
	}
}
