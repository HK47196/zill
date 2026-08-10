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
	iso := filepath.Join(root, "retail.iso")
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
	if err := os.WriteFile(iso, []byte("retail iso"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := release.Build(root, game, iso); err == nil {
		t.Fatal("Build succeeded without its required canonical inputs")
	}
	for path, want := range map[string]string{sourcePath: "retail", iso: "retail iso", destinationPath: "published"} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read preserved file %s: %v", path, err)
		}
		if string(got) != want {
			t.Fatalf("%s changed after failed build: got %q, want %q", path, got, want)
		}
	}
}

func TestBuildRejectsRetailInputAliasesToOutputs(t *testing.T) {
	t.Parallel()

	t.Run("PSP_GAME symlink", func(t *testing.T) {
		root := t.TempDir()
		destination := filepath.Join(root, "build", "PSP_GAME")
		if err := os.MkdirAll(destination, 0o755); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(destination, "retail-marker")
		if err := os.WriteFile(marker, []byte("retail"), 0o644); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(root, "retail-psp-game")
		if err := os.Symlink(destination, alias); err != nil {
			t.Fatal(err)
		}
		iso := filepath.Join(root, "retail.iso")
		if err := os.WriteFile(iso, []byte("retail iso"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := release.Build(root, alias, iso); err == nil {
			t.Fatal("Build accepted a source PSP_GAME alias to its destination")
		}
		if got, err := os.ReadFile(marker); err != nil || string(got) != "retail" {
			t.Fatalf("aliased retail source changed: contents %q, error %v", got, err)
		}
	})

	t.Run("ISO hard link", func(t *testing.T) {
		root := t.TempDir()
		game := filepath.Join(root, "retail", "PSP_GAME")
		if err := os.MkdirAll(game, 0o755); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(root, "build", "zill-english.iso")
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, []byte("retail iso"), 0o644); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(root, "retail.iso")
		if err := os.Link(output, alias); err != nil {
			t.Fatal(err)
		}
		if _, err := release.Build(root, game, alias); err == nil {
			t.Fatal("Build accepted a retail ISO hard link to its destination")
		}
		if got, err := os.ReadFile(alias); err != nil || string(got) != "retail iso" {
			t.Fatalf("aliased retail ISO changed: contents %q, error %v", got, err)
		}
	})
}
