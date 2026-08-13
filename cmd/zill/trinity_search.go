// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/HK47196/zill/internal/cp932"
)

const trinitySearchUsage = "zill trinity-search [--language english|japanese] [--ignore-case] [--max-count N] PATTERN"

const trinitySearchHelp = `Usage: zill trinity-search [options] PATTERN

Options:
  --language LANG    Search English or Japanese (default English)
  -i, --ignore-case  Search without regard to case
  --max-count N      Stop after N matching bilingual records (default 50)
  -h, --help         Show this help

Use ripgrep regular-expression syntax to search structurally paired LINKDATA
strings. Inputs are loaded from build/trinity/{english,japanese}. Each match
displays the searched language followed by its counterpart. ripgrep (rg) must
be installed and on PATH.
`

type trinitySearchLanguage string

const (
	trinitySearchEnglish  trinitySearchLanguage = "english"
	trinitySearchJapanese trinitySearchLanguage = "japanese"
)

type trinitySearchOptions struct {
	english    string
	japanese   string
	pattern    string
	language   trinitySearchLanguage
	ignoreCase bool
	maxCount   int
}

type trinityStringPair struct {
	Member         int
	Path           []int
	EnglishOffset  int
	JapaneseOffset int
	English        string
	Japanese       string
}

func runTrinitySearch(root string, args []string, stdout, stderr io.Writer) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprint(stdout, trinitySearchHelp)
			return 0
		}
	}
	options, err := parseTrinitySearchOptions(root, args)
	if err != nil {
		fmt.Fprintf(stderr, "zill: trinity-search: %v\n", err)
		fmt.Fprintf(stderr, "zill: usage: %s\n", trinitySearchUsage)
		return 2
	}
	pairs, err := loadTrinityStringPairs(options.english, options.japanese)
	if err != nil {
		fmt.Fprintf(stderr, "zill: trinity-search: %v\n", err)
		return 1
	}
	matches, err := searchTrinityPairsWithRG(pairs, options)
	if err != nil {
		fmt.Fprintf(stderr, "zill: trinity-search: %v\n", err)
		return 1
	}
	if len(matches) == 0 {
		fmt.Fprintf(stdout, "No Trinity %s matches.\n", trinitySearchLanguageName(options.language))
		return 0
	}
	writeTrinitySearchMatches(stdout, matches, options.language)
	return 0
}

func writeTrinitySearchMatches(output io.Writer, matches []trinityStringPair, language trinitySearchLanguage) {
	for index, pair := range matches {
		if index > 0 {
			fmt.Fprintln(output)
		}
		fmt.Fprintf(output, "%s (EN 0x%x, JP 0x%x)\n", trinityPairLocator(pair), pair.EnglishOffset, pair.JapaneseOffset)
		if language == trinitySearchJapanese {
			fmt.Fprintf(output, "  Japanese: %s\n", displayTrinityText(pair.Japanese, "            "))
			fmt.Fprintf(output, "  English: %s\n", displayTrinityText(pair.English, "           "))
			continue
		}
		fmt.Fprintf(output, "  English: %s\n", displayTrinityText(pair.English, "           "))
		fmt.Fprintf(output, "  Japanese: %s\n", displayTrinityText(pair.Japanese, "            "))
	}
}

func trinitySearchLanguageName(language trinitySearchLanguage) string {
	if language == trinitySearchJapanese {
		return "Japanese"
	}
	return "English"
}

func trinityPairLocator(pair trinityStringPair) string {
	var result strings.Builder
	fmt.Fprintf(&result, "LINKDATA:%d", pair.Member)
	for _, ordinal := range pair.Path {
		fmt.Fprintf(&result, "/%d", ordinal)
	}
	return result.String()
}

func parseTrinitySearchOptions(root string, args []string) (trinitySearchOptions, error) {
	options := trinitySearchOptions{
		english:  filepath.Join(root, "build", "trinity", "english"),
		japanese: filepath.Join(root, "build", "trinity", "japanese"),
		language: trinitySearchEnglish,
		maxCount: 50,
	}
	seen := make(map[string]bool)
	for index := 0; index < len(args); index++ {
		name, value, hasEquals := strings.Cut(args[index], "=")
		next := func() (string, error) {
			if hasEquals {
				if value == "" {
					return "", fmt.Errorf("%s requires a value", name)
				}
				return value, nil
			}
			if index+1 == len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			index++
			return args[index], nil
		}
		var err error
		switch name {
		case "--language", "--max-count":
			if seen[name] {
				return options, fmt.Errorf("%s may be specified only once", name)
			}
			seen[name] = true
			var item string
			item, err = next()
			if err == nil {
				switch name {
				case "--language":
					options.language = trinitySearchLanguage(item)
					if options.language != trinitySearchEnglish && options.language != trinitySearchJapanese {
						return options, errors.New("--language must be english or japanese")
					}
				case "--max-count":
					options.maxCount, err = strconv.Atoi(item)
					if err != nil || options.maxCount < 1 || options.maxCount > 100000 {
						return options, errors.New("--max-count must be an integer from 1 through 100000")
					}
				}
			}
		case "-i", "--ignore-case":
			if hasEquals {
				return options, fmt.Errorf("%s does not accept a value", name)
			}
			if options.ignoreCase {
				return options, errors.New("--ignore-case may be specified only once")
			}
			options.ignoreCase = true
		default:
			if strings.HasPrefix(name, "-") {
				return options, fmt.Errorf("unknown option %q", name)
			}
			if options.pattern != "" {
				return options, errors.New("PATTERN must be one argument")
			}
			options.pattern = args[index]
		}
		if err != nil {
			return options, err
		}
	}
	if options.pattern == "" {
		return options, errors.New("PATTERN is required")
	}
	return options, nil
}

func loadTrinityStringPairs(englishDir, japaneseDir string) ([]trinityStringPair, error) {
	if err := validateTrinitySearchManifest(englishDir, "BLUS30503"); err != nil {
		return nil, err
	}
	if err := validateTrinitySearchManifest(japaneseDir, "BLJM60212"); err != nil {
		return nil, err
	}
	var result []trinityStringPair
	for _, member := range trinityTextMembers {
		base := fmt.Sprintf("%06d-%s.bin", member.index, member.name)
		englishPath := filepath.Join(englishDir, "linkdata", base)
		japanesePath := filepath.Join(japaneseDir, "linkdata", base)
		english, err := os.ReadFile(englishPath)
		if err != nil {
			return nil, fmt.Errorf("read English %s: %w", base, err)
		}
		japanese, err := os.ReadFile(japanesePath)
		if err != nil {
			return nil, fmt.Errorf("read Japanese %s: %w", base, err)
		}
		if trinityHash(english) != member.english.sha256 || trinityHash(japanese) != member.japanese.sha256 {
			return nil, fmt.Errorf("%s is not the authenticated English/Japanese member pair", base)
		}
		pairs, err := pairTrinityMemberStrings(member.index, english, japanese)
		if err != nil {
			return nil, fmt.Errorf("pair LINKDATA member %d: %w", member.index, err)
		}
		result = append(result, pairs...)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Member != result[right].Member {
			return result[left].Member < result[right].Member
		}
		return result[left].EnglishOffset < result[right].EnglishOffset
	})
	if len(result) == 0 {
		return nil, errors.New("authenticated outputs contain no structurally paired strings")
	}
	return result, nil
}

func validateTrinitySearchManifest(directory, titleID string) error {
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read %s manifest: %w", titleID, err)
	}
	var manifest struct {
		SchemaVersion int            `json:"schema_version"`
		Release       trinityRelease `json:"release"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode %s manifest: %w", titleID, err)
	}
	if manifest.SchemaVersion != 1 || manifest.Release.TitleID != titleID {
		return fmt.Errorf("%s is not a Trinity %s extraction", directory, titleID)
	}
	return nil
}

type trinityPairNode struct {
	data   []byte
	offset int
}

type trinityNodeKind byte

const (
	trinityNodeNone trinityNodeKind = iota
	trinityNodeOffsets
	trinityNodeSized
)

func pairTrinityMemberStrings(member int, english, japanese []byte) ([]trinityStringPair, error) {
	specification, ok := map[int]struct {
		kind  trinityNodeKind
		count int
	}{
		0: {trinityNodeOffsets, 41}, 2: {trinityNodeOffsets, 2}, 4: {trinityNodeOffsets, 2},
		5: {trinityNodeSized, 300}, 6: {trinityNodeOffsets, 1000}, 14: {trinityNodeSized, 7},
		58: {trinityNodeOffsets, 1}, 275: {trinityNodeSized, 4},
	}[member]
	if !ok {
		return nil, fmt.Errorf("unsupported localized member %d", member)
	}
	rootKind, rootCount := specification.kind, specification.count
	enRoot := trinityPairNode{data: english}
	jpRoot := trinityPairNode{data: japanese}
	enKind, enChildren := classifyTrinityNode(enRoot)
	jpKind, jpChildren := classifyTrinityNode(jpRoot)
	if enKind != rootKind || jpKind != rootKind || len(enChildren) != rootCount || len(jpChildren) != rootCount {
		return nil, fmt.Errorf("root layout is not the authenticated kind/count %d/%d", rootKind, rootCount)
	}
	if member == 275 {
		return pairTrinityMember275(enChildren, jpChildren)
	}
	var result []trinityStringPair
	for ordinal := 0; ordinal < rootCount; ordinal++ {
		result = append(result, pairTrinityNodes(member, []int{ordinal}, enChildren[ordinal], jpChildren[ordinal], 0)...)
	}
	return result, nil
}

func pairTrinityMember275(english, japanese []trinityPairNode) ([]trinityStringPair, error) {
	const rowCount = 1200
	if len(english[0].data) != rowCount*9 || len(japanese[0].data) != rowCount*9 || len(english[2].data) != rowCount*4 || len(japanese[2].data) != rowCount*4 {
		return nil, errors.New("row metadata layout is not the authenticated 1200-row form")
	}
	enLabelKind, enLabels := classifyTrinityNode(english[1])
	jpLabelKind, jpLabels := classifyTrinityNode(japanese[1])
	if enLabelKind != trinityNodeOffsets || jpLabelKind != trinityNodeOffsets || len(enLabels) != 135 || len(jpLabels) != 135 {
		return nil, errors.New("label layout is not the authenticated 135-row form")
	}
	enTextKind, enText := classifyTrinityNode(english[3])
	jpTextKind, jpText := classifyTrinityNode(japanese[3])
	if enTextKind != trinityNodeOffsets || jpTextKind != trinityNodeOffsets || len(enText) != rowCount || len(jpText) != rowCount {
		return nil, errors.New("dialogue-label layout is not the authenticated 1200-row form")
	}
	var result []trinityStringPair
	for ordinal := range enLabels {
		result = append(result, pairTrinityNodes(275, []int{1, ordinal}, enLabels[ordinal], jpLabels[ordinal], 0)...)
	}
	// Root child 3 is intentionally not paired. Its 1,200 rows are reordered
	// between releases and the parallel 9-byte values are non-unique, so neither
	// ordinal nor metadata equality establishes translation identity.
	return result, nil
}

func pairTrinityNodes(member int, path []int, english, japanese trinityPairNode, depth int) []trinityStringPair {
	if depth > 64 {
		return nil
	}
	enKind, enChildren := classifyTrinityNode(english)
	jpKind, jpChildren := classifyTrinityNode(japanese)
	if enKind != trinityNodeNone || jpKind != trinityNodeNone {
		if enKind == trinityNodeNone || enKind != jpKind || len(enChildren) != len(jpChildren) {
			return nil
		}
		var result []trinityStringPair
		for ordinal := range enChildren {
			childPath := append(append([]int(nil), path...), ordinal)
			result = append(result, pairTrinityNodes(member, childPath, enChildren[ordinal], jpChildren[ordinal], depth+1)...)
		}
		return result
	}
	enText, enRelative, enOK := trinityTerminalNodeText(english.data, false)
	jpText, jpRelative, jpOK := trinityTerminalNodeText(japanese.data, true)
	if !enOK || !jpOK || enText == "" || jpText == "" {
		return nil
	}
	return []trinityStringPair{{
		Member: member, Path: append([]int(nil), path...), EnglishOffset: english.offset + enRelative,
		JapaneseOffset: japanese.offset + jpRelative, English: enText, Japanese: jpText,
	}}
}

func classifyTrinityNode(node trinityPairNode) (trinityNodeKind, []trinityPairNode) {
	offsetChildren, offsetsOK := parseTrinityOffsetNode(node)
	sizedChildren, sizedOK := parseTrinitySizedNode(node)
	if offsetsOK == sizedOK {
		return trinityNodeNone, nil
	}
	if offsetsOK {
		return trinityNodeOffsets, offsetChildren
	}
	return trinityNodeSized, sizedChildren
}

func parseTrinityOffsetNode(node trinityPairNode) ([]trinityPairNode, bool) {
	if len(node.data) < 8 {
		return nil, false
	}
	count := uint64(binary.BigEndian.Uint32(node.data))
	headerSize := uint64(4) + count*4
	if count == 0 || count > 20000 || headerSize >= uint64(len(node.data)) {
		return nil, false
	}
	offsets := make([]int, int(count))
	for index := range offsets {
		offsets[index] = int(binary.BigEndian.Uint32(node.data[4+index*4:]))
		if index == 0 && uint64(offsets[index]) != headerSize || index > 0 && offsets[index] <= offsets[index-1] || offsets[index] >= len(node.data) {
			return nil, false
		}
	}
	children := make([]trinityPairNode, len(offsets))
	for index, start := range offsets {
		end := len(node.data)
		if index+1 < len(offsets) {
			end = offsets[index+1]
		}
		children[index] = trinityPairNode{data: node.data[start:end], offset: node.offset + start}
	}
	return children, true
}

func parseTrinitySizedNode(node trinityPairNode) ([]trinityPairNode, bool) {
	if len(node.data) < 12 {
		return nil, false
	}
	count := uint64(binary.BigEndian.Uint32(node.data))
	headerSize := uint64(4) + count*8
	if count == 0 || count > 20000 || headerSize >= uint64(len(node.data)) {
		return nil, false
	}
	children := make([]trinityPairNode, int(count))
	previousEnd := int(headerSize)
	for index := range children {
		position := 4 + index*8
		start := uint64(binary.BigEndian.Uint32(node.data[position:]))
		size := uint64(binary.BigEndian.Uint32(node.data[position+4:]))
		if index == 0 && start != headerSize || start < uint64(previousEnd) || start > uint64(len(node.data)) || size == 0 || size > uint64(len(node.data))-start {
			return nil, false
		}
		if !trinityAllZero(node.data[previousEnd:int(start)]) {
			return nil, false
		}
		end := int(start + size)
		children[index] = trinityPairNode{data: node.data[int(start):end], offset: node.offset + int(start)}
		previousEnd = end
	}
	if !trinityAllZero(node.data[previousEnd:]) {
		return nil, false
	}
	return children, true
}

func trinityTerminalNodeText(data []byte, japanese bool) (string, int, bool) {
	if len(data) == 0 {
		return "", 0, false
	}
	end := bytes.IndexByte(data, 0)
	if end < 0 {
		end = len(data)
	} else if !trinityAllZero(data[end:]) {
		return trinitySingletonCandidate(data, japanese)
	}
	text, ok := decodeTrinitySearchText(data[:end], japanese)
	if !ok {
		return trinitySingletonCandidate(data, japanese)
	}
	return text, 0, true
}

func trinitySingletonCandidate(data []byte, japanese bool) (string, int, bool) {
	candidates := trinityCandidates(data, japanese)
	if len(candidates) != 1 {
		return "", 0, false
	}
	return candidates[0].Text, candidates[0].Offset, true
}

func decodeTrinitySearchText(encoded []byte, japanese bool) (string, bool) {
	var text string
	if japanese {
		var err error
		text, err = cp932.Decode(encoded)
		if err != nil {
			return "", false
		}
	} else {
		if !utf8.Valid(encoded) {
			return "", false
		}
		text = string(encoded)
	}
	for _, r := range text {
		if !unicode.IsGraphic(r) && r != ' ' && r != '\n' && r != '\r' && r != '\t' && r != 0x1b {
			return "", false
		}
	}
	return text, true
}

func trinityAllZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

func searchTrinityPairsWithRG(pairs []trinityStringPair, options trinitySearchOptions) ([]trinityStringPair, error) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		return nil, errors.New("ripgrep (rg) was not found on PATH")
	}
	corpus, err := os.CreateTemp("", "zill-trinity-search-*.txt")
	if err != nil {
		return nil, fmt.Errorf("create temporary %s search corpus: %w", options.language, err)
	}
	name := corpus.Name()
	defer os.Remove(name)
	for _, pair := range pairs {
		searchText := pair.English
		if options.language == trinitySearchJapanese {
			searchText = pair.Japanese
		}
		if strings.IndexByte(searchText, 0) >= 0 {
			_ = corpus.Close()
			return nil, fmt.Errorf("paired %s text contains a NUL byte", options.language)
		}
		if _, err := corpus.WriteString(searchText); err != nil {
			_ = corpus.Close()
			return nil, fmt.Errorf("write temporary %s search corpus: %w", options.language, err)
		}
		if _, err := corpus.Write([]byte{0}); err != nil {
			_ = corpus.Close()
			return nil, fmt.Errorf("write temporary %s search corpus: %w", options.language, err)
		}
	}
	if err := corpus.Close(); err != nil {
		return nil, fmt.Errorf("close temporary %s search corpus: %w", options.language, err)
	}
	arguments := []string{"--json", "--null-data", "--color", "never", "--max-count", strconv.Itoa(options.maxCount)}
	if options.ignoreCase {
		arguments = append(arguments, "--ignore-case")
	}
	arguments = append(arguments, "-e", options.pattern, name)
	command := exec.Command(rg, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
			return nil, nil
		}
		if message := strings.TrimSpace(string(output)); message != "" {
			return nil, fmt.Errorf("run rg: %s", message)
		}
		return nil, fmt.Errorf("run rg: %w", err)
	}
	var result []trinityStringPair
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var event struct {
			Type string `json:"type"`
			Data struct {
				LineNumber int `json:"line_number"`
			} `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode rg output: %w", err)
		}
		if event.Type != "match" {
			continue
		}
		if event.Data.LineNumber < 1 || event.Data.LineNumber > len(pairs) {
			return nil, errors.New("rg returned an invalid search-corpus line number")
		}
		result = append(result, pairs[event.Data.LineNumber-1])
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read rg output: %w", err)
	}
	return result, nil
}

func displayTrinityText(text, continuationIndent string) string {
	var result strings.Builder
	for _, r := range text {
		switch r {
		case '\n':
			result.WriteByte('\n')
			result.WriteString(continuationIndent)
		case '\r':
			result.WriteString("<CR>")
		case '\t':
			result.WriteString("<TAB>")
		default:
			if unicode.IsGraphic(r) || r == ' ' {
				result.WriteRune(r)
			} else if r <= 0xff {
				fmt.Fprintf(&result, "<%02X>", r)
			} else {
				fmt.Fprintf(&result, "<U+%04X>", r)
			}
		}
	}
	return result.String()
}
