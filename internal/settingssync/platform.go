package settingssync

import "strings"

func supportedPlatform(platform string) bool {
	return platform == "darwin" || platform == "linux"
}

func preservePlatformSetting(path, source, target string) bool {
	switch path {
	case "desktop.mac-menu-bar-enabled", "desktop.dock-icon-preference", "desktop.useFontSmoothing":
		return source != "darwin" || target != "darwin"
	}
	if source == target {
		return false
	}
	return path == "desktop.open-in-target-preferences.global" ||
		strings.HasPrefix(path, "desktop.appearanceLightChromeTheme.fonts.") ||
		strings.HasPrefix(path, "desktop.appearanceDarkChromeTheme.fonts.")
}

// Keep complete entry maps for validation and rendering. An omitted entry means
// "clear" elsewhere, so skipped preferences must take the target's value.
func preferencesForTarget(proposed, current Preferences, source, target string) Preferences {
	merge := func(incoming, local map[string]Entry) map[string]Entry {
		result := make(map[string]Entry, len(configSpecs))
		for _, spec := range configSpecs {
			entry := incoming[spec.Path]
			if preservePlatformSetting(spec.Path, source, target) {
				entry = local[spec.Path]
			}
			result[spec.Path] = entry
		}
		return result
	}
	proposed.ConfigToml = merge(proposed.ConfigToml, current.ConfigToml)
	profiles := make(map[string]map[string]Entry)
	for name, entries := range proposed.ConfigProfiles {
		profiles[name] = merge(entries, current.ConfigProfiles[name])
	}
	// Target-only profiles still clear shared preferences while retaining local
	// platform-specific values, just like the main configuration.
	for name, entries := range current.ConfigProfiles {
		if _, ok := profiles[name]; !ok {
			profiles[name] = merge(nil, entries)
		}
	}
	proposed.ConfigProfiles = profiles
	if source != target {
		proposed.Keybindings = current.Keybindings
	}
	return proposed
}
