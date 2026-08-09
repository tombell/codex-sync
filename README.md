# codex-sync

Pull selected Codex desktop settings from one Mac to another.

Each operation is deliberately one-way: the local Mac pulls settings from the source Mac you name.

## Setup

You need macOS, the Codex desktop application, Homebrew, and SSH access from the target Mac to the source Mac. The source can be an SSH hostname or an alias from `~/.ssh/config`.

Install `codex-sync` from the `tombell/formulae` tap on the source and each target Mac:

```sh
brew install tombell/formulae/codex-sync
```

Remote pulls resolve `codex-sync` from the source Mac's login-shell `PATH`. A standard Homebrew shell setup makes the installed binary available without an additional symlink.

For a nonstandard remote installation, use `--source-binary <absolute-path>`. The remote login shell defaults to `$SHELL`; override it with `--source-shell <absolute-path>`.

SSH connections time out after `10s` and the complete remote export after `1m` by default. Override them with `--ssh-connect-timeout <duration>` and `--export-timeout <duration>`; values must be whole seconds from `1s` to `1h`.

SSH uses `ssh_user` from the configuration file, then the local `$USER`, as the remote login name by default. If the source uses a different account, pass `--user <user>` (or `-u <user>`).

The application bundle defaults to `/Applications/ChatGPT.app`. Override it with `--app-path <absolute-path>`. Pulls and status checks forward the override to the source Mac, so the application must be at that path on both Macs.

Settings default to `~/.codex` on each Mac and honor that machine's `CODEX_HOME` when set. Use `--codex-home <absolute-path>` to override the local settings root. For a one-off remote override during pull or status, use `--source-codex-home <absolute-path>`.

Backups default to `$XDG_STATE_HOME/codex-sync/backups/` when `XDG_STATE_HOME` is set, or `~/.local/state/codex-sync/backups/` otherwise. Use `--state-home <absolute-path>` to override the local state root.

Check that the same version is installed on every Mac with `codex-sync --version`.

## Configuration

`codex-sync` loads `$XDG_CONFIG_HOME/codex-sync/config.toml`, falling back to `~/.config/codex-sync/config.toml`. The default file is optional. Use `--config <absolute-path>` to load another file, or `--no-config` to disable configuration loading.

```toml
app_path = "/Applications/ChatGPT.app"
codex_home = "/Users/alice/.codex"
state_home = "/Users/alice/.local/state"

ssh_user = "alice"
source_codex_home = "/Users/alice/.codex"
source_binary = "/opt/homebrew/bin/codex-sync"
source_shell = "/bin/zsh"

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
codex-sync status source-mac                 # exit 1 when changes are available
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

To inspect the sanitized data produced on a source Mac:

```sh
codex-sync export
```

## What gets synced

Settings are collected from these files under `$CODEX_HOME` (default `~/.codex`):

- `config.toml`: model, reasoning, and capability defaults; Git preferences; desktop appearance and behavior; notifications; Browser; and Computer Use preferences.
- `*.config.toml`: the same allowlisted settings for named profiles.
- `.codex-global-state.json`: Browser and Computer Use plugin auto-install flags.
- `keybindings.json`: custom bindings for known command IDs.
- `rules/*.rules`: the complete set of user command rules.

Unrelated values in the config, profile, and global-state files are left alone. Missing allowlisted values are also synced, so a target override can be reset to the application default. Rules are synced exactly: target-only `.rules` files are removed, while unrelated files in the rules directory are untouched.

Dock icon preference is limited to the app's canonical icon modes. Selected avatar IDs are limited to built-in companions; downloaded or custom avatar assets are never included.

Auth, chats, sessions, history, projects, device state, browser data, permission profiles, skills, and downloaded assets are not synced.

## Safety

Before applying a pull, `codex-sync`:

- checks the bundle schema and hash;
- requires matching tool and ChatGPT versions on both Macs;
- prints a redacted diff;
- rejects unknown exported settings and shortcut commands;
- creates a backup under `$XDG_STATE_HOME/codex-sync/backups/` (default `~/.local/state/codex-sync/backups/`);
- writes files atomically and restores the backup if the apply fails or is interrupted.

It also prevents concurrent operations and limits SSH exports to 1 MiB. If the source cannot be reached, local settings are not changed.

Backups contain complete copies of the affected local files, including rules and profiles, and should be treated as private.

## Adding a setting

1. Confirm the setting's path and type from official documentation or a one-setting before/after comparison.
2. Add it to `configSpecs` or `globalSpecs`. Profile values use `configSpecs` automatically. For shortcuts, add only a verified command ID.
3. Add fixtures covering the setting and sensitive decoy values.
4. Test export filtering, audit, dry-run, apply, and rollback, then run `codex-sync audit` on the source Mac.

Do not extend an allowlist based only on a plausible key name.

## Development

```sh
go test ./...
make                 # current platform
make prod            # macOS binaries
```

Tests use fixtures and temporary directories; they do not touch live Codex settings.
