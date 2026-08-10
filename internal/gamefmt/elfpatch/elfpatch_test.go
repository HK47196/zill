package elfpatch_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/HK47196/zill/internal/gamefmt/elfpatch"
)

func TestProductionManifestDefinesCompleteGuardedPatchSet(t *testing.T) {
	data, err := os.ReadFile("../../../patches/executable/manifest.toml")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := elfpatch.ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(manifest.Patches) != 35 {
		t.Fatalf("patch count = %d, want 35", len(manifest.Patches))
	}
}

func TestValidateRejectsIncompleteOrSemanticallyUnorderedPatchSets(t *testing.T) {
	_, valid, _ := syntheticPatchSet()
	tests := map[string]func(*elfpatch.Manifest){
		"missing patch": func(manifest *elfpatch.Manifest) {
			manifest.Patches = manifest.Patches[:34]
		},
		"feature out of order": func(manifest *elfpatch.Manifest) {
			manifest.Patches[1], manifest.Patches[12] = manifest.Patches[12], manifest.Patches[1]
		},
		"instruction without disassembly": func(manifest *elfpatch.Manifest) {
			manifest.Patches[3].Before = ""
		},
		"wrong patch kind": func(manifest *elfpatch.Manifest) {
			manifest.Patches[3].Kind = "elf-field"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			manifest := valid
			manifest.Patches = append([]elfpatch.Patch(nil), valid.Patches...)
			mutate(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatal("Validate accepted an incomplete or semantically unordered patch set")
			}
		})
	}
}

func TestApplyReplacesAllGuardsAndPreservesUnrelatedBytes(t *testing.T) {
	source, manifest, want := syntheticPatchSet()
	original := append([]byte(nil), source...)

	got, err := elfpatch.Apply(source, manifest)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("Apply result does not contain the complete ordered replacement set")
	}
	if !bytes.Equal(source, original) {
		t.Fatal("Apply mutated the authenticated source")
	}
	if got[0x20] != source[0x20] || got[len(got)-1] != source[len(source)-1] {
		t.Fatal("Apply changed bytes outside patch ranges")
	}
}

func TestGuardFailureReturnsNoOutputAndDoesNotMutateSource(t *testing.T) {
	source, manifest, _ := syntheticPatchSet()
	source[0x80+17*4] ^= 0xff
	manifest.SourceSHA256 = hash(source)
	original := append([]byte(nil), source...)

	got, err := elfpatch.Apply(source, manifest)
	if err == nil {
		t.Fatal("Apply accepted a mismatched guarded byte")
	}
	if got != nil {
		t.Fatal("Apply returned publishable output after a guard failure")
	}
	if !bytes.Equal(source, original) {
		t.Fatal("Apply mutated source before reporting the guard failure")
	}
}

func TestUnsupportedFingerprintReturnsNoOutputAndDoesNotMutateSource(t *testing.T) {
	source, manifest, _ := syntheticPatchSet()
	manifest.SourceSHA256 = hash([]byte("another executable"))
	original := append([]byte(nil), source...)

	got, err := elfpatch.Apply(source, manifest)
	if err == nil || got != nil {
		t.Fatal("Apply exposed output for an unsupported executable fingerprint")
	}
	if !bytes.Equal(source, original) {
		t.Fatal("Apply mutated an unsupported executable")
	}
}

func TestIndependentResultHashRejectsIncompleteContract(t *testing.T) {
	source, manifest, _ := syntheticPatchSet()
	manifest.ResultSHA256 = hash([]byte("not the patched executable"))
	original := append([]byte(nil), source...)

	got, err := elfpatch.Apply(source, manifest)
	if err == nil || got != nil {
		t.Fatal("Apply exposed output that failed independent result authentication")
	}
	if !bytes.Equal(source, original) {
		t.Fatal("Apply mutated source when result authentication failed")
	}
}

func syntheticPatchSet() ([]byte, elfpatch.Manifest, []byte) {
	source := bytes.Repeat([]byte{0x7e}, 0x180)
	want := append([]byte(nil), source...)
	features := []struct {
		name  string
		count int
	}{{"large-memory", 1}, {"message-arena", 11}, {"wide-message-offsets", 12}, {"profile-biography", 9}, {"title-attribution", 2}}
	manifest := elfpatch.Manifest{
		Format:  "zill-executable-patches",
		Version: 1,
		Target:  "SYSDIR/BOOT.BIN",
	}
	patchIndex := 0
	for _, feature := range features {
		for range feature.count {
			offset := uint64(0x80 + patchIndex*4)
			expected := []byte{byte(patchIndex), 0xaa, 0xbb, 0xcc}
			replacement := []byte{byte(patchIndex), 0x11, 0x22, 0x33}
			copy(source[offset:offset+4], expected)
			copy(want[offset:offset+4], replacement)
			kind := "mips32le"
			patch := elfpatch.Patch{
				ID:             fmt.Sprintf("%s-%07x", feature.name, offset),
				Feature:        feature.name,
				Document:       feature.name + ".md",
				Kind:           kind,
				Offset:         offset,
				Expected:       hex.EncodeToString(expected),
				Replacement:    hex.EncodeToString(replacement),
				Purpose:        "Exercise one guarded synthetic replacement.",
				VirtualAddress: uint64Pointer(offset - 0x80),
				Before:         "synthetic before instruction",
				After:          "synthetic after instruction",
			}
			if patchIndex == 0 {
				patch.Kind = "little-endian-u32"
				patch.VirtualAddress, patch.Before, patch.After = nil, "", ""
				patch.Field = "synthetic little-endian field"
			} else if patchIndex == 1 || patchIndex == 2 {
				patch.Kind = "elf-field"
				patch.VirtualAddress, patch.Before, patch.After = nil, "", ""
				patch.Field = "synthetic ELF32 field"
			}
			manifest.Patches = append(manifest.Patches, patch)
			patchIndex++
		}
	}
	manifest.SourceSHA256 = hash(source)
	manifest.ResultSHA256 = hash(want)
	return source, manifest, want
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func uint64Pointer(value uint64) *uint64 { return &value }
