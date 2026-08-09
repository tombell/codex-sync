# codex-sync

Pull selected Codex desktop settings from Pyra to another Mac.

The tool is deliberately one-way: Pyra is the source, and other Macs only pull from it.

## Setup

You need macOS, `/Applications/ChatGPT.app`, Homebrew, and an SSH alias named `pyra`.

Install `codex-sync` from the `tombell/formulae` tap on Pyra and each target Mac:

```sh
brew install tombell/formulae/codex-sync
```

Remote pulls resolve `codex-sync` from Pyra's login-shell `PATH`. A standard Homebrew shell setup makes the installed binary available without an additional symlink.

Check that the same version is installed on every Mac with `codex-sync --version`.

## Usage

Preview a pull:

```sh
codex-sync pull pyra --dry-run
```

Quit ChatGPT, then apply it:

```sh
codex-sync pull pyra
```

Other commands:

```sh
codex-sync status pyra  # exit 1 when changes are available
codex-sync audit        # exit 2 for unknown settings or commands
codex-sync rollback     # restore the latest completed backup
```

`status` and dry runs are safe while ChatGPT is open. Pulls and rollbacks require it to be fully quit.

To inspect the sanitized data produced on Pyra:

```sh
codex-sync export
```

## What gets synced

Only allowlisted values from these files are included:

- `~/.codex/config.toml`: model, reasoning, and capability defaults; Git preferences; desktop appearance and behavior; notifications; Browser; and Computer Use preferences.
- `~/.codex/.codex-global-state.json`: Browser and Computer Use plugin auto-install flags.
- `~/.codex/keybindings.json`: custom bindings for known command IDs.

Unrelated values in the first two files are left alone. Missing values are also synced, so a target override can be reset to the application default.

Auth, chats, sessions, history, projects, device state, browser data, permissions, skills, and downloaded assets are not synced.

## Safety

Before applying a pull, `codex-sync`:

- checks the bundle schema and hash;
- requires matching tool and ChatGPT versions on both Macs;
- prints a redacted diff;
- rejects unknown exported settings and shortcut commands;
- creates a backup under `~/.local/state/codex-sync/backups/`;
- writes files atomically and restores the backup if the apply fails or is interrupted.

It also prevents concurrent operations and limits SSH exports to 1 MiB. If Pyra cannot be reached, local settings are not changed.

Backups contain complete copies of the affected local files and should be treated as private.

## Adding a setting

1. Confirm the setting's path and type from official documentation or a one-setting before/after comparison.
2. Add it to `configSpecs` or `globalSpecs`. For shortcuts, add only a verified command ID.
3. Add fixtures covering the setting and sensitive decoy values.
4. Test export filtering, audit, dry-run, apply, and rollback, then run `codex-sync audit` on Pyra.

Do not extend an allowlist based only on a plausible key name.

## Development

```sh
go test ./...
make                 # current platform
make prod            # macOS and Linux binaries
```

Tests use fixtures and temporary directories; they do not touch live Codex settings.
