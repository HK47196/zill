// SPDX-License-Identifier: GPL-3.0-or-later

// Package cp932 provides the patch's fail-closed text encoding boundary.
package cp932

import (
	"bytes"
	"fmt"
	"unicode"

	"golang.org/x/text/encoding/japanese"
)

// Encode converts Unicode text to CP932-compatible Shift JIS bytes.
// It rejects characters that the game encoding cannot represent.
func Encode(text string) ([]byte, error) {
	encoded, err := japanese.ShiftJIS.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("encode CP932: %w", err)
	}
	return encoded, nil
}

// Decode converts CP932-compatible Shift JIS bytes to Unicode text.
// The x/text decoder substitutes U+FFFD for malformed input, so this wrapper
// rejects that substitution rather than silently accepting damaged game text.
func Decode(encoded []byte) (string, error) {
	decoded, err := japanese.ShiftJIS.NewDecoder().Bytes(encoded)
	if err != nil {
		return "", fmt.Errorf("decode CP932: %w", err)
	}
	if bytes.ContainsRune(decoded, unicode.ReplacementChar) {
		return "", fmt.Errorf("decode CP932: invalid byte sequence")
	}
	return string(decoded), nil
}
