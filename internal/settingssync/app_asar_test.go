package settingssync

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureASAR(t *testing.T, metadata []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	entry := map[string]any{"size": len(metadata), "offset": "0"}
	if mutate != nil {
		mutate(entry)
	}
	header, err := json.Marshal(map[string]any{"files": map[string]any{"package.json": entry}})
	if err != nil {
		t.Fatal(err)
	}
	headerSize := 8 + (len(header)+3)&^3
	data := make([]byte, 8+headerSize)
	binary.LittleEndian.PutUint32(data[0:4], 4)
	binary.LittleEndian.PutUint32(data[4:8], uint32(headerSize))
	binary.LittleEndian.PutUint32(data[8:12], uint32(headerSize-4))
	binary.LittleEndian.PutUint32(data[12:16], uint32(len(header)))
	copy(data[16:], header)
	return append(data, metadata...)
}

func writeLinuxFixture(t *testing.T, root, version, build string) {
	t.Helper()
	metadata, err := json.Marshal(map[string]string{"name": "openai-codex-electron", "version": version, "codexBuildNumber": build})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "resources"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "resources", "app.asar"), fixtureASAR(t, metadata, nil), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ChatGPT"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxAppInfo(t *testing.T) {
	root := t.TempDir()
	writeLinuxFixture(t, root, "26.908.40834", "8881")
	got, err := getLinuxAppInfo(Layout{AppPath: root})
	if err != nil {
		t.Fatal(err)
	}
	want := AppInfo{Platform: "linux", BundleID: ExpectedBundleID, Version: "26.908.40834", Build: "8881", Executable: "ChatGPT"}
	if got != want {
		t.Fatalf("metadata = %#v, want %#v", got, want)
	}
	if err := os.Remove(filepath.Join(root, "ChatGPT")); err != nil {
		t.Fatal(err)
	}
	if _, err := getLinuxAppInfo(Layout{AppPath: root}); err == nil {
		t.Fatal("accepted missing executable")
	}
}

func TestLinuxAppRejectsUntrustedMetadata(t *testing.T) {
	for _, metadata := range []string{
		`{"name":"other-app","version":"1","codexBuildNumber":"2"}`,
		`{"name":"openai-codex-electron","version":"1"}`,
		`{"name":"openai-codex-electron","version":"","codexBuildNumber":"2"}`,
		`{"name":"openai-codex-electron","version":"1\n","codexBuildNumber":"2"}`,
		`{"name":"openai-codex-electron","version":"1","codexBuildNumber":2}`,
		`{`,
	} {
		root := t.TempDir()
		writeLinuxFixture(t, root, "1", "2")
		if err := os.WriteFile(filepath.Join(root, "resources", "app.asar"), fixtureASAR(t, []byte(metadata), nil), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := getLinuxAppInfo(Layout{AppPath: root}); err == nil {
			t.Fatalf("accepted %s", metadata)
		}
	}
	if _, err := getLinuxAppInfo(Layout{AppPath: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "--app-path") {
		t.Fatalf("missing app error: %v", err)
	}
}

func TestASARRejectsInvalidBounds(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"negative offset":    func(e map[string]any) { e["offset"] = "-1" },
		"overflow offset":    func(e map[string]any) { e["offset"] = "9223372036854775808" },
		"outside archive":    func(e map[string]any) { e["offset"] = "9223372036854775807" },
		"negative size":      func(e map[string]any) { e["size"] = -1 },
		"oversized metadata": func(e map[string]any) { e["size"] = maxAppMetadataBytes + 1 },
		"truncated metadata": func(e map[string]any) { e["size"] = 100 },
		"unpacked":           func(e map[string]any) { e["unpacked"] = true },
		"link":               func(e map[string]any) { e["link"] = "../../secret" },
		"missing offset":     func(e map[string]any) { delete(e, "offset") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "app.asar")
			if err := os.WriteFile(path, fixtureASAR(t, []byte(`{}`), mutate), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readASARPackage(path); err == nil {
				t.Fatal("accepted invalid archive")
			}
		})
	}
	for _, size := range []uint32{0, 7, maxASARHeaderBytes + 1, ^uint32(0)} {
		data := fixtureASAR(t, []byte(`{}`), nil)
		binary.LittleEndian.PutUint32(data[4:8], size)
		path := filepath.Join(t.TempDir(), "app.asar")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readASARPackage(path); err == nil {
			t.Fatalf("accepted header size %d", size)
		}
	}
}
