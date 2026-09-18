package settingssync

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCustomAvatarPullPreservesTarget(t *testing.T) {
	for _, avatar := range []string{"", "seedy", "target-custom-avatar"} {
		t.Run("target-"+avatar, func(t *testing.T) {
			env := newFixtureEnvironment(t)
			source := "[desktop]\nselected-avatar-id = \"private-custom-avatar\"\ncomposerPlainTextMode = true\n"
			target := "[desktop]\ncomposerPlainTextMode = false\n"
			if avatar != "" {
				target += "selected-avatar-id = \"" + avatar + "\" # keep avatar\n"
			}
			for _, path := range []string{env.source.Config(), env.source.Profile("avatar.config.toml")} {
				if err := os.WriteFile(path, []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{env.target.Config(), env.target.Profile("avatar.config.toml")} {
				if err := os.WriteFile(path, []byte(target), 0600); err != nil {
					t.Fatal(err)
				}
			}
			bundle, err := buildBundle(env.source, testToolVersion)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(bundle)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte("private-custom-avatar")) {
				t.Fatal("export leaked custom avatar ID")
			}
			bundle, err = decodeBundle(data)
			if err != nil {
				t.Fatal(err)
			}
			if !bundle.Content.Preferences.ConfigToml["desktop.selected-avatar-id"].Preserve {
				t.Fatal("missing preservation marker")
			}
			var stdout, stderr bytes.Buffer
			runner := NewRunner(env.target, &stdout, &stderr, testToolVersion)
			runner.Fetch = func(_, _ string, _ remoteExportOptions) (Bundle, error) { return bundle, nil }
			runner.AppRunning = func(string) (bool, error) { return false, nil }
			before := snapshot(t, env.target)
			if code := runner.Run([]string{"pull", "source", "--dry-run"}); code != 0 {
				t.Fatalf("dry run: %s", stderr.String())
			}
			if !reflect.DeepEqual(before, snapshot(t, env.target)) {
				t.Fatal("dry run changed settings")
			}
			if code := runner.Run([]string{"pull", "source"}); code != 0 {
				t.Fatalf("pull: %s", stderr.String())
			}
			for _, path := range []string{env.target.Config(), env.target.Profile("avatar.config.toml")} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				values, _, _, err := scanConfig(string(data))
				if err != nil {
					t.Fatal(err)
				}
				if values["desktop.composerPlainTextMode"] != true {
					t.Fatal("other preference was not synced")
				}
				if avatar == "" {
					if strings.Contains(string(data), "selected-avatar-id") {
						t.Fatal("created absent target avatar")
					}
				} else if !strings.Contains(string(data), "selected-avatar-id = \""+avatar+"\"") {
					t.Fatalf("target avatar changed: %s", data)
				}
			}
		})
	}
}

func TestAvatarValidation(t *testing.T) {
	for _, text := range []string{
		"[desktop]\nselected-avatar-id = false\n",
		"[desktop]\nselected-avatar-id = 'custom'\nselected-avatar-id = 'codex'\n",
	} {
		if _, _, _, err := scanConfig(text); err == nil {
			t.Fatalf("accepted invalid config %q", text)
		}
	}
	for _, value := range []string{"codex", "seedy"} {
		values, _, _, err := scanConfig("[desktop]\nselected-avatar-id = '" + value + "'\n")
		if err != nil {
			t.Fatal(err)
		}
		entry := preferenceEntries(configSpecs, values)["desktop.selected-avatar-id"]
		if !entry.Present || entry.Value != value || entry.Preserve {
			t.Fatalf("built-in avatar not synced: %#v", entry)
		}
	}
	entries := preferenceEntries(configSpecs, nil)
	entries["desktop.selected-avatar-id"] = Entry{Preserve: true}
	if err := validateEntryMap(entries, configSpecs, "config"); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []Entry{{Preserve: true, Present: true}, {Preserve: true, Value: "custom"}, {Present: true, Value: "custom"}} {
		entries["desktop.selected-avatar-id"] = entry
		if err := validateEntryMap(entries, configSpecs, "config"); err == nil {
			t.Fatalf("accepted invalid entry %#v", entry)
		}
	}
	entries = preferenceEntries(configSpecs, nil)
	entries["model"] = Entry{Preserve: true}
	if err := validateEntryMap(entries, configSpecs, "config"); err == nil {
		t.Fatal("accepted preservation for other preference")
	}
	for _, raw := range []string{`{"present":true,"preserve":true}`, `{"present":false,"preserve":true,"value":"custom"}`, `{"present":false,"preserve":false}`, `{"present":false,"preserve":null}`} {
		var entry Entry
		if err := json.Unmarshal([]byte(raw), &entry); err == nil {
			t.Fatalf("accepted invalid marker %s", raw)
		}
	}
}
