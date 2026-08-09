package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigurationMergesAllOptions(t *testing.T) {
	path := writeTestConfig(t, `
app_path = "/Applications/ChatGPT Preview.app"
codex_home = "/Volumes/config/codex"
state_home = "/Volumes/config/state"
ssh_user = "alice"
source_codex_home = "/Volumes/source/codex"
source_binary = "/opt/homebrew/bin/codex-sync"
source_shell = "/bin/zsh"
ssh_connect_timeout = "25s"
export_timeout = "2m"
`)
	options, err := loadConfiguration(globalOptions{
		ConfigPath:    path,
		CodexHome:     "/Volumes/cli/codex",
		ExportTimeout: 3 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := options.AppPath, "/Applications/ChatGPT Preview.app"; got != want {
		t.Fatalf("app path = %q, want %q", got, want)
	}
	if got, want := options.CodexHome, "/Volumes/cli/codex"; got != want {
		t.Fatalf("Codex home = %q, want CLI value %q", got, want)
	}
	if got, want := options.StateHome, "/Volumes/config/state"; got != want {
		t.Fatalf("state home = %q, want %q", got, want)
	}
	if got, want := options.SSHUser, "alice"; got != want {
		t.Fatalf("SSH user = %q, want %q", got, want)
	}
	if got, want := options.SourceCodexHome, "/Volumes/source/codex"; got != want {
		t.Fatalf("source Codex home = %q, want %q", got, want)
	}
	if got, want := options.SourceBinary, "/opt/homebrew/bin/codex-sync"; got != want {
		t.Fatalf("source binary = %q, want %q", got, want)
	}
	if got, want := options.SourceShell, "/bin/zsh"; got != want {
		t.Fatalf("source shell = %q, want %q", got, want)
	}
	if got, want := options.SSHConnectTimeout, 25*time.Second; got != want {
		t.Fatalf("SSH timeout = %s, want %s", got, want)
	}
	if got, want := options.ExportTimeout, 3*time.Minute; got != want {
		t.Fatalf("export timeout = %s, want CLI value %s", got, want)
	}
}

func TestLoadConfigurationUsesXDGDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	path := filepath.Join(root, "codex-sync", configFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("ssh_user = \"xdg-user\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options, err := loadConfiguration(globalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := options.SSHUser, "xdg-user"; got != want {
		t.Fatalf("SSH user = %q, want %q", got, want)
	}
}

func TestLoadConfigurationAllowsMissingImplicitFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	options, err := loadConfiguration(globalOptions{CodexHome: "/Volumes/codex"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := options.CodexHome, "/Volumes/codex"; got != want {
		t.Fatalf("Codex home = %q, want %q", got, want)
	}
}

func TestLoadConfigurationRejectsMissingExplicitFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")
	if _, err := loadConfiguration(globalOptions{ConfigPath: path}); err == nil || !strings.Contains(err.Error(), "load config") {
		t.Fatalf("missing config error = %v", err)
	}
}

func TestLoadConfigurationRejectsUnknownKeys(t *testing.T) {
	path := writeTestConfig(t, "unknown_option = true\nsource = { host = \"mac\" }\n")
	if _, err := loadConfiguration(globalOptions{ConfigPath: path}); err == nil || !strings.Contains(err.Error(), "unknown keys: source, source.host, unknown_option") {
		t.Fatalf("unknown key error = %v", err)
	}
}

func TestLoadConfigurationRejectsInvalidValues(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		message string
	}{
		{"relative path", "app_path = \"Applications/ChatGPT.app\"\n", "absolute path"},
		{"empty path", "state_home = \"\"\n", "absolute path"},
		{"invalid user", "ssh_user = \"alice@example.com\"\n", "unsupported characters"},
		{"empty user", "ssh_user = \"\"\n", "must not be empty"},
		{"fractional timeout", "ssh_connect_timeout = \"500ms\"\n", "whole-second"},
		{"long timeout", "export_timeout = \"2h\"\n", "whole-second"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeTestConfig(t, test.content)
			if _, err := loadConfiguration(globalOptions{ConfigPath: path}); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("config error = %v, want text %q", err, test.message)
			}
		})
	}
}

func TestLoadConfigurationRejectsMalformedTOML(t *testing.T) {
	path := writeTestConfig(t, "app_path = [\n")
	if _, err := loadConfiguration(globalOptions{ConfigPath: path}); err == nil || !strings.Contains(err.Error(), "load config") {
		t.Fatalf("malformed config error = %v", err)
	}
}

func TestNoConfigSkipsDefaultFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	path := filepath.Join(root, "codex-sync", configFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("malformed = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfiguration(globalOptions{NoConfig: true}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveConfigPathRejectsRelativeXDGRoot(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "relative/config")
	if _, _, err := resolveConfigPath(""); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative XDG config error = %v", err)
	}
}

func TestConfiguredRemoteOptionsDoNotRestrictLocalCommands(t *testing.T) {
	path := writeTestConfig(t, "source_binary = \"/opt/homebrew/bin/codex-sync\"\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"unknown", "--config", path}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), `unknown command "unknown"`) {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "remote options are only valid") {
		t.Fatalf("configured remote option was treated as a command-line restriction: %q", stderr.String())
	}
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), configFileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
