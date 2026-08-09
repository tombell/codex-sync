package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tombell/codex-sync/internal/settingssync"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	args, appPath, err := parseGlobalArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "codex-sync: %v\n", err)
		return 1
	}
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Fprintf(stdout, "codex-sync %s (%s)\n", Version, Commit)
		return 0
	}

	layout, err := settingssync.LiveLayout()
	if err != nil {
		fmt.Fprintf(stderr, "codex-sync: %v\n", err)
		return 1
	}
	if appPath != "" {
		layout.AppPath = appPath
	}
	runner := settingssync.NewRunner(layout, stdout, stderr, Version)
	return runner.Run(args)
}

func parseGlobalArgs(args []string) ([]string, string, error) {
	remaining := make([]string, 0, len(args))
	appPath := ""
	for index := 0; index < len(args); index++ {
		argument := args[index]
		value := ""
		switch {
		case argument == "--app-path":
			index++
			if index == len(args) {
				return nil, "", fmt.Errorf("--app-path requires an absolute path")
			}
			value = args[index]
		case strings.HasPrefix(argument, "--app-path="):
			value = strings.TrimPrefix(argument, "--app-path=")
		default:
			remaining = append(remaining, argument)
			continue
		}
		if appPath != "" {
			return nil, "", fmt.Errorf("--app-path may only be specified once")
		}
		if !filepath.IsAbs(value) {
			return nil, "", fmt.Errorf("--app-path requires an absolute path")
		}
		appPath = filepath.Clean(value)
	}
	return remaining, appPath, nil
}
