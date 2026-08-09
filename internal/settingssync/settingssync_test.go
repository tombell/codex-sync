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

const testToolVersion = "1.2.3"

func TestSSHExportCommandUsesSelectedUserAndRemoteLoginPath(t *testing.T) {
	command := sshExportCommand(context.Background(), "source-mac", "alice")
	want := []string{
		"ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-l", "alice", "source-mac",
		`exec "$SHELL" -lc 'exec codex-sync export'`,
	}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("SSH command = %#v, want %#v", command.Args, want)
	}
	if strings.Contains(strings.Join(command.Args, " "), "/Users/") {
		t.Fatalf("SSH command contains a hardcoded user path: %#v", command.Args)
	}
}

func TestNewRunnerUsesEnvironmentSSHUser(t *testing.T) {
	t.Setenv("USER", "alice")
	var output bytes.Buffer
	runner := NewRunner(Layout{}, &output, &output, testToolVersion)
	if runner.SSHUser != "alice" {
		t.Fatalf("SSH user = %q, want %q", runner.SSHUser, "alice")
	}
	if runner.Version != testToolVersion {
		t.Fatalf("CLI version = %q, want %q", runner.Version, testToolVersion)
	}
}

func TestParseRemoteArgsDefaultsToConfiguredUser(t *testing.T) {
	target, dryRun, err := parseRemoteArgs([]string{"source-mac", "--dry-run"}, "alice", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := (sshTarget{Host: "source-mac", User: "alice"}); target != want {
		t.Fatalf("target = %#v, want %#v", target, want)
	}
	if !dryRun {
		t.Fatal("dry run was not enabled")
	}
}

func TestParseRemoteArgsAllowsUserOverride(t *testing.T) {
	for _, args := range [][]string{
		{"source-mac", "--user", "bob"},
		{"--user=bob", "source-mac"},
		{"-u", "bob", "source-mac"},
	} {
		target, dryRun, err := parseRemoteArgs(args, "alice", false)
		if err != nil {
			t.Fatalf("parseRemoteArgs(%q): %v", args, err)
		}
		if want := (sshTarget{Host: "source-mac", User: "bob"}); target != want {
			t.Fatalf("parseRemoteArgs(%q) target = %#v, want %#v", args, target, want)
		}
		if dryRun {
			t.Fatalf("parseRemoteArgs(%q) unexpectedly enabled dry run", args)
		}
	}
}

func TestParseRemoteArgsRejectsMissingUserAndEmbeddedUser(t *testing.T) {
	if _, _, err := parseRemoteArgs([]string{"source-mac"}, "", false); err == nil || !strings.Contains(err.Error(), "set USER") {
		t.Fatalf("missing user error = %v", err)
	}
	if _, _, err := parseRemoteArgs([]string{"alice@source-mac"}, "alice", false); err == nil || !strings.Contains(err.Error(), "--user") {
		t.Fatalf("embedded user error = %v", err)
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
	paths, err := existingTargetPaths(layout)
	if err != nil {
		t.Fatal(err)
	}
	for name, path := range paths {
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
	bundle, err := buildBundle(environment.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	app, err := getAppInfo(environment.target)
	if err != nil {
		t.Fatal(err)
	}
	content, err := validateBundle(bundle, app, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	return content.Preferences
}

func TestExportContainsOnlyAllowlistedPreferences(t *testing.T) {
	environment := newFixtureEnvironment(t)
	bundle, err := buildBundle(environment.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := canonicalJSON(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, decoy := range []string{
		"DECOY_AUTH_CONFIG_VALUE", "DECOY_UNKNOWN_SETTING_VALUE",
		"DECOY_PROFILE_AUTH_VALUE",
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
	if dockIcon := bundle.Content.Preferences.ConfigToml["desktop.dock-icon-preference"]; dockIcon.Value != "codex-system" {
		t.Fatalf("unexpected dock icon preference: %#v", dockIcon)
	}
	if avatar := bundle.Content.Preferences.ConfigToml["desktop.selected-avatar-id"]; avatar.Value != "seedy" {
		t.Fatalf("unexpected selected avatar: %#v", avatar)
	}
	profileModel := bundle.Content.Preferences.ConfigProfiles["review.config.toml"]["model"]
	if profileModel.Value != "fixture-profile-model" {
		t.Fatalf("unexpected profile model: %#v", profileModel)
	}
	if !strings.Contains(bundle.Content.Preferences.Rules["default.rules"], `pattern = ["git", "status"]`) {
		t.Fatalf("unexpected default rules: %#v", bundle.Content.Preferences.Rules)
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

func TestBundleUsesAndValidatesCLIVersion(t *testing.T) {
	environment := newFixtureEnvironment(t)
	bundle, err := buildBundle(environment.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manifest.ToolVersion != testToolVersion {
		t.Fatalf("manifest tool version = %q, want %q", bundle.Manifest.ToolVersion, testToolVersion)
	}
	app, err := getAppInfo(environment.target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateBundle(bundle, app, testToolVersion); err != nil {
		t.Fatalf("matching CLI version was rejected: %v", err)
	}
	if _, err := validateBundle(bundle, app, "1.2.4"); err == nil || !strings.Contains(err.Error(), `source "1.2.3", local "1.2.4"`) {
		t.Fatalf("mismatched CLI version error = %v", err)
	}
}

func TestBundleWithoutKeybindingsHasValidAuditSchema(t *testing.T) {
	environment := newFixtureEnvironment(t)
	if err := os.Remove(environment.source.Keybindings()); err != nil {
		t.Fatal(err)
	}
	bundle, err := buildBundle(environment.source, testToolVersion)
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
	if _, err := validateBundle(decoded, app, testToolVersion); err != nil {
		t.Fatalf("bundle without keybindings failed validation: %v", err)
	}
}

func TestDryRunPerformsNoSettingsWrites(t *testing.T) {
	environment := newFixtureEnvironment(t)
	before := snapshot(t, environment.target)
	bundle, err := buildBundle(environment.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	runner := NewRunner(environment.target, &stdout, &stderr, testToolVersion)
	runner.Fetch = func(string, string) (Bundle, error) { return bundle, nil }
	runner.AppRunning = func() bool { return true }
	if code := runner.Run([]string{"pull", "source-mac", "--dry-run"}); code != 0 {
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
	bundle, err := buildBundle(environment.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Content.Preferences.ConfigToml["desktop.future"] = Entry{Present: true, Value: true}
	content, _ := canonicalJSON(bundle.Content)
	bundle.Manifest.ContentSHA256 = sha256Bytes(content)
	app, _ := getAppInfo(environment.target)
	if _, err := validateBundle(bundle, app, testToolVersion); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown bundle key was accepted: %v", err)
	}

	data := []byte(`[{"command":"futureUnknownCommand","key":"Command+X"}]`)
	if _, _, err := validateKeybindings(data, true); err == nil {
		t.Fatal("unknown keybinding command was accepted")
	}

	bundle, err = buildBundle(environment.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Content.Preferences.Rules["../invalid.rules"] = ""
	content, _ = canonicalJSON(bundle.Content)
	bundle.Manifest.ContentSHA256 = sha256Bytes(content)
	if _, err := validateBundle(bundle, app, testToolVersion); err == nil {
		t.Fatal("unsafe rule filename was accepted")
	}

	bundle, err = buildBundle(environment.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Content.Preferences.ConfigToml["desktop.selected-avatar-id"] = Entry{Present: true, Value: "downloaded-avatar"}
	content, _ = canonicalJSON(bundle.Content)
	bundle.Manifest.ContentSHA256 = sha256Bytes(content)
	if _, err := validateBundle(bundle, app, testToolVersion); err == nil {
		t.Fatal("non-built-in avatar was accepted")
	}
}

func TestUnknownLocalKeybindingBlocksNormalPull(t *testing.T) {
	environment := newFixtureEnvironment(t)
	if err := os.WriteFile(environment.target.Keybindings(), []byte("[{\"command\":\"futureUnknownCommand\",\"key\":\"Command+X\"}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, environment.target)
	bundle, err := buildBundle(environment.source, testToolVersion)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	runner := NewRunner(environment.target, &stdout, &stderr, testToolVersion)
	runner.Fetch = func(string, string) (Bundle, error) { return bundle, nil }
	runner.AppRunning = func() bool { return false }
	if code := runner.Run([]string{"pull", "source-mac"}); code == 0 || !strings.Contains(stderr.String(), "unknown commands") {
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

func TestRollbackSupportsSchemaOneBackups(t *testing.T) {
	layout := Layout{Home: t.TempDir()}
	backup := filepath.Join(layout.Backups(), "legacy")
	if err := os.MkdirAll(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := backupManifest{
		SchemaVersion: 1,
		CreatedAt:     "2026-01-01T00:00:00Z",
		Stores:        make(map[string]backupStore),
	}
	for name, path := range coreTargetPaths(layout) {
		original := []byte("original " + name + "\n")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("changed "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backup, name+".backup"), original, 0o600); err != nil {
			t.Fatal(err)
		}
		manifest.Stores[name] = backupStore{Present: true, Mode: 0o600, SHA256: sha256Bytes(original)}
	}
	data, err := canonicalJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "applied-at"), []byte("2026-01-01T00:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := rollback(layout)
	if err != nil {
		t.Fatal(err)
	}
	if restored != backup {
		t.Fatalf("restored backup %s, want %s", restored, backup)
	}
	for name, path := range coreTargetPaths(layout) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "original "+name+"\n" {
			t.Fatalf("legacy backup did not restore %s", name)
		}
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
	profile, _ := os.ReadFile(environment.target.Profile("review.config.toml"))
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
	if !bytes.Contains(profile, []byte("TARGET_PROFILE_AUTH_MUST_STAY")) {
		t.Error("target profile did not preserve unrelated auth setting")
	}
	localProfile, _ := os.ReadFile(environment.target.Profile("local.config.toml"))
	if !bytes.Contains(localProfile, []byte("TARGET_LOCAL_PROFILE_AUTH_MUST_STAY")) || bytes.Contains(localProfile, []byte("target-local-model")) {
		t.Error("target-only profile did not preserve unrelated values and clear managed values")
	}
	if !fileExists(environment.target.Profile("optimize.config.toml")) {
		t.Error("source-only profile was not created")
	}
	if fileExists(environment.target.Rule("local.rules")) {
		t.Error("target-only rule was not removed")
	}
	if !fileExists(environment.target.Rule("review.rules")) {
		t.Error("source-only rule was not created")
	}
}
