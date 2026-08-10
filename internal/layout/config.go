// SPDX-License-Identifier: GPL-3.0-or-later

package layout

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"

	"github.com/pelletier/go-toml/v2"
)

type glyph struct {
	Advance int
}

type metricsFile struct {
	Format  string         `toml:"format"`
	Version int            `toml:"version"`
	Glyph   map[string]int `toml:"glyph"`
}

type c20Group struct {
	IDs []int `toml:"ids"`
}

type posting struct {
	ID       int               `toml:"id"`
	Bindings map[string]string `toml:"bindings"`
}

type postingCandidates struct {
	Destination   []int    `toml:"destination"`
	Escorted      []int    `toml:"escorted role/name"`
	Qualifier     []int    `toml:"qualifier/title"`
	TargetItem    []int    `toml:"target item"`
	TargetMonster []int    `toml:"target monster"`
	IntegerRoles  []string `toml:"integer_roles"`
}

type consumersFile struct {
	Format            string            `toml:"format"`
	Version           int               `toml:"version"`
	SupportedGame     string            `toml:"supported_game"`
	BoundedLabelIDs   []int             `toml:"bounded_label_ids"`
	C5IDs             []int             `toml:"c5_ids"`
	C5PortraitIDs     []int             `toml:"c5_portrait_ids"`
	C22IDs            []int             `toml:"c22_ids"`
	SinglePageC5IDs   []int             `toml:"single_page_c5_ids"`
	GuildClientIDs    []int             `toml:"guild_client_ids"`
	GuildRegionIDs    []int             `toml:"guild_region_ids"`
	C20Groups         []c20Group        `toml:"c20_group"`
	Postings          []posting         `toml:"posting"`
	PostingCandidates postingCandidates `toml:"posting_candidates"`
}

type categoryRange struct {
	First, Last     int
	Category, Basis string
}

type categoriesFile struct {
	Format  string `toml:"format"`
	Version int    `toml:"version"`
	Ranges  []struct {
		First    int    `toml:"first"`
		Last     int    `toml:"last"`
		Category string `toml:"category"`
		Basis    string `toml:"basis"`
	} `toml:"range"`
}

// Load constructs a deterministic layout engine from the release-owned inputs.
// Every document is decoded with unknown fields rejected.
func Load(consumers, metrics, categories []byte) (*Engine, error) {
	var c consumersFile
	if err := decodeStrict("message consumers", consumers, &c); err != nil {
		return nil, err
	}
	var m metricsFile
	if err := decodeStrict("font metrics", metrics, &m); err != nil {
		return nil, err
	}
	var cats categoriesFile
	if err := decodeStrict("translation categories", categories, &cats); err != nil {
		return nil, err
	}
	if c.Format != "zill-message-consumers" || c.Version != 1 {
		return nil, fmt.Errorf("message consumers: unsupported format %q version %d", c.Format, c.Version)
	}
	if c.SupportedGame != "ULJM05410 1.03" {
		return nil, fmt.Errorf("message consumers: unsupported game %q", c.SupportedGame)
	}
	if m.Format != "zill-font-metrics" || m.Version != 1 {
		return nil, fmt.Errorf("font metrics: unsupported format %q version %d", m.Format, m.Version)
	}
	if cats.Format != "zill-message-categories" || cats.Version != 1 {
		return nil, fmt.Errorf("translation categories: unsupported format %q version %d", cats.Format, cats.Version)
	}
	if len(m.Glyph) == 0 {
		return nil, fmt.Errorf("font metrics: glyph repertoire is empty")
	}
	idLists := [][]int{c.BoundedLabelIDs, c.C5IDs, c.C5PortraitIDs, c.C22IDs, c.SinglePageC5IDs, c.GuildClientIDs, c.GuildRegionIDs}
	for _, ids := range idLists {
		for i, id := range ids {
			if id < 0 || i > 0 && id <= ids[i-1] {
				return nil, fmt.Errorf("message consumers: ID sets must be nonnegative, sorted, and unique")
			}
		}
	}
	for _, id := range c.C5PortraitIDs {
		i := sort.SearchInts(c.C5IDs, id)
		if i == len(c.C5IDs) || c.C5IDs[i] != id {
			return nil, fmt.Errorf("message consumers: portrait C5 ID %d is not a C5 message", id)
		}
	}
	for _, group := range c.C20Groups {
		if len(group.IDs) == 0 {
			return nil, fmt.Errorf("message consumers: C20 group must contain an ID")
		}
		for i, id := range group.IDs {
			if id < 0 || i > 0 && id <= group.IDs[i-1] {
				return nil, fmt.Errorf("message consumers: C20 group IDs must be nonnegative, sorted, and unique")
			}
		}
	}
	e := &Engine{
		consumers:  c,
		glyphs:     make(map[uint16]glyph, len(m.Glyph)),
		categories: make([]categoryRange, len(cats.Ranges)),
	}
	for key, advance := range m.Glyph {
		n, err := strconv.ParseUint(key, 0, 16)
		if err != nil || advance < 0 {
			return nil, fmt.Errorf("font metrics: invalid glyph %q", key)
		}
		e.glyphs[uint16(n)] = glyph{Advance: advance}
	}
	previous := -1
	for i, r := range cats.Ranges {
		if r.First < 0 || r.First > r.Last || r.First <= previous || r.Category == "" || (r.Basis != "verified" && r.Basis != "unknown") {
			return nil, fmt.Errorf("translation categories: invalid range %d", i)
		}
		e.categories[i] = categoryRange{r.First, r.Last, r.Category, r.Basis}
		previous = r.Last
	}
	for _, g := range e.glyphs {
		e.playerNameAdvance = max(e.playerNameAdvance, g.Advance*playerNameMaxCharacters)
	}
	return e, nil
}

func decodeStrict(name string, data []byte, target any) error {
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%s: invalid TOML: %w", name, err)
	}
	return nil
}
