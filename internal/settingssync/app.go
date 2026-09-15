package settingssync

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func getAppInfo(layout Layout) (AppInfo, error) {
	switch runtime.GOOS {
	case "darwin":
		if layout.AppPath == "" {
			layout.AppPath = defaultAppPath
		}
		return getMacAppInfo(layout)
	case "linux":
		if layout.AppPath == "" {
			layout.AppPath = defaultLinuxAppPath
		}
		return getLinuxAppInfo(layout)
	default:
		return AppInfo{}, fmt.Errorf("unsupported platform %s; codex-sync supports macOS and Linux", runtime.GOOS)
	}
}

func getMacAppInfo(layout Layout) (AppInfo, error) {
	path := filepath.Join(layout.AppPath, "Contents", "Info.plist")
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
	executable, err := read("CFBundleExecutable")
	if err != nil {
		return AppInfo{}, err
	}
	info := AppInfo{Platform: "darwin", BundleID: bundleID, Version: version, Build: build, Executable: executable}
	if info.BundleID == "" || info.Version == "" || info.Build == "" || info.Executable == "" {
		return AppInfo{}, fmt.Errorf("ChatGPT application metadata is incomplete")
	}
	if info.BundleID != ExpectedBundleID {
		return AppInfo{}, fmt.Errorf("unexpected ChatGPT bundle ID: %s", info.BundleID)
	}
	return info, nil
}
