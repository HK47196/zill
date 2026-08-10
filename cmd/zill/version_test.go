// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestResolveBuildVersionUsesOverrideWithoutGit(t *testing.T) {
	got, err := resolveBuildVersion(t.TempDir(), "v1.0-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.0-alpha" {
		t.Fatalf("resolved version = %q, want explicit override", got)
	}
}

func TestResolveBuildVersionReportsTagAndDirtyCheckout(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.name", "Zill Test")
	runGit(t, root, "config", "user.email", "zill@example.invalid")
	tracked := filepath.Join(root, "translation.txt")
	if err := os.WriteFile(tracked, []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "translation.txt")
	runGit(t, root, "commit", "-q", "-m", "fixture")
	runGit(t, root, "tag", "-a", "v1.0-alpha", "-m", "fixture tag")

	got, err := resolveBuildVersion(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.0-alpha" {
		t.Fatalf("clean tagged version = %q, want %q", got, "v1.0-alpha")
	}
	if err := os.WriteFile(tracked, []byte("modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = resolveBuildVersion(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.0-alpha-dirty" {
		t.Fatalf("dirty tagged version = %q, want %q", got, "v1.0-alpha-dirty")
	}
	runGit(t, root, "add", "translation.txt")
	runGit(t, root, "commit", "-q", "-m", "post-tag fixture")
	got, err = resolveBuildVersion(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{7,}$`).MatchString(got) {
		t.Fatalf("untagged commit version = %q, want abbreviated commit hash", got)
	}
}

func runGit(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(arguments, " "), err, output)
	}
}
