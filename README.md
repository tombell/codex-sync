# codex-sync

Pull selected Codex desktop settings between macOS and Linux hosts.

Each operation is deliberately one-way: the local host pulls settings from the source host you name.

## Setup

You need macOS or Linux, the Codex desktop application, and SSH access from the target host to the source host. Linux also requires `pgrep`, usually provided by procps. The source can be an SSH hostname or an alias from `~/.ssh/config`.

On macOS, install `codex-sync` from the `tombell/formulae` tap:

```sh
brew install tombell/formulae/codex-sync
```

On Linux, build and install it with Go 1.22 or later:

```sh
go build -o codex-sync ./cmd/codex-sync
install -Dm755 codex-sync ~/.local/bin/codex-sync
```

Install the same codex-sync version on the source and target hosts. `~/.local/bin` must be on the remote login shell's `PATH`, or you can pass `--source-binary`.

Remote pulls resolve `codex-sync` from the source host's login-shell `PATH`. A standard Homebrew shell setup makes the installed binary available without an additional symlink.

For a nonstandard remote installation, use `--source-binary <absolute-path>`. The remote login shell defaults to `$SHELL`; override it with `--source-shell <absolute-path>`.

SSH connections time out after `10s` and the complete remote export after `1m` by default. Override them with `--ssh-connect-timeout <duration>` and `--export-timeout <duration>`; values must be whole seconds from `1s` to `1h`.

SSH uses `ssh_user` from the configuration file, then the local `$USER`, as the remote login name by default. If the source uses a different account, pass `--user <user>` (or `-u <user>`).

The local app directory defaults to `/Applications/ChatGPT.app` on macOS and `/usr/lib/chatgpt` on Linux. Use `--app-path <absolute-path>` for another local installation. Linux support expects the packaged `ChatGPT` executable and `resources/app.asar` inside that directory. CLI-only installations are not supported.

Each source uses its own platform default or its own configured `app_path`. Use `--source-app-path <absolute-path>` to override the remote location. `--app-path` now applies only locally; configurations that previously relied on forwarding it must also set `source_app_path`.

Settings default to `~/.codex` on each host and honor that machine's `CODEX_HOME` when set. Use `--codex-home <absolute-path>` to override the local settings root. For a one-off remote override during pull or status, use `--source-codex-home <absolute-path>`.

Backups default to `$XDG_STATE_HOME/codex-sync/backups/` when `XDG_STATE_HOME` is set, or `~/.local/state/codex-sync/backups/` otherwise. Use `--state-home <absolute-path>` to override the local state root.

Check that the same version is installed on every host with `codex-sync --version`.

## Configuration

`codex-sync` loads `$XDG_CONFIG_HOME/codex-sync/config.toml`, falling back to `~/.config/codex-sync/config.toml`. The default file is optional. Use `--config <absolute-path>` to load another file, or `--no-config` to disable configuration loading.

```toml
app_path = "/Applications/ChatGPT.app"
codex_home = "/Users/alice/.codex"
state_home = "/Users/alice/.local/state"

ssh_user = "alice"
source_app_path = "/usr/lib/chatgpt"
source_codex_home = "/home/alice/.codex"
source_binary = "/home/alice/.local/bin/codex-sync"
source_shell = "/bin/bash"

ssh_connect_timeout = "10s"
export_timeout = "1m"
```

Every key is optional. Paths must be absolute, durations must be whole seconds from `1s` to `1h`, and unknown keys are rejected. Command-line flags take precedence over the configuration file, which takes precedence over `USER`, `CODEX_HOME`, and `XDG_STATE_HOME`; built-in defaults apply last. `--user` overrides `ssh_user` for an individual pull or status command.

## Usage

Preview a pull:

```sh
codex-sync pull source-mac --dry-run
```

Quit Codex desktop, then apply it:

```sh
codex-sync pull source-mac
```

Other commands:

```sh
codex-sync status source-host                # exit 1 when changes are available
codex-sync pull linux-host --source-app-path /opt/chatgpt --dry-run
codex-sync pull source-mac --user other-user # override the SSH user
codex-sync audit --app-path "/Applications/ChatGPT Beta.app"
codex-sync pull source-mac --codex-home /Volumes/settings/codex \
  --source-codex-home /Users/other-user/.codex-preview
codex-sync status source-mac --source-binary /opt/homebrew/bin/codex-sync \
  --source-shell /bin/zsh
codex-sync status source-mac --ssh-connect-timeout 20s --export-timeout 2m
codex-sync audit                             # exit 2 for unknown settings or commands
codex-sync rollback                          # restore the latest completed backup
codex-sync rollback --state-home /Volumes/settings/state
```

`status` and dry runs are safe while Codex desktop is open. Pulls and rollbacks require it to be fully quit.

To inspect the sanitized data produced on a source host:

```sh
codex-sync export
```

## What gets synced

Settings are collected from these files under `$CODEX_HOME` (default `~/.codex`):

- `config.toml`: model, reasoning, and capability defaults; Git preferences; desktop appearance and behavior (including plain-text composition, educational tips, review delivery, terminal placement, and questions outside Plan mode); notifications; Browser; and Computer Use preferences.
- `*.config.toml`: the same allowlisted settings for named profiles.
- `.codex-global-state.json`: Browser and Computer Use plugin auto-install flags.
- `keybindings.json`: custom bindings for known command IDs.
- `rules/*.rules`: the complete set of user command rules.

Unrelated values in the config, profile, and global-state files are left alone. Missing allowlisted values are also synced, so a target override can be reset to the application default. Rules are synced exactly: target-only `.rules` files are removed, while unrelated files in the rules directory are untouched.

Dock icon preference is limited to the app's canonical icon modes. Selected avatar IDs are limited to built-in companions; downloaded or custom avatar assets are never included. When the source selects a custom avatar, that preference is skipped and the target's avatar is preserved, including in named profiles.

Auth, chats, sessions, history, projects, device state, browser data, permission profiles, skills, and downloaded assets are not synced.

### Platform differences

Mac-to-Linux and Linux-to-Mac pulls preserve the target's custom keybindings, theme font families, and default open-in target. Shortcut modifiers and installed applications differ between platforms. No shortcut translation is attempted. Same-platform pulls still sync these preferences.

When either host is Linux, menu-bar visibility, Dock icon preference, and font smoothing remain unchanged on the target. This also applies to named profiles, including profiles absent on the source. Shared settings retain the usual reset-to-default behavior.

Linux desktop version and build come from `package.json` in `resources/app.asar`. Both must match the target, including for cross-platform pulls. This release uses export schema 5; update codex-sync on both hosts before syncing.

The verified Linux layout and storage details are in [Linux host investigation](docs/linux-hosts.md).

## Safety

Before applying a pull, `codex-sync`:

- checks the bundle schema and hash;
- requires matching tool and ChatGPT versions on both hosts;
- prints a redacted diff;
- rejects unknown exported settings and shortcut commands;
- creates a backup under `$XDG_STATE_HOME/codex-sync/backups/` (default `~/.local/state/codex-sync/backups/`);
- writes files atomically and restores the backup if the apply fails or is interrupted.

It also prevents concurrent operations and limits SSH exports to 1 MiB. If the source cannot be reached, local settings are not changed.

Backups contain complete copies of the affected local files, including rules and profiles, and should be treated as private.

## Adding a setting

1. Confirm the setting's path and type from official documentation, the installed app's settings registry, or a one-setting before/after comparison.
2. Add it to `configSpecs` or `globalSpecs`. Profile values use `configSpecs` automatically. For shortcuts, add only a verified command ID.
3. Add fixtures covering the setting and sensitive decoy values.
4. Test export filtering, audit, dry-run, apply, and rollback, then run `codex-sync audit` on the source host.

Do not extend an allowlist based only on a plausible key name.

## Development

```sh
go test ./...
make                 # current platform
make prod            # macOS and Linux, amd64 and arm64 binaries
```

Tests use fixtures and temporary directories; they do not touch live Codex settings.

The desktop fields added for ChatGPT 26.901.51231 (build 8109) were checked against the bundled `app.asar` settings registry: `composerPlainTextMode` and `show-educational-tips` are booleans, `reviewDelivery` accepts `inline` or `detached`, `defaultTerminalLocation` accepts `bottom` or `right`, and `default-mode-request-user-input-enabled` is a boolean. These use the `desktop` configuration table and the same allowlist for named profiles.
