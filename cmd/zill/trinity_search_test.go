// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os/exec"
	"slices"
	"testing"
)

func TestTrinitySearchPreservesRawRegexTextAcrossNewlines(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not installed")
	}
	pairs := []trinityStringPair{
		{Member: 6, Path: []int{1, 0}, English: "first \"quote\" \\ path\nline two", Japanese: "対応する日本語"},
		{Member: 6, Path: []int{1, 1}, English: "another record", Japanese: "別の行"},
	}
	matches, err := searchTrinityPairsWithRG(pairs, trinitySearchOptions{pattern: `(?s)^first "quote" \\ path.line two$`, maxCount: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || !slices.Equal(matches[0].Path, []int{1, 0}) || matches[0].Japanese != "対応する日本語" {
		t.Fatalf("matches = %#v", matches)
	}
}
