package settingssync

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type renderedTarget struct {
	Name    string
	Path    string
	Data    []byte
	Present bool
}

type backupStore struct {
	Present bool   `json:"present"`
	Mode    uint32 `json:"mode,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

type backupManifest struct {
	SchemaVersion int                    `json:"schema_version"`
	CreatedAt     string                 `json:"created_at"`
	Stores        map[string]backupStore `json:"stores"`
}

func coreTargetPaths(layout Layout) map[string]string {
	return map[string]string{
		"config":       layout.Config(),
		"global_state": layout.GlobalState(),
		"keybindings":  layout.Keybindings(),
	}
}

func existingTargetPaths(layout Layout) (map[string]string, error) {
	result := coreTargetPaths(layout)
	rules, err := readRules(layout)
	if err != nil {
		return nil, err
	}
	for name := range rules {
		result["rules/"+name] = layout.Rule(name)
	}
	profiles, err := readManagedTextFiles(layout.CodexHome(), ".config.toml")
	if err != nil {
		return nil, err
	}
	for name := range profiles {
		result["profiles/"+name] = layout.Profile(name)
	}
	return result, nil
}

func renderGlobalState(original []byte, present bool, entries map[string]Entry) ([]byte, error) {
	state := make(map[string]any)
	if present {
		if err := json.Unmarshal(original, &state); err != nil || state == nil {
			return nil, fmt.Errorf("local global state is not a JSON object")
		}
	}
	for _, spec := range globalSpecs {
		entry := entries[spec.Path]
		if entry.Present {
			state[spec.Path] = entry.Value
		} else {
			delete(state, spec.Path)
		}
	}
	data, err := canonicalJSON(state)
	return append(data, '\n'), err
}

func renderKeybindings(keybindings Keybindings) ([]byte, bool, error) {
	if !keybindings.Present {
		return nil, false, nil
	}
	data, err := json.MarshalIndent(keybindings.Bindings, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(data, '\n'), true, nil
}

func renderTargets(layout Layout, proposed Preferences) ([]renderedTarget, error) {
	config, err := readRequired(layout.Config())
	if err != nil {
		return nil, err
	}
	renderedConfig, err := renderConfig(string(config), proposed.ConfigToml)
	if err != nil {
		return nil, err
	}
	global, globalPresent, err := readOptional(layout.GlobalState())
	if err != nil {
		return nil, err
	}
	renderedGlobal, err := renderGlobalState(global, globalPresent, proposed.GlobalState)
	if err != nil {
		return nil, err
	}
	renderedBindings, bindingsPresent, err := renderKeybindings(proposed.Keybindings)
	if err != nil {
		return nil, err
	}
	targets := []renderedTarget{
		{Name: "config", Path: layout.Config(), Data: renderedConfig, Present: true},
		{Name: "global_state", Path: layout.GlobalState(), Data: renderedGlobal, Present: true},
		{Name: "keybindings", Path: layout.Keybindings(), Data: renderedBindings, Present: bindingsPresent},
	}

	localProfiles, err := readManagedTextFiles(layout.CodexHome(), ".config.toml")
	if err != nil {
		return nil, err
	}
	profileNames := make(map[string]struct{})
	for name := range localProfiles {
		profileNames[name] = struct{}{}
	}
	for name := range proposed.ConfigProfiles {
		profileNames[name] = struct{}{}
	}
	for _, name := range sortedSet(profileNames) {
		entries, ok := proposed.ConfigProfiles[name]
		if !ok {
			entries = preferenceEntries(configSpecs, nil)
		}
		rendered, err := renderConfig(localProfiles[name], entries)
		if err != nil {
			return nil, fmt.Errorf("render profile %s: %w", name, err)
		}
		targets = append(targets, renderedTarget{Name: "profiles/" + name, Path: layout.Profile(name), Data: rendered, Present: true})
	}

	localRules, err := readRules(layout)
	if err != nil {
		return nil, err
	}
	ruleNames := make(map[string]struct{})
	for name := range localRules {
		ruleNames[name] = struct{}{}
	}
	for name := range proposed.Rules {
		ruleNames[name] = struct{}{}
	}
	for _, name := range sortedSet(ruleNames) {
		content, present := proposed.Rules[name]
		targets = append(targets, renderedTarget{Name: "rules/" + name, Path: layout.Rule(name), Data: []byte(content), Present: present})
	}
	return targets, nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func atomicWrite(path string, data []byte, reference string) error {
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	uid, gid := -1, -1
	if reference != "" {
		if info, err := os.Stat(reference); err == nil {
			mode = info.Mode().Perm()
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				uid, gid = int(stat.Uid), int(stat.Gid)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".codex-sync.*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	clean := func() {
		temporary.Close()
		_ = os.Remove(temporaryName)
	}
	if err := temporary.Chmod(mode); err != nil {
		clean()
		return err
	}
	if uid >= 0 {
		if err := temporary.Chown(uid, gid); err != nil && !errors.Is(err, syscall.EPERM) {
			clean()
			return err
		}
	}
	if _, err := temporary.Write(data); err != nil {
		clean()
		return err
	}
	if err := temporary.Sync(); err != nil {
		clean()
		return err
	}
	if err := temporary.Close(); err != nil {
		clean()
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		_ = os.Remove(temporaryName)
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func atomicRemove(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func backupID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random), nil
}

func backupTargetPath(layout Layout, name string) (string, error) {
	if path, ok := coreTargetPaths(layout)[name]; ok {
		return path, nil
	}
	if strings.HasPrefix(name, "rules/") {
		filename := strings.TrimPrefix(name, "rules/")
		if err := validateManagedName(filename, ".rules"); err != nil {
			return "", err
		}
		return layout.Rule(filename), nil
	}
	if strings.HasPrefix(name, "profiles/") {
		filename := strings.TrimPrefix(name, "profiles/")
		if err := validateManagedName(filename, ".config.toml"); err != nil {
			return "", err
		}
		return layout.Profile(filename), nil
	}
	return "", fmt.Errorf("unknown backup store %q", name)
}

func backupDataPath(backup, name string, schemaVersion int) string {
	if schemaVersion == 1 {
		return filepath.Join(backup, name+".backup")
	}
	return filepath.Join(backup, "stores", filepath.FromSlash(name)+".backup")
}

func createBackup(layout Layout, paths map[string]string) (string, error) {
	if err := ensurePrivateDirectory(layout.Backups()); err != nil {
		return "", err
	}
	id, err := backupID()
	if err != nil {
		return "", err
	}
	destination := filepath.Join(layout.Backups(), id)
	if err := os.Mkdir(destination, 0o700); err != nil {
		return "", err
	}
	manifest := backupManifest{SchemaVersion: 2, CreatedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), Stores: make(map[string]backupStore)}
	for name, source := range paths {
		resolved, err := backupTargetPath(layout, name)
		if err != nil || resolved != source {
			return "", fmt.Errorf("invalid backup target %q", name)
		}
		data, present, err := readOptional(source)
		if err != nil {
			return "", err
		}
		if !present {
			manifest.Stores[name] = backupStore{Present: false}
			continue
		}
		info, err := os.Stat(source)
		if err != nil {
			return "", err
		}
		backupPath := backupDataPath(destination, name, manifest.SchemaVersion)
		if err := atomicWrite(backupPath, data, ""); err != nil {
			return "", err
		}
		manifest.Stores[name] = backupStore{Present: true, Mode: uint32(info.Mode().Perm()), SHA256: sha256Bytes(data)}
	}
	data, err := canonicalJSON(manifest)
	if err != nil {
		return "", err
	}
	if err := atomicWrite(filepath.Join(destination, "manifest.json"), append(data, '\n'), ""); err != nil {
		return "", err
	}
	return destination, nil
}

func loadBackupManifest(backup string) (backupManifest, error) {
	data, err := os.ReadFile(filepath.Join(backup, "manifest.json"))
	if err != nil {
		return backupManifest{}, fmt.Errorf("backup manifest is missing: %w", err)
	}
	var manifest backupManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || (manifest.SchemaVersion != 1 && manifest.SchemaVersion != 2) || len(manifest.Stores) < 3 || len(manifest.Stores) > 3+4*MaxManagedFiles {
		return backupManifest{}, fmt.Errorf("backup manifest has an invalid schema")
	}
	for _, name := range []string{"config", "global_state", "keybindings"} {
		if _, ok := manifest.Stores[name]; !ok {
			return backupManifest{}, fmt.Errorf("backup manifest is missing %s", name)
		}
	}
	if manifest.SchemaVersion == 1 && len(manifest.Stores) != 3 {
		return backupManifest{}, fmt.Errorf("backup manifest has an invalid schema")
	}
	for name := range manifest.Stores {
		if _, err := backupTargetPath(Layout{}, name); err != nil {
			return backupManifest{}, fmt.Errorf("backup manifest contains an invalid store: %w", err)
		}
	}
	return manifest, nil
}

func restoreBackup(layout Layout, backup string) error {
	manifest, err := loadBackupManifest(backup)
	if err != nil {
		return err
	}
	for name, metadata := range manifest.Stores {
		destination, err := backupTargetPath(layout, name)
		if err != nil {
			return err
		}
		if !metadata.Present {
			if err := atomicRemove(destination); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(backupDataPath(backup, name, manifest.SchemaVersion))
		if err != nil || sha256Bytes(data) != metadata.SHA256 {
			return fmt.Errorf("backup file is missing or corrupt: %s", name)
		}
		if err := atomicWrite(destination, data, destination); err != nil {
			return err
		}
		if err := os.Chmod(destination, os.FileMode(metadata.Mode)); err != nil {
			return err
		}
	}
	return nil
}

func markBackup(backup, name string) error {
	return atomicWrite(filepath.Join(backup, name), []byte(time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)+"\n"), "")
}

func applyPreferences(layout Layout, proposed Preferences, failAfterReplace int) (string, error) {
	targets, err := renderTargets(layout, proposed)
	if err != nil {
		return "", err
	}
	paths := make(map[string]string, len(targets))
	for _, target := range targets {
		paths[target.Name] = target.Path
	}
	backup, err := createBackup(layout, paths)
	if err != nil {
		return "", err
	}

	var interrupted atomic.Bool
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		select {
		case <-signals:
			interrupted.Store(true)
		case <-done:
		}
	}()
	defer func() {
		signal.Stop(signals)
		close(done)
	}()

	installed := 0
	var applyErr error
	for _, target := range targets {
		if interrupted.Load() {
			applyErr = fmt.Errorf("apply interrupted by signal")
			break
		}
		if target.Present {
			applyErr = atomicWrite(target.Path, target.Data, target.Path)
		} else {
			applyErr = atomicRemove(target.Path)
		}
		if applyErr != nil {
			break
		}
		installed++
		if failAfterReplace > 0 && installed >= failAfterReplace {
			applyErr = fmt.Errorf("simulated interrupted apply")
			break
		}
	}
	if applyErr == nil && interrupted.Load() {
		applyErr = fmt.Errorf("apply interrupted by signal")
	}
	if applyErr == nil {
		applyErr = markBackup(backup, "applied-at")
	}
	if applyErr != nil {
		restoreErr := restoreBackup(layout, backup)
		markErr := markBackup(backup, "restored-after-failure-at")
		return "", errors.Join(applyErr, restoreErr, markErr)
	}
	return backup, nil
}

func latestRollbackCandidate(layout Layout) (string, error) {
	entries, err := os.ReadDir(layout.Backups())
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no completed backup is available for rollback")
	}
	if err != nil {
		return "", err
	}
	candidates := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(layout.Backups(), entry.Name())
		if fileExists(filepath.Join(path, "applied-at")) && !fileExists(filepath.Join(path, "rolled-back-at")) {
			candidates = append(candidates, path)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no completed backup is available for rollback")
	}
	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
	return candidates[0], nil
}

func rollback(layout Layout) (string, error) {
	backup, err := latestRollbackCandidate(layout)
	if err != nil {
		return "", err
	}
	if err := restoreBackup(layout, backup); err != nil {
		return "", err
	}
	if err := markBackup(backup, "rolled-back-at"); err != nil {
		return "", err
	}
	return backup, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
