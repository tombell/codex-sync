package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if options.hasRemoteOptions() && (len(args) == 0 || (args[0] != "pull" && args[0] != "status")) {
		fmt.Fprintln(stderr, "codex-sync: remote options are only valid with pull or status")
		return 1
	}

	layout, err := settingssync.LiveLayout(options.CodexHome, options.StateHome)
	if err != nil {
		fmt.Fprintf(stderr, "codex-sync: %v\n", err)
		return 1
	}
	if options.AppPath != "" {
		layout.AppPath = options.AppPath
	}
	runner := settingssync.NewRunner(layout, stdout, stderr, Version)
	runner.SourceCodexHome = options.SourceCodexHome
	if options.SourceBinary != "" {
		runner.SourceBinary = options.SourceBinary
	}
	runner.SourceShell = options.SourceShell
	if options.SSHConnectTimeout != 0 {
		runner.SSHConnectTimeout = options.SSHConnectTimeout
	}
	if options.ExportTimeout != 0 {
		runner.ExportTimeout = options.ExportTimeout
	}
	return runner.Run(args)
}

type globalOptions struct {
	AppPath           string
	CodexHome         string
	StateHome         string
	SourceCodexHome   string
	SourceBinary      string
	SourceShell       string
	SSHConnectTimeout time.Duration
	ExportTimeout     time.Duration
}

func (options globalOptions) hasRemoteOptions() bool {
	return options.SourceCodexHome != "" || options.SourceBinary != "" || options.SourceShell != "" || options.SSHConnectTimeout != 0 || options.ExportTimeout != 0
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
		{"--state-home", &options.StateHome},
		{"--source-codex-home", &options.SourceCodexHome},
		{"--source-binary", &options.SourceBinary},
		{"--source-shell", &options.SourceShell},
	}
	durationOptions := []struct {
		name  string
		value *time.Duration
	}{
		{"--ssh-connect-timeout", &options.SSHConnectTimeout},
		{"--export-timeout", &options.ExportTimeout},
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
			for _, option := range durationOptions {
				value := ""
				switch {
				case argument == option.name:
					index++
					if index == len(args) {
						return nil, globalOptions{}, fmt.Errorf("%s requires a duration", option.name)
					}
					value = args[index]
				case strings.HasPrefix(argument, option.name+"="):
					value = strings.TrimPrefix(argument, option.name+"=")
				default:
					continue
				}
				matched = true
				if *option.value != 0 {
					return nil, globalOptions{}, fmt.Errorf("%s may only be specified once", option.name)
				}
				duration, err := time.ParseDuration(value)
				if err != nil || duration < time.Second || duration > time.Hour || duration%time.Second != 0 {
					return nil, globalOptions{}, fmt.Errorf("%s requires a whole-second duration from 1s to 1h", option.name)
				}
				*option.value = duration
				break
			}
		}
		if !matched {
			remaining = append(remaining, argument)
		}
	}
	return remaining, options, nil
}
