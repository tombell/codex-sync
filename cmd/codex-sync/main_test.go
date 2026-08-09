package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	oldVersion, oldCommit := Version, Commit
	Version, Commit = "1.2.3", "abc12345"
	t.Cleanup(func() { Version, Commit = oldVersion, oldCommit })

	for _, flag := range []string{"--version", "-v"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{flag}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(%q) code = %d, stderr = %q", flag, code, stderr.String())
		}
		if got, want := stdout.String(), "codex-sync 1.2.3 (abc12345)\n"; got != want {
			t.Fatalf("run(%q) output = %q, want %q", flag, got, want)
		}
	}
}

func TestParseGlobalArgsAcceptsAppPathAnywhere(t *testing.T) {
	for _, args := range [][]string{
		{"--app-path", "/Applications/ChatGPT Beta.app", "audit"},
		{"audit", "--app-path", "/Applications/ChatGPT Beta.app"},
		{"audit", "--app-path=/Applications/ChatGPT Beta.app"},
	} {
		remaining, options, err := parseGlobalArgs(args)
		if err != nil {
			t.Fatalf("parseGlobalArgs(%q): %v", args, err)
		}
		if want := []string{"audit"}; !reflect.DeepEqual(remaining, want) {
			t.Fatalf("parseGlobalArgs(%q) args = %q, want %q", args, remaining, want)
		}
		if want := "/Applications/ChatGPT Beta.app"; options.AppPath != want {
			t.Fatalf("parseGlobalArgs(%q) app path = %q, want %q", args, options.AppPath, want)
		}
	}
}

func TestParseGlobalArgsAcceptsSourceOverrides(t *testing.T) {
	args := []string{
		"pull", "source-mac",
		"--codex-home", "/Users/local/.codex-beta",
		"--source-codex-home=/Users/remote/.codex-preview",
		"--source-binary", "/opt/homebrew/bin/codex-sync",
		"--source-shell=/bin/zsh",
	}
	remaining, options, err := parseGlobalArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pull", "source-mac"}; !reflect.DeepEqual(remaining, want) {
		t.Fatalf("remaining args = %q, want %q", remaining, want)
	}
	if want := "/Users/local/.codex-beta"; options.CodexHome != want {
		t.Fatalf("Codex home = %q, want %q", options.CodexHome, want)
	}
	if want := "/Users/remote/.codex-preview"; options.SourceCodexHome != want {
		t.Fatalf("source Codex home = %q, want %q", options.SourceCodexHome, want)
	}
	if want := "/opt/homebrew/bin/codex-sync"; options.SourceBinary != want {
		t.Fatalf("source binary = %q, want %q", options.SourceBinary, want)
	}
	if want := "/bin/zsh"; options.SourceShell != want {
		t.Fatalf("source shell = %q, want %q", options.SourceShell, want)
	}
}

func TestParseGlobalArgsRejectsInvalidPaths(t *testing.T) {
	for _, args := range [][]string{
		{"audit", "--app-path"},
		{"audit", "--app-path", "ChatGPT.app"},
		{"audit", "--app-path=/Applications/ChatGPT.app", "--app-path", "/Applications/Other.app"},
		{"audit", "--codex-home", ".codex-beta"},
		{"pull", "source-mac", "--source-codex-home"},
		{"pull", "source-mac", "--source-binary", "bin/codex-sync"},
		{"pull", "source-mac", "--source-shell"},
	} {
		if _, _, err := parseGlobalArgs(args); err == nil || !strings.Contains(err.Error(), "--") {
			t.Fatalf("parseGlobalArgs(%q) error = %v", args, err)
		}
	}
}

func TestRunRejectsSourceOverridesForLocalCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"audit", "--source-binary", "/opt/homebrew/bin/codex-sync"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "only valid with pull or status") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}
