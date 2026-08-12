// SPDX-License-Identifier: GPL-3.0-or-later

// Command zill helps contributors edit and check the Zill O'll Infinite Plus translation.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/HK47196/zill/internal/corpus"
	"github.com/HK47196/zill/internal/fixeddata"
	"github.com/HK47196/zill/internal/release"
)

const usage = `Usage: zill <command> [options]

Commands:
	check             Validate contributor translation data
	context           Find and review recovered dialogue scenes
	search            Search IDs, Japanese, and English
	show              Show one record and nearby context
	ppsspp-debugger   Control a running PPSSPP instance through JSON Lines
	build             Maintainer-only: build PSP_GAME, ISO, and xdelta outputs
	help              Show this help
`

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "zill: determine project root: %v\n", err)
		return 1
	}
	switch args[0] {
	case "build":
		return runBuild(root, args[1:], stdout, stderr)
	case "check":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "zill: usage: zill check")
			return 2
		}
		return runCheck(root, stdout, stderr)
	case "context":
		return runContext(root, args[1:], stdout, stderr)
	case "show":
		return runShow(root, args[1:], stdout, stderr)
	case "search":
		return runSearch(root, args[1:], stdout, stderr)
	case "ppsspp-debugger":
		return runPPSSPPDebugger(args[1:], stdin, stdout, stderr)
	}

	fmt.Fprintf(stderr, "zill: unknown command %q\n\n", args[0])
	fmt.Fprint(stderr, usage)
	return 2
}

func runCheck(root string, stdout, stderr io.Writer) int {
	project, summary, err := corpus.LoadProject(root)
	if err != nil {
		fmt.Fprintf(stderr, "zill: check: %v\n", err)
		return 1
	}
	if err := release.Check(root, project); err != nil {
		fmt.Fprintf(stderr, "zill: check: %v\n", err)
		return 1
	}
	fmt.Fprintf(
		stdout,
		"OK: %d records in %d banks; %d translated, %d keep_japanese, %d todo\n",
		summary.Records, summary.Banks, summary.Translated,
		summary.KeepJapanese, summary.Todo,
	)
	return 0
}

func runShow(root string, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "zill: usage: zill show ID (the editable English is in translations/messages/msgsecNNN.toml)")
		return 2
	}
	id, err := strconv.Atoi(args[0])
	if err != nil || id < 0 {
		fmt.Fprintf(stderr, "zill: show: invalid decimal ID %q\n", args[0])
		return 2
	}
	project, _, err := corpus.LoadProject(root)
	if err != nil {
		fmt.Fprintf(stderr, "zill: show: %v\n", err)
		return 1
	}
	item, ok := project.Find(id)
	if !ok {
		fmt.Fprintf(stderr, "zill: show: ID %d does not exist\n", id)
		return 1
	}
	fmt.Fprintf(stdout, "ID: %d\nSection: %03d\nIndex: %d\nEditable: %s\nState: %s\n", id, item.Record.ID/10_000, item.Record.Index, editablePath(id), item.Translation.State)
	fmt.Fprintf(stdout, "Japanese: %s\n", item.Record.Display)
	fmt.Fprintf(stdout, "English: %s\n", item.Translation.Text)
	terms, err := loadTerminology(root)
	if err != nil {
		fmt.Fprintf(stderr, "zill: show: %v\n", err)
		return 1
	}
	for _, authority := range terms.Applicable(item) {
		fmt.Fprintf(stdout, "Authority: %s: %s → %s\n", authority.Kind, authority.Term.Japanese, authority.Term.English)
	}
	fmt.Fprintln(stdout, "Context:")
	position := 0
	for index := range project.Items {
		if project.Items[index].Record.ID == id {
			position = index
			break
		}
	}
	first := max(0, position-2)
	last := min(len(project.Items), position+3)
	for index := first; index < last; index++ {
		marker := " "
		if index == position {
			marker = ">"
		}
		context := project.Items[index]
		fmt.Fprintf(
			stdout, "%s %d [%s]\n  Japanese: %s\n  English: %s\n", marker, context.Record.ID,
			context.Translation.State, snippet(context.Record.Display, 120), snippet(context.Translation.Text, 120),
		)
	}
	return 0
}

func runSearch(root string, args []string, stdout, stderr io.Writer) int {
	state := ""
	queryParts := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		switch {
		case args[index] == "--state":
			if index+1 == len(args) {
				fmt.Fprintln(stderr, "zill: search: --state requires a value")
				return 2
			}
			index++
			state = args[index]
		case strings.HasPrefix(args[index], "--state="):
			state = strings.TrimPrefix(args[index], "--state=")
		case strings.HasPrefix(args[index], "-"):
			fmt.Fprintf(stderr, "zill: search: unknown option %q\n", args[index])
			return 2
		default:
			queryParts = append(queryParts, args[index])
		}
	}
	if state != "" && state != string(corpus.Translated) && state != string(corpus.KeepJapanese) && state != string(corpus.Todo) {
		fmt.Fprintf(stderr, "zill: search: unsupported state %q\n", state)
		return 2
	}
	query := strings.ToLower(strings.Join(queryParts, " "))
	if state == "" && query == "" {
		fmt.Fprintln(stderr, "zill: search: provide a query, --state, or both")
		return 2
	}
	project, _, err := corpus.LoadProject(root)
	if err != nil {
		fmt.Fprintf(stderr, "zill: search: %v\n", err)
		return 1
	}
	matches := 0
	for _, item := range project.Items {
		if state != "" && string(item.Translation.State) != state {
			continue
		}
		haystack := strings.ToLower(
			strconv.Itoa(item.Record.ID) + "\n" + item.Record.Display + "\n" + item.Translation.Text,
		)
		if query != "" && !strings.Contains(haystack, query) {
			continue
		}
		fmt.Fprintf(stdout, "Message %d [%s]\n  Japanese: %s\n  English: %s\n  Editable: %s\n", item.Record.ID, item.Translation.State, snippet(item.Record.Display, 140), snippet(item.Translation.Text, 140), editablePath(item.Record.ID))
		matches++
	}
	if state == "" && query != "" {
		terms, err := loadTerminology(root)
		if err != nil {
			fmt.Fprintf(stderr, "zill: search: %v\n", err)
			return 1
		}
		for _, match := range terms.Search(query) {
			fmt.Fprintf(stdout, "Terminology [%s]\n  Japanese: %s\n  English: %s\n", match.Kind, match.Term.Japanese, match.Term.English)
			matches++
		}
	}
	if matches == 0 {
		fmt.Fprintln(stderr, "zill: search: no matching records")
		return 1
	}
	return 0
}

func loadTerminology(root string) (fixeddata.Terminology, error) {
	names, err := os.ReadFile(filepath.Join(root, "translations", "terminology", "names.toml"))
	if err != nil {
		return fixeddata.Terminology{}, fmt.Errorf("read names terminology: %w", err)
	}
	glossary, err := os.ReadFile(filepath.Join(root, "translations", "terminology", "glossary.toml"))
	if err != nil {
		return fixeddata.Terminology{}, fmt.Errorf("read glossary terminology: %w", err)
	}
	return fixeddata.ParseTerminology(names, glossary)
}

func editablePath(id int) string {
	return filepath.ToSlash(filepath.Join("translations", "messages", fmt.Sprintf("msgsec%03d.toml", id/10_000)))
}

func snippet(text string, maximum int) string {
	text = strings.ReplaceAll(text, "\n", " ")
	if utf8.RuneCountInString(text) <= maximum {
		return text
	}
	runes := []rune(text)
	return string(runes[:maximum-1]) + "…"
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
