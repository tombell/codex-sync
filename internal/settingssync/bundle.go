package settingssync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"time"
)

func buildBundle(layout Layout, toolVersion string) (Bundle, error) {
	app, err := getAppInfo(layout)
	if err != nil {
		return Bundle{}, err
	}
	content, err := buildContent(layout, true)
	if err != nil {
		return Bundle{}, err
	}
	encoded, err := canonicalJSON(content)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{
		Manifest: Manifest{
			SchemaVersion: BundleSchemaVersion,
			ToolVersion:   toolVersion,
			SourceRole:    BundleSourceRole,
			AppBundleID:   app.BundleID,
			AppVersion:    app.Version,
			AppBuild:      app.Build,
			ExportedAt:    time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
			ContentSHA256: sha256Bytes(encoded),
		},
		Content: content,
	}, nil
}

func decodeBundle(data []byte) (Bundle, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var bundle Bundle
	if err := decoder.Decode(&bundle); err != nil {
		return Bundle{}, fmt.Errorf("source returned an invalid export bundle: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Bundle{}, fmt.Errorf("source returned trailing export data")
	}
	return bundle, nil
}

func validateEntryMap(entries map[string]Entry, specs []settingSpec, label string) error {
	if entries == nil {
		return fmt.Errorf("%s preferences must be an object", label)
	}
	expected := specsByPath(specs)
	unknown := make([]string, 0)
	missing := make([]string, 0)
	for path := range entries {
		if _, ok := expected[path]; !ok {
			unknown = append(unknown, path)
		}
	}
	for path := range expected {
		if _, ok := entries[path]; !ok {
			missing = append(missing, path)
		}
	}
	if len(unknown) > 0 || len(missing) > 0 {
		sort.Strings(unknown)
		sort.Strings(missing)
		return fmt.Errorf("%s preference allowlist mismatch (unknown=%v missing=%v)", label, unknown, missing)
	}
	for path, entry := range entries {
		if entry.Present {
			if entry.Value == nil {
				return fmt.Errorf("%s is missing its value", path)
			}
			if err := validateValue(expected[path], entry.Value); err != nil {
				return err
			}
		} else if entry.Value != nil {
			return fmt.Errorf("%s contains a value while marked absent", path)
		}
	}
	return nil
}

func validateBundle(bundle Bundle, target AppInfo, toolVersion string) (Content, error) {
	manifest := bundle.Manifest
	if manifest.SchemaVersion != BundleSchemaVersion {
		return Content{}, fmt.Errorf("unsupported export schema version")
	}
	if manifest.ToolVersion != toolVersion {
		return Content{}, fmt.Errorf("tool version mismatch (source %q, local %q); update codex-sync on both Macs", manifest.ToolVersion, toolVersion)
	}
	if manifest.SourceRole != BundleSourceRole {
		return Content{}, fmt.Errorf("bundle was not exported by a codex-sync source")
	}
	if manifest.AppBundleID != ExpectedBundleID {
		return Content{}, fmt.Errorf("bundle has an unexpected application bundle ID")
	}
	if manifest.ExportedAt == "" || manifest.ContentSHA256 == "" {
		return Content{}, fmt.Errorf("export manifest is incomplete")
	}
	encoded, err := canonicalJSON(bundle.Content)
	if err != nil {
		return Content{}, err
	}
	if manifest.ContentSHA256 != sha256Bytes(encoded) {
		return Content{}, fmt.Errorf("bundle content hash does not match its manifest")
	}
	if manifest.AppVersion != target.Version || manifest.AppBuild != target.Build {
		return Content{}, fmt.Errorf("ChatGPT version mismatch (source %s (%s), local %s (%s))", manifest.AppVersion, manifest.AppBuild, target.Version, target.Build)
	}
	if err := validateEntryMap(bundle.Content.Preferences.ConfigToml, configSpecs, "config.toml"); err != nil {
		return Content{}, err
	}
	profiles := bundle.Content.Preferences.ConfigProfiles
	if profiles == nil || len(profiles) > MaxManagedFiles {
		return Content{}, fmt.Errorf("config profiles have an invalid schema")
	}
	for name, entries := range profiles {
		if err := validateManagedName(name, ".config.toml"); err != nil {
			return Content{}, err
		}
		if err := validateEntryMap(entries, configSpecs, "config profile "+name); err != nil {
			return Content{}, err
		}
	}
	if err := validateEntryMap(bundle.Content.Preferences.GlobalState, globalSpecs, "global state"); err != nil {
		return Content{}, err
	}
	keybindings := bundle.Content.Preferences.Keybindings
	if keybindings.Bindings == nil {
		return Content{}, fmt.Errorf("bundle keybindings are missing bindings")
	}
	if !keybindings.Present && len(keybindings.Bindings) > 0 {
		return Content{}, fmt.Errorf("absent bundle keybindings cannot contain entries")
	}
	for index, binding := range keybindings.Bindings {
		if _, ok := allowedCommandIDs[binding.Command]; !ok {
			return Content{}, fmt.Errorf("bundle keybindings contain unknown command ID at entry %d", index)
		}
		if binding.Key != nil && (len(*binding.Key) == 0 || len(*binding.Key) > 128 || hasControl(*binding.Key)) {
			return Content{}, fmt.Errorf("bundle keybindings contain invalid key at entry %d", index)
		}
	}
	rules := bundle.Content.Preferences.Rules
	if rules == nil || len(rules) > MaxManagedFiles {
		return Content{}, fmt.Errorf("rules have an invalid schema")
	}
	for name, content := range rules {
		if err := validateManagedText(name, content, ".rules"); err != nil {
			return Content{}, err
		}
	}
	audit := bundle.Content.Audit
	if audit.UnknownConfigPaths == nil || audit.UnknownKeybindingCommands == nil || audit.ExcludedConfigCount < 0 || audit.ExcludedGlobalStateCount < 0 {
		return Content{}, fmt.Errorf("bundle audit has an invalid schema")
	}
	return bundle.Content, nil
}

func comparePreferences(current, proposed Preferences) []string {
	changes := make([]string, 0)
	for _, store := range []struct {
		name   string
		before map[string]Entry
		after  map[string]Entry
	}{
		{"config_toml", current.ConfigToml, proposed.ConfigToml},
		{"global_state", current.GlobalState, proposed.GlobalState},
	} {
		paths := make([]string, 0, len(store.after))
		for path := range store.after {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			before, after := store.before[path], store.after[path]
			if reflect.DeepEqual(before, after) {
				continue
			}
			action := "change"
			if !after.Present {
				action = "clear"
			} else if !before.Present {
				action = "set"
			}
			changes = append(changes, fmt.Sprintf("%s:%s:%s", store.name, path, action))
		}
	}
	if !reflect.DeepEqual(current.Keybindings, proposed.Keybindings) {
		changes = append(changes, "keybindings:custom bindings:change")
	}
	profileNames := make(map[string]struct{})
	for name := range current.ConfigProfiles {
		profileNames[name] = struct{}{}
	}
	for name := range proposed.ConfigProfiles {
		profileNames[name] = struct{}{}
	}
	for _, name := range sortedSet(profileNames) {
		before, ok := current.ConfigProfiles[name]
		if !ok {
			before = preferenceEntries(configSpecs, nil)
		}
		after, ok := proposed.ConfigProfiles[name]
		if !ok {
			after = preferenceEntries(configSpecs, nil)
		}
		for _, spec := range configSpecs {
			beforeEntry, afterEntry := before[spec.Path], after[spec.Path]
			if reflect.DeepEqual(beforeEntry, afterEntry) {
				continue
			}
			action := "change"
			if !afterEntry.Present {
				action = "clear"
			} else if !beforeEntry.Present {
				action = "set"
			}
			changes = append(changes, fmt.Sprintf("config_profile[%s]:%s:%s", name, spec.Path, action))
		}
	}
	ruleNames := make(map[string]struct{})
	for name := range current.Rules {
		ruleNames[name] = struct{}{}
	}
	for name := range proposed.Rules {
		ruleNames[name] = struct{}{}
	}
	for _, name := range sortedSet(ruleNames) {
		before, beforePresent := current.Rules[name]
		after, afterPresent := proposed.Rules[name]
		if beforePresent == afterPresent && before == after {
			continue
		}
		action := "change"
		if !afterPresent {
			action = "clear"
		} else if !beforePresent {
			action = "set"
		}
		changes = append(changes, fmt.Sprintf("rules:%s:%s", name, action))
	}
	return changes
}
