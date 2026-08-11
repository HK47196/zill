// SPDX-License-Identifier: GPL-3.0-or-later

// Package fixeddata compiles authenticated translations stored outside message banks.
package fixeddata

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/cp932"
	"github.com/pelletier/go-toml/v2"
)

var patchedELFSHA256 = [sha256.Size]byte{0x3a, 0x1a, 0xe5, 0x41, 0xe2, 0xb6, 0x8f, 0xeb, 0x03, 0xb2, 0xbe, 0x32, 0xe1, 0x6a, 0xaf, 0x82, 0xab, 0x7d, 0x1c, 0x4e, 0xea, 0x42, 0x5d, 0x97, 0x16, 0xf2, 0x92, 0x80, 0x2e, 0x6c, 0x74, 0xf5}

const ebootFieldCount = 557

// EBOOTField is one guarded fixed-width executable string.
type EBOOTField struct {
	Source      string `toml:"source"`
	Replacement string `toml:"replacement"`
}

// EBOOTTranslations maps ELF file offsets to fixed-width replacements.
type EBOOTTranslations map[uint64]EBOOTField

// ParseEBOOT strictly parses and validates release/strings/eboot.toml.
func ParseEBOOT(data []byte) (EBOOTTranslations, error) {
	var raw map[string]EBOOTField
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode eboot translations: %w", err)
	}
	if len(raw) != ebootFieldCount {
		return nil, fmt.Errorf("eboot translations contain %d fields; want %d", len(raw), ebootFieldCount)
	}
	translations := make(EBOOTTranslations, len(raw))
	for key, field := range raw {
		offset, err := strconv.ParseUint(key, 0, 64)
		if err != nil || !strings.HasPrefix(key, "0x") {
			return nil, fmt.Errorf("invalid eboot offset %q", key)
		}
		translations[offset] = field
	}
	if err := validateEBOOT(translations); err != nil {
		return nil, err
	}
	return translations, nil
}

// ApplyEBOOT authenticates the supported decrypted ELF after its runtime patch
// manifest has been applied, then verifies every source field before returning
// a translated copy. It never mutates source.
func ApplyEBOOT(source []byte, translations EBOOTTranslations) ([]byte, error) {
	if err := validateEBOOT(translations); err != nil {
		return nil, err
	}
	if sha256.Sum256(source) != patchedELFSHA256 {
		return nil, fmt.Errorf("unsupported patched ELF fingerprint")
	}
	type replacement struct {
		offset, capacity int
		data             []byte
	}
	replacements := make([]replacement, 0, len(translations))
	for offset, field := range translations {
		if field.Source == "" || field.Replacement == "" {
			return nil, fmt.Errorf("eboot field %#x requires source and replacement", offset)
		}
		sourceText, err := executableNewlines(field.Source)
		if err != nil {
			return nil, fmt.Errorf("eboot field %#x source: %w", offset, err)
		}
		replacementText, err := executableNewlines(field.Replacement)
		if err != nil {
			return nil, fmt.Errorf("eboot field %#x replacement: %w", offset, err)
		}
		expected, err := cp932.Encode(sourceText)
		if err != nil {
			return nil, fmt.Errorf("eboot field %#x source: %w", offset, err)
		}
		encoded, err := cp932.Encode(replacementText)
		if err != nil {
			return nil, fmt.Errorf("eboot field %#x replacement: %w", offset, err)
		}
		if len(encoded) > len(expected) {
			return nil, fmt.Errorf("eboot field %#x replacement uses %d bytes; capacity is %d", offset, len(encoded), len(expected))
		}
		if offset > uint64(len(source)) || uint64(len(expected)+1) > uint64(len(source))-offset {
			return nil, fmt.Errorf("eboot field %#x is outside the ELF", offset)
		}
		start := int(offset)
		if !bytes.Equal(source[start:start+len(expected)], expected) || source[start+len(expected)] != 0 {
			return nil, fmt.Errorf("eboot field %#x source guard does not match", offset)
		}
		replacements = append(replacements, replacement{start, len(expected), encoded})
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].offset < replacements[j].offset })
	for i := 1; i < len(replacements); i++ {
		if replacements[i].offset < replacements[i-1].offset+replacements[i-1].capacity {
			return nil, fmt.Errorf("eboot translation fields overlap")
		}
	}
	result := bytes.Clone(source)
	for _, replacement := range replacements {
		clear(result[replacement.offset : replacement.offset+replacement.capacity])
		copy(result[replacement.offset:], replacement.data)
	}
	return result, nil
}

func validateEBOOT(translations EBOOTTranslations) error {
	if len(translations) != ebootFieldCount {
		return fmt.Errorf("eboot translations contain %d fields; want %d", len(translations), ebootFieldCount)
	}
	for offset, field := range translations {
		if field.Source == "" || field.Replacement == "" {
			return fmt.Errorf("eboot field %#x requires source and replacement", offset)
		}
		sourceText, err := executableNewlines(field.Source)
		if err != nil {
			return fmt.Errorf("eboot field %#x source: %w", offset, err)
		}
		replacementText, err := executableNewlines(field.Replacement)
		if err != nil {
			return fmt.Errorf("eboot field %#x replacement: %w", offset, err)
		}
		expected, err := cp932.Encode(sourceText)
		if err != nil {
			return fmt.Errorf("eboot field %#x source: %w", offset, err)
		}
		encoded, err := cp932.Encode(replacementText)
		if err != nil {
			return fmt.Errorf("eboot field %#x replacement: %w", offset, err)
		}
		if len(encoded) > len(expected) {
			return fmt.Errorf("eboot field %#x replacement uses %d bytes; capacity is %d", offset, len(encoded), len(expected))
		}
	}
	return nil
}

func executableNewlines(text string) (string, error) {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if strings.ContainsRune(normalized, '\r') {
		return "", fmt.Errorf("lone carriage return")
	}
	return normalized, nil
}
