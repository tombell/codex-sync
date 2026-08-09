package settingssync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Layout struct {
	Home    string
	AppPath string
}

func LiveLayout() (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, fmt.Errorf("find home directory: %w", err)
	}
	return Layout{Home: home, AppPath: "/Applications/ChatGPT.app"}, nil
}

func (l Layout) Config() string    { return filepath.Join(l.Home, ".codex", "config.toml") }
func (l Layout) CodexHome() string { return filepath.Join(l.Home, ".codex") }
func (l Layout) GlobalState() string {
	return filepath.Join(l.Home, ".codex", ".codex-global-state.json")
}
func (l Layout) Keybindings() string { return filepath.Join(l.Home, ".codex", "keybindings.json") }
func (l Layout) Rules() string       { return filepath.Join(l.Home, ".codex", "rules") }
func (l Layout) Rule(name string) string {
	return filepath.Join(l.Rules(), name)
}
func (l Layout) Profile(name string) string {
	return filepath.Join(l.CodexHome(), name)
}
func (l Layout) Backups() string {
	return filepath.Join(l.Home, ".local", "state", "codex-sync", "backups")
}

type AppInfo struct {
	BundleID string
	Version  string
	Build    string
}

type Entry struct {
	Present bool `json:"present"`
	Value   any  `json:"value,omitempty"`
}

func (entry *Entry) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	presentData, hasPresent := raw["present"]
	if !hasPresent {
		return fmt.Errorf("preference entry is missing its presence marker")
	}
	if err := json.Unmarshal(presentData, &entry.Present); err != nil {
		return fmt.Errorf("preference entry has an invalid presence marker")
	}
	valueData, hasValue := raw["value"]
	if entry.Present {
		if !hasValue || len(raw) != 2 {
			return fmt.Errorf("present preference entry must contain exactly present and value")
		}
		if err := json.Unmarshal(valueData, &entry.Value); err != nil {
			return err
		}
		return nil
	}
	if hasValue || len(raw) != 1 {
		return fmt.Errorf("absent preference entry must contain only present")
	}
	entry.Value = nil
	return nil
}

type Keybinding struct {
	Command string  `json:"command"`
	Key     *string `json:"key"`
}

func (binding *Keybinding) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) != 2 || raw["command"] == nil || raw["key"] == nil {
		return fmt.Errorf("keybinding must contain exactly command and key")
	}
	if err := json.Unmarshal(raw["command"], &binding.Command); err != nil {
		return fmt.Errorf("keybinding command must be a string")
	}
	if string(raw["key"]) == "null" {
		binding.Key = nil
		return nil
	}
	var key string
	if err := json.Unmarshal(raw["key"], &key); err != nil {
		return fmt.Errorf("keybinding key must be a string or null")
	}
	binding.Key = &key
	return nil
}

type Keybindings struct {
	Present  bool         `json:"present"`
	Bindings []Keybinding `json:"bindings"`
}

func (keybindings *Keybindings) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) != 2 || raw["present"] == nil || raw["bindings"] == nil {
		return fmt.Errorf("keybindings must contain exactly present and bindings")
	}
	if err := json.Unmarshal(raw["present"], &keybindings.Present); err != nil {
		return fmt.Errorf("keybindings presence marker must be boolean")
	}
	if err := json.Unmarshal(raw["bindings"], &keybindings.Bindings); err != nil {
		return err
	}
	return nil
}

type Preferences struct {
	ConfigToml     map[string]Entry            `json:"config_toml"`
	ConfigProfiles map[string]map[string]Entry `json:"config_profiles"`
	GlobalState    map[string]Entry            `json:"global_state"`
	Keybindings    Keybindings                 `json:"keybindings"`
	Rules          map[string]string           `json:"rules"`
}

type Audit struct {
	UnknownConfigPaths        []string `json:"unknown_config_paths"`
	UnknownKeybindingCommands []string `json:"unknown_keybinding_commands"`
	ExcludedConfigCount       int      `json:"excluded_config_count"`
	ExcludedGlobalStateCount  int      `json:"excluded_global_state_count"`
}

type Content struct {
	Preferences Preferences `json:"preferences"`
	Audit       Audit       `json:"audit"`
}

type Manifest struct {
	SchemaVersion int    `json:"schema_version"`
	ToolVersion   string `json:"tool_version"`
	SourceRole    string `json:"source_role"`
	AppBundleID   string `json:"app_bundle_id"`
	AppVersion    string `json:"app_version"`
	AppBuild      string `json:"app_build"`
	ExportedAt    string `json:"exported_at"`
	ContentSHA256 string `json:"content_sha256"`
}

type Bundle struct {
	Manifest Manifest `json:"manifest"`
	Content  Content  `json:"content"`
}

func canonicalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func sha256Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func redactedValueHash(value any) (string, error) {
	data, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	return sha256Bytes(data), nil
}

func validateValue(spec settingSpec, value any) error {
	switch spec.Kind {
	case kindBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s has invalid type; expected boolean", spec.Path)
		}
	case kindNumber:
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("%s has invalid type; expected number", spec.Path)
		}
		if spec.Minimum != nil && number < *spec.Minimum {
			return fmt.Errorf("%s is below its allowed range", spec.Path)
		}
		if spec.Maximum != nil && number > *spec.Maximum {
			return fmt.Errorf("%s is above its allowed range", spec.Path)
		}
	case kindString:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s has invalid type; expected string", spec.Path)
		}
		if len(spec.Choices) > 0 && !contains(spec.Choices, text) {
			return fmt.Errorf("%s contains a value outside its allowlist", spec.Path)
		}
		if spec.Pattern != nil && !spec.Pattern.MatchString(text) {
			return fmt.Errorf("%s contains an unsupported string", spec.Path)
		}
	default:
		return errors.New("internal error: unsupported allowlist type")
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sortedSet(set map[string]struct{}) []string {
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func splitPath(path string) ([]string, string) {
	parts := strings.Split(path, ".")
	return parts[:len(parts)-1], parts[len(parts)-1]
}
