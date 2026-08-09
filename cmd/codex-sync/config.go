package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const configFileName = "config.toml"

type fileConfig struct {
	AppPath           string `toml:"app_path"`
	CodexHome         string `toml:"codex_home"`
	StateHome         string `toml:"state_home"`
	SSHUser           string `toml:"ssh_user"`
	SourceCodexHome   string `toml:"source_codex_home"`
	SourceBinary      string `toml:"source_binary"`
	SourceShell       string `toml:"source_shell"`
	SSHConnectTimeout string `toml:"ssh_connect_timeout"`
	ExportTimeout     string `toml:"export_timeout"`
}

func loadConfiguration(options globalOptions) (globalOptions, error) {
	if options.NoConfig {
		return options, nil
	}
	path, optional, err := resolveConfigPath(options.ConfigPath)
	if err != nil {
		return globalOptions{}, err
	}
	var config fileConfig
	metadata, err := toml.DecodeFile(path, &config)
	if err != nil {
		if optional && os.IsNotExist(err) {
			return options, nil
		}
		return globalOptions{}, fmt.Errorf("load config %s: %w", path, err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) != 0 {
		keys := make([]string, len(undecoded))
		for index, key := range undecoded {
			keys[index] = key.String()
		}
		sort.Strings(keys)
		return globalOptions{}, fmt.Errorf("load config %s: unknown keys: %s", path, strings.Join(keys, ", "))
	}
	configured, err := config.options(metadata)
	if err != nil {
		return globalOptions{}, fmt.Errorf("load config %s: %w", path, err)
	}
	return mergeOptions(options, configured), nil
}

func resolveConfigPath(explicit string) (string, bool, error) {
	if explicit != "" {
		return explicit, false, nil
	}
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false, fmt.Errorf("find home directory: %w", err)
		}
		root = filepath.Join(home, ".config")
	} else if !filepath.IsAbs(root) {
		return "", false, fmt.Errorf("XDG_CONFIG_HOME must be an absolute path")
	}
	return filepath.Join(filepath.Clean(root), "codex-sync", configFileName), true, nil
}

func (config fileConfig) options(metadata toml.MetaData) (globalOptions, error) {
	options := globalOptions{}
	paths := []struct {
		key    string
		value  string
		target *string
	}{
		{"app_path", config.AppPath, &options.AppPath},
		{"codex_home", config.CodexHome, &options.CodexHome},
		{"state_home", config.StateHome, &options.StateHome},
		{"source_codex_home", config.SourceCodexHome, &options.SourceCodexHome},
		{"source_binary", config.SourceBinary, &options.SourceBinary},
		{"source_shell", config.SourceShell, &options.SourceShell},
	}
	for _, path := range paths {
		if !metadata.IsDefined(path.key) {
			continue
		}
		if !filepath.IsAbs(path.value) {
			return globalOptions{}, fmt.Errorf("%s requires an absolute path", path.key)
		}
		*path.target = filepath.Clean(path.value)
	}
	if metadata.IsDefined("ssh_user") {
		if err := validateSSHUser(config.SSHUser); err != nil {
			return globalOptions{}, fmt.Errorf("ssh_user %w", err)
		}
		options.SSHUser = config.SSHUser
	}
	durations := []struct {
		key    string
		value  string
		target *time.Duration
	}{
		{"ssh_connect_timeout", config.SSHConnectTimeout, &options.SSHConnectTimeout},
		{"export_timeout", config.ExportTimeout, &options.ExportTimeout},
	}
	for _, duration := range durations {
		if !metadata.IsDefined(duration.key) {
			continue
		}
		parsed, err := parseDuration(duration.value)
		if err != nil {
			return globalOptions{}, fmt.Errorf("%s %w", duration.key, err)
		}
		*duration.target = parsed
	}
	return options, nil
}

func validateSSHUser(user string) error {
	if user == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.HasPrefix(user, "-") || strings.ContainsAny(user, "@ \t\x00\r\n") {
		return fmt.Errorf("contains unsupported characters")
	}
	return nil
}

func parseDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Second || duration > time.Hour || duration%time.Second != 0 {
		return 0, fmt.Errorf("requires a whole-second duration from 1s to 1h")
	}
	return duration, nil
}

func mergeOptions(commandLine, configured globalOptions) globalOptions {
	if commandLine.AppPath == "" {
		commandLine.AppPath = configured.AppPath
	}
	if commandLine.CodexHome == "" {
		commandLine.CodexHome = configured.CodexHome
	}
	if commandLine.StateHome == "" {
		commandLine.StateHome = configured.StateHome
	}
	if commandLine.SSHUser == "" {
		commandLine.SSHUser = configured.SSHUser
	}
	if commandLine.SourceCodexHome == "" {
		commandLine.SourceCodexHome = configured.SourceCodexHome
	}
	if commandLine.SourceBinary == "" {
		commandLine.SourceBinary = configured.SourceBinary
	}
	if commandLine.SourceShell == "" {
		commandLine.SourceShell = configured.SourceShell
	}
	if commandLine.SSHConnectTimeout == 0 {
		commandLine.SSHConnectTimeout = configured.SSHConnectTimeout
	}
	if commandLine.ExportTimeout == 0 {
		commandLine.ExportTimeout = configured.ExportTimeout
	}
	return commandLine
}
