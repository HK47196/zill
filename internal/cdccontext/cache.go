// SPDX-License-Identifier: GPL-3.0-or-later

package cdccontext

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	retailCacheMagic           = "ZILLCIX\x00"
	retailCacheFormatVersion   = uint32(2)
	retailIndexSemanticVersion = uint32(1)
	retailCacheHeaderSize      = 8 + 4 + 4 + 8 + sha256.Size
	maxRetailCachePayload      = 512 << 20
)

// LoadOrBuildRetailIndex returns an immutable retail index from a compatible
// local cache when possible. Cache failures are deliberately nonfatal: an
// unavailable, stale, or corrupt cache is rebuilt from the already-opened
// archives.
func LoadOrBuildRetailIndex(archives []Archive, cacheDir string) (*RetailIndex, error) {
	key, cacheable := retailCacheKey(archives)
	if cacheable && cacheDir != "" {
		path := filepath.Join(cacheDir, fmt.Sprintf("retail-%x.cix", key))
		if index, ok := readRetailCache(path); ok {
			return index, nil
		}

		index, err := BuildRetailIndex(archives)
		if err != nil {
			return nil, err
		}
		if writeRetailCache(cacheDir, path, index) == nil {
			// Return the persisted representation on the first query too so the
			// cold and hot paths exercise the same validated cache contract.
			if persisted, ok := readRetailCache(path); ok {
				return persisted, nil
			}
		}
		return index, nil
	}

	return BuildRetailIndex(archives)
}

func retailCacheKey(archives []Archive) ([sha256.Size]byte, bool) {
	hash := sha256.New()
	writeUint32(hash, retailIndexSemanticVersion)
	writeUint32(hash, uint32(len(archives)))
	for _, archive := range archives {
		if archive.Pair == nil {
			return [sha256.Size]byte{}, false
		}
		identity := archive.Pair.Identity()
		writeIdentityString(hash, archive.Name)
		writeIdentityString(hash, identity.IndexPath)
		writeIdentityString(hash, identity.ArchivePath)
		_, _ = hash.Write(identity.IndexSHA256[:])
		writeUint64(hash, uint64(identity.ArchiveSize))
		writeUint64(hash, uint64(identity.ArchiveModTimeNano))
		writeUint64(hash, uint64(identity.ArchiveChangeNano))
		writeUint64(hash, identity.ArchiveDevice)
		writeUint64(hash, identity.ArchiveInode)
	}
	var key [sha256.Size]byte
	copy(key[:], hash.Sum(nil))
	return key, true
}

func writeIdentityString(dst io.Writer, value string) {
	writeUint64(dst, uint64(len(value)))
	_, _ = io.WriteString(dst, value)
}

func writeUint32(dst io.Writer, value uint32) {
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], value)
	_, _ = dst.Write(data[:])
}

func writeUint64(dst io.Writer, value uint64) {
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], value)
	_, _ = dst.Write(data[:])
}

func readRetailCache(path string) (*RetailIndex, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.Size() < retailCacheHeaderSize || info.Size() > retailCacheHeaderSize+maxRetailCachePayload {
		return nil, false
	}
	header := make([]byte, retailCacheHeaderSize)
	if _, err := io.ReadFull(file, header); err != nil || string(header[:8]) != retailCacheMagic {
		return nil, false
	}
	if binary.LittleEndian.Uint32(header[8:12]) != retailCacheFormatVersion ||
		binary.LittleEndian.Uint32(header[12:16]) != retailIndexSemanticVersion {
		return nil, false
	}
	payloadLength := binary.LittleEndian.Uint64(header[16:24])
	if payloadLength > maxRetailCachePayload || uint64(info.Size()-retailCacheHeaderSize) != payloadLength {
		return nil, false
	}
	payload := make([]byte, int(payloadLength))
	if _, err := io.ReadFull(file, payload); err != nil {
		return nil, false
	}
	wantHash := header[24 : 24+sha256.Size]
	gotHash := sha256.Sum256(payload)
	if !bytes.Equal(gotHash[:], wantHash) {
		return nil, false
	}

	var index RetailIndex
	if err := json.Unmarshal(payload, &index); err != nil {
		return nil, false
	}
	if err := index.validate(); err != nil {
		return nil, false
	}
	return &index, true
}

func writeRetailCache(directory, path string, index *RetailIndex) error {
	payload, err := json.Marshal(index)
	if err != nil {
		return err
	}
	if len(payload) > maxRetailCachePayload {
		return fmt.Errorf("retail cache payload is too large: %d bytes", len(payload))
	}

	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".retail-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	header := make([]byte, retailCacheHeaderSize)
	copy(header, retailCacheMagic)
	binary.LittleEndian.PutUint32(header[8:12], retailCacheFormatVersion)
	binary.LittleEndian.PutUint32(header[12:16], retailIndexSemanticVersion)
	binary.LittleEndian.PutUint64(header[16:24], uint64(len(payload)))
	payloadHash := sha256.Sum256(payload)
	copy(header[24:], payloadHash[:])
	_, err = temporary.Write(header)
	if err == nil {
		_, err = temporary.Write(payload)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
