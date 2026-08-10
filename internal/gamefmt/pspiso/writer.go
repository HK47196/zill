// SPDX-License-Identifier: GPL-3.0-or-later

package pspiso

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Build authors an untouched PSP ISO from manifest and tree. It never opens or
// reads the source ISO from which the manifest was made, and verifies that the
// new image matches the manifest's source hash. outputPath must not exist.
func Build(outputPath string, manifest Manifest, tree fs.FS) error {
	return build(outputPath, manifest, tree, true)
}

// BuildModified authors a PSP ISO from the source manifest and replacement
// payloads in tree. Growing extents reflow later files while preserving their
// source order and alignment. outputPath must not exist.
func BuildModified(outputPath string, manifest Manifest, tree fs.FS) error {
	return build(outputPath, manifest, tree, false)
}

func build(outputPath string, manifest Manifest, tree fs.FS, requireExact bool) error {
	if err := validateManifestForWrite(manifest); err != nil {
		return err
	}
	if tree == nil {
		return errors.New("PSP ISO source tree is nil")
	}
	if outputPath == "" {
		return errors.New("PSP ISO output path is empty")
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("PSP ISO output %q already exists", outputPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat PSP ISO output: %w", err)
	}

	directory := filepath.Dir(outputPath)
	temporary, err := os.CreateTemp(directory, ".pspiso-*")
	if err != nil {
		return fmt.Errorf("create temporary PSP ISO: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := buildFile(temporary, manifest, tree, requireExact); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary PSP ISO: %w", err)
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("PSP ISO output %q was created while staging", outputPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("restat PSP ISO output: %w", err)
	}
	if err := os.Link(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish PSP ISO: %w", err)
	}
	_ = os.Remove(temporaryPath)
	return nil
}

// BuildFile authors an untouched PSP ISO into a newly created, empty regular
// file. It does not close output. Callers that need atomic pathname publication
// should use Build.
func BuildFile(output *os.File, manifest Manifest, tree fs.FS) error {
	return buildFile(output, manifest, tree, true)
}

// BuildModifiedFile is BuildModified for an already created, empty regular
// file. It does not close output.
func BuildModifiedFile(output *os.File, manifest Manifest, tree fs.FS) error {
	return buildFile(output, manifest, tree, false)
}

func buildFile(output *os.File, manifest Manifest, tree fs.FS, requireExact bool) error {
	if output == nil {
		return errors.New("PSP ISO output file is nil")
	}
	if tree == nil {
		return errors.New("PSP ISO source tree is nil")
	}
	if !requireExact {
		planned, err := planModifiedManifest(manifest, tree)
		if err != nil {
			return err
		}
		manifest = planned
	}
	if err := validateManifestForWrite(manifest); err != nil {
		return err
	}
	info, err := output.Stat()
	if err != nil {
		return fmt.Errorf("stat PSP ISO output: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() != 0 {
		return errors.New("PSP ISO output must be a new, empty regular file")
	}
	imageSize := int64(manifest.VolumeSpaceSize) * SectorSize
	if err := output.Truncate(imageSize); err != nil {
		return fmt.Errorf("size PSP ISO output: %w", err)
	}
	if err := writeManifest(output, manifest, tree); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("sync PSP ISO output: %w", err)
	}
	verified, err := inspect(output)
	if err != nil {
		return fmt.Errorf("validate newly authored PSP ISO: %w", err)
	}
	if verified.SourceSize != manifest.SourceSize {
		return errors.New("newly authored PSP ISO size does not match the source manifest")
	}
	if requireExact && verified.SourceSHA256 != manifest.SourceSHA256 {
		return errors.New("newly authored PSP ISO does not match the source manifest")
	}
	return nil
}

func validateManifestForWrite(manifest Manifest) error {
	if manifest.SectorSize != SectorSize {
		return fmt.Errorf("PSP ISO manifest sector size is %d, want %d", manifest.SectorSize, SectorSize)
	}
	if manifest.VolumeSpaceSize == 0 {
		return errors.New("PSP ISO manifest has no sectors")
	}
	if manifest.SourceSize != int64(manifest.VolumeSpaceSize)*SectorSize {
		return errors.New("PSP ISO manifest source size does not match its volume size")
	}
	if len(manifest.SystemArea) != 16*SectorSize {
		return fmt.Errorf("PSP ISO System Area is %d bytes, want %d", len(manifest.SystemArea), 16*SectorSize)
	}
	claimed := make([]bool, manifest.VolumeSpaceSize)
	claim := func(start, count uint32, kind string) error {
		if count == 0 || start >= manifest.VolumeSpaceSize || count > manifest.VolumeSpaceSize-start {
			return fmt.Errorf("PSP ISO manifest %s is outside the volume", kind)
		}
		for lba := start; lba < start+count; lba++ {
			if claimed[lba] {
				return fmt.Errorf("PSP ISO manifest %s overlaps another extent at LBA %d", kind, lba)
			}
			claimed[lba] = true
		}
		return nil
	}
	if err := claim(0, 16, "System Area"); err != nil {
		return err
	}
	for _, descriptor := range manifest.Descriptors {
		if descriptor.LBA >= manifest.VolumeSpaceSize || len(descriptor.Data) != SectorSize {
			return errors.New("PSP ISO manifest has an invalid volume descriptor")
		}
		if err := claim(descriptor.LBA, 1, "volume descriptor"); err != nil {
			return err
		}
	}
	for _, table := range manifest.PathTables {
		allocated := sectorsFor(table.Size) * SectorSize
		if table.LBA >= manifest.VolumeSpaceSize || uint64(table.LBA)*SectorSize+uint64(allocated) > uint64(manifest.VolumeSpaceSize)*SectorSize || uint32(len(table.Data)) != allocated {
			return errors.New("PSP ISO manifest has an invalid path table")
		}
		if err := claim(table.LBA, sectorsFor(table.Size), "path table"); err != nil {
			return err
		}
	}
	for _, directory := range manifest.Directories {
		allocated := sectorsFor(directory.Size) * SectorSize
		if directory.LBA >= manifest.VolumeSpaceSize || uint32(len(directory.Data)) != allocated || uint64(directory.LBA)*SectorSize+uint64(allocated) > uint64(manifest.VolumeSpaceSize)*SectorSize {
			return errors.New("PSP ISO manifest has an invalid directory extent")
		}
		if err := claim(directory.LBA, sectorsFor(directory.Size), "directory extent"); err != nil {
			return err
		}
	}
	for _, entry := range manifest.Entries {
		if entry.IsDir {
			continue
		}
		if _, err := treePath(entry.Path); err != nil {
			return fmt.Errorf("PSP ISO manifest file %q: %w", entry.Path, err)
		}
		allocated := sectorsFor(entry.Size) * SectorSize
		if uint64(entry.LBA)*SectorSize+uint64(allocated) > uint64(manifest.VolumeSpaceSize)*SectorSize {
			return fmt.Errorf("PSP ISO file %q extends past volume", entry.Path)
		}
		if len(entry.Padding) != int(allocated-entry.Size) {
			return fmt.Errorf("PSP ISO file %q has %d padding bytes, want %d", entry.Path, len(entry.Padding), allocated-entry.Size)
		}
		if allocated != 0 {
			if err := claim(entry.LBA, sectorsFor(entry.Size), "file payload"); err != nil {
				return err
			}
		}
	}
	for _, fill := range manifest.FillRanges {
		if fill.StartLBA >= fill.EndLBA || fill.EndLBA > manifest.VolumeSpaceSize {
			return errors.New("PSP ISO manifest has an invalid fill range")
		}
		if err := claim(fill.StartLBA, fill.EndLBA-fill.StartLBA, "fill range"); err != nil {
			return err
		}
	}
	for lba, isClaimed := range claimed {
		if !isClaimed {
			return fmt.Errorf("PSP ISO manifest does not describe LBA %d", lba)
		}
	}
	return nil
}

func writeManifest(output *os.File, manifest Manifest, tree fs.FS) error {
	if err := writeAt(output, manifest.SystemArea, 0); err != nil {
		return fmt.Errorf("write PSP ISO System Area: %w", err)
	}
	for _, descriptor := range manifest.Descriptors {
		if err := writeAt(output, descriptor.Data, int64(descriptor.LBA)*SectorSize); err != nil {
			return fmt.Errorf("write PSP ISO descriptor at LBA %d: %w", descriptor.LBA, err)
		}
	}
	for _, table := range manifest.PathTables {
		if err := writeAt(output, table.Data, int64(table.LBA)*SectorSize); err != nil {
			return fmt.Errorf("write PSP ISO path table at LBA %d: %w", table.LBA, err)
		}
	}
	for _, directory := range manifest.Directories {
		if err := writeAt(output, directory.Data, int64(directory.LBA)*SectorSize); err != nil {
			return fmt.Errorf("write PSP ISO directory %q: %w", directory.Path, err)
		}
	}
	for _, fill := range manifest.FillRanges {
		if err := writeFill(output, int64(fill.StartLBA)*SectorSize, int64(fill.EndLBA-fill.StartLBA)*SectorSize, fill.Byte); err != nil {
			return fmt.Errorf("write PSP ISO fill at LBA %d: %w", fill.StartLBA, err)
		}
	}

	entries := append([]Entry(nil), manifest.Entries...)
	sort.SliceStable(entries, func(a, b int) bool { return entries[a].LBA < entries[b].LBA })
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		if err := writeTreeFile(output, tree, entry); err != nil {
			return err
		}
	}
	return nil
}

func writeTreeFile(output *os.File, tree fs.FS, entry Entry) error {
	path, err := treePath(entry.Path)
	if err != nil {
		return err
	}
	source, err := tree.Open(path)
	if err != nil {
		return fmt.Errorf("open extracted PSP ISO file %q: %w", entry.Path, err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return fmt.Errorf("stat extracted PSP ISO file %q: %w", entry.Path, err)
	}
	if !info.Mode().IsRegular() || info.Size() != int64(entry.Size) {
		return fmt.Errorf("extracted PSP ISO file %q has size %d, want %d", entry.Path, info.Size(), entry.Size)
	}
	if err := writeReaderAt(output, source, int64(entry.LBA)*SectorSize, int64(entry.Size)); err != nil {
		return fmt.Errorf("write PSP ISO file %q: %w", entry.Path, err)
	}
	if len(entry.Padding) != 0 {
		if err := writeAt(output, entry.Padding, int64(entry.LBA)*SectorSize+int64(entry.Size)); err != nil {
			return fmt.Errorf("write PSP ISO file padding %q: %w", entry.Path, err)
		}
	}
	return nil
}

func sectorsFor(size uint32) uint32 {
	if size == 0 {
		return 0
	}
	return (size-1)/SectorSize + 1
}

func writeAt(output *os.File, data []byte, offset int64) error {
	for len(data) > 0 {
		count, err := output.WriteAt(data, offset)
		offset += int64(count)
		data = data[count:]
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func writeReaderAt(output *os.File, source io.Reader, offset, size int64) error {
	buffer := make([]byte, 64*1024)
	for size > 0 {
		chunk := int64(len(buffer))
		if size < chunk {
			chunk = size
		}
		count, err := io.ReadFull(source, buffer[:chunk])
		if err != nil {
			return err
		}
		if count != int(chunk) {
			return io.ErrUnexpectedEOF
		}
		if err := writeAt(output, buffer[:chunk], offset); err != nil {
			return err
		}
		offset += chunk
		size -= chunk
	}
	var extra [1]byte
	if count, err := source.Read(extra[:]); count != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return errors.New("file contents changed while building PSP ISO")
	}
	return nil
}

func writeFill(output *os.File, offset, size int64, value byte) error {
	buffer := make([]byte, 64*1024)
	for n := range buffer {
		buffer[n] = value
	}
	for size > 0 {
		chunk := int64(len(buffer))
		if size < chunk {
			chunk = size
		}
		if err := writeAt(output, buffer[:chunk], offset); err != nil {
			return err
		}
		offset += chunk
		size -= chunk
	}
	return nil
}
