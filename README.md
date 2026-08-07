# ChatGPT settings sync

`chatgpt-settings-sync` is a manual, pull-only macOS tool for copying a strict allowlist of ChatGPT/Codex desktop preferences from Pyra to another Mac. It never pushes target state back to Pyra.

The implementation is a dependency-free Go command. It does not require Python or modify a complete mixed-purpose state store.

## Architecture and safety model

Pyra is the canonical source. A target asks Pyra over SSH to run `chatgpt-settings-sync export`; the sanitized JSON bundle is streamed into a private temporary staging directory on the target. A single bounded stream avoids creating remote temporary data, so `rsync` is unnecessary for the small bundle.

Before a normal pull, the target:

1. validates the bundle schema, exact allowlist, content hash, tool version, bundle ID, and exact ChatGPT version/build;
2. compares only allowlisted values and prints names/actions with values redacted;
3. refuses to continue while ChatGPT is running;
4. creates a timestamped private backup;
5. renders every target file before changing anything;
6. replaces files atomically while preserving existing ownership and modes;
7. restores the backup if an apply is interrupted by an error, `SIGINT`, `SIGTERM`, or `SIGHUP`.

An advisory lock prevents concurrent operations. SSH uses batch mode, a connection timeout, a fixed remote executable path, and a 1 MiB maximum bundle size. Pyra being unavailable never changes local settings.

## Synced preferences

The version-one allowlist contains:

- `~/.codex/config.toml`: personality; composer Enter behavior; follow-up behavior; sleep prevention; menu-bar and context-window toggles; notifications; suggested prompts; appearance mode, colors, font names and sizes, code themes, diff markers, reduced motion, font smoothing and pointer cursors; Browser link targets and screenshot behavior; Computer Use picture-in-picture behavior.
- `~/.codex/.codex-global-state.json`: only the Browser and Computer Use bundled-plugin auto-install flags. The mixed-purpose file is edited key-by-key and never copied wholesale.
- `~/.codex/keybindings.json`: validated custom bindings from an explicit command-ID allowlist.

Absent allowlisted values are represented explicitly in the bundle, allowing a target override to be cleared when Pyra uses the application default.

The tool does not sync auth, Keychain data, cookies, browser logins, chats, sessions, drafts, projects, archives, histories, memories, goals, queues, logs, model caches, installation/device IDs, remote-control state, TCC permissions, browser profiles, downloaded plugin caches, `AGENTS.md`, `~/.agents/skills`, databases, or custom theme/pet assets.

Browser site allow/block lists and notification-permission prompt state are excluded because they have not been mapped to safe dedicated local preferences.

## Build and install

Requirements:

- macOS
- Go 1.22 or newer
- SSH access from each target Mac to Pyra
- `/Applications/ChatGPT.app`

Build and test:

```sh
make test
make build
```

Install the same revision on Pyra and each target:

```sh
make install
```

This writes the binary to the fixed path used by remote pulls:

```text
/Users/tombell/.local/bin/chatgpt-settings-sync
```

## First pull

Run a dry run on a target while ChatGPT is open or closed:

```sh
chatgpt-settings-sync pull pyra --dry-run
```

For a normal pull, quit ChatGPT manually first:

```sh
chatgpt-settings-sync pull pyra
```

The tool never quits or restarts ChatGPT. It refuses a live apply.

Compare without applying:

```sh
chatgpt-settings-sync status pyra
```

`status` exits `0` when settings match and `1` when changes are available.

On Pyra, generate a sanitized export:

```sh
umask 077
chatgpt-settings-sync export > /tmp/chatgpt-settings.json
```

Exports contain allowed preference values. Treat them as private even though they contain no authentication, session, or history state.

## Audit, backup, and rollback

Audit prints allowlisted paths, presence, and hashes without values:

```sh
chatgpt-settings-sync audit
```

Unknown desktop setting paths or shortcut command IDs are reported and never exported. `audit` exits `2` when it finds any.

Completed pulls store private backups below:

```text
~/.local/state/chatgpt-settings-sync/backups/
```

Backups are local recovery data and contain complete pre-change copies of the three touched local files. Quit ChatGPT and restore the newest completed, not-yet-rolled-back backup with:

```sh
chatgpt-settings-sync rollback
```

## Adding a preference safely

1. Read the current official Settings and config documentation.
2. Verify the installed bundle ID/version and locate the preference through key/type/hash inspection or a one-setting before/after comparison.
3. Add one exact path and its type, enum/range, or string constraint to `configSpecs` or `globalSpecs`. For shortcuts, add only a verified configurable command ID.
4. Add intentionally machine-local or out-of-scope desktop keys to `knownExcludedDesktopPaths`.
5. Extend fixtures with the allowed value and sensitive decoys.
6. Verify export filtering, unknown-key rejection, dry-run, backup, interruption recovery, rollback, and local round-trip behavior.
7. Run `chatgpt-settings-sync audit` on Pyra before installing on targets.

Never broaden an allowlist based only on a suggestive key name.

## Troubleshooting

### SSH failures

Check the alias and fixed remote command:

```sh
ssh -T pyra /Users/tombell/.local/bin/chatgpt-settings-sync audit
```

Pulls use `BatchMode=yes` and will not wait for a password or host-key prompt. Resolve those interactively before retrying.

### Version mismatch

Normal pulls require the same ChatGPT version/build and the same tool version on Pyra and the target. Update both application installations, build the same project revision on both Macs, run `audit`, and retry.

### ChatGPT is running

Dry runs and status checks are safe. Normal pulls and rollback require ChatGPT to be fully quit so in-memory settings cannot overwrite the applied files.

### Unknown settings

Run `audit`. Unknown keys are reported by name and remain untouched. Follow the allowlist-extension process rather than copying their containing file.

## Tests

```sh
go test ./...
```

The fixture suite proves:

- only allowlisted values enter exports;
- auth, session, history and device decoys never enter exports;
- dry runs perform no settings or backup writes;
- unknown bundle keys and shortcut commands are rejected;
- unknown local shortcuts block a normal pull;
- backups and rollback restore original files;
- interrupted applies leave no partially written settings;
- a local round-trip preserves allowed preferences and unrelated target state.

Fixtures and temporary directories are used exclusively. Tests never write Pyra's live settings.

## Future work

- Automated `launchd` pulls.
- Syncing selected Codex configuration and plugin choices.
- Custom theme and pet assets.
- Per-host overrides.
- Detection of preference changes introduced by ChatGPT updates.
