package settingssync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Runner struct {
	Layout            Layout
	Stdout            io.Writer
	Stderr            io.Writer
	Fetch             func(string, string, remoteExportOptions) (Bundle, error)
	AppRunning        func(string) (bool, error)
	SSHUser           string
	Version           string
	SourceCodexHome   string
	SourceBinary      string
	SourceShell       string
	SSHConnectTimeout time.Duration
	ExportTimeout     time.Duration
}

const (
	defaultSourceBinary      = "codex-sync"
	defaultSSHConnectTimeout = 10 * time.Second
	defaultExportTimeout     = time.Minute
)

type remoteExportOptions struct {
	AppPath           string
	CodexHome         string
	Binary            string
	Shell             string
	SSHConnectTimeout time.Duration
	ExportTimeout     time.Duration
}

func NewRunner(layout Layout, stdout, stderr io.Writer, version string) Runner {
	return Runner{
		Layout:            layout,
		Stdout:            stdout,
		Stderr:            stderr,
		Fetch:             fetchBundle,
		AppRunning:        processIsRunning,
		SSHUser:           os.Getenv("USER"),
		Version:           version,
		SourceBinary:      defaultSourceBinary,
		SSHConnectTimeout: defaultSSHConnectTimeout,
		ExportTimeout:     defaultExportTimeout,
	}
}

func (runner Runner) Run(args []string) int {
	unlock, err := operationLock()
	if err != nil {
		fmt.Fprintf(runner.Stderr, "codex-sync: %v\n", err)
		return 1
	}
	defer unlock()

	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printUsage(runner.Stdout)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	var runErr error
	var code int
	switch args[0] {
	case "export":
		if len(args) != 1 {
			runErr = fmt.Errorf("usage: codex-sync export")
			break
		}
		var bundle Bundle
		bundle, runErr = buildBundle(runner.Layout, runner.Version)
		if runErr == nil {
			var data []byte
			data, runErr = canonicalJSON(bundle)
			if runErr == nil {
				_, runErr = runner.Stdout.Write(append(data, '\n'))
			}
		}
	case "pull":
		target, dryRun, parseErr := parseRemoteArgs(args[1:], runner.SSHUser, true)
		if parseErr != nil {
			runErr = parseErr
			break
		}
		code, runErr = runner.runPull(target, dryRun)
	case "status":
		target, _, parseErr := parseRemoteArgs(args[1:], runner.SSHUser, false)
		if parseErr != nil {
			runErr = parseErr
			break
		}
		code, runErr = runner.runStatus(target)
	case "audit":
		if len(args) != 1 {
			runErr = fmt.Errorf("usage: codex-sync audit")
			break
		}
		code, runErr = runner.runAudit()
	case "rollback":
		if len(args) != 1 {
			runErr = fmt.Errorf("usage: codex-sync rollback")
			break
		}
		app, appErr := getAppInfo(runner.Layout)
		if appErr != nil {
			runErr = appErr
			break
		}
		running, checkErr := runner.AppRunning(app.Executable)
		if checkErr != nil {
			runErr = checkErr
			break
		}
		if running {
			runErr = fmt.Errorf("Codex desktop is running; quit it before rollback")
			break
		}
		var backup string
		backup, runErr = rollback(runner.Layout)
		if runErr == nil {
			fmt.Fprintf(runner.Stdout, "Restored backup: %s\n", backup)
		}
	default:
		runErr = fmt.Errorf("unknown command %q", args[0])
	}
	if runErr != nil {
		fmt.Fprintf(runner.Stderr, "codex-sync: %v\n", runErr)
		return 1
	}
	return code
}

type sshTarget struct {
	Host string
	User string
}

func parseRemoteArgs(args []string, defaultUser string, allowDryRun bool) (sshTarget, bool, error) {
	usage := "usage: codex-sync status <host> [--user <user>]"
	if allowDryRun {
		usage = "usage: codex-sync pull <host> [--user <user>] [--dry-run]"
	}
	target := sshTarget{User: defaultUser}
	dryRun := false
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch argument {
		case "--dry-run":
			if !allowDryRun {
				return sshTarget{}, false, fmt.Errorf("%s", usage)
			}
			dryRun = true
		case "--user", "-u":
			index++
			if index == len(args) {
				return sshTarget{}, false, fmt.Errorf("%s", usage)
			}
			target.User = args[index]
		default:
			if strings.HasPrefix(argument, "--user=") {
				target.User = strings.TrimPrefix(argument, "--user=")
				continue
			}
			if strings.HasPrefix(argument, "-") || target.Host != "" {
				return sshTarget{}, false, fmt.Errorf("%s", usage)
			}
			target.Host = argument
		}
	}
	if target.Host == "" {
		return sshTarget{}, false, fmt.Errorf("%s", usage)
	}
	if strings.ContainsAny(target.Host, " \t\x00\r\n") {
		return sshTarget{}, false, fmt.Errorf("SSH host contains unsupported characters")
	}
	if target.User == "" {
		return sshTarget{}, false, fmt.Errorf("SSH user is empty; set USER or pass --user")
	}
	if strings.Contains(target.Host, "@") {
		return sshTarget{}, false, fmt.Errorf("SSH host must not include a user; pass --user instead")
	}
	if strings.HasPrefix(target.User, "-") || strings.ContainsAny(target.User, "@ \t\x00\r\n") {
		return sshTarget{}, false, fmt.Errorf("SSH user contains unsupported characters")
	}
	return target, dryRun, nil
}

func (runner Runner) runPull(target sshTarget, dryRun bool) (int, error) {
	bundle, err := runner.Fetch(target.Host, target.User, runner.remoteExportOptions())
	if err != nil {
		return 1, err
	}
	app, err := getAppInfo(runner.Layout)
	if err != nil {
		return 1, err
	}
	content, err := validateBundle(bundle, app, runner.Version)
	if err != nil {
		return 1, err
	}
	current, err := buildContent(runner.Layout, false)
	if err != nil {
		return 1, err
	}
	changes := comparePreferences(current.Preferences, content.Preferences)
	printChanges(runner.Stdout, changes)
	printUnknownReport(runner.Stdout, "Source", content.Audit)
	printUnknownReport(runner.Stdout, "Local audit", current.Audit)
	if dryRun {
		fmt.Fprintln(runner.Stdout, "Dry run only; no local settings or backups were written.")
		return 0, nil
	}
	if len(changes) == 0 {
		return 0, nil
	}
	running, err := runner.AppRunning(app.Executable)
	if err != nil {
		return 1, err
	}
	if running {
		return 1, fmt.Errorf("Codex desktop is running; quit it before applying settings")
	}
	if len(current.Audit.UnknownKeybindingCommands) > 0 {
		return 1, fmt.Errorf("local keybindings contain unknown commands; audit and update the allowlist first")
	}
	backup, err := applyPreferences(runner.Layout, content.Preferences, 0)
	if err != nil {
		return 1, err
	}
	fmt.Fprintf(runner.Stdout, "Applied settings atomically. Backup: %s\n", backup)
	fmt.Fprintln(runner.Stdout, "Rollback with: codex-sync rollback")
	return 0, nil
}

func (runner Runner) runStatus(target sshTarget) (int, error) {
	bundle, err := runner.Fetch(target.Host, target.User, runner.remoteExportOptions())
	if err != nil {
		return 1, err
	}
	app, err := getAppInfo(runner.Layout)
	if err != nil {
		return 1, err
	}
	content, err := validateBundle(bundle, app, runner.Version)
	if err != nil {
		return 1, err
	}
	current, err := buildContent(runner.Layout, false)
	if err != nil {
		return 1, err
	}
	changes := comparePreferences(current.Preferences, content.Preferences)
	printChanges(runner.Stdout, changes)
	printUnknownReport(runner.Stdout, "Source", content.Audit)
	printUnknownReport(runner.Stdout, "Local audit", current.Audit)
	if len(changes) > 0 {
		return 1, nil
	}
	return 0, nil
}

func (runner Runner) runAudit() (int, error) {
	app, err := getAppInfo(runner.Layout)
	if err != nil {
		return 1, err
	}
	content, err := buildContent(runner.Layout, false)
	if err != nil {
		return 1, err
	}
	fmt.Fprintf(runner.Stdout, "ChatGPT %s (%s) [%s]\n", app.Version, app.Build, app.BundleID)
	for _, store := range []struct {
		name    string
		entries map[string]Entry
	}{{"config_toml", content.Preferences.ConfigToml}, {"global_state", content.Preferences.GlobalState}} {
		fmt.Fprintf(runner.Stdout, "%s allowlist:\n", store.name)
		paths := make([]string, 0, len(store.entries))
		for path := range store.entries {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			entry := store.entries[path]
			if !entry.Present {
				fmt.Fprintf(runner.Stdout, "  %s: absent\n", path)
				continue
			}
			hash, err := redactedValueHash(entry.Value)
			if err != nil {
				return 1, err
			}
			fmt.Fprintf(runner.Stdout, "  %s: present sha256=%s\n", path, hash)
		}
	}
	fmt.Fprintf(runner.Stdout, "keybindings: %d custom entries\n", len(content.Preferences.Keybindings.Bindings))
	fmt.Fprintf(runner.Stdout, "config profiles: %d with allowlisted settings\n", len(content.Preferences.ConfigProfiles))
	fmt.Fprintf(runner.Stdout, "rules: %d files\n", len(content.Preferences.Rules))
	for _, path := range content.Audit.UnknownConfigPaths {
		fmt.Fprintf(runner.Stdout, "UNKNOWN config setting (not synced): %s\n", path)
	}
	for _, command := range content.Audit.UnknownKeybindingCommands {
		fmt.Fprintf(runner.Stdout, "UNKNOWN keybinding command (not synced): %s\n", command)
	}
	fmt.Fprintf(runner.Stdout, "Excluded mixed-purpose entries: config=%d global_state=%d\n", content.Audit.ExcludedConfigCount, content.Audit.ExcludedGlobalStateCount)
	if len(content.Audit.UnknownConfigPaths)+len(content.Audit.UnknownKeybindingCommands) > 0 {
		return 2, nil
	}
	return 0, nil
}

func printChanges(writer io.Writer, changes []string) {
	if len(changes) == 0 {
		fmt.Fprintln(writer, "Settings already match the source.")
		return
	}
	fmt.Fprintln(writer, "Proposed changes (values redacted):")
	for _, change := range changes {
		parts := strings.SplitN(change, ":", 3)
		fmt.Fprintf(writer, "  %s: %s (%s)\n", parts[0], parts[1], parts[2])
	}
}

func printUnknownReport(writer io.Writer, label string, audit Audit) {
	count := len(audit.UnknownConfigPaths) + len(audit.UnknownKeybindingCommands)
	if count > 0 {
		fmt.Fprintf(writer, "%s reports %d unknown setting(s); none will be synced.\n", label, count)
	}
}

func (runner Runner) remoteExportOptions() remoteExportOptions {
	return remoteExportOptions{
		AppPath:           runner.Layout.AppPath,
		CodexHome:         runner.SourceCodexHome,
		Binary:            runner.SourceBinary,
		Shell:             runner.SourceShell,
		SSHConnectTimeout: runner.SSHConnectTimeout,
		ExportTimeout:     runner.ExportTimeout,
	}
}

func fetchBundle(host, user string, options remoteExportOptions) (Bundle, error) {
	ctx, cancel := context.WithTimeout(context.Background(), options.exportTimeout())
	defer cancel()
	command := sshExportCommand(ctx, host, user, options)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Bundle{}, fmt.Errorf("prepare SSH to source: %w", err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return Bundle{}, fmt.Errorf("could not start SSH to source: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, MaxBundleBytes+1))
	if len(data) > MaxBundleBytes {
		_ = command.Process.Kill()
		_ = command.Wait()
		return Bundle{}, fmt.Errorf("source export exceeded the maximum safe bundle size")
	}
	waitErr := command.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		return Bundle{}, fmt.Errorf("source export timed out")
	}
	if readErr != nil {
		return Bundle{}, fmt.Errorf("read source export: %w", readErr)
	}
	if waitErr != nil {
		return Bundle{}, fmt.Errorf("source export failed over SSH")
	}

	staging, err := os.MkdirTemp("", "codex-sync.pull.*")
	if err != nil {
		return Bundle{}, err
	}
	defer os.RemoveAll(staging)
	if err := os.Chmod(staging, 0o700); err != nil {
		return Bundle{}, err
	}
	bundlePath := filepath.Join(staging, "bundle.json")
	if err := atomicWrite(bundlePath, data, ""); err != nil {
		return Bundle{}, err
	}
	staged, err := os.ReadFile(bundlePath)
	if err != nil {
		return Bundle{}, err
	}
	return decodeBundle(staged)
}

func sshExportCommand(ctx context.Context, host, user string, options remoteExportOptions) *exec.Cmd {
	innerCommand := "exec " + quoteShellArgument(options.Binary) + " --app-path " + quoteShellArgument(options.AppPath)
	if options.CodexHome != "" {
		innerCommand += " --codex-home " + quoteShellArgument(options.CodexHome)
	}
	innerCommand += " export"
	shell := `"$SHELL"`
	if options.Shell != "" {
		shell = quoteShellArgument(options.Shell)
	}
	remoteCommand := "exec " + shell + " -lc " + quoteShellArgument(innerCommand)
	connectTimeout := strconv.FormatInt(int64(options.sshConnectTimeout()/time.Second), 10)
	return exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout="+connectTimeout, "-l", user, host, remoteCommand)
}

func (options remoteExportOptions) sshConnectTimeout() time.Duration {
	if options.SSHConnectTimeout > 0 {
		return options.SSHConnectTimeout
	}
	return defaultSSHConnectTimeout
}

func (options remoteExportOptions) exportTimeout() time.Duration {
	if options.ExportTimeout > 0 {
		return options.ExportTimeout
	}
	return defaultExportTimeout
}

func quoteShellArgument(argument string) string {
	return "'" + strings.ReplaceAll(argument, "'", `'"'"'`) + "'"
}

func processIsRunning(executable string) (bool, error) {
	command := processLookupCommand(executable)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err == nil {
		return true, nil
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("check whether %s is running: %w", executable, err)
	}
}

func processLookupCommand(executable string) *exec.Cmd {
	return exec.Command("pgrep", "-x", regexp.QuoteMeta(executable))
}

func operationLock() (func(), error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("codex-sync-%d.lock", os.Getuid()))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another codex-sync operation is running")
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `Usage:
  codex-sync export
  codex-sync pull <host> [--user <user>] [--dry-run]
  codex-sync status <host> [--user <user>]
  codex-sync audit
  codex-sync rollback
  codex-sync --version

Global options:
  --app-path <path>          Application bundle path; forwarded to sources.
  --codex-home <path>        Local Codex configuration root.
  --source-codex-home <path> Source configuration root for pull/status.
  --source-binary <path>     Remote codex-sync executable for pull/status.
  --source-shell <path>      Remote login shell for pull/status.
  --ssh-connect-timeout <d>  SSH connection timeout (default 10s).
  --export-timeout <d>       Total source export timeout (default 1m).

SSH user defaults to $USER. Override it with --user <user> or -u <user>.`)
}
