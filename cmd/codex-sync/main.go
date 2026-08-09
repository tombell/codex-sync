package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tombell/codex-sync/internal/settingssync"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Fprintf(stdout, "codex-sync %s (%s)\n", Version, Commit)
		return 0
	}

	layout, err := settingssync.LiveLayout()
	if err != nil {
		fmt.Fprintf(stderr, "codex-sync: %v\n", err)
		return 1
	}
	runner := settingssync.NewRunner(layout, stdout, stderr, Version)
	return runner.Run(args)
}
