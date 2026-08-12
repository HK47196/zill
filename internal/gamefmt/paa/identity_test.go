package paa_test

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/HK47196/zill/internal/gamefmt/paa"
)

func TestPairIdentityRemainsAvailableAfterClose(t *testing.T) {
	directory := t.TempDir()
	indexPath, archivePath, indexData, archiveData := writeFixture(t, directory)
	pair, err := paa.Open(indexPath, archivePath)
	if err != nil {
		t.Fatal(err)
	}
	wantBeforeClose := pair.Identity()
	if err := pair.Close(); err != nil {
		t.Fatal(err)
	}
	got := pair.Identity()

	wantIndexPath, err := filepath.Abs(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	wantArchivePath, err := filepath.Abs(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantBeforeClose {
		t.Fatal("Pair identity changed after Close")
	}
	if got.IndexPath != wantIndexPath || got.ArchivePath != wantArchivePath {
		t.Fatalf("identity paths = %q, %q; want %q, %q", got.IndexPath, got.ArchivePath, wantIndexPath, wantArchivePath)
	}
	if got.IndexSHA256 != sha256.Sum256(indexData) {
		t.Fatal("identity index hash does not cover the loaded index")
	}
	if got.ArchiveSize != int64(len(archiveData)) || got.ArchiveModTimeNano != archiveInfo.ModTime().UnixNano() {
		t.Fatalf("identity archive metadata = size %d, mtime %d; want size %d, mtime %d", got.ArchiveSize, got.ArchiveModTimeNano, len(archiveData), archiveInfo.ModTime().UnixNano())
	}
	if runtime.GOOS == "linux" && (got.ArchiveChangeNano == 0 || got.ArchiveInode == 0) {
		t.Fatalf("identity omits archive replacement metadata: %#v", got)
	}
}
