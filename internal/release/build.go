// SPDX-License-Identifier: GPL-3.0-or-later

// Package release builds the complete translated PSP_GAME tree from an
// authenticated Japanese retail ULJM05410 1.03 tree.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/fixeddata"
	"github.com/HK47196/zill/internal/gamefmt/elfpatch"
	"github.com/HK47196/zill/internal/gamefmt/paa"
	"github.com/HK47196/zill/internal/gamefmt/sfo"
	"github.com/HK47196/zill/internal/gamefmt/staticpatch"
	"github.com/HK47196/zill/internal/gamefmt/textureoverride"
	"github.com/HK47196/zill/internal/layout"
	"github.com/HK47196/zill/internal/message"
)

const (
	bankCount         = 279
	sourceEBOOTSHA256 = "2a52012be00c07512dcde932ff6e9eb9b96912c59dd5a25c7c26ef821c124d68"
	retailBanksSHA256 = "c00a8537713a3e539beff9f021036830394cdefd56762bab7038aa6ed07e2a88"
)

var messageMember = regexp.MustCompile(`^message/msgsec([0-9]{3})\.dat$`)

// Result reports the published release outputs and non-fatal cleanup warnings.
type Result struct {
	GameDirectory string
	ISO           string
	Patch         string
	Warnings      []string
	Layout        []layout.Warning
}

type archive struct {
	name         string
	pair         *paa.Pair
	replacements []paa.Replacement
}

// Build validates all canonical inputs and retail sources, then stages and
// verifies the complete translated tree, ISO, and xdelta patch before replacing
// their destinations. Both retail inputs are opened read-only and never modified.
func Build(root, gameDir, isoPath string) (result Result, err error) {
	root, err = resolveExistingPath(root, "project root")
	if err != nil {
		return result, err
	}
	gameDir, err = resolveExistingPath(gameDir, "source PSP_GAME")
	if err != nil {
		return result, err
	}
	isoPath, err = resolveExistingPath(isoPath, "source ISO")
	if err != nil {
		return result, err
	}
	buildDirectory := filepath.Join(root, "build")
	gameDestination := filepath.Join(buildDirectory, "PSP_GAME")
	isoDestination := filepath.Join(buildDirectory, translatedISOName)
	patchDestination := filepath.Join(buildDirectory, translatedPatchName)
	if info, statErr := os.Lstat(buildDirectory); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return result, fmt.Errorf("%s: build output parent must be a real directory", buildDirectory)
		}
	} else if !os.IsNotExist(statErr) {
		return result, fmt.Errorf("inspect build output parent: %w", statErr)
	}
	if overlaps(gameDir, gameDestination) {
		return result, fmt.Errorf("source and output PSP_GAME trees must not overlap")
	}
	for _, output := range []string{gameDestination, isoDestination, patchDestination} {
		if overlaps(isoPath, output) {
			return result, fmt.Errorf("retail ISO and output %s must not overlap", output)
		}
	}
	if err := validateReleaseDestination(gameDestination, true); err != nil {
		return result, err
	}
	for _, output := range []string{isoDestination, patchDestination} {
		if err := validateReleaseDestination(output, false); err != nil {
			return result, err
		}
	}
	for _, pair := range []struct {
		input  string
		output string
	}{
		{gameDir, gameDestination},
		{isoPath, isoDestination},
		{isoPath, patchDestination},
	} {
		aliased, aliasErr := aliasesExistingPath(pair.input, pair.output)
		if aliasErr != nil {
			return result, aliasErr
		}
		if aliased {
			return result, fmt.Errorf("retail input %s aliases release output %s", pair.input, pair.output)
		}
	}

	xdelta, err := findXdelta()
	if err != nil {
		return result, err
	}
	retailISO, isoManifest, err := openRetailISO(isoPath)
	if err != nil {
		return result, err
	}
	defer retailISO.Close()

	project, _, err := corpus.LoadProject(root)
	if err != nil {
		return result, err
	}
	if err := Check(root, project); err != nil {
		return result, err
	}
	archives, err := openArchives(gameDir)
	if err != nil {
		return result, err
	}
	defer func() {
		for _, archive := range archives {
			_ = archive.pair.Close()
		}
	}()
	banks, owners, err := loadRetailBanks(archives)
	if err != nil {
		return result, err
	}
	if err := corpus.BindBanks(project, banks); err != nil {
		return result, err
	}
	flow, err := validateAndReflow(root, project)
	if err != nil {
		return result, err
	}
	for index := range project.Items {
		if text, ok := flow.Layouts[project.Items[index].Record.ID]; ok {
			project.Items[index].Layout = text
		}
	}
	result.Layout = flow.Warnings

	compiled, err := compileBanks(project, banks)
	if err != nil {
		return result, err
	}
	if err := addBanks(owners, compiled); err != nil {
		return result, err
	}
	if err := addFixedMembers(root, archives); err != nil {
		return result, err
	}
	if err := addTextureOverrides(root, archives); err != nil {
		return result, err
	}
	if err := addEquipment(root, archives); err != nil {
		return result, err
	}

	executable, err := buildExecutable(root, gameDir)
	if err != nil {
		return result, err
	}
	parameter, err := buildSFO(root, gameDir)
	if err != nil {
		return result, err
	}

	if err := os.MkdirAll(buildDirectory, 0o755); err != nil {
		return result, fmt.Errorf("create build directory: %w", err)
	}
	bundle, err := os.MkdirTemp(buildDirectory, ".release.stage.")
	if err != nil {
		return result, fmt.Errorf("create release staging directory: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(bundle); cleanupErr != nil {
			if err != nil {
				err = fmt.Errorf("%w; staged-output cleanup also failed: %v", err, cleanupErr)
			} else {
				result.Warnings = append(result.Warnings, fmt.Sprintf("remove release staging directory: %v", cleanupErr))
			}
		}
	}()
	staging := filepath.Join(bundle, "PSP_GAME")
	if err := copyTree(gameDir, staging); err != nil {
		return result, err
	}
	if err := os.WriteFile(filepath.Join(staging, "PARAM.SFO"), parameter, 0o644); err != nil {
		return result, fmt.Errorf("write staged PARAM.SFO: %w", err)
	}
	for _, name := range []string{"BOOT.BIN", "EBOOT.BIN"} {
		if err := os.WriteFile(filepath.Join(staging, "SYSDIR", name), executable, 0o755); err != nil {
			return result, fmt.Errorf("write staged SYSDIR/%s: %w", name, err)
		}
	}
	for _, archive := range archives {
		usrdir := filepath.Join(staging, "USRDIR")
		if err := archive.pair.Rebuild(
			filepath.Join(usrdir, archive.name+".bin"),
			filepath.Join(usrdir, archive.name+".arc"),
			archive.replacements...,
		); err != nil {
			return result, fmt.Errorf("rebuild %s archive: %w", archive.name, err)
		}
	}
	stagedISO := filepath.Join(bundle, translatedISOName)
	if err := authorTranslatedISO(stagedISO, retailISO, isoManifest, staging); err != nil {
		return result, err
	}
	if err := retailISO.Close(); err != nil {
		return result, fmt.Errorf("close retail ISO before xdelta encoding: %w", err)
	}
	stagedPatch := filepath.Join(bundle, translatedPatchName)
	if err := createAndVerifyPatch(xdelta, isoPath, stagedISO, stagedPatch); err != nil {
		return result, err
	}
	cleanup, err := publishAll([]publishItem{
		{staging: staging, destination: gameDestination},
		{staging: stagedISO, destination: isoDestination},
		{staging: stagedPatch, destination: patchDestination},
	})
	if err != nil {
		return result, fmt.Errorf("publish release: %w", err)
	}
	result.GameDirectory = gameDestination
	result.ISO = isoDestination
	result.Patch = patchDestination
	for _, warning := range cleanup {
		result.Warnings = append(result.Warnings, warning.Error())
	}
	return result, nil
}

func validateReleaseDestination(path string, directory bool) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect release output %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || directory != info.IsDir() || (!directory && !info.Mode().IsRegular()) {
		kind := "regular file"
		if directory {
			kind = "real directory"
		}
		return fmt.Errorf("%s: existing output must be a %s", path, kind)
	}
	return nil
}

func resolveExistingPath(path, description string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", description, err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", description, err)
	}
	return resolved, nil
}

func aliasesExistingPath(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, fmt.Errorf("inspect retail input %s: %w", left, err)
	}
	rightInfo, err := os.Stat(right)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect release output %s: %w", right, err)
	}
	return os.SameFile(leftInfo, rightInfo), nil
}

// Check performs the canonical ancillary checks available to contributors
// without loading retail message records.
func Check(root string, project *corpus.Project) error {
	inputs := []struct {
		name  string
		parse func([]byte) error
	}{
		{"release/strings/eboot.toml", func(data []byte) error { _, err := fixeddata.ParseEBOOT(data); return err }},
		{"release/strings/equipment.toml", func(data []byte) error { _, err := fixeddata.ParseEquipment(data); return err }},
	}
	for _, input := range inputs {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(input.name)))
		if err != nil {
			return fmt.Errorf("read %s: %w", input.name, err)
		}
		if err := input.parse(data); err != nil {
			return err
		}
	}
	names, err := read(root, "translations", "terminology", "names.toml")
	if err != nil {
		return err
	}
	glossary, err := read(root, "translations", "terminology", "glossary.toml")
	if err != nil {
		return err
	}
	terminology, err := fixeddata.ParseTerminology(names, glossary)
	if err != nil {
		return err
	}
	if err := terminology.Validate(project); err != nil {
		return err
	}
	engine, err := loadLayout(root)
	if err != nil {
		return err
	}
	return engine.CheckGlyphs(project)
}

func validateAndReflow(root string, project *corpus.Project) (layout.Result, error) {
	engine, err := loadLayout(root)
	if err != nil {
		return layout.Result{}, err
	}
	names, err := read(root, "translations", "terminology", "names.toml")
	if err != nil {
		return layout.Result{}, err
	}
	glossary, err := read(root, "translations", "terminology", "glossary.toml")
	if err != nil {
		return layout.Result{}, err
	}
	terminology, err := fixeddata.ParseTerminology(names, glossary)
	if err != nil {
		return layout.Result{}, err
	}
	if err := terminology.Validate(project); err != nil {
		return layout.Result{}, err
	}
	return engine.Reflow(project)
}

func loadLayout(root string) (*layout.Engine, error) {
	consumers, err := read(root, "release", "layout", "consumer-map.toml")
	if err != nil {
		return nil, err
	}
	metrics, err := read(root, "release", "font", "metrics.toml")
	if err != nil {
		return nil, err
	}
	categories, err := read(root, "release", "layout", "categories.toml")
	if err != nil {
		return nil, err
	}
	return layout.Load(consumers, metrics, categories)
}

func compileBanks(project *corpus.Project, banks []corpus.Bank) (map[string][]byte, error) {
	items := make(map[int][]corpus.Item, bankCount)
	for _, item := range project.Items {
		section := item.Record.ID / 10_000
		items[section] = append(items[section], item)
	}
	compiled := make(map[string][]byte, bankCount)
	for section := 0; section < bankCount; section++ {
		name := fmt.Sprintf("msgsec%03d.dat", section)
		data, err := message.CompileBank(banks[section], items[section])
		if err != nil {
			return nil, err
		}
		compiled[name] = data
	}
	return compiled, nil
}

func openArchives(gameDir string) ([]*archive, error) {
	var archives []*archive
	for _, name := range []string{"pa", "pami"} {
		usrdir := filepath.Join(gameDir, "USRDIR")
		pair, err := paa.Open(filepath.Join(usrdir, name+".bin"), filepath.Join(usrdir, name+".arc"))
		if err != nil {
			for _, opened := range archives {
				_ = opened.pair.Close()
			}
			return nil, err
		}
		archives = append(archives, &archive{name: name, pair: pair})
	}
	return archives, nil
}

type bankOwner struct {
	archive *archive
	index   int
}

func loadRetailBanks(archives []*archive) ([]corpus.Bank, map[string]bankOwner, error) {
	banks := make([]corpus.Bank, bankCount)
	owners := make(map[string]bankOwner, bankCount)
	payloads := make([][]byte, bankCount)
	for _, archive := range archives {
		for _, member := range archive.pair.Members() {
			match := messageMember.FindStringSubmatch(member.Name)
			if match == nil {
				continue
			}
			name := filepath.Base(member.Name)
			section, err := strconv.Atoi(match[1])
			if err != nil || section < 0 || section >= bankCount {
				return nil, nil, fmt.Errorf("retail message member %q has unsupported section", member.Name)
			}
			if payloads[section] != nil {
				return nil, nil, fmt.Errorf("retail message member %s occurs more than once", member.Name)
			}
			payload, err := archive.pair.Payload(member.Index)
			if err != nil {
				return nil, nil, err
			}
			bank, err := corpus.ParseBank(name, payload)
			if err != nil {
				return nil, nil, err
			}
			banks[section], payloads[section] = bank, payload
			owners[name] = bankOwner{archive: archive, index: member.Index}
		}
	}
	hasher := sha256.New()
	for section, payload := range payloads {
		if payload == nil {
			return nil, nil, fmt.Errorf("retail archives are missing message bank msgsec%03d.dat", section)
		}
		_, _ = hasher.Write(payload)
	}
	if actual := hex.EncodeToString(hasher.Sum(nil)); actual != retailBanksSHA256 {
		return nil, nil, fmt.Errorf("retail message-bank fingerprint is %s, want %s", actual, retailBanksSHA256)
	}
	return banks, owners, nil
}

func addBanks(owners map[string]bankOwner, compiled map[string][]byte) error {
	for section := range bankCount {
		name := fmt.Sprintf("msgsec%03d.dat", section)
		owner, ok := owners[name]
		if !ok {
			return fmt.Errorf("retail archives are missing message bank %s", name)
		}
		payload, ok := compiled[name]
		if !ok {
			return fmt.Errorf("compiled message bank %s is missing", name)
		}
		owner.archive.replacements = append(owner.archive.replacements, paa.IndexReplacement(owner.index, payload))
	}
	return nil
}

func addFixedMembers(root string, archives []*archive) error {
	data, err := read(root, "release", "font", "manifest.toml")
	if err != nil {
		return err
	}
	manifest, err := staticpatch.ParseManifest(data)
	if err != nil {
		return err
	}
	byName := map[string]*archive{}
	for _, archive := range archives {
		byName[archive.name] = archive
	}
	for _, member := range manifest.Members() {
		archive := byName[member.Archive]
		if archive == nil {
			return fmt.Errorf("static member targets unknown archive %q", member.Archive)
		}
		members := archive.pair.Members()
		if member.Index >= len(members) || members[member.Index].Name != member.Name {
			return fmt.Errorf("%s member %d does not have expected name %q", member.Archive, member.Index, member.Name)
		}
		source, err := archive.pair.Payload(member.Index)
		if err != nil {
			return err
		}
		patched, err := staticpatch.Apply(member, source, filepath.Join(root, "release", "font"))
		if err != nil {
			return err
		}
		archive.replacements = append(archive.replacements, paa.IndexReplacement(member.Index, patched))
	}
	return nil
}

func addTextureOverrides(root string, archives []*archive) error {
	pairs := make(map[string]*paa.Pair, len(archives))
	byName := make(map[string]*archive, len(archives))
	for _, archive := range archives {
		pairs[archive.name] = archive.pair
		byName[archive.name] = archive
	}
	replacements, err := textureoverride.Compile(filepath.Join(root, "assets", "texture_overrides"), pairs)
	if err != nil {
		return fmt.Errorf("compile texture overrides: %w", err)
	}
	for name, members := range replacements {
		byName[name].replacements = append(byName[name].replacements, members...)
	}
	return nil
}

func addEquipment(root string, archives []*archive) error {
	data, err := read(root, "release", "strings", "equipment.toml")
	if err != nil {
		return err
	}
	translations, err := fixeddata.ParseEquipment(data)
	if err != nil {
		return err
	}
	var owner *archive
	index := -1
	for _, archive := range archives {
		for _, member := range archive.pair.Members() {
			if member.Name == "data/bindata.dat" {
				if owner != nil {
					return fmt.Errorf("data/bindata.dat is ambiguous across retail archives")
				}
				owner, index = archive, member.Index
			}
		}
	}
	if owner == nil {
		return fmt.Errorf("retail archives do not contain data/bindata.dat")
	}
	source, err := owner.pair.Payload(index)
	if err != nil {
		return err
	}
	compiled, err := fixeddata.ApplyEquipment(source, translations)
	if err != nil {
		return err
	}
	owner.replacements = append(owner.replacements, paa.IndexReplacement(index, compiled))
	return nil
}

func buildExecutable(root, gameDir string) ([]byte, error) {
	eboot, err := os.ReadFile(filepath.Join(gameDir, "SYSDIR", "EBOOT.BIN"))
	if err != nil {
		return nil, fmt.Errorf("read retail EBOOT.BIN: %w", err)
	}
	if actual := fmt.Sprintf("%x", sha256.Sum256(eboot)); actual != sourceEBOOTSHA256 {
		return nil, fmt.Errorf("retail EBOOT.BIN does not match supported ULJM05410 1.03 fingerprint")
	}
	source, err := os.ReadFile(filepath.Join(gameDir, "SYSDIR", "BOOT.BIN"))
	if err != nil {
		return nil, fmt.Errorf("read retail BOOT.BIN: %w", err)
	}
	manifestData, err := read(root, "patches", "executable", "manifest.toml")
	if err != nil {
		return nil, err
	}
	manifest, err := elfpatch.ParseManifest(manifestData)
	if err != nil {
		return nil, err
	}
	patched, err := elfpatch.Apply(source, manifest)
	if err != nil {
		return nil, err
	}
	translationsData, err := read(root, "release", "strings", "eboot.toml")
	if err != nil {
		return nil, err
	}
	translations, err := fixeddata.ParseEBOOT(translationsData)
	if err != nil {
		return nil, err
	}
	return fixeddata.ApplyEBOOT(patched, translations)
}

func buildSFO(root, gameDir string) ([]byte, error) {
	source, err := os.ReadFile(filepath.Join(gameDir, "PARAM.SFO"))
	if err != nil {
		return nil, fmt.Errorf("read retail PARAM.SFO: %w", err)
	}
	manifestData, err := read(root, "patches", "system", "param-sfo.toml")
	if err != nil {
		return nil, err
	}
	manifest, err := sfo.ParseManifest(manifestData)
	if err != nil {
		return nil, err
	}
	return sfo.Apply(source, manifest)
}

func read(root string, parts ...string) ([]byte, error) {
	path := filepath.Join(append([]string{root}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func overlaps(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	return left == right || within(left, right) || within(right, left)
}

func within(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && len(relative) >= 3 && relative[:3] != ".."+string(filepath.Separator)
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == "SYSDIR/ULJM05410_EBOOT.BIN" {
			return nil
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.Mkdir(target, info.Mode().Perm())
		case entry.Type().IsRegular():
			input, err := os.Open(path)
			if err != nil {
				return err
			}
			output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
			if err != nil {
				input.Close()
				return err
			}
			_, copyErr := io.Copy(output, input)
			closeOut, closeIn := output.Close(), input.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeOut != nil {
				return closeOut
			}
			return closeIn
		default:
			return fmt.Errorf("%s: unsupported non-regular source entry", path)
		}
	})
}
