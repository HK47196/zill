// SPDX-License-Identifier: GPL-3.0-or-later

package release_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HK47196/zill/internal/release"
)

func TestBuildFailurePreservesSourceAndExistingDestination(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "retail", "PSP_GAME")
	destination := filepath.Join(root, "build", "PSP_GAME")
	if err := os.MkdirAll(game, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(game, "source-marker")
	destinationPath := filepath.Join(destination, "old-release-marker")
	if err := os.WriteFile(sourcePath, []byte("retail"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destinationPath, []byte("published"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := release.Build(root, game); err == nil {
		t.Fatal("Build succeeded without its required canonical inputs")
	}
	for path, want := range map[string]string{sourcePath: "retail", destinationPath: "published"} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read preserved file %s: %v", path, err)
		}
		if string(got) != want {
			t.Fatalf("%s changed after failed build: got %q, want %q", path, got, want)
		}
	}
}
