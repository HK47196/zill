// SPDX-License-Identifier: GPL-3.0-or-later

package pspiso

import (
	"fmt"
	"os"
)

// Image is an immutable, read-only description of a PSP ISO image.
type Image struct {
	file     *os.File
	manifest Manifest
}

// Open opens path read-only, validates its ISO 9660 PSP profile, and records
// the complete non-payload layout needed by Build.
func Open(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open PSP ISO: %w", err)
	}
	manifest, err := inspect(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Image{file: f, manifest: manifest}, nil
}

// Close closes the source ISO. A manifest obtained before Close remains valid.
func (i *Image) Close() error {
	if i == nil || i.file == nil {
		return nil
	}
	err := i.file.Close()
	i.file = nil
	return err
}

// Manifest returns an independent deep copy of the source-independent layout.
func (i *Image) Manifest() Manifest {
	if i == nil {
		return Manifest{}
	}
	return i.manifest.clone()
}
