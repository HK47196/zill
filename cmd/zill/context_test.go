// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestContextRequiresOneValidSelector(t *testing.T) {
	tests := map[string][]string{
		"missing game directory": {"--bank", "135"},
		"missing selector":       {"--game-dir", "PSP_GAME"},
		"two selectors":          {"--game-dir", "PSP_GAME", "--bank", "135", "--record", "1350035"},
		"invalid bank":           {"--game-dir", "PSP_GAME", "--bank", "279"},
		"invalid format":         {"--game-dir", "PSP_GAME", "--bank", "135", "--format", "yaml"},
	}
	for name, arguments := range tests {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runContext("../..", arguments, &stdout, &stderr); code != 2 {
				t.Fatalf("exit code = %d, want 2; stderr: %s", code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("invalid invocation wrote stdout: %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), contextUsage) {
				t.Fatalf("stderr does not contain usage: %q", stderr.String())
			}
		})
	}
}
