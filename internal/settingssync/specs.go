package settingssync

import "regexp"

const (
	BundleSchemaVersion = 3
	ExpectedBundleID    = "com.openai.codex"
	BundleSourceRole    = "source"
	MaxBundleBytes      = 1024 * 1024
	MaxManagedFiles     = 64
	MaxManagedFileBytes = 64 * 1024
)

type valueKind string

const (
	kindBoolean valueKind = "boolean"
	kindNumber  valueKind = "number"
	kindString  valueKind = "string"
)

type settingSpec struct {
	Path    string
	Kind    valueKind
	Choices []string
	Minimum *float64
	Maximum *float64
	Pattern *regexp.Regexp
}

func numberSpec(path string, minimum, maximum float64) settingSpec {
	return settingSpec{Path: path, Kind: kindNumber, Minimum: &minimum, Maximum: &maximum}
}

func stringSpec(path string, choices ...string) settingSpec {
	return settingSpec{Path: path, Kind: kindString, Choices: choices}
}

func patternSpec(path string, pattern *regexp.Regexp) settingSpec {
	return settingSpec{Path: path, Kind: kindString, Pattern: pattern}
}

func boolSpec(path string) settingSpec {
	return settingSpec{Path: path, Kind: kindBoolean}
}

var (
	textPattern        = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,256}$`)
	idPattern          = regexp.MustCompile(`^[A-Za-z0-9._:/+\-]{1,160}$`)
	instructionPattern = regexp.MustCompile(`^[^\x00-\x1f\x7f]{0,1000}$`)
	ruleNamePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}\.rules$`)
	profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}\.config\.toml$`)

	configSpecs = []settingSpec{
		patternSpec("model", idPattern),
		stringSpec("model_reasoning_effort", "minimal", "low", "medium", "high", "xhigh"),
		stringSpec("model_reasoning_summary", "auto", "concise", "detailed", "none"),
		stringSpec("plan_mode_reasoning_effort", "none", "minimal", "low", "medium", "high", "xhigh"),
		stringSpec("web_search", "disabled", "cached", "indexed", "live"),
		boolSpec("feedback.enabled"),
		boolSpec("agents.enabled"),
		boolSpec("agents.interrupt_message"),
		patternSpec("agents.default_subagent_model", idPattern),
		stringSpec("agents.default_subagent_reasoning_effort", "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"),
		stringSpec("personality", "friendly", "pragmatic", "none"),
		stringSpec("desktop.composerEnterBehavior", "enter", "cmdIfMultiline", "cmdAlways"),
		boolSpec("desktop.preventSleepWhileRunning"),
		boolSpec("desktop.keepRemoteControlAwakeWhilePluggedIn"),
		stringSpec("desktop.followUpQueueMode", "queue", "steer", "interrupt"),
		stringSpec("desktop.conversationDetailMode", "STEPS_PROSE", "STEPS_COMMANDS", "STEPS_EXECUTION"),
		boolSpec("desktop.mac-menu-bar-enabled"),
		boolSpec("desktop.show-context-window-usage"),
		stringSpec("desktop.notifications-turn-mode", "off", "unfocused", "always"),
		boolSpec("desktop.notifications-permissions-enabled"),
		boolSpec("desktop.notifications-questions-enabled"),
		boolSpec("desktop.ambient-suggestions-enabled"),
		stringSpec("desktop.appearanceTheme", "system", "light", "dark"),
		patternSpec("desktop.appearanceLightCodeThemeId", idPattern),
		patternSpec("desktop.appearanceDarkCodeThemeId", idPattern),
		stringSpec("desktop.appearanceDiffMarkerStyle", "color", "symbols"),
		numberSpec("desktop.sansFontSize", 8, 32),
		numberSpec("desktop.codeFontSize", 8, 32),
		stringSpec("desktop.reduced-motion-preference", "system", "on", "off"),
		boolSpec("desktop.useFontSmoothing"),
		boolSpec("desktop.usePointerCursors"),
		stringSpec("desktop.dock-icon-preference", "app-default", "codex-system"),
		stringSpec("desktop.selected-avatar-id", "codex", "dewey", "fireball", "hoots", "rocky", "seedy", "stacky", "bsod", "null-signal"),
		patternSpec("desktop.appearanceLightChromeTheme.accent", textPattern),
		numberSpec("desktop.appearanceLightChromeTheme.contrast", 0, 100),
		patternSpec("desktop.appearanceLightChromeTheme.ink", textPattern),
		boolSpec("desktop.appearanceLightChromeTheme.opaqueWindows"),
		patternSpec("desktop.appearanceLightChromeTheme.surface", textPattern),
		patternSpec("desktop.appearanceLightChromeTheme.fonts.code", textPattern),
		patternSpec("desktop.appearanceLightChromeTheme.fonts.ui", textPattern),
		patternSpec("desktop.appearanceLightChromeTheme.semanticColors.diffAdded", textPattern),
		patternSpec("desktop.appearanceLightChromeTheme.semanticColors.diffRemoved", textPattern),
		patternSpec("desktop.appearanceLightChromeTheme.semanticColors.skill", textPattern),
		patternSpec("desktop.appearanceDarkChromeTheme.accent", textPattern),
		numberSpec("desktop.appearanceDarkChromeTheme.contrast", 0, 100),
		patternSpec("desktop.appearanceDarkChromeTheme.ink", textPattern),
		boolSpec("desktop.appearanceDarkChromeTheme.opaqueWindows"),
		patternSpec("desktop.appearanceDarkChromeTheme.surface", textPattern),
		patternSpec("desktop.appearanceDarkChromeTheme.fonts.code", textPattern),
		patternSpec("desktop.appearanceDarkChromeTheme.fonts.ui", textPattern),
		patternSpec("desktop.appearanceDarkChromeTheme.semanticColors.diffAdded", textPattern),
		patternSpec("desktop.appearanceDarkChromeTheme.semanticColors.diffRemoved", textPattern),
		patternSpec("desktop.appearanceDarkChromeTheme.semanticColors.skill", textPattern),
		stringSpec("desktop.open-link-in-target-preference", "in-app-browser", "external-browser"),
		stringSpec("desktop.open-local-url-in-target-preference", "in-app-browser", "external-browser"),
		stringSpec("desktop.browser-annotation-screenshots-mode", "always", "necessary"),
		boolSpec("desktop.computerUseAlwaysHidePictureInPicture"),
		boolSpec("desktop.git-create-pull-request-as-draft"),
		stringSpec("desktop.git-pull-request-merge-method", "merge", "squash"),
		boolSpec("desktop.git-show-sidebar-pr-icons"),
		patternSpec("desktop.git-commit-instructions", instructionPattern),
		patternSpec("desktop.open-in-target-preferences.global", idPattern),
	}

	globalSpecs = []settingSpec{
		boolSpec("browser-use-bundled-plugin-auto-install-disabled"),
		boolSpec("computer-use-bundled-plugin-auto-install-disabled"),
	}

	knownExcludedDesktopPaths = stringSet(
		"desktop.dictationDictionary",
		"desktop.avatar-overlay-mascot-width-px",
		"desktop.enabled-reasoning-efforts",
		"desktop.browser-download-directory",
	)

	allowedCommandIDs = stringSet(
		"approval.approve", "approval.decline", "archiveThread", "closeTab", "closeWindow",
		"composer.addPhotos", "composer.openModelPicker", "composer.openProjectPicker",
		"composer.startDictation", "composer.startVoiceMode", "composer.submit",
		"environmentAction1", "environmentAction2", "findInThread", "focusBrowserAddressBar",
		"globalDictationHold", "globalDictationToggle", "goToLine", "hardReloadBrowserPage",
		"hotkeyWindow", "keyboardShortcuts", "logOut", "manageTasks", "mcpSettings",
		"navigateBack", "navigateBrowserBack", "navigateBrowserForward", "navigateForward",
		"newProjectlessTask", "newTask", "newWindow", "nextRecentThread", "nextTab", "nextThread",
		"openBrowserTab", "openCommandMenu", "openControlWindow", "openFolder", "openReviewTab",
		"openSideChat", "openSkills", "openThreadInNewWindow", "personalitySettings",
		"previousRecentThread", "previousTab", "previousThread", "quickChat", "realtimeVoice",
		"realtimeVoice.toggleMicrophoneMute", "redoAppAction", "reloadBrowserPage", "renameThread",
		"searchFiles", "settings", "showKeyboardShortcuts", "switchToMode1", "switchToMode2",
		"switchToMode3", "temporaryChat", "toggleBottomPanel", "toggleBrowserPanel",
		"toggleDebugModal", "toggleFileTreePanel", "toggleMaximizeSidePanel", "toggleReviewPanel",
		"toggleReviewTab", "toggleSidebar", "toggleTerminal", "toggleThreadPin",
		"toggleTraceRecording", "undoAppAction", "thread1", "thread2", "thread3", "thread4",
		"thread5", "thread6", "thread7", "thread8", "thread9",
	)
)

func stringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func specsByPath(specs []settingSpec) map[string]settingSpec {
	result := make(map[string]settingSpec, len(specs))
	for _, spec := range specs {
		result[spec.Path] = spec
	}
	return result
}
