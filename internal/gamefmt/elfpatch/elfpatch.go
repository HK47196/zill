// Package elfpatch applies the authenticated, declarative BOOT.BIN runtime
// patch set. Fixed-field translated strings intentionally do not belong here.
package elfpatch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	manifestFormat  = "zill-executable-patches"
	manifestVersion = 1
	manifestTarget  = "SYSDIR/BOOT.BIN"
	patchCount      = 35
)

// Manifest is the complete ordered executable patch contract.
type Manifest struct {
	Format       string  `toml:"format"`
	Version      int     `toml:"version"`
	Target       string  `toml:"target"`
	SourceSHA256 string  `toml:"source_sha256"`
	ResultSHA256 string  `toml:"result_sha256"`
	Patches      []Patch `toml:"patch"`
}

// Patch is one guarded in-place replacement from the manifest.
type Patch struct {
	ID             string  `toml:"id"`
	Feature        string  `toml:"feature"`
	Document       string  `toml:"document"`
	Kind           string  `toml:"kind"`
	Offset         uint64  `toml:"offset"`
	Expected       string  `toml:"expected"`
	Replacement    string  `toml:"replacement"`
	Purpose        string  `toml:"purpose"`
	Field          string  `toml:"field,omitempty"`
	VirtualAddress *uint64 `toml:"virtual_address,omitempty"`
	Before         string  `toml:"before,omitempty"`
	After          string  `toml:"after,omitempty"`
}

// ParseManifest strictly decodes and validates an executable patch manifest.
func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode executable patch manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate checks the complete manifest schema without consulting an
// executable. It is also called by Apply so manually constructed manifests do
// not bypass the contract.
func (m Manifest) Validate() error {
	if m.Format != manifestFormat || m.Version != manifestVersion || m.Target != manifestTarget {
		return fmt.Errorf("unsupported executable patch manifest identity")
	}
	if _, err := decodeHash(m.SourceSHA256); err != nil {
		return fmt.Errorf("invalid executable source_sha256: %w", err)
	}
	if _, err := decodeHash(m.ResultSHA256); err != nil {
		return fmt.Errorf("invalid executable result_sha256: %w", err)
	}
	if len(m.Patches) != patchCount {
		return fmt.Errorf("executable manifest has %d patches, want %d", len(m.Patches), patchCount)
	}

	wantFeatures := []struct {
		name  string
		count int
	}{
		{name: "large-memory", count: 1},
		{name: "message-arena", count: 11},
		{name: "wide-message-offsets", count: 12},
		{name: "profile-biography", count: 9},
		{name: "title-attribution", count: 2},
	}
	featureIndex, featureRemaining := 0, wantFeatures[0].count
	identities := make(map[string]struct{}, len(m.Patches))
	type span struct{ start, end uint64 }
	spans := make([]span, 0, len(m.Patches))
	for i, patch := range m.Patches {
		if featureIndex >= len(wantFeatures) || patch.Feature != wantFeatures[featureIndex].name {
			return fmt.Errorf("patch %d (%q) is out of feature order", i, patch.ID)
		}
		featureRemaining--
		if featureRemaining == 0 && featureIndex+1 < len(wantFeatures) {
			featureIndex++
			featureRemaining = wantFeatures[featureIndex].count
		}
		if patch.ID != fmt.Sprintf("%s-%07x", patch.Feature, patch.Offset) {
			return fmt.Errorf("patch %d has invalid identity %q", i, patch.ID)
		}
		if _, duplicate := identities[patch.ID]; duplicate {
			return fmt.Errorf("duplicate executable patch identity %q", patch.ID)
		}
		identities[patch.ID] = struct{}{}
		if patch.Document != patch.Feature+".md" || path.Base(patch.Document) != patch.Document {
			return fmt.Errorf("patch %q has invalid feature document %q", patch.ID, patch.Document)
		}
		if strings.TrimSpace(patch.Purpose) == "" {
			return fmt.Errorf("patch %q has no purpose", patch.ID)
		}
		expected, err := decodeBytes(patch.Expected)
		if err != nil {
			return fmt.Errorf("patch %q expected bytes: %w", patch.ID, err)
		}
		replacement, err := decodeBytes(patch.Replacement)
		if err != nil {
			return fmt.Errorf("patch %q replacement bytes: %w", patch.ID, err)
		}
		if len(expected) != 4 || len(replacement) != len(expected) {
			return fmt.Errorf("patch %q must replace exactly one four-byte field or instruction", patch.ID)
		}
		if bytes.Equal(expected, replacement) {
			return fmt.Errorf("patch %q replacement does not change its guarded bytes", patch.ID)
		}
		wantKind := "mips32le"
		if i == 0 {
			wantKind = "little-endian-u32"
		} else if i == 1 || i == 2 {
			wantKind = "elf-field"
		}
		if patch.Kind != wantKind {
			return fmt.Errorf("patch %q has kind %q, want %q", patch.ID, patch.Kind, wantKind)
		}
		switch patch.Kind {
		case "mips32le":
			if patch.VirtualAddress == nil || patch.Offset < 0x80 || *patch.VirtualAddress != patch.Offset-0x80 || strings.TrimSpace(patch.Before) == "" || strings.TrimSpace(patch.After) == "" || patch.Before == patch.After || patch.Field != "" {
				return fmt.Errorf("patch %q has invalid MIPS disassembly metadata", patch.ID)
			}
		case "elf-field", "little-endian-u32":
			if strings.TrimSpace(patch.Field) == "" || patch.VirtualAddress != nil || patch.Before != "" || patch.After != "" {
				return fmt.Errorf("patch %q has invalid structure-field metadata", patch.ID)
			}
		default:
			return fmt.Errorf("patch %q has unsupported kind %q", patch.ID, patch.Kind)
		}
		current := span{start: patch.Offset, end: patch.Offset + uint64(len(expected))}
		if current.end < current.start {
			return fmt.Errorf("patch %q range overflows", patch.ID)
		}
		for _, prior := range spans {
			if current.start < prior.end && prior.start < current.end {
				return fmt.Errorf("patch %q overlaps another patch", patch.ID)
			}
		}
		spans = append(spans, current)
	}
	return nil
}

// Apply authenticates source, verifies every guard before making any
// replacement, applies patches in manifest order, and independently verifies
// the complete result fingerprint. Source is never modified.
func Apply(source []byte, manifest Manifest) ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	wantSource, _ := decodeHash(manifest.SourceSHA256)
	if sha256.Sum256(source) != wantSource {
		return nil, fmt.Errorf("unsupported executable fingerprint")
	}
	type decodedPatch struct {
		offset      int
		replacement []byte
	}
	decoded := make([]decodedPatch, 0, len(manifest.Patches))
	for _, patch := range manifest.Patches {
		expected, _ := decodeBytes(patch.Expected)
		replacement, _ := decodeBytes(patch.Replacement)
		if patch.Offset > uint64(len(source)) || uint64(len(expected)) > uint64(len(source))-patch.Offset {
			return nil, fmt.Errorf("patch %q is outside the executable", patch.ID)
		}
		offset := int(patch.Offset)
		if !bytes.Equal(source[offset:offset+len(expected)], expected) {
			return nil, fmt.Errorf("patch %q source guard does not match", patch.ID)
		}
		decoded = append(decoded, decodedPatch{offset: offset, replacement: replacement})
	}

	result := append([]byte(nil), source...)
	for _, patch := range decoded {
		copy(result[patch.offset:patch.offset+len(patch.replacement)], patch.replacement)
	}
	wantResult, _ := decodeHash(manifest.ResultSHA256)
	if sha256.Sum256(result) != wantResult {
		return nil, fmt.Errorf("patched executable fingerprint does not match manifest result_sha256")
	}
	return result, nil
}

func decodeHash(value string) ([sha256.Size]byte, error) {
	var hash [sha256.Size]byte
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != value {
		return hash, fmt.Errorf("expected 64 lowercase hexadecimal characters")
	}
	copy(hash[:], decoded)
	return hash, nil
}

func decodeBytes(value string) ([]byte, error) {
	if value == "" || len(value)%2 != 0 {
		return nil, fmt.Errorf("expected non-empty lowercase hexadecimal bytes")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return nil, fmt.Errorf("expected non-empty lowercase hexadecimal bytes")
	}
	return decoded, nil
}
