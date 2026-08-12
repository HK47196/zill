// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/message"
	"github.com/HK47196/zill/internal/release"
)

const editRecordSchemaVersion = 1
const editRecordPatchLimit = 1 << 20

const editRecordUsage = "zill edit-record inspect --record ID [--format json] | zill edit-record apply --patch FILE|- [--dry-run] [--format json]"

const editRecordHelp = `Usage:
  zill edit-record inspect --record ID [--format json]
  zill edit-record apply --patch FILE|- [--dry-run] [--format json]

Patch schema:
  {
    "schema_version": 1,
    "record_id": 30028,
    "target": "controls/1/blocks/0",
    "expected_source_hash": "sha256:<from inspect>",
    "expected_english_hash": "sha256:<from inspect>",
    "english": "Replacement payload only; never include control delimiters."
  }

Use --patch - to read one strict JSON object from stdin. Success is one JSON
object on stdout with exit 0. Invocation/schema errors are JSON on stderr with
exit 2; stale, validation, and I/O errors use exit 1. --dry-run performs all
semantic validation without publishing the section file.
`

type editRecordFailure struct {
	Code    string
	Message string
	Exit    int
}

func (failure *editRecordFailure) Error() string { return failure.Message }

type editRecordErrorDocument struct {
	SchemaVersion int    `json:"schema_version"`
	OK            bool   `json:"ok"`
	Code          string `json:"code"`
	Message       string `json:"message"`
}

type editRecordInspection struct {
	SchemaVersion int                `json:"schema_version"`
	OK            bool               `json:"ok"`
	Operation     string             `json:"operation"`
	RecordID      int                `json:"record_id"`
	State         corpus.State       `json:"state"`
	File          string             `json:"file"`
	SourceHash    string             `json:"source_hash"`
	EnglishHash   string             `json:"english_hash"`
	Japanese      string             `json:"japanese"`
	English       string             `json:"english"`
	Targets       []editRecordTarget `json:"targets"`
}

type editRecordTarget struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Selector   string `json:"selector,omitempty"`
	Position   int    `json:"position"`
	Role       string `json:"role"`
	Condition  string `json:"condition,omitempty"`
	Japanese   string `json:"japanese"`
	English    string `json:"english"`
	Applicable bool   `json:"applicable"`
}

type editRecordPatch struct {
	SchemaVersion       int     `json:"schema_version"`
	RecordID            *int    `json:"record_id"`
	Target              string  `json:"target"`
	ExpectedSourceHash  string  `json:"expected_source_hash"`
	ExpectedEnglishHash string  `json:"expected_english_hash"`
	English             *string `json:"english"`
}

type editRecordApplyResult struct {
	SchemaVersion  int          `json:"schema_version"`
	OK             bool         `json:"ok"`
	Operation      string       `json:"operation"`
	RecordID       int          `json:"record_id"`
	Target         string       `json:"target"`
	DryRun         bool         `json:"dry_run"`
	Changed        bool         `json:"changed"`
	File           string       `json:"file"`
	OldEnglishHash string       `json:"old_english_hash"`
	NewEnglishHash string       `json:"new_english_hash"`
	State          corpus.State `json:"state"`
}

var editRecordHash = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func runEditRecord(root string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return writeEditRecordFailure(stderr, failEditRecord("invalid_invocation", 2, "usage: %s", editRecordUsage))
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, editRecordHelp)
		return 0
	}
	var document any
	var failure *editRecordFailure
	switch args[0] {
	case "inspect":
		var recordID int
		recordID, failure = parseEditRecordInspect(args[1:])
		if failure == nil {
			document, failure = inspectInlineRecord(root, recordID)
		}
	case "apply":
		var patchPath string
		var dryRun bool
		patchPath, dryRun, failure = parseEditRecordApply(args[1:])
		if failure == nil {
			var patch editRecordPatch
			patch, failure = readEditRecordPatch(patchPath, stdin)
			if failure == nil {
				document, failure = applyInlineRecordPatch(root, patch, dryRun)
			}
		}
	default:
		failure = failEditRecord("invalid_invocation", 2, "unknown operation %q; usage: %s", args[0], editRecordUsage)
	}
	if failure != nil {
		return writeEditRecordFailure(stderr, failure)
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return writeEditRecordFailure(stderr, failEditRecord("output_error", 1, "encode JSON: %v", err))
	}
	return 0
}

func parseEditRecordInspect(args []string) (int, *editRecordFailure) {
	recordID, recordSet, formatSet := 0, false, false
	for index := 0; index < len(args); index++ {
		name, value, hasEquals := strings.Cut(args[index], "=")
		next := func() (string, *editRecordFailure) {
			if hasEquals {
				if value == "" {
					return "", failEditRecord("invalid_invocation", 2, "%s requires a value", name)
				}
				return value, nil
			}
			if index+1 >= len(args) {
				return "", failEditRecord("invalid_invocation", 2, "%s requires a value", name)
			}
			index++
			return args[index], nil
		}
		switch name {
		case "--record":
			if recordSet {
				return 0, failEditRecord("invalid_invocation", 2, "--record may be specified only once")
			}
			recordSet = true
			raw, failure := next()
			if failure != nil {
				return 0, failure
			}
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 || parsed > 2_789_999 {
				return 0, failEditRecord("invalid_invocation", 2, "invalid record %q", raw)
			}
			recordID = parsed
		case "--format":
			if formatSet {
				return 0, failEditRecord("invalid_invocation", 2, "--format may be specified only once")
			}
			formatSet = true
			format, failure := next()
			if failure != nil {
				return 0, failure
			}
			if format != "json" {
				return 0, failEditRecord("invalid_invocation", 2, "unsupported format %q", format)
			}
		default:
			return 0, failEditRecord("invalid_invocation", 2, "unknown argument %q", args[index])
		}
	}
	if !recordSet {
		return 0, failEditRecord("invalid_invocation", 2, "--record is required")
	}
	return recordID, nil
}

func parseEditRecordApply(args []string) (string, bool, *editRecordFailure) {
	patchPath, patchSet, dryRun, dryRunSet, formatSet := "", false, false, false, false
	for index := 0; index < len(args); index++ {
		name, value, hasEquals := strings.Cut(args[index], "=")
		next := func() (string, *editRecordFailure) {
			if hasEquals {
				if value == "" {
					return "", failEditRecord("invalid_invocation", 2, "%s requires a value", name)
				}
				return value, nil
			}
			if index+1 >= len(args) {
				return "", failEditRecord("invalid_invocation", 2, "%s requires a value", name)
			}
			index++
			return args[index], nil
		}
		switch name {
		case "--patch":
			if patchSet {
				return "", false, failEditRecord("invalid_invocation", 2, "--patch may be specified only once")
			}
			patchSet = true
			var failure *editRecordFailure
			patchPath, failure = next()
			if failure != nil {
				return "", false, failure
			}
		case "--dry-run":
			if hasEquals {
				return "", false, failEditRecord("invalid_invocation", 2, "--dry-run does not take a value")
			}
			if dryRunSet {
				return "", false, failEditRecord("invalid_invocation", 2, "--dry-run may be specified only once")
			}
			dryRunSet, dryRun = true, true
		case "--format":
			if formatSet {
				return "", false, failEditRecord("invalid_invocation", 2, "--format may be specified only once")
			}
			formatSet = true
			format, failure := next()
			if failure != nil {
				return "", false, failure
			}
			if format != "json" {
				return "", false, failEditRecord("invalid_invocation", 2, "unsupported format %q", format)
			}
		default:
			return "", false, failEditRecord("invalid_invocation", 2, "unknown argument %q", args[index])
		}
	}
	if !patchSet {
		return "", false, failEditRecord("invalid_invocation", 2, "--patch is required")
	}
	return patchPath, dryRun, nil
}

func inspectInlineRecord(root string, recordID int) (editRecordInspection, *editRecordFailure) {
	project, _, err := corpus.LoadProject(root)
	if err != nil {
		return editRecordInspection{}, failEditRecord("project_invalid", 1, "%v", err)
	}
	item, ok := project.Find(recordID)
	if !ok {
		return editRecordInspection{}, failEditRecord("record_not_found", 1, "record %d does not exist", recordID)
	}
	source, err := message.ParseInlineControls(item.Translation.Japanese)
	if err != nil {
		return editRecordInspection{}, failEditRecord("source_structure_invalid", 1, "record %d Japanese inline control: %v", recordID, err)
	}
	if len(source) == 0 {
		return editRecordInspection{}, failEditRecord("no_inline_controls", 1, "record %d has no inline dialogue variants", recordID)
	}
	var translated []message.InlineControl
	if item.Translation.Text != "" {
		translated, err = message.ParseInlineControls(item.Translation.Text)
		if err != nil {
			return editRecordInspection{}, failEditRecord("english_structure_invalid", 1, "record %d English inline control: %v", recordID, err)
		}
		if err := message.ValidateInlineStructure(source, translated); err != nil {
			return editRecordInspection{}, failEditRecord("english_structure_invalid", 1, "record %d English inline control: %v", recordID, err)
		}
	}
	document := editRecordInspection{
		SchemaVersion: editRecordSchemaVersion, OK: true, Operation: "inspect", RecordID: recordID,
		State: item.Translation.State, File: editablePath(recordID), SourceHash: hashEditRecordText(item.Translation.Japanese),
		EnglishHash: hashEditRecordText(item.Translation.Text), Japanese: item.Translation.Japanese,
		English: item.Translation.Text, Targets: make([]editRecordTarget, 0),
	}
	for controlIndex, control := range source {
		for blockIndex, block := range control.Blocks {
			target := editRecordTarget{
				Path: fmt.Sprintf("controls/%d/blocks/%d", controlIndex, blockIndex), Kind: control.Kind,
				Selector: control.Selector, Position: block.Position, Role: block.Role, Condition: block.Condition,
				Japanese: block.Text, Applicable: item.Translation.Text != "",
			}
			if translated != nil {
				target.English = translated[controlIndex].Blocks[blockIndex].Text
			}
			document.Targets = append(document.Targets, target)
		}
	}
	return document, nil
}

func readEditRecordPatch(path string, stdin io.Reader) (editRecordPatch, *editRecordFailure) {
	var reader io.Reader
	if path == "-" {
		reader = stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return editRecordPatch{}, failEditRecord("patch_io_error", 1, "open patch: %v", err)
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, editRecordPatchLimit+1))
	if err != nil {
		return editRecordPatch{}, failEditRecord("patch_io_error", 1, "read patch: %v", err)
	}
	if len(data) > editRecordPatchLimit {
		return editRecordPatch{}, failEditRecord("invalid_patch", 2, "patch exceeds %d bytes", editRecordPatchLimit)
	}
	if failure := validateEditRecordPatchObject(data); failure != nil {
		return editRecordPatch{}, failure
	}
	var patch editRecordPatch
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		return editRecordPatch{}, failEditRecord("invalid_patch", 2, "decode patch JSON: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return editRecordPatch{}, failEditRecord("invalid_patch", 2, "patch must contain exactly one JSON object")
	}
	if patch.SchemaVersion != editRecordSchemaVersion || patch.RecordID == nil || *patch.RecordID < 0 || *patch.RecordID > 2_789_999 || patch.Target == "" || patch.English == nil || patch.ExpectedSourceHash == "" || patch.ExpectedEnglishHash == "" {
		return editRecordPatch{}, failEditRecord("invalid_patch", 2, "patch requires schema_version 1, record_id, target, expected_source_hash, expected_english_hash, and english")
	}
	if !editRecordHash.MatchString(patch.ExpectedSourceHash) || !editRecordHash.MatchString(patch.ExpectedEnglishHash) {
		return editRecordPatch{}, failEditRecord("invalid_patch", 2, "expected hashes must use sha256:<lowercase-hex>")
	}
	if *patch.English == "" {
		return editRecordPatch{}, failEditRecord("invalid_patch", 2, "english payload must be nonempty")
	}
	return patch, nil
}

func validateEditRecordPatchObject(data []byte) *editRecordFailure {
	allowed := map[string]bool{
		"schema_version": true, "record_id": true, "target": true,
		"expected_source_hash": true, "expected_english_hash": true, "english": true,
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return failEditRecord("invalid_patch", 2, "patch must be one JSON object")
	}
	seen := make(map[string]bool, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return failEditRecord("invalid_patch", 2, "decode patch JSON: %v", err)
		}
		key, ok := token.(string)
		if !ok || !allowed[key] {
			return failEditRecord("invalid_patch", 2, "patch contains unknown or incorrectly cased field %q", key)
		}
		if seen[key] {
			return failEditRecord("invalid_patch", 2, "patch contains duplicate field %q", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return failEditRecord("invalid_patch", 2, "decode patch field %q: %v", key, err)
		}
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return failEditRecord("invalid_patch", 2, "patch must be one JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return failEditRecord("invalid_patch", 2, "patch must contain exactly one JSON object")
	}
	return nil
}

func applyInlineRecordPatch(root string, patch editRecordPatch, dryRun bool) (editRecordApplyResult, *editRecordFailure) {
	recordID := *patch.RecordID
	relativeFile := editablePath(recordID)
	path := filepath.Join(root, filepath.FromSlash(relativeFile))
	if !dryRun {
		releaseLock, failure := acquireEditRecordLock(root, recordID/10_000)
		if failure != nil {
			return editRecordApplyResult{}, failure
		}
		defer releaseLock()
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return editRecordApplyResult{}, failEditRecord("section_io_error", 1, "read %s: %v", relativeFile, err)
	}
	project, _, err := corpus.LoadProject(root)
	if err != nil {
		return editRecordApplyResult{}, failEditRecord("project_invalid", 1, "%v", err)
	}
	stable, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, stable) {
		return editRecordApplyResult{}, failEditRecord("stale_section", 1, "%s changed while loading the project", relativeFile)
	}
	item, ok := project.Find(recordID)
	if !ok {
		return editRecordApplyResult{}, failEditRecord("record_not_found", 1, "record %d does not exist", recordID)
	}
	oldEnglishHash := hashEditRecordText(item.Translation.Text)
	if actual := hashEditRecordText(item.Translation.Japanese); actual != patch.ExpectedSourceHash {
		return editRecordApplyResult{}, failEditRecord("stale_source", 1, "record %d source hash is %s, expected %s", recordID, actual, patch.ExpectedSourceHash)
	}
	if oldEnglishHash != patch.ExpectedEnglishHash {
		return editRecordApplyResult{}, failEditRecord("stale_english", 1, "record %d English hash is %s, expected %s", recordID, oldEnglishHash, patch.ExpectedEnglishHash)
	}
	source, err := message.ParseInlineControls(item.Translation.Japanese)
	if err != nil || len(source) == 0 {
		return editRecordApplyResult{}, failEditRecord("no_inline_controls", 1, "record %d has no editable inline dialogue variants", recordID)
	}
	if item.Translation.Text == "" {
		return editRecordApplyResult{}, failEditRecord("incomplete_controlled_record", 1, "record %d has blank English; a single-leaf patch cannot create a partial controlled translation", recordID)
	}
	translated, err := message.ParseInlineControls(item.Translation.Text)
	if err != nil {
		return editRecordApplyResult{}, failEditRecord("english_structure_invalid", 1, "record %d English inline control: %v", recordID, err)
	}
	if err := message.ValidateInlineStructure(source, translated); err != nil {
		return editRecordApplyResult{}, failEditRecord("english_structure_invalid", 1, "record %d English inline control: %v", recordID, err)
	}
	controlIndex, blockIndex, found := findEditRecordTarget(source, patch.Target)
	if !found {
		return editRecordApplyResult{}, failEditRecord("target_not_found", 1, "record %d has no target %q", recordID, patch.Target)
	}
	if err := message.ValidateInlineBlock(recordID, source[controlIndex].Blocks[blockIndex].Text, *patch.English); err != nil {
		return editRecordApplyResult{}, failEditRecord("invalid_english", 1, "%v", err)
	}
	translated[controlIndex].Blocks[blockIndex].Text = *patch.English
	reconstructed, err := message.RenderInlineControls(translated)
	if err != nil {
		return editRecordApplyResult{}, failEditRecord("english_structure_invalid", 1, "%v", err)
	}
	reparsed, err := message.ParseInlineControls(reconstructed)
	if err != nil {
		return editRecordApplyResult{}, failEditRecord("english_structure_invalid", 1, "%v", err)
	}
	if err := message.ValidateInlineStructure(source, reparsed); err != nil {
		return editRecordApplyResult{}, failEditRecord("english_structure_invalid", 1, "%v", err)
	}
	for ci := range source {
		for bi := range source[ci].Blocks {
			if err := message.ValidateInlineBlock(recordID, source[ci].Blocks[bi].Text, reparsed[ci].Blocks[bi].Text); err != nil {
				return editRecordApplyResult{}, failEditRecord("invalid_english", 1, "%v", err)
			}
		}
	}
	candidate, err := project.WithEnglish(recordID, reconstructed)
	if err != nil {
		return editRecordApplyResult{}, failEditRecord("invalid_english", 1, "%v", err)
	}
	if err := release.Check(root, candidate); err != nil {
		return editRecordApplyResult{}, failEditRecord("validation_failed", 1, "%v", err)
	}
	candidateData, err := candidate.RenderSection(recordID / 10_000)
	if err != nil {
		return editRecordApplyResult{}, failEditRecord("validation_failed", 1, "%v", err)
	}
	changed := !bytes.Equal(original, candidateData)
	result := editRecordApplyResult{
		SchemaVersion: editRecordSchemaVersion, OK: true, Operation: "apply", RecordID: recordID,
		Target: patch.Target, DryRun: dryRun, Changed: changed, File: relativeFile,
		OldEnglishHash: oldEnglishHash, NewEnglishHash: hashEditRecordText(reconstructed), State: corpus.Translated,
	}
	if dryRun || !changed {
		return result, nil
	}
	if failure := publishEditRecordSection(path, original, candidateData); failure != nil {
		return editRecordApplyResult{}, failure
	}
	return result, nil
}

func acquireEditRecordLock(root string, section int) (func(), *editRecordFailure) {
	path := filepath.Join(root, fmt.Sprintf(".zill-edit-record-%03d.lock", section))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil, failEditRecord("section_locked", 1, "message section %03d is already being edited; remove %s only if no edit-record process is running", section, path)
	}
	if err != nil {
		return nil, failEditRecord("section_io_error", 1, "lock message section %03d: %v", section, err)
	}
	_, _ = fmt.Fprintf(file, "pid=%d\n", os.Getpid())
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, failEditRecord("section_io_error", 1, "close message section lock: %v", err)
	}
	return func() { _ = os.Remove(path) }, nil
}

func findEditRecordTarget(controls []message.InlineControl, target string) (int, int, bool) {
	for controlIndex, control := range controls {
		for blockIndex := range control.Blocks {
			if target == fmt.Sprintf("controls/%d/blocks/%d", controlIndex, blockIndex) {
				return controlIndex, blockIndex, true
			}
		}
	}
	return 0, 0, false
}

func publishEditRecordSection(path string, expected, candidate []byte) *editRecordFailure {
	info, err := os.Stat(path)
	if err != nil {
		return failEditRecord("section_io_error", 1, "inspect %s: %v", path, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".edit-record-*.tmp")
	if err != nil {
		return failEditRecord("section_io_error", 1, "create temporary section: %v", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	closeTemporary := func() {
		_ = temporary.Close()
	}
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		closeTemporary()
		return failEditRecord("section_io_error", 1, "set temporary section mode: %v", err)
	}
	if _, err := temporary.Write(candidate); err != nil {
		closeTemporary()
		return failEditRecord("section_io_error", 1, "write temporary section: %v", err)
	}
	if err := temporary.Sync(); err != nil {
		closeTemporary()
		return failEditRecord("section_io_error", 1, "sync temporary section: %v", err)
	}
	if err := temporary.Close(); err != nil {
		return failEditRecord("section_io_error", 1, "close temporary section: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, expected) {
		return failEditRecord("stale_section", 1, "%s changed before publication", path)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return failEditRecord("section_io_error", 1, "publish section: %v", err)
	}
	return nil
}

func hashEditRecordText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func failEditRecord(code string, exit int, format string, values ...any) *editRecordFailure {
	return &editRecordFailure{Code: code, Message: fmt.Sprintf(format, values...), Exit: exit}
}

func writeEditRecordFailure(output io.Writer, failure *editRecordFailure) int {
	document := editRecordErrorDocument{SchemaVersion: editRecordSchemaVersion, OK: false, Code: failure.Code, Message: failure.Message}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(document)
	return failure.Exit
}
