//go:build linux && amd64

// SPDX-License-Identifier: GPL-3.0-or-later

package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishAllReplacesEveryOutput(t *testing.T) {
	root := t.TempDir()
	stagingGame := filepath.Join(root, ".PSP_GAME.stage")
	destinationGame := filepath.Join(root, "PSP_GAME")
	if err := os.Mkdir(stagingGame, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destinationGame, 0o755); err != nil {
		t.Fatal(err)
	}
	writePublishTestFile(t, filepath.Join(stagingGame, "version"), "new game")
	writePublishTestFile(t, filepath.Join(destinationGame, "version"), "old game")

	items := []publishItem{
		{staging: stagingGame, destination: destinationGame},
		{staging: filepath.Join(root, ".game.iso.stage"), destination: filepath.Join(root, "game.iso")},
		{staging: filepath.Join(root, ".game.xdelta.stage"), destination: filepath.Join(root, "game.xdelta")},
	}
	for index := 1; index < len(items); index++ {
		writePublishTestFile(t, items[index].staging, "new")
		writePublishTestFile(t, items[index].destination, "old")
	}

	warnings, err := publishAll(items)
	if err != nil {
		t.Fatalf("publish bundle: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected cleanup warnings: %v", warnings)
	}
	assertPublishTestFile(t, filepath.Join(destinationGame, "version"), "new game")
	for index := 1; index < len(items); index++ {
		assertPublishTestFile(t, items[index].destination, "new")
	}
	for _, item := range items {
		if _, err := os.Lstat(item.staging); !os.IsNotExist(err) {
			t.Errorf("staging path %s remains after publication: %v", item.staging, err)
		}
	}
}

func TestPublishAllRollsBackReplacementAfterLaterFailure(t *testing.T) {
	root := t.TempDir()
	first := publishItem{staging: filepath.Join(root, "first.stage"), destination: filepath.Join(root, "first")}
	second := publishItem{staging: filepath.Join(root, "second.stage"), destination: filepath.Join(root, "missing", "second")}
	writePublishTestFile(t, first.staging, "new")
	writePublishTestFile(t, first.destination, "old")
	writePublishTestFile(t, second.staging, "second")

	if _, err := publishAll([]publishItem{first, second}); err == nil {
		t.Fatal("publishAll succeeded with an unusable later destination")
	}
	assertPublishTestFile(t, first.destination, "old")
	assertPublishTestFile(t, first.staging, "new")
	assertPublishTestFile(t, second.staging, "second")
}

func TestPublishAllRollsBackNewOutputAfterLaterFailure(t *testing.T) {
	root := t.TempDir()
	first := publishItem{staging: filepath.Join(root, "first.stage"), destination: filepath.Join(root, "first")}
	second := publishItem{staging: filepath.Join(root, "second.stage"), destination: filepath.Join(root, "missing", "second")}
	writePublishTestFile(t, first.staging, "first")
	writePublishTestFile(t, second.staging, "second")

	if _, err := publishAll([]publishItem{first, second}); err == nil {
		t.Fatal("publishAll succeeded with an unusable later destination")
	}
	if _, err := os.Lstat(first.destination); !os.IsNotExist(err) {
		t.Fatalf("new destination remains after rollback: %v", err)
	}
	assertPublishTestFile(t, first.staging, "first")
	assertPublishTestFile(t, second.staging, "second")
}

func TestPublishAllPreflightsEntireBundle(t *testing.T) {
	root := t.TempDir()
	first := publishItem{staging: filepath.Join(root, "first.stage"), destination: filepath.Join(root, "first")}
	missing := publishItem{staging: filepath.Join(root, "missing.stage"), destination: filepath.Join(root, "missing")}
	writePublishTestFile(t, first.staging, "first")

	if _, err := publishAll([]publishItem{first, missing}); err == nil {
		t.Fatal("publishAll succeeded with a missing staged output")
	}
	assertPublishTestFile(t, first.staging, "first")
	if _, err := os.Lstat(first.destination); !os.IsNotExist(err) {
		t.Fatalf("first destination was published before preflight completed: %v", err)
	}
}

func writePublishTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertPublishTestFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("contents of %s = %q, want %q", path, got, want)
	}
}
