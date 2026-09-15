package settingssync

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSourceAppPathIsIndependent(t *testing.T) {
	runner := NewRunner(Layout{AppPath: "/Applications/ChatGPT.app"}, nil, nil, testToolVersion)
	command := sshExportCommand(context.Background(), "linux-host", "alice", runner.remoteExportOptions())
	if strings.Contains(command.Args[len(command.Args)-1], "--app-path") {
		t.Fatal("local app path leaked into remote export")
	}
	runner.SourceAppPath = "/opt/chatgpt"
	if got := runner.remoteExportOptions().AppPath; got != runner.SourceAppPath {
		t.Fatalf("source path = %q", got)
	}
}

func TestCrossPlatformPullPreservesLocalPreferencesAndRollsBack(t *testing.T) {
	env := newFixtureEnvironment(t)
	before := snapshot(t, env.target)
	bundle, err := buildBundle(env.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manifest.SourcePlatform == "darwin" {
		bundle.Manifest.SourcePlatform = "linux"
	} else {
		bundle.Manifest.SourcePlatform = "darwin"
	}
	local, err := buildContent(env.target, false)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	runner := NewRunner(env.target, &output, &output, testToolVersion)
	runner.SSHUser = "alice"
	runner.Fetch = func(_, _ string, _ remoteExportOptions) (Bundle, error) { return bundle, nil }
	runner.AppRunning = func(string) (bool, error) { return false, nil }
	if code := runner.Run([]string{"pull", "source", "--dry-run"}); code != 0 {
		t.Fatalf("dry run: %s", &output)
	}
	if !reflect.DeepEqual(before, snapshot(t, env.target)) {
		t.Fatal("dry run wrote settings")
	}
	if code := runner.Run([]string{"pull", "source"}); code != 0 {
		t.Fatalf("pull: %s", &output)
	}
	after, err := buildContent(env.target, false)
	if err != nil {
		t.Fatal(err)
	}
	if after.Preferences.ConfigToml["model"].Value != "fixture-model" {
		t.Fatal("shared model was not synced")
	}
	if !reflect.DeepEqual(after.Preferences.Keybindings, local.Preferences.Keybindings) {
		t.Fatal("cross-platform pull changed shortcuts")
	}
	for _, key := range []string{"desktop.dock-icon-preference", "desktop.open-in-target-preferences.global", "desktop.appearanceDarkChromeTheme.fonts.code"} {
		if !reflect.DeepEqual(after.Preferences.ConfigToml[key], local.Preferences.ConfigToml[key]) {
			t.Fatalf("changed local %s", key)
		}
	}
	if code := runner.Run([]string{"status", "source"}); code != 0 {
		t.Fatalf("status after pull: %s", &output)
	}
	if code := runner.Run([]string{"rollback"}); code != 0 {
		t.Fatalf("rollback: %s", &output)
	}
	if !reflect.DeepEqual(before, snapshot(t, env.target)) {
		t.Fatal("rollback did not restore original files")
	}
}

func TestPlatformFilteringIncludesTargetOnlyProfiles(t *testing.T) {
	for _, pair := range [][2]string{{"darwin", "linux"}, {"linux", "darwin"}, {"linux", "linux"}, {"darwin", "darwin"}} {
		t.Run(strings.Join(pair[:], "-"), func(t *testing.T) {
			incoming := preferenceEntries(configSpecs, map[string]any{"model": "source", "desktop.dock-icon-preference": "codex-system", "desktop.appearanceDarkChromeTheme.fonts.code": "source-font"})
			local := preferenceEntries(configSpecs, map[string]any{"model": "target", "desktop.dock-icon-preference": "app-default", "desktop.appearanceDarkChromeTheme.fonts.code": "target-font"})
			proposed := Preferences{ConfigToml: incoming, ConfigProfiles: map[string]map[string]Entry{"shared.config.toml": incoming, "source.config.toml": incoming}}
			current := Preferences{ConfigToml: local, ConfigProfiles: map[string]map[string]Entry{"shared.config.toml": local, "target.config.toml": local}}
			got := preferencesForTarget(proposed, current, pair[0], pair[1])
			if got.ConfigToml["model"].Value != "source" || got.ConfigProfiles["target.config.toml"]["model"].Present {
				t.Fatal("shared preferences were not synced/cleared")
			}
			for _, key := range []string{"desktop.dock-icon-preference", "desktop.appearanceDarkChromeTheme.fonts.code"} {
				preserve := key == "desktop.dock-icon-preference" && (pair[0] == "linux" || pair[1] == "linux") || key != "desktop.dock-icon-preference" && pair[0] != pair[1]
				want := incoming[key]
				if preserve {
					want = local[key]
				}
				if got.ConfigToml[key] != want || got.ConfigProfiles["shared.config.toml"][key] != want {
					t.Fatalf("wrong %s value", key)
				}
				if preserve && got.ConfigProfiles["target.config.toml"][key] != local[key] {
					t.Fatalf("cleared target-only profile %s", key)
				}
				if preserve && got.ConfigProfiles["source.config.toml"][key].Present {
					t.Fatalf("imported platform setting %s into a new profile", key)
				}
			}
			if incoming["model"].Value != "source" || proposed.ConfigProfiles["target.config.toml"] != nil {
				t.Fatal("modified source bundle")
			}
		})
	}
}

func TestBundleRejectsUnknownPlatformAndDifferentRelease(t *testing.T) {
	env := newFixtureEnvironment(t)
	bundle, err := buildBundle(env.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	app, err := getAppInfo(env.target)
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"", "windows"} {
		bad := bundle
		bad.Manifest.SourcePlatform = platform
		if _, err := validateBundle(bad, app, testToolVersion); err == nil {
			t.Fatal("accepted invalid platform")
		}
	}
	for _, change := range []func(*Bundle){func(b *Bundle) { b.Manifest.AppVersion += ".1" }, func(b *Bundle) { b.Manifest.AppBuild += "1" }} {
		bad := bundle
		change(&bad)
		if _, err := validateBundle(bad, app, testToolVersion); err == nil || !strings.Contains(err.Error(), "version mismatch") {
			t.Fatalf("release mismatch: %v", err)
		}
	}
}

func TestThemeAccentSourceRoundTrip(t *testing.T) {
	env := newFixtureEnvironment(t)
	if err := os.WriteFile(env.source.Config(), []byte("[desktop.appearanceDarkChromeTheme]\naccentSource = \"chatgpt\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, err := buildBundle(env.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Content.Audit.UnknownConfigPaths) != 0 {
		t.Fatalf("unknown: %v", bundle.Content.Audit.UnknownConfigPaths)
	}
	if _, err := applyPreferences(env.target, bundle.Content.Preferences, 0); err != nil {
		t.Fatal(err)
	}
	after, err := buildContent(env.target, false)
	if err != nil {
		t.Fatal(err)
	}
	if after.Preferences.ConfigToml["desktop.appearanceDarkChromeTheme.accentSource"].Value != "chatgpt" {
		t.Fatal("lost accent source")
	}
}
