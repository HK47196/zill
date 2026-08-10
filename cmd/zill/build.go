// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/layout"
	"github.com/HK47196/zill/internal/release"
)

func runBuild(root string, args []string, stdout, stderr io.Writer) int {
	gameDir := ""
	isoPath := ""
	version := ""
	for index := 0; index < len(args); index++ {
		switch {
		case args[index] == "--game-dir":
			if gameDir != "" {
				fmt.Fprintln(stderr, "zill: build: --game-dir may be specified only once")
				return 2
			}
			if index+1 == len(args) {
				fmt.Fprintln(stderr, "zill: build: --game-dir requires a path")
				return 2
			}
			index++
			gameDir = args[index]
		case strings.HasPrefix(args[index], "--game-dir="):
			if gameDir != "" {
				fmt.Fprintln(stderr, "zill: build: --game-dir may be specified only once")
				return 2
			}
			gameDir = strings.TrimPrefix(args[index], "--game-dir=")
		case args[index] == "--iso":
			if isoPath != "" {
				fmt.Fprintln(stderr, "zill: build: --iso may be specified only once")
				return 2
			}
			if index+1 == len(args) {
				fmt.Fprintln(stderr, "zill: build: --iso requires a path")
				return 2
			}
			index++
			isoPath = args[index]
		case strings.HasPrefix(args[index], "--iso="):
			if isoPath != "" {
				fmt.Fprintln(stderr, "zill: build: --iso may be specified only once")
				return 2
			}
			isoPath = strings.TrimPrefix(args[index], "--iso=")
		case args[index] == "--version":
			if version != "" {
				fmt.Fprintln(stderr, "zill: build: --version may be specified only once")
				return 2
			}
			if index+1 == len(args) {
				fmt.Fprintln(stderr, "zill: build: --version requires a value")
				return 2
			}
			index++
			version = args[index]
			if version == "" {
				fmt.Fprintln(stderr, "zill: build: --version requires a value")
				return 2
			}
		case strings.HasPrefix(args[index], "--version="):
			if version != "" {
				fmt.Fprintln(stderr, "zill: build: --version may be specified only once")
				return 2
			}
			version = strings.TrimPrefix(args[index], "--version=")
			if version == "" {
				fmt.Fprintln(stderr, "zill: build: --version requires a value")
				return 2
			}
		default:
			fmt.Fprintf(stderr, "zill: build: unknown argument %q\n", args[index])
			return 2
		}
	}
	if gameDir == "" || isoPath == "" {
		fmt.Fprintln(stderr, "zill: usage: zill build --game-dir PATH --iso RETAIL_ISO [--version VERSION] (maintainer-only; contributors should run zill check)")
		return 2
	}
	version, err := resolveBuildVersion(root, version)
	if err != nil {
		fmt.Fprintf(stderr, "zill: build: %v\n", err)
		return 1
	}
	result, err := release.Build(root, gameDir, isoPath, version)
	if err != nil {
		fmt.Fprintf(stderr, "zill: build: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Built translated game tree at %s\n", result.GameDirectory)
	fmt.Fprintf(stdout, "Built translated ISO at %s\n", result.ISO)
	fmt.Fprintf(stdout, "Built xdelta patch at %s\n", result.Patch)
	fmt.Fprintf(stdout, "Title attribution version: %s\n", version)
	if len(result.Layout) > 0 {
		printLayoutWarnings(stdout, result.Layout)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(stderr, "zill: build: warning: %s\n", warning)
	}
	return 0
}

func printLayoutWarnings(output io.Writer, warnings []layout.Warning) {
	byCode := make(map[string][]int)
	for _, warning := range warnings {
		byCode[warning.Code] = append(byCode[warning.Code], warning.MessageID)
	}
	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	fmt.Fprintf(output, "QA: %d non-blocking layout warning(s) in %d categories\n", len(warnings), len(codes))
	for _, code := range codes {
		ids := byCode[code]
		limit := min(10, len(ids))
		parts := make([]string, limit)
		for index, id := range ids[:limit] {
			parts[index] = strconv.Itoa(id)
		}
		more := ""
		if len(ids) > limit {
			more = fmt.Sprintf("; %d more", len(ids)-limit)
		}
		fmt.Fprintf(output, "warning: %s: %d (%s%s)\n", code, len(ids), strings.Join(parts, ", "), more)
	}
}
