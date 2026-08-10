// SPDX-License-Identifier: GPL-3.0-or-later

package fixeddata

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/HK47196/zill/internal/cp932"
	"github.com/pelletier/go-toml/v2"
)

const (
	equipmentTableOffset = 0x7800
	equipmentRecordCount = 132
	equipmentRecordSize  = 0x24
	equipmentNameOffset  = 0x11
	equipmentNameSize    = 17
)

var bindataSHA256 = [sha256.Size]byte{0x32, 0x41, 0xfc, 0x00, 0x0f, 0x3d, 0x52, 0xfe, 0x85, 0x22, 0xba, 0xaa, 0x98, 0x5f, 0xd8, 0x66, 0xe2, 0x9d, 0x64, 0xd3, 0xa0, 0xf2, 0x3a, 0xc4, 0xe2, 0x8b, 0x66, 0xde, 0xe9, 0x57, 0xde, 0x3e}

// EquipmentName is one guarded fixed-width bindata.dat equipment name.
type EquipmentName struct {
	Source string `toml:"source"`
	Text   string `toml:"text"`
}

// EquipmentTranslations contains all selectors 1 through 132.
type EquipmentTranslations map[int]EquipmentName

// ParseEquipment strictly parses and validates release/strings/equipment.toml.
func ParseEquipment(data []byte) (EquipmentTranslations, error) {
	var translations EquipmentTranslations
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&translations); err != nil {
		return nil, fmt.Errorf("decode equipment translations: %w", err)
	}
	if err := validateEquipment(translations); err != nil {
		return nil, err
	}
	return translations, nil
}

func validateEquipment(translations EquipmentTranslations) error {
	if len(translations) != equipmentRecordCount {
		return fmt.Errorf("equipment translations contain %d names; want %d", len(translations), equipmentRecordCount)
	}
	for selector := 1; selector <= equipmentRecordCount; selector++ {
		name, ok := translations[selector]
		if !ok {
			return fmt.Errorf("equipment selector %d is missing", selector)
		}
		if name.Source == "" || name.Text == "" {
			return fmt.Errorf("equipment selector %d requires source and text", selector)
		}
		encoded, err := cp932.Encode(name.Text)
		if err != nil {
			return fmt.Errorf("equipment selector %d text: %w", selector, err)
		}
		if len(encoded) > equipmentNameSize-1 {
			return fmt.Errorf("equipment selector %d text uses %d bytes; maximum is 16", selector, len(encoded))
		}
	}
	return nil
}

// ApplyEquipment authenticates bindata.dat and every original CP932 name before
// returning a copy with only the 17-byte name fields changed.
func ApplyEquipment(source []byte, translations EquipmentTranslations) ([]byte, error) {
	if err := validateEquipment(translations); err != nil {
		return nil, err
	}
	if sha256.Sum256(source) != bindataSHA256 {
		return nil, fmt.Errorf("unsupported bindata.dat fingerprint")
	}
	type encodedName struct {
		selector int
		text     []byte
	}
	encoded := make([]encodedName, 0, equipmentRecordCount)
	for selector := 1; selector <= equipmentRecordCount; selector++ {
		field := equipmentTableOffset + (selector-1)*equipmentRecordSize + equipmentNameOffset
		original := source[field : field+equipmentNameSize]
		nul := bytes.IndexByte(original, 0)
		if nul < 0 || !bytes.Equal(original[nul+1:], bytes.Repeat([]byte{0xcd}, equipmentNameSize-nul-1)) {
			return nil, fmt.Errorf("equipment selector %d has invalid source field padding", selector)
		}
		decoded, err := cp932.Decode(original[:nul])
		if err != nil || decoded != translations[selector].Source {
			return nil, fmt.Errorf("equipment selector %d source guard does not match", selector)
		}
		text, _ := cp932.Encode(translations[selector].Text)
		encoded = append(encoded, encodedName{selector, text})
	}
	result := bytes.Clone(source)
	for _, name := range encoded {
		field := equipmentTableOffset + (name.selector-1)*equipmentRecordSize + equipmentNameOffset
		copy(result[field:field+equipmentNameSize], append(append(bytes.Clone(name.text), 0), bytes.Repeat([]byte{0xcd}, equipmentNameSize-1-len(name.text))...))
	}
	return result, nil
}
