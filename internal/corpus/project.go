// SPDX-License-Identifier: GPL-3.0-or-later

package corpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"unicode"

	"github.com/HK47196/zill/internal/cp932"
	"github.com/pelletier/go-toml/v2"
)

const englishLicenseLine = "# SPDX-License-Identifier: CC-BY-SA-4.0"

const (
	sectionCount        = 279
	recordCount         = 43116
	japaneseFingerprint = "b2a63329c00bb7372e356645ac505385da5022a9a3c61118840cb787d62ac89b"
)

// State is the inferred disposition of a paired contributor record.
type State string

const (
	Translated   State = "translated"
	KeepJapanese State = "keep_japanese"
	Todo         State = "todo"
)

// Translation is one canonical contributor-owned paired row. State is derived
// from Text and Todo when the paired corpus is loaded.
type Translation struct {
	ID       int
	Japanese string
	State    State
	Text     string
	Todo     bool
	File     string
}

// Item joins one contributor-owned row to a retail record. Before BindBanks,
// Record is a display-only placeholder with no raw retail data.
type Item struct {
	Record      Record
	Translation Translation
	// Layout is populated only by the release reflower before compilation.
	Layout string
}

// Project is the complete contributor corpus in stable ID order.
type Project struct {
	Items []Item
	byID  map[int]int
}

// Summary reports terminal and unfinished contributor states.
type Summary struct {
	Banks        int
	Records      int
	Translated   int
	KeepJapanese int
	Todo         int
}

type pairedValue struct {
	Japanese *string `toml:"japanese"`
	English  *string `toml:"english"`
	Todo     *bool   `toml:"todo"`
}

// LoadProject validates and loads the asset-free paired contributor corpus
// rooted at root. Retail message banks are authenticated separately by BindBanks.
func LoadProject(root string) (*Project, Summary, error) {
	pairDir := filepath.Join(root, "translations", "messages")
	if err := requireExactSectionFiles(pairDir, ".toml"); err != nil {
		return nil, Summary{}, err
	}

	project := &Project{Items: make([]Item, 0, recordCount), byID: make(map[int]int, recordCount)}
	summary := Summary{Banks: sectionCount}
	japanese := sha256.New()
	for section := range sectionCount {
		path := filepath.Join(pairDir, fmt.Sprintf("msgsec%03d.toml", section))
		values, err := readPairedFile(path, section)
		if err != nil {
			return nil, Summary{}, err
		}
		for _, value := range values {
			if _, exists := project.byID[value.ID]; exists {
				return nil, Summary{}, fmt.Errorf("%s: duplicate ID %d", path, value.ID)
			}
			project.byID[value.ID] = len(project.Items)
			_, _ = japanese.Write([]byte(strconv.Itoa(value.ID)))
			_, _ = japanese.Write([]byte{0})
			_, _ = japanese.Write([]byte(value.Japanese))
			_, _ = japanese.Write([]byte{0})
			project.Items = append(project.Items, Item{
				Record:      Record{ID: value.ID, Index: value.ID % 10_000, Display: value.Japanese},
				Translation: value,
			})
			summary.Records++
			switch value.State {
			case Translated:
				summary.Translated++
			case KeepJapanese:
				summary.KeepJapanese++
			case Todo:
				summary.Todo++
			}
		}
	}
	if summary.Records != recordCount {
		return nil, Summary{}, fmt.Errorf("%s: found %d paired records, want %d", pairDir, summary.Records, recordCount)
	}
	if actual := hex.EncodeToString(japanese.Sum(nil)); actual != japaneseFingerprint {
		return nil, Summary{}, fmt.Errorf("%s: Japanese reference fingerprint is %s, want %s; restore japanese fields", pairDir, actual, japaneseFingerprint)
	}
	return project, summary, nil
}

// BindBanks authenticates a loaded contributor project against all parsed retail
// banks. It does not mutate project unless every section and record validates.
func BindBanks(project *Project, banks []Bank) error {
	if project == nil {
		return fmt.Errorf("bind banks: nil project")
	}
	if len(project.Items) != recordCount || len(project.byID) != recordCount {
		return fmt.Errorf("bind banks: project has %d records, want %d", len(project.Items), recordCount)
	}
	if len(banks) != sectionCount {
		return fmt.Errorf("bind banks: got %d banks, want %d", len(banks), sectionCount)
	}
	bySection := make(map[int]Bank, sectionCount)
	for _, bank := range banks {
		if bank.Section < 0 || bank.Section >= sectionCount {
			return fmt.Errorf("bind banks: bank has invalid section %03d", bank.Section)
		}
		if _, exists := bySection[bank.Section]; exists {
			return fmt.Errorf("bind banks: duplicate section %03d", bank.Section)
		}
		bySection[bank.Section] = bank
	}
	records := make(map[int]Record, recordCount)
	for section := range sectionCount {
		bank, exists := bySection[section]
		if !exists {
			return fmt.Errorf("bind banks: missing section %03d", section)
		}
		for index, record := range bank.Records {
			if record.ID != section*10_000+index || record.Index != index {
				return fmt.Errorf("bind banks: section %03d record %d has inconsistent ID/index", section, index)
			}
			if _, exists := records[record.ID]; exists {
				return fmt.Errorf("bind banks: duplicate retail ID %d", record.ID)
			}
			records[record.ID] = record
		}
	}
	if len(records) != len(project.Items) {
		return fmt.Errorf("bind banks: retail coverage is %d records for %d contributor records", len(records), len(project.Items))
	}
	authenticated := make([]Record, len(project.Items))
	for index, item := range project.Items {
		record, exists := records[item.Record.ID]
		if !exists {
			return fmt.Errorf("bind banks: missing retail ID %d", item.Record.ID)
		}
		if item.Translation.Japanese != record.Display {
			return fmt.Errorf("bind banks: ID %d Japanese differs from retail display", record.ID)
		}
		authenticated[index] = record
	}
	for index := range project.Items {
		project.Items[index].Record = authenticated[index]
	}
	return nil
}

// Find returns one item by stable source ID.
func (project *Project) Find(id int) (Item, bool) {
	index, ok := project.byID[id]
	if !ok {
		return Item{}, false
	}
	return project.Items[index], true
}

// WithEnglish returns an independent project snapshot with one nonempty
// English translation replaced and its derived state updated.
func (project *Project) WithEnglish(id int, english string) (*Project, error) {
	if project == nil {
		return nil, fmt.Errorf("translation update: nil project")
	}
	index, ok := project.byID[id]
	if !ok {
		return nil, fmt.Errorf("translation update: ID %d does not exist", id)
	}
	if english == "" {
		return nil, fmt.Errorf("translation update: ID %d: english must be nonempty", id)
	}
	item := project.Items[index]
	if err := validateEncodedText(item.Translation.File, id, "english", english); err != nil {
		return nil, err
	}
	updated := &Project{Items: append([]Item(nil), project.Items...), byID: make(map[int]int, len(project.byID))}
	for key, value := range project.byID {
		updated.byID[key] = value
	}
	updated.Items[index].Translation.Text = english
	updated.Items[index].Translation.Todo = false
	updated.Items[index].Translation.State = Translated
	return updated, nil
}

// RenderSection returns one canonical contributor message file from a loaded
// project snapshot.
func (project *Project) RenderSection(section int) ([]byte, error) {
	if project == nil {
		return nil, fmt.Errorf("render translation section: nil project")
	}
	if section < 0 || section >= sectionCount {
		return nil, fmt.Errorf("render translation section: invalid section %03d", section)
	}
	rows := make([]Translation, 0)
	for _, item := range project.Items {
		if item.Translation.ID/10_000 == section {
			rows = append(rows, item.Translation)
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("render translation section: section %03d has no records", section)
	}
	return renderPairedFile(rows), nil
}

func readPairedFile(path string, section int) ([]Translation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: required paired section: %w", path, err)
	}
	var decoded map[string]pairedValue
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%s: invalid TOML: %w", path, err)
	}
	ids := make([]int, 0, len(decoded))
	values := make(map[int]Translation, len(decoded))
	for key, row := range decoded {
		id, err := strconv.Atoi(key)
		if err != nil || key != strconv.Itoa(id) {
			return nil, fmt.Errorf("%s: record key %q is not a decimal ID", path, key)
		}
		if id/10_000 != section {
			return nil, fmt.Errorf("%s: ID %d belongs to section %03d", path, id, id/10_000)
		}
		if row.Japanese == nil || row.English == nil {
			return nil, fmt.Errorf("%s: ID %d: japanese and english are required", path, id)
		}
		translation := Translation{ID: id, Japanese: *row.Japanese, Text: *row.English, File: path}
		if row.Todo != nil {
			if !*row.Todo || translation.Text != "" {
				return nil, fmt.Errorf("%s: ID %d: todo is allowed only as true with empty english", path, id)
			}
			translation.Todo = true
		}
		switch {
		case translation.Text != "":
			translation.State = Translated
			if err := validateEncodedText(path, id, "english", translation.Text); err != nil {
				return nil, err
			}
		case translation.Todo:
			translation.State = Todo
		default:
			translation.State = KeepJapanese
		}
		ids = append(ids, id)
		values[id] = translation
	}
	sort.Ints(ids)
	ordered := make([]Translation, len(ids))
	for index, id := range ids {
		ordered[index] = values[id]
	}
	if !bytes.Equal(data, renderPairedFile(ordered)) {
		return nil, fmt.Errorf("%s: file is not in canonical paired format", path)
	}
	return ordered, nil
}

func renderPairedFile(rows []Translation) []byte {
	var output bytes.Buffer
	output.WriteString(englishLicenseLine)
	output.WriteByte('\n')
	for _, row := range rows {
		fmt.Fprintf(&output, "\n[%q]\n", strconv.Itoa(row.ID))
		fmt.Fprintf(&output, "japanese = %s\n", strconv.Quote(row.Japanese))
		fmt.Fprintf(&output, "english = %s\n", strconv.Quote(row.Text))
		if row.Todo {
			output.WriteString("todo = true\n")
		}
	}
	return output.Bytes()
}

func requireExactSectionFiles(dir, extension string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("%s: list section files: %w", dir, err)
	}
	if len(entries) != sectionCount {
		return fmt.Errorf("%s: found %d files, want exactly %d", dir, len(entries), sectionCount)
	}
	for section, entry := range entries {
		expected := fmt.Sprintf("msgsec%03d%s", section, extension)
		if entry.IsDir() || entry.Name() != expected {
			return fmt.Errorf("%s: expected section file %s", dir, expected)
		}
	}
	return nil
}

func validateEncodedText(path string, id int, field, text string) error {
	for _, character := range text {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s: ID %d: %s contains a raw Unicode control character", path, id, field)
		}
	}
	encoded, err := cp932.Encode(text)
	if err != nil {
		return fmt.Errorf("%s: ID %d: %s is not CP932 encodable: %w", path, id, field, err)
	}
	decoded, err := cp932.Decode(encoded)
	if err != nil || decoded != text {
		return fmt.Errorf("%s: ID %d: %s does not round-trip through CP932", path, id, field)
	}
	for index := 0; index < len(encoded); index++ {
		if isDoubleByteLead(encoded[index]) {
			index++
			continue
		}
		if 0xa1 <= encoded[index] && encoded[index] <= 0xdf {
			return fmt.Errorf("%s: ID %d: %s contains half-width kana", path, id, field)
		}
	}
	return nil
}
