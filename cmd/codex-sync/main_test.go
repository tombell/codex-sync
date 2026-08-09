package main

import (
	"bytes"
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
