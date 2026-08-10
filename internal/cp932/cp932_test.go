// SPDX-License-Identifier: GPL-3.0-or-later

package cp932

import (
	"bytes"
	"testing"
)

func TestRoundTripProjectText(t *testing.T) {
	text := "Zill O'll ― … ’ “ ” × Ｖ 日本語 ｶﾅ"

	encoded, err := Encode(text)
	if err != nil {
		t.Fatalf("Encode(%q): %v", text, err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode(encoded): %v", err)
	}
	if decoded != text {
		t.Fatalf("round trip = %q, want %q", decoded, text)
	}
}

func TestEncodeRejectsUnsupportedText(t *testing.T) {
	if _, err := Encode("not in CP932: 😀"); err == nil {
		t.Fatal("Encode accepted an unsupported character")
	}
}

func TestDecodeRejectsMalformedBytes(t *testing.T) {
	input := []byte{0x82}
	before := bytes.Clone(input)

	if _, err := Decode(input); err == nil {
		t.Fatal("Decode accepted an incomplete lead byte")
	}
	if !bytes.Equal(input, before) {
		t.Fatal("Decode mutated its input")
	}
}
