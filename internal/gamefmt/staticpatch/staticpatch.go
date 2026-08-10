// Package staticpatch applies authenticated, compressed XOR patches to retail
// archive members. It deliberately cannot patch an unauthenticated input.
package staticpatch

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type manifest struct {
	Format      string   `toml:"format"`
	Version     int      `toml:"version"`
	Compression string   `toml:"compression"`
	Operation   string   `toml:"operation"`
	Members     []Member `toml:"member"`
}

// Member identifies one authenticated archive-member transform.
type Member struct {
	Archive      string `toml:"archive"`
	Index        int    `toml:"index"`
	Name         string `toml:"name"`
	Size         int    `toml:"size"`
	SourceSHA256 string `toml:"source_sha256"`
	ResultSHA256 string `toml:"result_sha256"`
	Patch        string `toml:"patch"`
	PatchSHA256  string `toml:"patch_sha256"`
	XORSHA256    string `toml:"xor_sha256"`
}

// Manifest is a validated collection of static retail-member transforms.
type Manifest struct {
	members []Member
}

// ParseManifest parses the only supported static patch format.
func ParseManifest(data []byte) (*Manifest, error) {
	var raw manifest
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("static patch manifest: %w", err)
	}
	if raw.Format != "zill-static-member-patches" || raw.Version != 1 || raw.Compression != "zlib" || raw.Operation != "xor" {
		return nil, fmt.Errorf("unsupported static patch manifest")
	}
	if len(raw.Members) == 0 {
		return nil, fmt.Errorf("static patch manifest has no members")
	}
	seen := make(map[string]bool, len(raw.Members))
	for _, member := range raw.Members {
		key := fmt.Sprintf("%s:%d", member.Archive, member.Index)
		if seen[key] || member.Archive == "" || member.Index < 0 || member.Name == "" || member.Size <= 0 || filepath.Base(member.Patch) != member.Patch {
			return nil, fmt.Errorf("invalid or duplicate static patch member %s", key)
		}
		for label, value := range map[string]string{
			"source": member.SourceSHA256, "result": member.ResultSHA256,
			"patch": member.PatchSHA256, "xor": member.XORSHA256,
		} {
			decoded, err := hex.DecodeString(value)
			if err != nil || len(decoded) != sha256.Size {
				return nil, fmt.Errorf("static patch member %s has invalid %s hash", key, label)
			}
		}
		seen[key] = true
	}
	return &Manifest{members: append([]Member(nil), raw.Members...)}, nil
}

// Members returns the ordered patch targets.
func (m *Manifest) Members() []Member { return append([]Member(nil), m.members...) }

// Apply authenticates source and the patch file, then reconstructs and
// authenticates the complete replacement member.
func Apply(member Member, source []byte, patchDir string) ([]byte, error) {
	if len(source) != member.Size || digest(source) != member.SourceSHA256 {
		return nil, fmt.Errorf("%s member %d %q does not match the supported retail revision", member.Archive, member.Index, member.Name)
	}
	compressed, err := os.ReadFile(filepath.Join(patchDir, member.Patch))
	if err != nil {
		return nil, fmt.Errorf("read static patch %s: %w", member.Patch, err)
	}
	if digest(compressed) != member.PatchSHA256 {
		return nil, fmt.Errorf("static patch %s failed authentication", member.Patch)
	}
	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("open static patch %s: %w", member.Patch, err)
	}
	delta, readErr := io.ReadAll(io.LimitReader(reader, int64(member.Size)+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(delta) != member.Size || digest(delta) != member.XORSHA256 {
		return nil, fmt.Errorf("static patch %s has invalid decompressed contents", member.Patch)
	}
	result := make([]byte, member.Size)
	for index := range result {
		result[index] = source[index] ^ delta[index]
	}
	if digest(result) != member.ResultSHA256 {
		return nil, fmt.Errorf("static patch %s produced an unauthenticated result", member.Patch)
	}
	return result, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
