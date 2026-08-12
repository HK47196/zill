// SPDX-License-Identifier: GPL-3.0-or-later

package fixeddata

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/pelletier/go-toml/v2"
)

const (
	terminologyVersion = 2
)

// Term is one compact contributor-facing terminology authority.
type Term struct {
	Japanese  string `toml:"japanese"`
	English   string `toml:"english"`
	Scope     string `toml:"scope"`
	SourceIDs []int  `toml:"source_ids,omitempty"`
}

// Terminology is the canonical name and glossary authority.
type Terminology struct {
	Names    []Term
	Glossary []Term
}

// SearchEntry identifies a terminology search result and its authority kind.
type SearchEntry struct {
	Kind string
	Term Term
}

type terminologyFile struct {
	Format  string `toml:"format"`
	Version int    `toml:"version"`
	Entries []Term `toml:"entry"`
}

// ParseTerminology strictly loads the native compact name and glossary TOML files.
func ParseTerminology(namesTOML, glossaryTOML []byte) (Terminology, error) {
	names, err := parseTerminologyFile(namesTOML, "zill-names", true)
	if err != nil {
		return Terminology{}, fmt.Errorf("names terminology: %w", err)
	}
	glossary, err := parseTerminologyFile(glossaryTOML, "zill-glossary", false)
	if err != nil {
		return Terminology{}, fmt.Errorf("glossary terminology: %w", err)
	}
	return Terminology{Names: names, Glossary: glossary}, nil
}

func parseTerminologyFile(data []byte, format string, scoped bool) ([]Term, error) {
	var file terminologyFile
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("invalid TOML: %w", err)
	}
	if file.Format != format || file.Version != terminologyVersion {
		return nil, fmt.Errorf("unsupported terminology identity")
	}
	if len(file.Entries) == 0 {
		return nil, fmt.Errorf("contains no entries")
	}
	for i, term := range file.Entries {
		if term.Japanese == "" || term.English == "" {
			return nil, fmt.Errorf("entry %d requires Japanese and English", i+1)
		}
		switch term.Scope {
		case "global_surface":
			if len(term.SourceIDs) != 0 {
				return nil, fmt.Errorf("entry %d (%q) global scope cannot carry source IDs", i+1, term.Japanese)
			}
		case "source_records", "speaker_label":
			if !scoped || len(term.SourceIDs) == 0 {
				return nil, fmt.Errorf("entry %d (%q) has invalid scope %q", i+1, term.Japanese, term.Scope)
			}
			prior := -1
			for _, id := range term.SourceIDs {
				if id <= prior {
					return nil, fmt.Errorf("entry %d (%q) source IDs must be positive, unique, and sorted", i+1, term.Japanese)
				}
				prior = id
			}
		default:
			return nil, fmt.Errorf("entry %d (%q) has invalid scope %q", i+1, term.Japanese, term.Scope)
		}
	}
	return file.Entries, nil
}

// Search returns case-insensitive substring matches in stable file order.
func (t Terminology) Search(query string) []SearchEntry {
	query = strings.ToLower(strings.TrimSpace(query))
	results := make([]SearchEntry, 0)
	appendMatches := func(kind string, terms []Term) {
		for _, term := range terms {
			haystack := strings.ToLower(term.Japanese + "\n" + term.English)
			if query == "" || strings.Contains(haystack, query) {
				results = append(results, SearchEntry{Kind: kind, Term: term})
			}
		}
	}
	appendMatches("name", t.Names)
	appendMatches("glossary", t.Glossary)
	return results
}

// Applicable returns exact scoped authorities and advisory global terms whose
// Japanese surface occurs in this source record. It does not infer speakers.
func (t Terminology) Applicable(item corpus.Item) []SearchEntry {
	var results []SearchEntry
	appendApplicable := func(kind string, terms []Term) {
		for _, term := range terms {
			matches := term.Scope == "global_surface" && strings.Contains(item.Record.Display, term.Japanese)
			if term.Scope != "global_surface" {
				index := sort.SearchInts(term.SourceIDs, item.Record.ID)
				matches = index < len(term.SourceIDs) && term.SourceIDs[index] == item.Record.ID
			}
			if matches {
				results = append(results, SearchEntry{Kind: kind, Term: term})
			}
		}
	}
	appendApplicable("name", t.Names)
	appendApplicable("glossary", t.Glossary)
	return results
}

// Validate enforces only exact record-scoped and speaker-label authorities.
// Global terminology remains contributor guidance rather than a build gate.
func (t Terminology) Validate(project *corpus.Project) error {
	items := make(map[int]corpus.Item, len(project.Items))
	for _, item := range project.Items {
		items[item.Record.ID] = item
	}
	for termIndex, term := range t.Names {
		if term.Scope == "global_surface" {
			continue
		}
		for _, id := range term.SourceIDs {
			item, ok := items[id]
			if !ok {
				return fmt.Errorf("name entry %d (%q) references absent source ID %d", termIndex+1, term.Japanese, id)
			}
			wantSource := term.Japanese + "<end>"
			if item.Record.Display != wantSource {
				return fmt.Errorf("name entry %d (%q) source ID %d Japanese is %q, want %q", termIndex+1, term.Japanese, id, item.Record.Display, wantSource)
			}
			if item.Translation.State != corpus.Translated {
				continue
			}
			want := term.English + "<end>"
			if item.Translation.Text != want {
				return fmt.Errorf("name entry %d (%q) source ID %d translation is %q, want %q", termIndex+1, term.Japanese, id, item.Translation.Text, want)
			}
		}
	}
	return nil
}
