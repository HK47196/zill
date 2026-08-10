// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildRequiresGameDirectoryAndRetailISO(t *testing.T) {
	t.Parallel()

	for name, arguments := range map[string][]string{
		"neither input": nil,
		"game only":     {"--game-dir", "/retail/PSP_GAME"},
		"ISO only":      {"--iso", "/retail/game.iso"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := runBuild(t.TempDir(), arguments, &stdout, &stderr); code != 2 {
				t.Fatalf("runBuild exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), "--game-dir PATH --iso RETAIL_ISO") {
				t.Fatalf("usage does not name both required inputs: %q", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("invalid build wrote stdout: %q", stdout.String())
			}
		})
	}
}
