// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEditRecordInspectReturnsStableMixedControlTargets(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runEditRecord("../..", []string{"inspect", "--record", "30028"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("inspect exit = %d; stderr: %s", code, stderr.String())
	}
	var document editRecordInspection
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if !document.OK || document.RecordID != 30028 || document.State != "translated" || !strings.HasPrefix(document.SourceHash, "sha256:") || !strings.HasPrefix(document.EnglishHash, "sha256:") {
		t.Fatalf("inspection metadata = %#v", document)
	}
	wantPaths := []string{"controls/0/blocks/0", "controls/1/blocks/0", "controls/1/blocks/1"}
	if len(document.Targets) != len(wantPaths) {
		t.Fatalf("targets = %#v", document.Targets)
	}
	for index, path := range wantPaths {
		if target := document.Targets[index]; target.Path != path || target.Japanese == "" || target.English == "" || !target.Applicable {
			t.Fatalf("target %d = %#v", index, target)
		}
	}
	if document.Targets[0].Kind != "conditional" || document.Targets[1].Kind != "selection" {
		t.Fatalf("mixed target kinds = %#v", document.Targets)
	}
}

func TestEditRecordApplyDryRunWritesOneVariantAtomicallyAndRejectsStaleOrUnsafePatches(t *testing.T) {
	root := cloneEditRecordProject(t)
	inspection := inspectEditRecordForTest(t, root, 30028)
	target := inspection.Targets[1]
	replacement := target.English + " Revised."
	patch := editRecordPatch{
		SchemaVersion: editRecordSchemaVersion, RecordID: intPointer(inspection.RecordID), Target: target.Path,
		ExpectedSourceHash: inspection.SourceHash, ExpectedEnglishHash: inspection.EnglishHash, English: &replacement,
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	sectionPath := filepath.Join(root, filepath.FromSlash(inspection.File))
	original, err := os.ReadFile(sectionPath)
	if err != nil {
		t.Fatal(err)
	}

	dryRun := applyEditRecordForTest(t, root, patchJSON, true, 0)
	if !dryRun.OK || !dryRun.DryRun || !dryRun.Changed || dryRun.NewEnglishHash == inspection.EnglishHash {
		t.Fatalf("dry-run result = %#v", dryRun)
	}
	assertEditRecordFileBytes(t, sectionPath, original)

	applied := applyEditRecordForTest(t, root, patchJSON, false, 0)
	if !applied.OK || applied.DryRun || !applied.Changed || applied.NewEnglishHash != dryRun.NewEnglishHash {
		t.Fatalf("apply result = %#v", applied)
	}
	after := inspectEditRecordForTest(t, root, 30028)
	if after.EnglishHash != applied.NewEnglishHash || after.Targets[1].English != replacement || after.Targets[0].English != inspection.Targets[0].English || after.Targets[2].English != inspection.Targets[2].English {
		t.Fatalf("post-apply targets = %#v", after.Targets)
	}

	written, err := os.ReadFile(sectionPath)
	if err != nil {
		t.Fatal(err)
	}
	noOpText := after.Targets[1].English
	noOpPatch := editRecordPatch{
		SchemaVersion: editRecordSchemaVersion, RecordID: intPointer(after.RecordID), Target: target.Path,
		ExpectedSourceHash: after.SourceHash, ExpectedEnglishHash: after.EnglishHash, English: &noOpText,
	}
	noOpJSON, err := json.Marshal(noOpPatch)
	if err != nil {
		t.Fatal(err)
	}
	beforeNoOp, err := os.Stat(sectionPath)
	if err != nil {
		t.Fatal(err)
	}
	noOp := applyEditRecordForTest(t, root, noOpJSON, false, 0)
	afterNoOp, err := os.Stat(sectionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !noOp.OK || noOp.Changed || !os.SameFile(beforeNoOp, afterNoOp) {
		t.Fatalf("no-op patch rewrote the section: %#v", noOp)
	}
	assertEditRecordFileBytes(t, sectionPath, written)

	stale := applyEditRecordForTest(t, root, patchJSON, false, 1)
	if stale.OK || stale.Code != "stale_english" {
		t.Fatalf("stale patch result = %#v", stale)
	}
	assertEditRecordFileBytes(t, sectionPath, written)

	unsafeText := "attempted control injection<end>"
	unsafePatch := editRecordPatch{
		SchemaVersion: editRecordSchemaVersion, RecordID: intPointer(after.RecordID), Target: target.Path,
		ExpectedSourceHash: after.SourceHash, ExpectedEnglishHash: after.EnglishHash, English: &unsafeText,
	}
	unsafeJSON, err := json.Marshal(unsafePatch)
	if err != nil {
		t.Fatal(err)
	}
	unsafe := applyEditRecordForTest(t, root, unsafeJSON, false, 1)
	if unsafe.OK || unsafe.Code != "invalid_english" {
		t.Fatalf("unsafe patch result = %#v", unsafe)
	}
	assertEditRecordFileBytes(t, sectionPath, written)

	blank := inspectEditRecordForTest(t, root, 280130)
	blankText := "Translated one branch."
	blankPatch := editRecordPatch{
		SchemaVersion: editRecordSchemaVersion, RecordID: intPointer(blank.RecordID), Target: blank.Targets[0].Path,
		ExpectedSourceHash: blank.SourceHash, ExpectedEnglishHash: blank.EnglishHash, English: &blankText,
	}
	blankJSON, err := json.Marshal(blankPatch)
	if err != nil {
		t.Fatal(err)
	}
	blankPath := filepath.Join(root, filepath.FromSlash(blank.File))
	blankBefore, err := os.ReadFile(blankPath)
	if err != nil {
		t.Fatal(err)
	}
	blankResult := applyEditRecordForTest(t, root, blankJSON, false, 1)
	if blankResult.OK || blankResult.Code != "incomplete_controlled_record" {
		t.Fatalf("blank controlled patch result = %#v", blankResult)
	}
	assertEditRecordFileBytes(t, blankPath, blankBefore)

	lockPath := filepath.Join(root, ".zill-edit-record-003.lock")
	if err := os.WriteFile(lockPath, []byte("competing writer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	locked := applyEditRecordForTest(t, root, noOpJSON, false, 1)
	if locked.OK || locked.Code != "section_locked" {
		t.Fatalf("locked section result = %#v", locked)
	}
	assertEditRecordFileBytes(t, sectionPath, written)
}

func TestEditRecordRejectsMalformedPatchJSONAsMachineReadableError(t *testing.T) {
	for _, patch := range []string{
		`{"schema_version":1,"record_id":30028,"unknown":true}`,
		`{"schema_version":1,"record_id":30028,"record_id":30029}`,
		`{"schema_version":1,"Record_ID":30028}`,
		`{"schema_version":1} {"schema_version":1}`,
	} {
		var stdout, stderr bytes.Buffer
		code := runEditRecord("../..", []string{"apply", "--patch", "-"}, strings.NewReader(patch), &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 {
			t.Fatalf("invalid patch exit = %d; stdout: %s; stderr: %s", code, stdout.String(), stderr.String())
		}
		var failure editRecordErrorDocument
		if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil {
			t.Fatal(err)
		}
		if failure.OK || failure.Code != "invalid_patch" {
			t.Fatalf("invalid patch failure = %#v", failure)
		}
	}
}

type editRecordTestResult struct {
	editRecordApplyResult
	Code string `json:"code"`
}

func inspectEditRecordForTest(t *testing.T, root string, recordID int) editRecordInspection {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := runEditRecord(root, []string{"inspect", "--record", strconv.Itoa(recordID)}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("inspect %d exit = %d; stderr: %s", recordID, code, stderr.String())
	}
	var document editRecordInspection
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func applyEditRecordForTest(t *testing.T, root string, patch []byte, dryRun bool, wantExit int) editRecordTestResult {
	t.Helper()
	args := []string{"apply", "--patch", "-"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	var stdout, stderr bytes.Buffer
	code := runEditRecord(root, args, bytes.NewReader(patch), &stdout, &stderr)
	if code != wantExit {
		t.Fatalf("apply exit = %d, want %d; stdout: %s; stderr: %s", code, wantExit, stdout.String(), stderr.String())
	}
	data := stdout.Bytes()
	if code != 0 {
		data = stderr.Bytes()
	}
	var result editRecordTestResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func cloneEditRecordProject(t *testing.T) string {
	t.Helper()
	sourceRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	messages := filepath.Join(root, "translations", "messages")
	if err := os.MkdirAll(messages, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(sourceRoot, "translations", "messages"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		source := filepath.Join(sourceRoot, "translations", "messages", entry.Name())
		destination := filepath.Join(messages, entry.Name())
		if entry.Name() == "msgsec003.toml" {
			data, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.Symlink(source, destination); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(sourceRoot, "translations", "terminology"), filepath.Join(root, "translations", "terminology")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sourceRoot, "release"), filepath.Join(root, "release")); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertEditRecordFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s changed unexpectedly", path)
	}
}
