package settingssync

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSSHExportCommandUsesRemoteLoginPath(t *testing.T) {
	command := sshExportCommand(context.Background(), "pyra")
	want := []string{
		"ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "pyra",
		`exec "$SHELL" -lc 'exec codex-sync export'`,
	}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("SSH command = %#v, want %#v", command.Args, want)
	}
	if strings.Contains(strings.Join(command.Args, " "), "/Users/") {
		t.Fatalf("SSH command contains a hardcoded user path: %#v", command.Args)
	}
}

type fixtureEnvironment struct {
	source Layout
	target Layout
}

func newFixtureEnvironment(t *testing.T) fixtureEnvironment {
	t.Helper()
	root := t.TempDir()
	app := filepath.Join(root, "ChatGPT.app")
	copyTree(t, filepath.Join("testdata", "ChatGPT.app"), app)
	sourceHome := filepath.Join(root, "source-home")
	targetHome := filepath.Join(root, "target-home")
	copyTree(t, filepath.Join("testdata", "source-home"), sourceHome)
	copyTree(t, filepath.Join("testdata", "target-home"), targetHome)
	return fixtureEnvironment{
		source: Layout{Home: sourceHome, AppPath: app},
		target: Layout{Home: targetHome, AppPath: app},
	}
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
}

func snapshot(t *testing.T, layout Layout) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	for name, path := range targetPaths(layout) {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			result[name] = nil
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		result[name] = data
	}
	return result
}

func proposed(t *testing.T, environment fixtureEnvironment) Preferences {
	t.Helper()
	bundle, err := buildBundle(environment.source)
	if err != nil {
		t.Fatal(err)
	}
	app, err := getAppInfo(environment.target)
	if err != nil {
		t.Fatal(err)
	}
	content, err := validateBundle(bundle, app)
	if err != nil {
		t.Fatal(err)
	}
	return content.Preferences
}

func TestExportContainsOnlyAllowlistedPreferences(t *testing.T) {
	environment := newFixtureEnvironment(t)
	bundle, err := buildBundle(environment.source)
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := canonicalJSON(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, decoy := range []string{
		"DECOY_AUTH_CONFIG_VALUE", "DECOY_UNKNOWN_SETTING_VALUE",
		"DECOY_PROMPT_HISTORY_VALUE", "DECOY_AUTH_GLOBAL_VALUE", "DECOY_SESSION_VALUE",
		"DECOY_HISTORY_VALUE", "DECOY_INSTALLATION_ID_VALUE", "DECOY_THREAD_VALUE",
		"/fixture/private-project",
	} {
		if bytes.Contains(serialized, []byte(decoy)) {
			t.Errorf("export leaked decoy %q", decoy)
		}
	}
	if !contains(bundle.Content.Audit.UnknownConfigPaths, "desktop.futurePreference") {
		t.Fatalf("unknown preference was not audited: %#v", bundle.Content.Audit.UnknownConfigPaths)
	}
	personality := bundle.Content.Preferences.ConfigToml["personality"]
	if personality.Value != "pragmatic" {
		t.Fatalf("unexpected personality: %#v", personality)
	}
	model := bundle.Content.Preferences.ConfigToml["model"]
	if model.Value != "fixture-model" {
		t.Fatalf("unexpected model: %#v", model)
	}
	reasoning := bundle.Content.Preferences.ConfigToml["model_reasoning_effort"]
	if reasoning.Value != "high" {
		t.Fatalf("unexpected model reasoning effort: %#v", reasoning)
	}
	webSearch := bundle.Content.Preferences.ConfigToml["web_search"]
	if webSearch.Value != "live" {
		t.Fatalf("unexpected web search mode: %#v", webSearch)
	}
	agentsEnabled := bundle.Content.Preferences.ConfigToml["agents.enabled"]
	if agentsEnabled.Value != true {
		t.Fatalf("unexpected agents enabled preference: %#v", agentsEnabled)
	}
	subagentModel := bundle.Content.Preferences.ConfigToml["agents.default_subagent_model"]
	if subagentModel.Value != "fixture-subagent" {
		t.Fatalf("unexpected default subagent model: %#v", subagentModel)
	}
	subagentReasoning := bundle.Content.Preferences.ConfigToml["agents.default_subagent_reasoning_effort"]
	if subagentReasoning.Value != "xhigh" {
		t.Fatalf("unexpected default subagent reasoning effort: %#v", subagentReasoning)
	}
	mergeMethod := bundle.Content.Preferences.ConfigToml["desktop.git-pull-request-merge-method"]
	if mergeMethod.Value != "squash" {
		t.Fatalf("unexpected pull request merge method: %#v", mergeMethod)
	}
	commitInstructions := bundle.Content.Preferences.ConfigToml["desktop.git-commit-instructions"]
	if commitInstructions.Value != "Use fixture commit messages." {
		t.Fatalf("unexpected git commit instructions: %#v", commitInstructions)
	}
	conversationDetail := bundle.Content.Preferences.ConfigToml["desktop.conversationDetailMode"]
	if conversationDetail.Value != "STEPS_COMMANDS" {
		t.Fatalf("unexpected conversation detail mode: %#v", conversationDetail)
	}
	openTarget := bundle.Content.Preferences.ConfigToml["desktop.open-in-target-preferences.global"]
	if openTarget.Value != "fileManager" {
		t.Fatalf("unexpected global open target: %#v", openTarget)
	}
}

func TestBundleWithoutKeybindingsHasValidAuditSchema(t *testing.T) {
	environment := newFixtureEnvironment(t)
	if err := os.Remove(environment.source.Keybindings()); err != nil {
		t.Fatal(err)
	}
	bundle, err := buildBundle(environment.source)
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := canonicalJSON(bundle)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeBundle(serialized)
	if err != nil {
		t.Fatal(err)
	}
	app, err := getAppInfo(environment.target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateBundle(decoded, app); err != nil {
		t.Fatalf("bundle without keybindings failed validation: %v", err)
	}
}

func TestDryRunPerformsNoSettingsWrites(t *testing.T) {
	environment := newFixtureEnvironment(t)
	before := snapshot(t, environment.target)
	bundle, err := buildBundle(environment.source)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	runner := NewRunner(environment.target, &stdout, &stderr)
	runner.Fetch = func(string) (Bundle, error) { return bundle, nil }
	runner.AppRunning = func() bool { return true }
	if code := runner.Run([]string{"pull", "pyra", "--dry-run"}); code != 0 {
		t.Fatalf("dry run failed: code=%d stderr=%s", code, stderr.String())
	}
	if !reflect.DeepEqual(before, snapshot(t, environment.target)) {
		t.Fatal("dry run changed settings")
	}
	if _, err := os.Stat(filepath.Dir(environment.target.Backups())); !os.IsNotExist(err) {
		t.Fatal("dry run created local backup state")
	}
}

func TestUnknownBundleKeyAndKeybindingAreRejected(t *testing.T) {
	environment := newFixtureEnvironment(t)
	bundle, err := buildBundle(environment.source)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Content.Preferences.ConfigToml["desktop.future"] = Entry{Present: true, Value: true}
	content, _ := canonicalJSON(bundle.Content)
	bundle.Manifest.ContentSHA256 = sha256Bytes(content)
	app, _ := getAppInfo(environment.target)
	if _, err := validateBundle(bundle, app); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown bundle key was accepted: %v", err)
	}

	data := []byte(`[{"command":"futureUnknownCommand","key":"Command+X"}]`)
	if _, _, err := validateKeybindings(data, true); err == nil {
		t.Fatal("unknown keybinding command was accepted")
	}
}

func TestUnknownLocalKeybindingBlocksNormalPull(t *testing.T) {
	environment := newFixtureEnvironment(t)
	if err := os.WriteFile(environment.target.Keybindings(), []byte("[{\"command\":\"futureUnknownCommand\",\"key\":\"Command+X\"}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, environment.target)
	bundle, err := buildBundle(environment.source)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	runner := NewRunner(environment.target, &stdout, &stderr)
	runner.Fetch = func(string) (Bundle, error) { return bundle, nil }
	runner.AppRunning = func() bool { return false }
	if code := runner.Run([]string{"pull", "pyra"}); code == 0 || !strings.Contains(stderr.String(), "unknown commands") {
		t.Fatalf("unknown local command did not block pull: code=%d stderr=%s", code, stderr.String())
	}
	if !reflect.DeepEqual(before, snapshot(t, environment.target)) {
		t.Fatal("blocked pull changed settings")
	}
}

func TestBackupAndRollbackRestoreOriginalFiles(t *testing.T) {
	environment := newFixtureEnvironment(t)
	before := snapshot(t, environment.target)
	backup, err := applyPreferences(environment.target, proposed(t, environment), 0)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before, snapshot(t, environment.target)) {
		t.Fatal("apply did not change settings")
	}
	if !fileExists(filepath.Join(backup, "applied-at")) {
		t.Fatal("backup was not marked complete")
	}
	restored, err := rollback(environment.target)
	if err != nil {
		t.Fatal(err)
	}
	if restored != backup || !reflect.DeepEqual(before, snapshot(t, environment.target)) {
		t.Fatal("rollback did not restore original files")
	}
}

func TestInterruptedApplyRestoresEveryTarget(t *testing.T) {
	environment := newFixtureEnvironment(t)
	before := snapshot(t, environment.target)
	if _, err := applyPreferences(environment.target, proposed(t, environment), 1); err == nil || !strings.Contains(err.Error(), "simulated interrupted apply") {
		t.Fatalf("interrupted apply unexpectedly succeeded: %v", err)
	}
	if !reflect.DeepEqual(before, snapshot(t, environment.target)) {
		t.Fatal("interrupted apply left partial settings")
	}
}

func TestLocalRoundTripPreservesAllowedPreferencesOnly(t *testing.T) {
	environment := newFixtureEnvironment(t)
	want := proposed(t, environment)
	if _, err := applyPreferences(environment.target, want, 0); err != nil {
		t.Fatal(err)
	}
	after, err := buildContent(environment.target, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Preferences, want) {
		encodedWant, _ := json.Marshal(want)
		encodedAfter, _ := json.Marshal(after.Preferences)
		t.Fatalf("round trip mismatch\nwant: %s\nafter: %s", encodedWant, encodedAfter)
	}
	config, _ := os.ReadFile(environment.target.Config())
	global, _ := os.ReadFile(environment.target.GlobalState())
	for _, decoy := range []string{"TARGET_AUTH_MUST_STAY", "TARGET_HISTORY_MUST_STAY"} {
		if !bytes.Contains(config, []byte(decoy)) {
			t.Errorf("target config did not preserve %s", decoy)
		}
	}
	for _, decoy := range []string{"TARGET_AUTH_GLOBAL_MUST_STAY", "TARGET_DEVICE_ID_MUST_STAY"} {
		if !bytes.Contains(global, []byte(decoy)) {
			t.Errorf("target global state did not preserve %s", decoy)
		}
	}
}
