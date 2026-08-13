// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/HK47196/zill/internal/cp932"
	"github.com/HK47196/zill/internal/gamefmt/trinitylink"
)

const trinityExtractUsage = "zill trinity-extract --game-dir PATH --output PATH [--eboot-elf PATH]"

const trinityExtractHelp = `Usage: zill trinity-extract --game-dir PATH --output PATH [options]

Options:
  --eboot-elf PATH  Include an EBOOT.elf already decrypted from EBOOT.BIN
  -h, --help        Show this help

Extract the English or Japanese text assets from a supported Trinity PS3 retail
tree. PATH must be the directory containing PS3_GAME. The source tree is opened
read-only. The command validates the release, LINKDATA pair, and text containers,
writes to a temporary directory, and publishes the output atomically. Existing
output is not replaced.
`

type trinityReleaseSpec struct {
	titleID      string
	language     string
	ebootSize    int64
	ebootSHA256  string
	ebootMarkers []string
	japanese     bool
}

var trinityReleaseSpecs = map[string]trinityReleaseSpec{
	"BLUS30503": {
		titleID: "BLUS30503", language: "english", ebootSize: 10431072,
		ebootSHA256:  "def4002541c5659ec34c663fc6c64e4f11dd3a99cdd56d94a87b44b41ab80370",
		ebootMarkers: []string{"BLUS30503", "TRINITY: Souls of Zill O'll"},
	},
	"BLJM60212": {
		titleID: "BLJM60212", language: "japanese", ebootSize: 10365136,
		ebootSHA256:  "62dde95e7025c63091694ceeb53437185a1477651214ebb89cd7555f001d7249",
		ebootMarkers: []string{"BLJM60212", "TRINITY Zill O'll Zero"}, japanese: true,
	},
}

type trinityTextMember struct {
	index    int
	name     string
	category string
	english  trinityMemberIdentity
	japanese trinityMemberIdentity
}

// These are the standalone localized tables identified by comparing every
// decoded English/Japanese member and classifying the remaining signatures.
var trinityTextMembers = []trinityTextMember{
	{0, "system-menu-items", "system, menu, item, and help text", trinityMemberIdentity{490256, "c76750cdd2989342cd40dbbee58ab4afaf5be45dabc12117b7ba97bd18c3486a"}, trinityMemberIdentity{450968, "96a13f62db21ceecdbcc0936ce608fa6b3eb4c980f9cb4f6964e32061b166448"}},
	{2, "missions", "mission and arena text", trinityMemberIdentity{62200, "5000fc94fe9dfd9a1fdb15ad3e96d077133025ac8c8c1f7a0a6c5edf098f78aa"}, trinityMemberIdentity{57684, "edd4cc510bd83ea84b35ce5c0e414bc6adb9cbde09bf47b75bae8e5f53e753bb"}},
	{4, "mission-results", "mission completion and return text", trinityMemberIdentity{9016, "8d96308f57b202b8b0df2979f012165f105167b44a6deabfbff89f8ab0f9ec6b"}, trinityMemberIdentity{7404, "570cb8ed6815bff27e8303d190eea8168d0ace10ef3bb378c47a80ab08c666b5"}},
	{5, "scripted-messages", "scripted and battle messages", trinityMemberIdentity{2156472, "5ea7ef070c4f370cb52ded713b631adcf3b9cbff68fd50b504bbbd23fe8c9992"}, trinityMemberIdentity{2137068, "6103a441d99bb2ed15c1d96c075068dc297ec69429c79b0a2d87ee9b9f74e918"}},
	{6, "dialogue", "dialogue", trinityMemberIdentity{769300, "be74f147be260b84ced85b2acc6bc28b205c34f507a0b30804743ad18fcb2729"}, trinityMemberIdentity{671248, "1a47e07d5f72f44f93b52909baa257f4f7490e825963f266b21b1f1ff5f7ec7f"}},
	{14, "narration-subtitles", "narration and movie subtitles", trinityMemberIdentity{59684, "05ee42f4dfbca49bc92ed8c686106a8a69f80d70140c4d46820d4e7abacf78f9"}, trinityMemberIdentity{54684, "9381340f1849b31b77b9c0ff5514862664d06e967524d3bf6db476d0b214983c"}},
	{58, "locations", "location descriptions", trinityMemberIdentity{9048, "cdab8c97334cb4160b0edc6356f259404c2f28bad7f3e7e5d8147145e6d0a1c7"}, trinityMemberIdentity{7556, "58cb3e119f3e3f48684aac9eb4d06d0cedeae6114b01bc9facb0d00f0ac71279"}},
	{275, "labels", "speaker, actor, and role labels", trinityMemberIdentity{121328, "0e9be39c936ebaf8c2d98021a2a778f96818a4ddb9324fe18556b3b464b25b2d"}, trinityMemberIdentity{111112, "5c024248957257dd4ea67ad458b252fad799bd7db97b16066815344043309215"}},
}

type trinityMemberIdentity struct {
	size   uint32
	sha256 string
}

func (member trinityTextMember) identity(release trinityReleaseSpec) trinityMemberIdentity {
	if release.japanese {
		return member.japanese
	}
	return member.english
}

type trinityExtractOptions struct {
	gameDir  string
	output   string
	ebootELF string
}

type trinityManifest struct {
	SchemaVersion  int                    `json:"schema_version"`
	SourceGameDir  string                 `json:"source_game_dir"`
	Release        trinityRelease         `json:"release"`
	SourceFiles    []trinitySourceFile    `json:"source_files"`
	LINKDATA       trinityLINKDATASummary `json:"linkdata"`
	TextAssets     []trinityTextAsset     `json:"text_assets"`
	ExcludedInputs []trinityExclusion     `json:"excluded_inputs"`
}

type trinityRelease struct {
	Title    string `json:"title"`
	TitleID  string `json:"title_id"`
	Version  string `json:"version"`
	Language string `json:"language"`
}

type trinitySourceFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type trinityLINKDATASummary struct {
	MemberCount int `json:"member_count"`
	Raw         int `json:"raw_members"`
	Deflate     int `json:"deflate_members"`
	ZeroFill    int `json:"zero_fill_members"`
	Absent      int `json:"absent_members"`
}

type trinityTextAsset struct {
	Source         string  `json:"source"`
	Category       string  `json:"category"`
	DecodedFile    string  `json:"decoded_file"`
	StringsFile    string  `json:"strings_file,omitempty"`
	Size           int     `json:"size"`
	SHA256         string  `json:"sha256"`
	CandidateCount int     `json:"candidate_count,omitempty"`
	LINKDATAIndex  *int    `json:"linkdata_index,omitempty"`
	StoredOffset   *int64  `json:"stored_offset,omitempty"`
	StoredSize     *uint32 `json:"stored_size,omitempty"`
	StorageKind    string  `json:"storage_kind,omitempty"`
}

type trinityExclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type trinityCandidateDocument struct {
	SchemaVersion int                `json:"schema_version"`
	Source        string             `json:"source"`
	Encoding      string             `json:"encoding"`
	Candidates    []trinityCandidate `json:"candidates"`
}

type trinityCandidate struct {
	Offset          int    `json:"offset"`
	Text            string `json:"text"`
	TerminatedByNUL bool   `json:"terminated_by_nul"`
}

type trinitySFOStringDocument struct {
	SchemaVersion int                `json:"schema_version"`
	Source        string             `json:"source"`
	Strings       []trinitySFOString `json:"strings"`
}

type trinitySFOString struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type trinityTrophyFile struct {
	name string
	data []byte
}

func runTrinityExtract(args []string, stdout, stderr io.Writer) int {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fmt.Fprint(stdout, trinityExtractHelp)
			return 0
		}
	}
	options, err := parseTrinityExtractOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "zill: trinity-extract: %v\n", err)
		fmt.Fprintf(stderr, "zill: usage: %s\n", trinityExtractUsage)
		return 2
	}
	manifest, err := extractTrinityText(options)
	if err != nil {
		fmt.Fprintf(stderr, "zill: trinity-extract: %v\n", err)
		return 1
	}
	linkdataAssets := 0
	for _, asset := range manifest.TextAssets {
		if asset.LINKDATAIndex != nil {
			linkdataAssets++
		}
	}
	fmt.Fprintf(stdout, "Extracted %d LINKDATA text assets and %d executable/metadata/trophy text assets to %s\n", linkdataAssets, len(manifest.TextAssets)-linkdataAssets, options.output)
	return 0
}

func parseTrinityExtractOptions(args []string) (trinityExtractOptions, error) {
	var options trinityExtractOptions
	seen := make(map[string]bool)
	for index := 0; index < len(args); index++ {
		name, value, hasEquals := strings.Cut(args[index], "=")
		if seen[name] {
			return options, fmt.Errorf("%s may be specified only once", name)
		}
		seen[name] = true
		next := func() (string, error) {
			if hasEquals {
				if value == "" {
					return "", fmt.Errorf("%s requires a value", name)
				}
				return value, nil
			}
			if index+1 == len(args) || args[index+1] == "" {
				return "", fmt.Errorf("%s requires a value", name)
			}
			index++
			return args[index], nil
		}
		var err error
		switch name {
		case "--game-dir":
			options.gameDir, err = next()
		case "--output":
			options.output, err = next()
		case "--eboot-elf":
			options.ebootELF, err = next()
		default:
			return options, fmt.Errorf("unknown option %q", name)
		}
		if err != nil {
			return options, err
		}
	}
	if options.gameDir == "" || options.output == "" {
		return options, errors.New("--game-dir and --output are required")
	}
	gameDir, err := resolveTrinityPath(options.gameDir)
	if err != nil {
		return options, err
	}
	output, err := resolveTrinityPath(options.output)
	if err != nil {
		return options, err
	}
	if pathWithinTrinityTree(output, gameDir) {
		return options, errors.New("output must be outside the Trinity retail tree")
	}
	if options.ebootELF != "" {
		options.ebootELF, err = resolveTrinityPath(options.ebootELF)
		if err != nil {
			return options, err
		}
	}
	options.gameDir = gameDir
	options.output = output
	return options, nil
}

func extractTrinityText(options trinityExtractOptions) (trinityManifest, error) {
	if info, err := os.Stat(options.gameDir); err != nil {
		return trinityManifest{}, fmt.Errorf("open retail tree: %w", err)
	} else if !info.IsDir() {
		return trinityManifest{}, errors.New("retail path is not a directory")
	}
	if _, err := os.Lstat(options.output); err == nil {
		return trinityManifest{}, fmt.Errorf("output already exists: %s", options.output)
	} else if !os.IsNotExist(err) {
		return trinityManifest{}, fmt.Errorf("inspect output: %w", err)
	}

	indexPath := filepath.Join(options.gameDir, "PS3_GAME", "USRDIR", "LINKDATA.IDX")
	dataPath := filepath.Join(options.gameDir, "PS3_GAME", "USRDIR", "LINKDATA.BIN")
	ebootPath := filepath.Join(options.gameDir, "PS3_GAME", "USRDIR", "EBOOT.BIN")
	sfoPath := filepath.Join(options.gameDir, "PS3_GAME", "PARAM.SFO")
	trophyMatches, err := filepath.Glob(filepath.Join(options.gameDir, "PS3_GAME", "TROPDIR", "*", "TROPHY.TRP"))
	if err != nil {
		return trinityManifest{}, fmt.Errorf("find TROPHY.TRP: %w", err)
	}
	if len(trophyMatches) != 1 {
		return trinityManifest{}, fmt.Errorf("expected one PS3_GAME/TROPDIR/*/TROPHY.TRP, found %d", len(trophyMatches))
	}

	archive, err := trinitylink.Open(indexPath, dataPath)
	if err != nil {
		return trinityManifest{}, err
	}
	defer archive.Close()
	entries := archive.Entries()
	if len(entries) != 6057 {
		return trinityManifest{}, fmt.Errorf("LINKDATA has %d members, want 6057 for Trinity", len(entries))
	}

	sfoData, err := os.ReadFile(sfoPath)
	if err != nil {
		return trinityManifest{}, fmt.Errorf("read PARAM.SFO: %w", err)
	}
	sfoStrings, err := parseTrinitySFO(sfoData)
	if err != nil {
		return trinityManifest{}, err
	}
	sfoValues := make(map[string]string, len(sfoStrings))
	for _, item := range sfoStrings {
		sfoValues[item.Key] = item.Value
	}
	release, ok := trinityReleaseSpecs[sfoValues["TITLE_ID"]]
	if !ok {
		return trinityManifest{}, fmt.Errorf("unsupported Trinity release TITLE_ID %q; want BLUS30503 or BLJM60212", sfoValues["TITLE_ID"])
	}
	for _, specification := range trinityTextMembers {
		entry := entries[specification.index]
		identity := specification.identity(release)
		if entry.Kind == trinitylink.KindAbsent {
			return trinityManifest{}, fmt.Errorf("required text member %d is absent", specification.index)
		}
		if entry.DecodedSize != identity.size {
			return trinityManifest{}, fmt.Errorf("required text member %d decoded size is %d, want authenticated %s retail size %d", specification.index, entry.DecodedSize, release.titleID, identity.size)
		}
	}

	trophyData, err := os.ReadFile(trophyMatches[0])
	if err != nil {
		return trinityManifest{}, fmt.Errorf("read TROPHY.TRP: %w", err)
	}
	trophyFiles, err := parseTrinityTRP(trophyData)
	if err != nil {
		return trinityManifest{}, err
	}
	var ebootELF []byte
	var ebootCandidates []trinityCandidate
	if options.ebootELF != "" {
		info, err := os.Stat(options.ebootELF)
		if err != nil {
			return trinityManifest{}, fmt.Errorf("stat decrypted EBOOT ELF: %w", err)
		}
		if info.Size() != release.ebootSize {
			return trinityManifest{}, fmt.Errorf("decrypted EBOOT ELF size is %d, want authenticated %s size %d", info.Size(), release.titleID, release.ebootSize)
		}
		ebootELF, err = os.ReadFile(options.ebootELF)
		if err != nil {
			return trinityManifest{}, fmt.Errorf("read decrypted EBOOT ELF: %w", err)
		}
		if trinityHash(ebootELF) != release.ebootSHA256 {
			return trinityManifest{}, fmt.Errorf("decrypted EBOOT ELF fingerprint is not the authenticated %s executable", release.titleID)
		}
		ebootCandidates, err = trinityELFCandidates(ebootELF, release.japanese)
		if err != nil {
			return trinityManifest{}, fmt.Errorf("validate decrypted EBOOT ELF: %w", err)
		}
		for _, marker := range release.ebootMarkers {
			if !bytes.Contains(ebootELF, append([]byte(marker), 0)) {
				return trinityManifest{}, fmt.Errorf("decrypted EBOOT ELF does not identify Trinity %s", release.titleID)
			}
		}
	}

	manifest := trinityManifest{
		SchemaVersion: 1,
		SourceGameDir: options.gameDir,
		Release: trinityRelease{
			Title: sfoValues["TITLE"], TitleID: sfoValues["TITLE_ID"],
			Version: sfoValues["VERSION"], Language: release.language,
		},
		ExcludedInputs: []trinityExclusion{
			{Path: "LINKDATA members other than 0, 2, 4, 5, 6, 14, 58, and 275", Reason: "absent/zero-fill members or classified gameplay data, model, texture, shader, and audio assets; no other standalone localized text table was identified"},
			{Path: "PS3_GAME/ICON1.PAM and PS3_GAME/USRDIR/movie/*.pam", Reason: "video streams, not standalone text tables; narration and subtitles are present in LINKDATA member 14"},
			{Path: "PS3_GAME/USRDIR/sound/* and PS3_GAME/SND0.AT3", Reason: "audio containers, not text assets"},
			{Path: "PS3_GAME/*.PNG and PS3_GAME/PS3LOGO.DAT", Reason: "graphics, not text assets"},
			{Path: "TROPHY.TRP:*.PNG", Reason: "trophy icon graphics; all SFM text entries from the same container were extracted"},
			{Path: "PS3_DISC.SFB, PS3_GAME/LICDIR/LIC.DAT, and PS3_UPDATE/PS3UPDAT.PUP", Reason: "disc metadata, license payload, and system update package rather than localized text assets"},
		},
	}
	if options.ebootELF == "" {
		manifest.ExcludedInputs = append([]trinityExclusion{{Path: "PS3_GAME/USRDIR/EBOOT.BIN", Reason: "encrypted PS3 SELF executable; pass an independently decrypted EBOOT.elf with --eboot-elf to include its strings"}}, manifest.ExcludedInputs...)
	}
	for _, entry := range entries {
		switch entry.Kind {
		case trinitylink.KindRaw:
			manifest.LINKDATA.Raw++
		case trinitylink.KindDeflate:
			manifest.LINKDATA.Deflate++
		case trinitylink.KindZeroFill:
			manifest.LINKDATA.ZeroFill++
		case trinitylink.KindAbsent:
			manifest.LINKDATA.Absent++
		}
	}
	manifest.LINKDATA.MemberCount = len(entries)
	for _, source := range []struct {
		path     string
		relative string
	}{
		{indexPath, "PS3_GAME/USRDIR/LINKDATA.IDX"},
		{dataPath, "PS3_GAME/USRDIR/LINKDATA.BIN"},
		{ebootPath, "PS3_GAME/USRDIR/EBOOT.BIN"},
		{sfoPath, "PS3_GAME/PARAM.SFO"},
		{trophyMatches[0], filepath.ToSlash(mustTrinityRelative(options.gameDir, trophyMatches[0]))},
	} {
		identity, err := identifyTrinityFile(source.path, source.relative)
		if err != nil {
			return trinityManifest{}, err
		}
		manifest.SourceFiles = append(manifest.SourceFiles, identity)
	}
	if options.ebootELF != "" {
		identity, err := identifyTrinityFile(options.ebootELF, options.ebootELF)
		if err != nil {
			return trinityManifest{}, err
		}
		manifest.SourceFiles = append(manifest.SourceFiles, identity)
	}

	parent := filepath.Dir(options.output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return trinityManifest{}, fmt.Errorf("create output parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".trinity-extract-stage-*")
	if err != nil {
		return trinityManifest{}, fmt.Errorf("create output staging directory: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()
	directories := []string{"linkdata", "metadata", "trophies"}
	if options.ebootELF != "" {
		directories = append(directories, "executable")
	}
	for _, directory := range directories {
		if err := os.Mkdir(filepath.Join(staging, directory), 0o755); err != nil {
			return trinityManifest{}, fmt.Errorf("create output directory: %w", err)
		}
	}

	for _, specification := range trinityTextMembers {
		entry := entries[specification.index]
		identity := specification.identity(release)
		var decoded bytes.Buffer
		decoded.Grow(int(entry.DecodedSize))
		if err := archive.WriteEntry(specification.index, &decoded); err != nil {
			return trinityManifest{}, err
		}
		if trinityHash(decoded.Bytes()) != identity.sha256 {
			return trinityManifest{}, fmt.Errorf("decoded LINKDATA member %d fingerprint is not the authenticated %s text asset", specification.index, release.titleID)
		}
		base := fmt.Sprintf("%06d-%s", specification.index, specification.name)
		decodedRelative := filepath.ToSlash(filepath.Join("linkdata", base+".bin"))
		stringsRelative := filepath.ToSlash(filepath.Join("linkdata", base+".strings.json"))
		if err := os.WriteFile(filepath.Join(staging, filepath.FromSlash(decodedRelative)), decoded.Bytes(), 0o644); err != nil {
			return trinityManifest{}, fmt.Errorf("write decoded LINKDATA member %d: %w", specification.index, err)
		}
		candidates := trinityCandidates(decoded.Bytes(), release.japanese)
		document := trinityCandidateDocument{SchemaVersion: 1, Source: fmt.Sprintf("LINKDATA:%d", specification.index), Encoding: trinityCandidateEncoding(release.japanese), Candidates: candidates}
		if err := writeTrinityJSON(filepath.Join(staging, filepath.FromSlash(stringsRelative)), document); err != nil {
			return trinityManifest{}, err
		}
		index := specification.index
		offset := entry.Offset
		storedSize := entry.StoredSize
		manifest.TextAssets = append(manifest.TextAssets, trinityTextAsset{
			Source:         fmt.Sprintf("LINKDATA:%d", specification.index),
			Category:       specification.category,
			DecodedFile:    decodedRelative,
			StringsFile:    stringsRelative,
			Size:           decoded.Len(),
			SHA256:         trinityHash(decoded.Bytes()),
			CandidateCount: len(candidates),
			LINKDATAIndex:  &index,
			StoredOffset:   &offset,
			StoredSize:     &storedSize,
			StorageKind:    string(entry.Kind),
		})
	}
	if options.ebootELF != "" {
		elfRelative := "executable/EBOOT.elf"
		stringsRelative := "executable/EBOOT.elf.strings.json"
		if err := os.WriteFile(filepath.Join(staging, filepath.FromSlash(elfRelative)), ebootELF, 0o644); err != nil {
			return trinityManifest{}, fmt.Errorf("write decrypted EBOOT ELF: %w", err)
		}
		document := trinityCandidateDocument{SchemaVersion: 1, Source: "EBOOT.elf non-executable sections", Encoding: trinityCandidateEncoding(release.japanese), Candidates: ebootCandidates}
		if err := writeTrinityJSON(filepath.Join(staging, filepath.FromSlash(stringsRelative)), document); err != nil {
			return trinityManifest{}, err
		}
		manifest.TextAssets = append(manifest.TextAssets, trinityTextAsset{
			Source: "decrypted EBOOT.elf", Category: "executable string data", DecodedFile: elfRelative,
			StringsFile: stringsRelative, Size: len(ebootELF), SHA256: trinityHash(ebootELF), CandidateCount: len(ebootCandidates),
		})
	}

	sfoRelative := "metadata/PARAM.SFO.strings.json"
	sfoOutputPath := filepath.Join(staging, filepath.FromSlash(sfoRelative))
	if err := writeTrinityJSON(sfoOutputPath, trinitySFOStringDocument{SchemaVersion: 1, Source: "PS3_GAME/PARAM.SFO", Strings: sfoStrings}); err != nil {
		return trinityManifest{}, err
	}
	sfoOutput, err := identifyTrinityFile(sfoOutputPath, sfoRelative)
	if err != nil {
		return trinityManifest{}, err
	}
	manifest.TextAssets = append(manifest.TextAssets, trinityTextAsset{
		Source: "PS3_GAME/PARAM.SFO", Category: "release metadata strings", DecodedFile: sfoRelative,
		Size: int(sfoOutput.Size), SHA256: sfoOutput.SHA256, CandidateCount: len(sfoStrings),
	})
	for _, trophy := range trophyFiles {
		relative := filepath.ToSlash(filepath.Join("trophies", trophy.name))
		if err := os.WriteFile(filepath.Join(staging, filepath.FromSlash(relative)), trophy.data, 0o644); err != nil {
			return trinityManifest{}, fmt.Errorf("write trophy text %s: %w", trophy.name, err)
		}
		manifest.TextAssets = append(manifest.TextAssets, trinityTextAsset{
			Source: "TROPHY.TRP:" + trophy.name, Category: "trophy XML", DecodedFile: relative,
			Size: len(trophy.data), SHA256: trinityHash(trophy.data),
		})
	}
	if err := writeTrinityJSON(filepath.Join(staging, "manifest.json"), manifest); err != nil {
		return trinityManifest{}, err
	}
	if err := os.Rename(staging, options.output); err != nil {
		return trinityManifest{}, fmt.Errorf("publish output: %w", err)
	}
	published = true
	return manifest, nil
}

func parseTrinitySFO(data []byte) ([]trinitySFOString, error) {
	const headerSize = 20
	const indexSize = 16
	if len(data) < headerSize || binary.LittleEndian.Uint32(data[:4]) != 0x46535000 {
		return nil, errors.New("PARAM.SFO has an invalid header")
	}
	keyStart := uint64(binary.LittleEndian.Uint32(data[8:12]))
	dataStart := uint64(binary.LittleEndian.Uint32(data[12:16]))
	count := uint64(binary.LittleEndian.Uint32(data[16:20]))
	indexEnd := uint64(headerSize) + count*indexSize
	if count > uint64(len(data))/indexSize || indexEnd > keyStart || keyStart > dataStart || dataStart > uint64(len(data)) {
		return nil, errors.New("PARAM.SFO has invalid table bounds")
	}
	seen := make(map[string]bool)
	var stringsFound []trinitySFOString
	for index := uint64(0); index < count; index++ {
		position := uint64(headerSize) + index*indexSize
		record := data[position : position+indexSize]
		keyPosition := keyStart + uint64(binary.LittleEndian.Uint16(record[:2]))
		if keyPosition >= dataStart {
			return nil, fmt.Errorf("PARAM.SFO entry %d key is out of bounds", index)
		}
		keyEnd := bytes.IndexByte(data[keyPosition:dataStart], 0)
		if keyEnd < 0 {
			return nil, fmt.Errorf("PARAM.SFO entry %d key is unterminated", index)
		}
		key := string(data[keyPosition : keyPosition+uint64(keyEnd)])
		if key == "" || seen[key] {
			return nil, fmt.Errorf("PARAM.SFO entry %d has an empty or duplicate key", index)
		}
		seen[key] = true
		length := uint64(binary.LittleEndian.Uint32(record[4:8]))
		maximum := uint64(binary.LittleEndian.Uint32(record[8:12]))
		offset := uint64(binary.LittleEndian.Uint32(record[12:16]))
		if length > maximum || offset > uint64(len(data))-dataStart || maximum > uint64(len(data))-dataStart-offset {
			return nil, fmt.Errorf("PARAM.SFO entry %q value is out of bounds", key)
		}
		if binary.LittleEndian.Uint16(record[2:4]) != 0x0204 {
			continue
		}
		value := data[dataStart+offset : dataStart+offset+length]
		if len(value) == 0 || value[len(value)-1] != 0 || !utf8.Valid(value[:len(value)-1]) {
			return nil, fmt.Errorf("PARAM.SFO entry %q has an invalid string", key)
		}
		stringsFound = append(stringsFound, trinitySFOString{Key: key, Value: string(value[:len(value)-1])})
	}
	return stringsFound, nil
}

func parseTrinityTRP(data []byte) ([]trinityTrophyFile, error) {
	const headerSize = 0x40
	if len(data) < headerSize || binary.BigEndian.Uint32(data[:4]) != 0xdca24d00 || binary.BigEndian.Uint32(data[4:8]) != 1 {
		return nil, errors.New("TROPHY.TRP has an invalid header")
	}
	if binary.BigEndian.Uint64(data[8:16]) != uint64(len(data)) {
		return nil, errors.New("TROPHY.TRP declared size differs from file size")
	}
	count := uint64(binary.BigEndian.Uint32(data[16:20]))
	recordSize := uint64(binary.BigEndian.Uint32(data[20:24]))
	if recordSize != 0x40 || count > uint64(len(data))/recordSize || headerSize+count*recordSize > uint64(len(data)) {
		return nil, errors.New("TROPHY.TRP has invalid entry-table bounds")
	}
	seen := make(map[string]bool)
	var result []trinityTrophyFile
	for index := uint64(0); index < count; index++ {
		position := uint64(headerSize) + index*recordSize
		record := data[position : position+recordSize]
		nameEnd := bytes.IndexByte(record[:0x20], 0)
		if nameEnd <= 0 {
			return nil, fmt.Errorf("TROPHY.TRP entry %d has an invalid name", index)
		}
		name := string(record[:nameEnd])
		if seen[name] || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
			return nil, fmt.Errorf("TROPHY.TRP entry %d has an unsafe or duplicate name", index)
		}
		seen[name] = true
		offset := binary.BigEndian.Uint64(record[0x20:0x28])
		size := binary.BigEndian.Uint64(record[0x28:0x30])
		if offset > uint64(len(data)) || size > uint64(len(data))-offset {
			return nil, fmt.Errorf("TROPHY.TRP entry %q is out of bounds", name)
		}
		if !strings.HasSuffix(name, ".SFM") {
			continue
		}
		contents := data[offset : offset+size]
		if !utf8.Valid(contents) {
			return nil, fmt.Errorf("TROPHY.TRP entry %q is not UTF-8", name)
		}
		decoder := xml.NewDecoder(bytes.NewReader(contents))
		for {
			if _, err := decoder.Token(); err == io.EOF {
				break
			} else if err != nil {
				return nil, fmt.Errorf("TROPHY.TRP entry %q is not valid XML: %w", name, err)
			}
		}
		result = append(result, trinityTrophyFile{name: name, data: append([]byte(nil), contents...)})
	}
	if len(result) == 0 {
		return nil, errors.New("TROPHY.TRP contains no SFM text assets")
	}
	return result, nil
}

func trinityCandidateEncoding(japanese bool) string {
	if japanese {
		return "cp932"
	}
	return "utf-8"
}

func trinityCandidates(data []byte, japanese bool) []trinityCandidate {
	var result []trinityCandidate
	start := -1
	for position := 0; position <= len(data); {
		if position == len(data) {
			if start >= 0 && position-start >= 2 {
				text, err := trinityDecodeCandidate(data[start:position], japanese)
				if err == nil {
					result = append(result, trinityCandidate{Offset: start, Text: text})
				}
			}
			break
		}
		length := trinityTextRuneLength(data[position:], japanese)
		if length > 0 {
			if start < 0 {
				start = position
			}
			position += length
			continue
		}
		if start >= 0 && position-start >= 2 {
			text, err := trinityDecodeCandidate(data[start:position], japanese)
			if err == nil {
				result = append(result, trinityCandidate{Offset: start, Text: text, TerminatedByNUL: data[position] == 0})
			}
		}
		start = -1
		position++
	}
	return result
}

func trinityELFCandidates(data []byte, japanese bool) ([]trinityCandidate, error) {
	const elfHeaderSize = 64
	const sectionHeaderSize = 64
	const (
		sectionProgramBits = 1
		sectionAllocated   = 2
		sectionExecutable  = 4
	)
	if len(data) < elfHeaderSize || !bytes.Equal(data[:4], []byte("\x7fELF")) || data[4] != 2 || data[5] != 2 || binary.BigEndian.Uint16(data[16:18]) != 2 || binary.BigEndian.Uint16(data[18:20]) != 21 {
		return nil, errors.New("expected a big-endian 64-bit PowerPC executable ELF")
	}
	sectionOffset := binary.BigEndian.Uint64(data[40:48])
	entrySize := uint64(binary.BigEndian.Uint16(data[58:60]))
	sectionCount := uint64(binary.BigEndian.Uint16(data[60:62]))
	if entrySize != sectionHeaderSize || sectionCount == 0 || sectionOffset > uint64(len(data)) || sectionCount > (uint64(len(data))-sectionOffset)/entrySize {
		return nil, errors.New("invalid ELF section-table bounds")
	}
	type sectionRange struct {
		index  uint64
		offset uint64
		size   uint64
	}
	var ranges []sectionRange
	for index := uint64(0); index < sectionCount; index++ {
		position := sectionOffset + index*entrySize
		header := data[position : position+entrySize]
		if binary.BigEndian.Uint32(header[4:8]) != sectionProgramBits {
			continue
		}
		flags := binary.BigEndian.Uint64(header[8:16])
		if flags&sectionAllocated == 0 || flags&sectionExecutable != 0 {
			continue
		}
		offset := binary.BigEndian.Uint64(header[24:32])
		size := binary.BigEndian.Uint64(header[32:40])
		if offset > uint64(len(data)) || size > uint64(len(data))-offset {
			return nil, fmt.Errorf("ELF section %d is out of bounds", index)
		}
		if size > 0 {
			ranges = append(ranges, sectionRange{index: index, offset: offset, size: size})
		}
	}
	sort.Slice(ranges, func(left, right int) bool {
		return ranges[left].offset < ranges[right].offset
	})
	var result []trinityCandidate
	var previousEnd uint64
	for index, section := range ranges {
		if index > 0 && section.offset < previousEnd {
			return nil, fmt.Errorf("ELF section %d overlaps another scanned section", section.index)
		}
		previousEnd = section.offset + section.size
		result = append(result, trinityTerminatedCandidates(data[section.offset:previousEnd], int(section.offset), japanese)...)
	}
	if len(result) == 0 {
		return nil, errors.New("ELF contains no NUL-terminated string candidates in non-executable sections")
	}
	return result, nil
}

func trinityTerminatedCandidates(data []byte, baseOffset int, japanese bool) []trinityCandidate {
	var result []trinityCandidate
	start := -1
	for position := 0; position < len(data); {
		length := trinityTextRuneLength(data[position:], japanese)
		if length > 0 {
			if start < 0 {
				start = position
			}
			position += length
			continue
		}
		if data[position] == 0 && start >= 0 && position-start >= 2 {
			text, err := trinityDecodeCandidate(data[start:position], japanese)
			if err == nil {
				result = append(result, trinityCandidate{Offset: baseOffset + start, Text: text, TerminatedByNUL: true})
			}
		}
		start = -1
		position++
	}
	return result
}

func trinityDecodeCandidate(data []byte, japanese bool) (string, error) {
	if japanese {
		return cp932.Decode(data)
	}
	return string(data), nil
}

func trinityTextRuneLength(data []byte, japanese bool) int {
	if japanese {
		return trinityCP932RuneLength(data)
	}
	if data[0] < utf8.RuneSelf {
		if data[0] == '\t' || data[0] == '\n' || data[0] == '\r' || data[0] >= 0x20 && data[0] <= 0x7e {
			return 1
		}
		return 0
	}
	r, size := utf8.DecodeRune(data)
	if r == utf8.RuneError && size == 1 || !unicode.IsPrint(r) {
		return 0
	}
	return size
}

func trinityCP932RuneLength(data []byte) int {
	if data[0] < utf8.RuneSelf {
		if data[0] == '\t' || data[0] == '\n' || data[0] == '\r' || data[0] >= 0x20 && data[0] <= 0x7e {
			return 1
		}
		return 0
	}
	length := 1
	if data[0] >= 0x81 && data[0] <= 0x9f || data[0] >= 0xe0 && data[0] <= 0xfc {
		if len(data) < 2 || data[1] < 0x40 || data[1] == 0x7f || data[1] > 0xfc {
			return 0
		}
		length = 2
	} else if data[0] < 0xa1 || data[0] > 0xdf {
		return 0
	}
	decoded, err := cp932.Decode(data[:length])
	if err != nil {
		return 0
	}
	for _, r := range decoded {
		if !unicode.IsPrint(r) {
			return 0
		}
	}
	return length
}

func identifyTrinityFile(path, relative string) (trinitySourceFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return trinitySourceFile{}, fmt.Errorf("open source %s: %w", relative, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return trinitySourceFile{}, fmt.Errorf("stat source %s: %w", relative, err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return trinitySourceFile{}, fmt.Errorf("hash source %s: %w", relative, err)
	}
	return trinitySourceFile{Path: relative, Size: info.Size(), SHA256: hex.EncodeToString(hasher.Sum(nil))}, nil
}

func writeTrinityJSON(path string, value any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(value)
	closeErr := file.Close()
	if encodeErr != nil {
		return fmt.Errorf("encode %s: %w", path, encodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	return nil
}

func trinityHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func resolveTrinityPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	var missing []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("resolve path %s", path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func pathWithinTrinityTree(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func mustTrinityRelative(root, path string) string {
	relative, _ := filepath.Rel(root, path)
	return relative
}
