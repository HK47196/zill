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
	terminologyVersion = 1
)

// Term is one compact contributor-facing terminology authority.
type Term struct {
	Key       string `toml:"key"`
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
	names, err := parseTerminologyFile(namesTOML, "zill-names", "name_", true)
	if err != nil {
		return Terminology{}, fmt.Errorf("names terminology: %w", err)
	}
	glossary, err := parseTerminologyFile(glossaryTOML, "zill-glossary", "term_", false)
	if err != nil {
		return Terminology{}, fmt.Errorf("glossary terminology: %w", err)
	}
	return Terminology{Names: names, Glossary: glossary}, nil
}

func parseTerminologyFile(data []byte, format, keyPrefix string, scoped bool) ([]Term, error) {
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
	previous := ""
	for i, term := range file.Entries {
		if !strings.HasPrefix(term.Key, keyPrefix) || term.Key <= previous {
			return nil, fmt.Errorf("entry %d has invalid or unordered key %q", i+1, term.Key)
		}
		if term.Japanese == "" || term.English == "" {
			return nil, fmt.Errorf("entry %q requires Japanese and English", term.Key)
		}
		switch term.Scope {
		case "global_surface":
			if len(term.SourceIDs) != 0 {
				return nil, fmt.Errorf("entry %q global scope cannot carry source IDs", term.Key)
			}
		case "source_records", "speaker_label":
			if !scoped || len(term.SourceIDs) == 0 {
				return nil, fmt.Errorf("entry %q has invalid scope %q", term.Key, term.Scope)
			}
			prior := -1
			for _, id := range term.SourceIDs {
				if id <= prior {
					return nil, fmt.Errorf("entry %q source IDs must be positive, unique, and sorted", term.Key)
				}
				prior = id
			}
		default:
			return nil, fmt.Errorf("entry %q has invalid scope %q", term.Key, term.Scope)
		}
		previous = term.Key
	}
	return file.Entries, nil
}

// Search returns case-insensitive substring matches in stable key order.
func (t Terminology) Search(query string) []SearchEntry {
	query = strings.ToLower(strings.TrimSpace(query))
	results := make([]SearchEntry, 0)
	appendMatches := func(kind string, terms []Term) {
		for _, term := range terms {
			haystack := strings.ToLower(term.Key + "\n" + term.Japanese + "\n" + term.English)
			if query == "" || strings.Contains(haystack, query) {
				results = append(results, SearchEntry{Kind: kind, Term: term})
			}
		}
	}
	appendMatches("name", t.Names)
	appendMatches("glossary", t.Glossary)
	sort.Slice(results, func(i, j int) bool { return results[i].Term.Key < results[j].Term.Key })
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
	sort.Slice(results, func(i, j int) bool { return results[i].Term.Key < results[j].Term.Key })
	return results
}

// Validate enforces only exact record-scoped and speaker-label authorities.
// Global terminology remains contributor guidance rather than a build gate.
func (t Terminology) Validate(project *corpus.Project) error {
	items := make(map[int]corpus.Item, len(project.Items))
	for _, item := range project.Items {
		items[item.Record.ID] = item
	}
	for _, term := range t.Names {
		if term.Scope == "global_surface" {
			continue
		}
		for _, id := range term.SourceIDs {
			item, ok := items[id]
			if !ok {
				return fmt.Errorf("terminology %s references absent source ID %d", term.Key, id)
			}
			wantSource := term.Japanese + "<end>"
			if item.Record.Display != wantSource {
				return fmt.Errorf("terminology %s source ID %d Japanese is %q, want %q", term.Key, id, item.Record.Display, wantSource)
			}
			if item.Translation.State != corpus.Translated {
				continue
			}
			want := term.English + "<end>"
			if item.Translation.Text != want {
				return fmt.Errorf("terminology %s source ID %d translation is %q, want %q", term.Key, id, item.Translation.Text, want)
			}
		}
	}
	return nil
}
