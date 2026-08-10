// SPDX-License-Identifier: GPL-3.0-or-later

package pspiso

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Extract writes every file in the image below destination. It never follows
// an ISO path outside destination and never replaces an existing file.
func (i *Image) Extract(destination string) error {
	if i == nil || i.file == nil {
		return errors.New("PSP ISO image is closed")
	}
	root, err := prepareExtractionRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	return i.ExtractTo(root)
}

// ExtractTo writes every file through an already bound filesystem root. It
// never closes root and never replaces an existing file.
func (i *Image) ExtractTo(root *os.Root) error {
	if i == nil || i.file == nil {
		return errors.New("PSP ISO image is closed")
	}
	if root == nil {
		return errors.New("PSP ISO extraction root is nil")
	}

	for _, entry := range i.manifest.Entries {
		if !entry.IsDir || entry.Path == "/" {
			continue
		}
		rel, err := treePath(entry.Path)
		if err != nil {
			return fmt.Errorf("extract %q: %w", entry.Path, err)
		}
		if err := makeExtractionParents(root, rel); err != nil {
			return fmt.Errorf("extract %q: %w", entry.Path, err)
		}
		if err := root.Mkdir(rel, 0o755); err != nil && !os.IsExist(err) {
			return fmt.Errorf("extract %q: %w", entry.Path, err)
		}
		info, err := root.Lstat(rel)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("extract %q: destination directory is not a real directory", entry.Path)
		}
	}

	for _, entry := range i.manifest.Entries {
		if entry.IsDir {
			continue
		}
		rel, err := treePath(entry.Path)
		if err != nil {
			return fmt.Errorf("extract %q: %w", entry.Path, err)
		}
		if err := makeExtractionParents(root, rel); err != nil {
			return fmt.Errorf("extract %q: %w", entry.Path, err)
		}
		output, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("create extracted file %q: %w", entry.Path, err)
		}
		err = copyExtent(output, i.file, int64(entry.LBA)*SectorSize, int64(entry.Size))
		closeErr := output.Close()
		if err != nil {
			_ = root.Remove(rel)
			return fmt.Errorf("extract %q: %w", entry.Path, err)
		}
		if closeErr != nil {
			_ = root.Remove(rel)
			return fmt.Errorf("close extracted file %q: %w", entry.Path, closeErr)
		}
	}
	return nil
}

func prepareExtractionRoot(destination string) (*os.Root, error) {
	if destination == "" {
		return nil, errors.New("extraction destination is empty")
	}
	root, err := filepath.Abs(destination)
	if err != nil {
		return nil, fmt.Errorf("resolve extraction destination: %w", err)
	}
	parent := filepath.Dir(root)
	base := filepath.Base(root)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create extraction parent: %w", err)
	}
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return nil, fmt.Errorf("open extraction parent: %w", err)
	}
	defer parentRoot.Close()
	if err := parentRoot.Mkdir(base, 0o755); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("create extraction destination: %w", err)
	}
	before, err := parentRoot.Lstat(base)
	if err != nil {
		return nil, fmt.Errorf("stat extraction destination: %w", err)
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("extraction destination must be a directory, not a symlink")
	}
	opened, err := parentRoot.OpenRoot(base)
	if err != nil {
		return nil, fmt.Errorf("open extraction destination: %w", err)
	}
	after, err := opened.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = opened.Close()
		return nil, errors.New("extraction destination changed while opening")
	}
	return opened, nil
}

func treePath(isoPath string) (string, error) {
	if !strings.HasPrefix(isoPath, "/") {
		return "", errors.New("ISO path is not absolute")
	}
	parts := strings.Split(strings.TrimPrefix(isoPath, "/"), "/")
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return "", errors.New("ISO path names the root")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsRune(part, '\x00') {
			return "", errors.New("unsafe ISO path")
		}
	}
	return filepath.Join(parts...), nil
}

func makeExtractionParents(root *os.Root, rel string) error {
	parent := filepath.Dir(rel)
	if parent == "." {
		return nil
	}
	current := ""
	for _, component := range strings.Split(parent, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		if err := root.Mkdir(current, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := root.Stat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("extraction parent %q is not a directory", current)
		}
	}
	return nil
}

func copyExtent(destination io.Writer, source io.ReaderAt, offset, size int64) error {
	buffer := make([]byte, 64*1024)
	for size > 0 {
		chunk := int64(len(buffer))
		if size < chunk {
			chunk = size
		}
		if _, err := source.ReadAt(buffer[:chunk], offset); err != nil {
			return err
		}
		if _, err := destination.Write(buffer[:chunk]); err != nil {
			return err
		}
		offset += chunk
		size -= chunk
	}
	return nil
}
