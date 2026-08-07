package settingssync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"unicode/utf8"
)

func readRequired(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read required settings file %s: %w", path, err)
	}
	return data, nil
}

func readOptional(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read local file %s: %w", path, err)
	}
	return data, true, nil
}

func readGlobalState(layout Layout) (map[string]any, int, error) {
	data, err := readRequired(layout.GlobalState())
	if err != nil {
		return nil, 0, err
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil || state == nil {
		return nil, 0, fmt.Errorf("global state is not a JSON object")
	}
	values := make(map[string]any)
	for _, spec := range globalSpecs {
		if value, ok := state[spec.Path]; ok {
			if err := validateValue(spec, value); err != nil {
				return nil, 0, err
			}
			values[spec.Path] = value
		}
	}
	return values, len(state) - len(values), nil
}

func validateKeybindings(data []byte, strict bool) ([]Keybinding, []string, error) {
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("keybindings.json must contain a JSON array")
	}
	clean := make([]Keybinding, 0, len(raw))
	unknownSet := make(map[string]struct{})
	for index, item := range raw {
		if len(item) != 2 || item["command"] == nil || item["key"] == nil {
			return nil, nil, fmt.Errorf("keybindings entry %d has an invalid schema", index)
		}
		var command string
		if err := json.Unmarshal(item["command"], &command); err != nil {
			return nil, nil, fmt.Errorf("keybindings entry %d has an invalid command", index)
		}
		if _, ok := allowedCommandIDs[command]; !ok {
			unknownSet[command] = struct{}{}
			continue
		}
		var key *string
		if !bytes.Equal(bytes.TrimSpace(item["key"]), []byte("null")) {
			var text string
			if err := json.Unmarshal(item["key"], &text); err != nil || utf8.RuneCountInString(text) < 1 || utf8.RuneCountInString(text) > 128 || hasControl(text) {
				return nil, nil, fmt.Errorf("keybindings entry %d has an invalid key", index)
			}
			key = &text
		}
		clean = append(clean, Keybinding{Command: command, Key: key})
	}
	unknown := sortedSet(unknownSet)
	if strict && len(unknown) > 0 {
		return nil, unknown, fmt.Errorf("keybindings.json contains unknown command IDs: %v", unknown)
	}
	return clean, unknown, nil
}

func hasControl(text string) bool {
	for _, character := range text {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func readKeybindings(layout Layout, strict bool) (Keybindings, []string, error) {
	data, present, err := readOptional(layout.Keybindings())
	if err != nil {
		return Keybindings{}, nil, err
	}
	if !present {
		return Keybindings{Present: false, Bindings: []Keybinding{}}, nil, nil
	}
	bindings, unknown, err := validateKeybindings(data, strict)
	return Keybindings{Present: true, Bindings: bindings}, unknown, err
}

func getAppInfo(layout Layout) (AppInfo, error) {
	path := layout.AppPath + "/Contents/Info.plist"
	read := func(key string) (string, error) {
		output, err := exec.Command("/usr/bin/plutil", "-extract", key, "raw", "-o", "-", path).Output()
		if err != nil {
			return "", fmt.Errorf("read %s from ChatGPT application metadata: %w", key, err)
		}
		return strings.TrimSpace(string(output)), nil
	}
	bundleID, err := read("CFBundleIdentifier")
	if err != nil {
		return AppInfo{}, err
	}
	version, err := read("CFBundleShortVersionString")
	if err != nil {
		return AppInfo{}, err
	}
	build, err := read("CFBundleVersion")
	if err != nil {
		return AppInfo{}, err
	}
	info := AppInfo{BundleID: bundleID, Version: version, Build: build}
	if info.BundleID == "" || info.Version == "" || info.Build == "" {
		return AppInfo{}, fmt.Errorf("ChatGPT application metadata is incomplete")
	}
	if info.BundleID != ExpectedBundleID {
		return AppInfo{}, fmt.Errorf("unexpected ChatGPT bundle ID: %s", info.BundleID)
	}
	return info, nil
}

func buildContent(layout Layout, strictKeybindings bool) (Content, error) {
	configData, err := readRequired(layout.Config())
	if err != nil {
		return Content{}, err
	}
	configValues, unknownConfig, excludedConfig, err := scanConfig(string(configData))
	if err != nil {
		return Content{}, err
	}
	globalValues, excludedGlobal, err := readGlobalState(layout)
	if err != nil {
		return Content{}, err
	}
	keybindings, unknownCommands, err := readKeybindings(layout, strictKeybindings)
	if err != nil {
		return Content{}, err
	}
	sort.Strings(unknownConfig)
	sort.Strings(unknownCommands)
	return Content{
		Preferences: Preferences{
			ConfigToml:  preferenceEntries(configSpecs, configValues),
			GlobalState: preferenceEntries(globalSpecs, globalValues),
			Keybindings: keybindings,
		},
		Audit: Audit{
			UnknownConfigPaths:        unknownConfig,
			UnknownKeybindingCommands: unknownCommands,
			ExcludedConfigCount:       excludedConfig,
			ExcludedGlobalStateCount:  excludedGlobal,
		},
	}, nil
}
