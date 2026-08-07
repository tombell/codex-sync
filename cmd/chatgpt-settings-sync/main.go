package main

import (
	"os"

	"github.com/tombell/codex-sync/internal/settingssync"
)

func main() {
	layout, err := settingssync.LiveLayout()
	if err != nil {
		_, _ = os.Stderr.WriteString("chatgpt-settings-sync: " + err.Error() + "\n")
		os.Exit(1)
	}
	runner := settingssync.NewRunner(layout, os.Stdout, os.Stderr)
	os.Exit(runner.Run(os.Args[1:]))
}
