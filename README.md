# codex-sync

Pull selected Codex desktop settings from one Mac to another.

Each operation is deliberately one-way: the local Mac pulls settings from the source Mac you name.

## Setup

You need macOS, `/Applications/ChatGPT.app`, Homebrew, and SSH access from the target Mac to the source Mac. The source can be an SSH hostname or an alias from `~/.ssh/config`.

Install `codex-sync` from the `tombell/formulae` tap on the source and each target Mac:

```sh
brew install tombell/formulae/codex-sync
```

Remote pulls resolve `codex-sync` from the source Mac's login-shell `PATH`. A standard Homebrew shell setup makes the installed binary available without an additional symlink.

SSH uses the local `$USER` as the remote login name by default. If the source uses a different account, pass `--user <user>` (or `-u <user>`).

Check that the same version is installed on every Mac with `codex-sync --version`.

## Usage

Preview a pull:

```sh
codex-sync pull source-mac --dry-run
```

Quit ChatGPT, then apply it:

```sh
codex-sync pull source-mac
```

Other commands:

```sh
codex-sync status source-mac                 # exit 1 when changes are available
codex-sync pull source-mac --user other-user # override the SSH user
codex-sync audit                             # exit 2 for unknown settings or commands
codex-sync rollback                          # restore the latest completed backup
```

`status` and dry runs are safe while ChatGPT is open. Pulls and rollbacks require it to be fully quit.

To inspect the sanitized data produced on a source Mac:

```sh
codex-sync export
```

## What gets synced

Settings are collected from these files:

- `~/.codex/config.toml`: model, reasoning, and capability defaults; Git preferences; desktop appearance and behavior; notifications; Browser; and Computer Use preferences.
- `~/.codex/*.config.toml`: the same allowlisted settings for named profiles.
- `~/.codex/.codex-global-state.json`: Browser and Computer Use plugin auto-install flags.
- `~/.codex/keybindings.json`: custom bindings for known command IDs.
- `~/.codex/rules/*.rules`: the complete set of user command rules.

Unrelated values in the config, profile, and global-state files are left alone. Missing allowlisted values are also synced, so a target override can be reset to the application default. Rules are synced exactly: target-only `.rules` files are removed, while unrelated files in the rules directory are untouched.

Dock icon preference is limited to the app's canonical icon modes. Selected avatar IDs are limited to built-in companions; downloaded or custom avatar assets are never included.

Auth, chats, sessions, history, projects, device state, browser data, permission profiles, skills, and downloaded assets are not synced.

## Safety

Before applying a pull, `codex-sync`:

- checks the bundle schema and hash;
- requires matching tool and ChatGPT versions on both Macs;
- prints a redacted diff;
- rejects unknown exported settings and shortcut commands;
- creates a backup under `~/.local/state/codex-sync/backups/`;
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
