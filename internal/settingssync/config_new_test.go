package settingssync

import (
	"strings"
	"testing"
)

func TestNewDesktopSettingsValidationAndReset(t *testing.T) {
	for _, tc := range []struct{ key, valid, invalid string }{
		{"composerPlainTextMode", "true", `"true"`},
		{"show-educational-tips", "false", "1"},
		{"reviewDelivery", `"detached"`, `"other"`},
		{"defaultTerminalLocation", `"right"`, `"left"`},
		{"default-mode-request-user-input-enabled", "false", `"false"`},
	} {
		t.Run(tc.key, func(t *testing.T) {
			original := "[desktop]\n" + tc.key + " = " + tc.valid + "\n"
			values, unknown, _, err := scanConfig(original)
			if err != nil || len(unknown) != 0 || len(values) != 1 {
				t.Fatalf("scan: %v %v %v", values, unknown, err)
			}
			if _, _, _, err := scanConfig("[desktop]\n" + tc.key + " = " + tc.invalid + "\n"); err == nil {
				t.Fatal("invalid value accepted")
			}
			rendered, err := renderConfig(original, preferenceEntries(configSpecs, nil))
			if err != nil || strings.Contains(string(rendered), tc.key) {
				t.Fatalf("reset: %s %v", rendered, err)
			}
		})
	}
}

func TestQuotedProjectSectionsDoNotCaptureManagedSettings(t *testing.T) {
	original := "model = \"root-model\"\n[projects.\"/private/project\"]\nmodel = \"private-model\"\n[[private.items]]\nmodel = \"private-array-model\"\n[desktop]\ncomposerPlainTextMode = true\n"
	values, _, _, err := scanConfig(original)
	if err != nil || values["model"] != "root-model" || len(values) != 2 {
		t.Fatalf("scan: %v %v", values, err)
	}
	values["model"] = "replacement-model"
	rendered, err := renderConfig(original, preferenceEntries(configSpecs, values))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(original, "root-model", "replacement-model", 1)
	if string(rendered) != want {
		t.Fatalf("rendered %q, want %q", rendered, want)
	}
}
