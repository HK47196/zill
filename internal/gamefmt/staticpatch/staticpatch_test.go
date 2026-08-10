package staticpatch

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyAuthenticatesBothInputsAndResult(t *testing.T) {
	directory := t.TempDir()
	source := []byte{1, 2, 3, 4}
	want := []byte{9, 2, 7, 4}
	delta := []byte{8, 0, 4, 0}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(delta); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "member.zpatch"), compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	member := Member{Archive: "pa", Index: 2, Name: "member", Size: len(source), SourceSHA256: hash(source), ResultSHA256: hash(want), Patch: "member.zpatch", PatchSHA256: hash(compressed.Bytes()), XORSHA256: hash(delta)}
	got, err := Apply(member, source, directory)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("result = %x, want %x", got, want)
	}
	source[0] ^= 1
	if _, err := Apply(member, source, directory); err == nil {
		t.Fatal("modified retail input was accepted")
	}
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
