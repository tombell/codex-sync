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
		remaining, appPath, err := parseGlobalArgs(args)
		if err != nil {
			t.Fatalf("parseGlobalArgs(%q): %v", args, err)
		}
		if want := []string{"audit"}; !reflect.DeepEqual(remaining, want) {
			t.Fatalf("parseGlobalArgs(%q) args = %q, want %q", args, remaining, want)
		}
		if want := "/Applications/ChatGPT Beta.app"; appPath != want {
			t.Fatalf("parseGlobalArgs(%q) app path = %q, want %q", args, appPath, want)
		}
	}
}

func TestParseGlobalArgsRejectsInvalidAppPath(t *testing.T) {
	for _, args := range [][]string{
		{"audit", "--app-path"},
		{"audit", "--app-path", "ChatGPT.app"},
		{"audit", "--app-path=/Applications/ChatGPT.app", "--app-path", "/Applications/Other.app"},
	} {
		if _, _, err := parseGlobalArgs(args); err == nil || !strings.Contains(err.Error(), "--app-path") {
			t.Fatalf("parseGlobalArgs(%q) error = %v", args, err)
		}
	}
}
