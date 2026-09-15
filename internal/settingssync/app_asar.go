package settingssync

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultLinuxAppPath = "/usr/lib/chatgpt"
	maxASARHeaderBytes  = 16 * 1024 * 1024
	maxAppMetadataBytes = 64 * 1024
)

// Linux's packaged desktop has no Info.plist. Read its identity and release
// metadata without launching the application or executing its JavaScript.
func getLinuxAppInfo(layout Layout) (AppInfo, error) {
	data, err := readASARPackage(filepath.Join(layout.AppPath, "resources", "app.asar"))
	if err != nil {
		return AppInfo{}, fmt.Errorf("read Linux desktop metadata in %s (use --app-path for another installation): %w", layout.AppPath, err)
	}
	var metadata struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Build   string `json:"codexBuildNumber"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return AppInfo{}, fmt.Errorf("invalid desktop package.json: %w", err)
	}
	if metadata.Name != "openai-codex-electron" {
		return AppInfo{}, fmt.Errorf("unexpected Linux desktop package identity %q", metadata.Name)
	}
	for _, value := range []string{metadata.Version, metadata.Build} {
		if value == "" || len(value) > 128 || strings.TrimSpace(value) != value || hasControl(value) {
			return AppInfo{}, fmt.Errorf("Linux desktop version or build is missing or invalid")
		}
	}
	const executable = "ChatGPT"
	info, err := os.Stat(filepath.Join(layout.AppPath, executable))
	if err != nil {
		return AppInfo{}, fmt.Errorf("find Linux desktop executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return AppInfo{}, fmt.Errorf("Linux desktop ChatGPT is not an executable file")
	}
	return AppInfo{Platform: "linux", BundleID: ExpectedBundleID, Version: metadata.Version, Build: metadata.Build, Executable: executable}, nil
}

func readASARPackage(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("app.asar is not a regular file")
	}
	var prefix [16]byte
	if _, err := io.ReadFull(f, prefix[:]); err != nil {
		return nil, fmt.Errorf("read ASAR header: %w", err)
	}
	sizePickle := binary.LittleEndian.Uint32(prefix[0:4])
	headerSize := int64(binary.LittleEndian.Uint32(prefix[4:8]))
	payloadSize := int64(binary.LittleEndian.Uint32(prefix[8:12]))
	jsonSize := int64(binary.LittleEndian.Uint32(prefix[12:16]))
	if sizePickle != 4 || headerSize < 8 || headerSize > maxASARHeaderBytes || payloadSize != headerSize-4 || jsonSize < 2 || jsonSize > headerSize-8 || headerSize-8-jsonSize > 3 || headerSize%4 != 0 || 8+headerSize > info.Size() {
		return nil, fmt.Errorf("invalid or oversized ASAR header")
	}
	header := make([]byte, jsonSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, err
	}
	var archive struct {
		Files map[string]json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(header, &archive); err != nil {
		return nil, fmt.Errorf("invalid ASAR file index: %w", err)
	}
	var entry struct {
		Size     int64  `json:"size"`
		Offset   string `json:"offset"`
		Unpacked bool   `json:"unpacked"`
		Link     string `json:"link"`
	}
	if err := json.Unmarshal(archive.Files["package.json"], &entry); err != nil {
		return nil, fmt.Errorf("ASAR package.json entry is missing or invalid")
	}
	offset, err := strconv.ParseInt(entry.Offset, 10, 64)
	dataSize := info.Size() - (8 + headerSize)
	if err != nil || offset < 0 || entry.Size <= 0 || entry.Size > maxAppMetadataBytes || entry.Unpacked || entry.Link != "" || offset > dataSize || entry.Size > dataSize-offset {
		return nil, fmt.Errorf("invalid ASAR package.json bounds or storage")
	}
	data := make([]byte, entry.Size)
	if _, err := f.ReadAt(data, 8+headerSize+offset); err != nil {
		return nil, fmt.Errorf("read ASAR package.json: %w", err)
	}
	return data, nil
}
