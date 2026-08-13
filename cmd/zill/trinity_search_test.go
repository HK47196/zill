// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTrinitySearchOptionsDiscoverProjectBuildOutputs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	options, err := parseTrinitySearchOptions(root, []string{"Areus"})
	if err != nil {
		t.Fatal(err)
	}
	if options.english != filepath.Join(root, "build", "trinity", "english") || options.japanese != filepath.Join(root, "build", "trinity", "japanese") {
		t.Fatalf("automatic directories = %q and %q", options.english, options.japanese)
	}
	if options.language != trinitySearchEnglish || options.pattern != "Areus" {
		t.Fatalf("default search options = %#v", options)
	}
}

func TestTrinitySearchRejectsUnsupportedLanguage(t *testing.T) {
	if _, err := parseTrinitySearchOptions(t.TempDir(), []string{"--language", "klingon", "query"}); err == nil || !strings.Contains(err.Error(), "english or japanese") {
		t.Fatalf("unsupported language error = %v", err)
	}
}

func TestTrinitySearchPreservesRawRegexTextAcrossNewlines(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not installed")
	}
	pairs := []trinityStringPair{
		{Member: 6, Path: []int{1, 0}, English: "first \"quote\" \\ path\nline two", Japanese: "対応する日本語"},
		{Member: 6, Path: []int{1, 1}, English: "another record", Japanese: "別の行"},
	}
	matches, err := searchTrinityPairsWithRG(pairs, trinitySearchOptions{pattern: `(?s)^first "quote" \\ path.line two$`, language: trinitySearchEnglish, maxCount: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || !slices.Equal(matches[0].Path, []int{1, 0}) || matches[0].Japanese != "対応する日本語" {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestTrinitySearchJapaneseDisplaysEnglishCounterpart(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not installed")
	}
	pairs := []trinityStringPair{
		{Member: 6, Path: []int{0, 1, 0}, English: "Areus! A fine match.", Japanese: "アレウス！\nいい試合だったな"},
		{Member: 14, Path: []int{4, 0}, English: "One Year Earlier", Japanese: "一年前"},
	}
	options := trinitySearchOptions{pattern: "アレウス", language: trinitySearchJapanese, maxCount: 50}
	matches, err := searchTrinityPairsWithRG(pairs, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || !slices.Equal(matches[0].Path, []int{0, 1, 0}) {
		t.Fatalf("Japanese matches = %#v", matches)
	}
	var output bytes.Buffer
	writeTrinitySearchMatches(&output, matches, options.language)
	japaneseAt := strings.Index(output.String(), "Japanese: アレウス！")
	englishAt := strings.Index(output.String(), "English: Areus! A fine match.")
	if japaneseAt < 0 || englishAt < 0 || japaneseAt >= englishAt {
		t.Fatalf("Japanese search output order = %q", output.String())
	}
}
