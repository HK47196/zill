// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func resolveBuildVersion(root, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if output, err := gitVersionCommand(root, "describe", "--tags", "--exact-match", "--dirty", "--match=v[0-9]*").CombinedOutput(); err == nil {
		version := strings.TrimSpace(string(output))
		if version != "" && !strings.ContainsAny(version, "\r\n") {
			return version, nil
		}
	}
	output, err := gitVersionCommand(root, "rev-parse", "--short=7", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("derive build version from Git: %w: %s; use --version for a source archive", err, strings.TrimSpace(string(output)))
	}
	version := strings.TrimSpace(string(output))
	dirty := gitVersionCommand(root, "diff-index", "--quiet", "HEAD", "--")
	if err := dirty.Run(); err != nil {
		if status, ok := err.(*exec.ExitError); ok && status.ExitCode() == 1 {
			version += "-dirty"
		} else {
			return "", fmt.Errorf("inspect Git worktree state: %w", err)
		}
	}
	if version == "" || strings.ContainsAny(version, "\r\n") {
		return "", fmt.Errorf("derive build version from Git: invalid output %q", version)
	}
	return version, nil
}

func gitVersionCommand(root string, arguments ...string) *exec.Cmd {
	command := exec.Command("git", arguments...)
	command.Dir = root
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "GIT_DIR=") || strings.HasPrefix(value, "GIT_WORK_TREE=") || strings.HasPrefix(value, "GIT_INDEX_FILE=") {
			continue
		}
		command.Env = append(command.Env, value)
	}
	return command
}
