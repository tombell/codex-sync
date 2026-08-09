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
	args, options, err := parseGlobalArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "codex-sync: %v\n", err)
		return 1
	}
	if len(args) > 0 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Fprintf(stdout, "codex-sync %s (%s)\n", Version, Commit)
		return 0
	}
	if options.SourceCodexHome != "" && (len(args) == 0 || (args[0] != "pull" && args[0] != "status")) {
		fmt.Fprintln(stderr, "codex-sync: --source-codex-home is only valid with pull or status")
		return 1
	}

	layout, err := settingssync.LiveLayout(options.CodexHome)
	if err != nil {
		fmt.Fprintf(stderr, "codex-sync: %v\n", err)
		return 1
	}
	if options.AppPath != "" {
		layout.AppPath = options.AppPath
	}
	runner := settingssync.NewRunner(layout, stdout, stderr, Version)
	runner.SourceCodexHome = options.SourceCodexHome
	return runner.Run(args)
}

type globalOptions struct {
	AppPath         string
	CodexHome       string
	SourceCodexHome string
}

func parseGlobalArgs(args []string) ([]string, globalOptions, error) {
	remaining := make([]string, 0, len(args))
	options := globalOptions{}
	pathOptions := []struct {
		name  string
		value *string
	}{
		{"--app-path", &options.AppPath},
		{"--codex-home", &options.CodexHome},
		{"--source-codex-home", &options.SourceCodexHome},
	}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		matched := false
		for _, option := range pathOptions {
			value := ""
			switch {
			case argument == option.name:
				index++
				if index == len(args) {
					return nil, globalOptions{}, fmt.Errorf("%s requires an absolute path", option.name)
				}
				value = args[index]
			case strings.HasPrefix(argument, option.name+"="):
				value = strings.TrimPrefix(argument, option.name+"=")
			default:
				continue
			}
			matched = true
			if *option.value != "" {
				return nil, globalOptions{}, fmt.Errorf("%s may only be specified once", option.name)
			}
			if !filepath.IsAbs(value) {
				return nil, globalOptions{}, fmt.Errorf("%s requires an absolute path", option.name)
			}
			*option.value = filepath.Clean(value)
			break
		}
		if !matched {
			remaining = append(remaining, argument)
		}
	}
	return remaining, options, nil
}
