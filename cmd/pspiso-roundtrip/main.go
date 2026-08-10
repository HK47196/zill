// SPDX-License-Identifier: GPL-3.0-or-later

// Command pspiso-roundtrip proves that the supported retail PSP ISO can be
// extracted and authored as a new, byte-identical image.
package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/HK47196/zill/internal/gamefmt/pspiso"
)

const (
	supportedSize   = 728104960
	supportedSHA256 = "6e0827585c94694a3aeaca6322713a347cdce86bcbab768fa4b9d093f38d68fa"
)

func main() {
	source := flag.String("source", "", "path to the supported read-only retail ISO")
	work := flag.String("work", "", "new private work directory below build/iso-roundtrip")
	flag.Parse()
	if flag.NArg() != 0 || *source == "" || *work == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/pspiso-roundtrip --source PATH --work build/iso-roundtrip/NAME")
		os.Exit(2)
	}
	if err := run(*source, *work); err != nil {
		fmt.Fprintf(os.Stderr, "pspiso-roundtrip: %v\n", err)
		os.Exit(1)
	}
}

func run(source, work string) error {
	expectedHash, err := hex.DecodeString(supportedSHA256)
	if err != nil {
		return fmt.Errorf("decode supported hash: %w", err)
	}
	work, err = filepath.Abs(work)
	if err != nil {
		return fmt.Errorf("resolve work directory: %w", err)
	}
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}
	allowedParent := filepath.Join(projectRoot, "build", "iso-roundtrip")
	if filepath.Dir(work) != allowedParent || filepath.Base(work) == "." || filepath.Base(work) == ".." {
		return errors.New("work directory must be a direct child of build/iso-roundtrip")
	}

	project, err := os.OpenRoot(".")
	if err != nil {
		return fmt.Errorf("open project root: %w", err)
	}
	defer project.Close()
	build, err := createAndOpenRealChild(project, "build")
	if err != nil {
		return fmt.Errorf("open build directory: %w", err)
	}
	defer build.Close()
	roundtrip, err := createAndOpenRealChild(build, "iso-roundtrip")
	if err != nil {
		return fmt.Errorf("open round-trip directory: %w", err)
	}
	defer roundtrip.Close()
	workName := filepath.Base(work)
	if err := roundtrip.Mkdir(workName, 0o755); err != nil {
		return fmt.Errorf("create new work directory: %w", err)
	}
	workRoot, err := openRealChild(roundtrip, workName)
	if err != nil {
		return fmt.Errorf("open new work directory: %w", err)
	}
	defer workRoot.Close()

	image, err := pspiso.Open(source)
	if err != nil {
		return err
	}
	manifest := image.Manifest()
	if manifest.SourceSize != supportedSize || !bytes.Equal(manifest.SourceSHA256[:], expectedHash) {
		_ = image.Close()
		return fmt.Errorf("source is not the supported retail ISO: size %d, SHA-256 %x", manifest.SourceSize, manifest.SourceSHA256)
	}

	if err := workRoot.Mkdir("tree", 0o755); err != nil {
		_ = image.Close()
		return fmt.Errorf("create extraction tree: %w", err)
	}
	treeRoot, err := openRealChild(workRoot, "tree")
	if err != nil {
		_ = image.Close()
		return fmt.Errorf("open extraction tree: %w", err)
	}
	defer treeRoot.Close()
	if err := image.ExtractTo(treeRoot); err != nil {
		_ = image.Close()
		return err
	}
	if err := image.Close(); err != nil {
		return fmt.Errorf("close source ISO before authoring: %w", err)
	}

	const outputName = "rebuilt.iso"
	output, err := workRoot.OpenFile(outputName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create rebuilt PSP ISO: %w", err)
	}
	if err := pspiso.BuildFile(output, manifest, treeRoot.FS()); err != nil {
		_ = output.Close()
		_ = workRoot.Remove(outputName)
		return err
	}
	if err := output.Close(); err != nil {
		_ = workRoot.Remove(outputName)
		return fmt.Errorf("close rebuilt PSP ISO: %w", err)
	}
	outputPath := filepath.Join(work, outputName)
	fmt.Printf("source_size=%d\nsource_sha256=%x\nentries=%d\nrebuilt=%s\n", manifest.SourceSize, manifest.SourceSHA256, len(manifest.Entries), outputPath)
	return nil
}

func createAndOpenRealChild(parent *os.Root, name string) (*os.Root, error) {
	if err := parent.Mkdir(name, 0o755); err != nil && !os.IsExist(err) {
		return nil, err
	}
	return openRealChild(parent, name)
}

func openRealChild(parent *os.Root, name string) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("path must be a real directory")
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = child.Close()
		return nil, errors.New("directory changed while opening")
	}
	return child, nil
}
