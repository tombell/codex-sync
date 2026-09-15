# Linux host investigation

Inspected `mythra.local` over SSH on 2026-09-15. This documents the observed Linux desktop build and the changes needed in codex-sync. Linux syncing is implemented on the Linux support branch. The findings below record the investigation that informed it.

## Verified installation

Mythra runs Linux x86_64. The installed CLI is Arch's `openai-codex` package, version `0.154.0-1`, at `/usr/bin/codex`.

The desktop is a separate application. It was installed as `chatgpt-desktop` version `26.908.40834-1`, then removed on September 13. A desktop process was still running during inspection with `/usr/lib/chatgpt/ChatGPT (deleted)` as its executable. The normal installation directory no longer exists. No package was reinstalled and no application settings were changed.

The running process retained an open `resources/app.asar`. Reading that archive through its `/proc/<pid>/fd/` entry established these package.json fields:

| Field | Value |
| --- | --- |
| `name` | `openai-codex-electron` |
| `productName` | `Codex` |
| `version` | `26.908.40834` |
| `codexBuildNumber` | `8881` |
| `codexBuildFlavor` | `prod` |
| `desktopName` | `chatgpt.desktop` |

The cached Arch package and Debian source package remain under `~/.cache/yay/chatgpt-desktop/`. The cached launcher executes `ChatGPT` beside itself and reads optional flags from `$XDG_CONFIG_HOME/chatgpt-flags.conf`, falling back to `~/.config/chatgpt-flags.conf`.

These paths describe this package, not every Linux packaging format. Deleted process files are useful investigation evidence, not an installation-discovery strategy.

## Settings storage

`CODEX_HOME` was unset and `XDG_CONFIG_HOME` was `/home/tombell/.config` in the SSH environment.

| Path | Observed purpose |
| --- | --- |
| `~/.codex/config.toml` | CLI configuration and desktop preferences under `[desktop]` |
| `~/.codex/.codex-global-state.json` | Desktop state, migration markers, projects, and persisted atoms |
| `~/.config/Codex/` | Electron/Chromium profile, browser state, caches, and telemetry data |
| `~/.codex/keybindings.json` | Custom shortcuts, confirmed in app code; absent on this host |

The live TOML contains `desktop.followUpQueueMode`, `desktop.appearanceDarkCodeThemeId`, and the dark chrome theme object. The shared app code resolves `CODEX_HOME` or falls back to `$HOME/.codex`.

The app's settings registry still describes legacy configuration, global-state, and persisted-atom storage. Its desktop settings writer migrates those values and writes `desktop.<key>` through `batchWriteConfigValues`. Reading only the legacy registry storage labels would give the wrong current destination.

The shortcut parser reads an array of `{command, key}` objects, with a string or null key. It understands `CmdOrCtrl`, and the event parser maps that modifier to Command on macOS and Control elsewhere. Literal `Cmd` remains a Meta modifier; blindly copying it does not turn it into Control.

The observed theme schema also accepts `accentSource` with `chatgpt` or `custom`. Mythra has this field, but codex-sync does not currently allowlist it. The registry includes additional font fields, including font-face descriptors. These need a separate compatibility review before expanding the allowlist.

Keep the existing exclusions for credentials, sessions, projects, device state, and downloaded assets. Do not sync the whole Electron profile or global-state object. Profile TOML files and user rule behavior were not independently exercised on Linux during this inspection.

## Evidence locations

- Live configuration keys and JSON object keys, inspected without printing credential values or conversation contents.
- `/var/log/pacman.log`, which records installation and removal of `chatgpt-desktop`.
- Running desktop archive `package.json`.
- Archive `.vite/build/src-CCXHtyvY.js`, containing the settings schema and shortcut parser.
- Archive `.vite/build/window-all-closed-BxbCP6YG.js`, containing desktop config writes and legacy migration.
- Archive `.vite/build/main-DaMR-wdT.js`, containing platform-specific shortcut modifier handling.
- Local `internal/settingssync/stores.go`, `types.go`, `commands.go`, `bundle.go`, `specs.go`, and `Makefile`.

## Implemented behavior

- Default Linux installation directory `/usr/lib/chatgpt`, with `--app-path` for other locations.
- Bounded ASAR metadata parsing, verified package name, and required version/build and executable.
- Independent `--source-app-path` and `source_app_path` overrides.
- Export schema 4 includes `source_platform`; version and build equality remain required.
- Cross-platform pulls preserve all target custom shortcuts, font families, and open-in defaults. Pulls involving Linux preserve Mac menu-bar, Dock, and font-smoothing preferences. Profile handling follows the same rules.
- Both light and dark `accentSource` fields are allowlisted with the observed enum.

The implementation does not install or repair the desktop app. Mythra's deleted installation requires reinstalling before normal live syncing.

## Validation

The complete settingssync and CLI test suites passed on Mythra using cross-compiled Linux amd64 test binaries and temporary synthetic settings. These cover export filtering, audit, dry-run, apply, rollback, malformed metadata, platform filtering, and profile preservation. macOS tests also passed with the race detector, and `go vet ./...` passed.

A real SSH Linux-to-macOS pull also passed dry-run, apply, status, and rollback using synthetic settings. The Linux source read Mythra's retained app archive through temporary symlinks and reported version `26.908.40834`, build `8881`. The macOS target used fixture app metadata with that release. Rollback restored the target fixture files byte-for-byte. No live user preferences were applied. `make prod` built all four macOS/Linux architecture targets; Linux arm64 was cross-compiled, not run.
