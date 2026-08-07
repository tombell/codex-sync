package settingssync

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type Runner struct {
	Layout     Layout
	Stdout     io.Writer
	Stderr     io.Writer
	Fetch      func(string) (Bundle, error)
	AppRunning func() bool
	Hostname   func() string
}

func NewRunner(layout Layout, stdout, stderr io.Writer) Runner {
	return Runner{
		Layout:     layout,
		Stdout:     stdout,
		Stderr:     stderr,
		Fetch:      fetchBundle,
		AppRunning: chatgptIsRunning,
		Hostname:   localHostname,
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
		if runner.Hostname() != CanonicalHost {
			runErr = fmt.Errorf("export is restricted to the canonical Pyra host")
			break
		}
		var bundle Bundle
		bundle, runErr = buildBundle(runner.Layout)
		if runErr == nil {
			var data []byte
			data, runErr = canonicalJSON(bundle)
			if runErr == nil {
				_, runErr = runner.Stdout.Write(append(data, '\n'))
			}
		}
	case "pull":
		host, dryRun, parseErr := parsePullArgs(args[1:])
		if parseErr != nil {
			runErr = parseErr
			break
		}
		code, runErr = runner.runPull(host, dryRun)
	case "status":
		if len(args) != 2 {
			runErr = fmt.Errorf("usage: codex-sync status pyra")
			break
		}
		code, runErr = runner.runStatus(args[1])
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
		if runner.AppRunning() {
			runErr = fmt.Errorf("ChatGPT is running; quit it before rollback")
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

func parsePullArgs(args []string) (string, bool, error) {
	host := ""
	dryRun := false
	for _, argument := range args {
		switch argument {
		case "--dry-run":
			dryRun = true
		default:
			if strings.HasPrefix(argument, "-") || host != "" {
				return "", false, fmt.Errorf("usage: codex-sync pull pyra [--dry-run]")
			}
			host = argument
		}
	}
	if host == "" {
		return "", false, fmt.Errorf("usage: codex-sync pull pyra [--dry-run]")
	}
	return host, dryRun, nil
}

func (runner Runner) runPull(host string, dryRun bool) (int, error) {
	bundle, err := runner.Fetch(host)
	if err != nil {
		return 1, err
	}
	app, err := getAppInfo(runner.Layout)
	if err != nil {
		return 1, err
	}
	content, err := validateBundle(bundle, app)
	if err != nil {
		return 1, err
	}
	current, err := buildContent(runner.Layout, false)
	if err != nil {
		return 1, err
	}
	changes := comparePreferences(current.Preferences, content.Preferences)
	printChanges(runner.Stdout, changes)
	printUnknownReport(runner.Stdout, "Pyra", content.Audit)
	printUnknownReport(runner.Stdout, "Local audit", current.Audit)
	if dryRun {
		fmt.Fprintln(runner.Stdout, "Dry run only; no local settings or backups were written.")
		return 0, nil
	}
	if len(changes) == 0 {
		return 0, nil
	}
	if runner.AppRunning() {
		return 1, fmt.Errorf("ChatGPT is running; quit it before applying settings")
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

func (runner Runner) runStatus(host string) (int, error) {
	bundle, err := runner.Fetch(host)
	if err != nil {
		return 1, err
	}
	app, err := getAppInfo(runner.Layout)
	if err != nil {
		return 1, err
	}
	content, err := validateBundle(bundle, app)
	if err != nil {
		return 1, err
	}
	current, err := buildContent(runner.Layout, false)
	if err != nil {
		return 1, err
	}
	changes := comparePreferences(current.Preferences, content.Preferences)
	printChanges(runner.Stdout, changes)
	printUnknownReport(runner.Stdout, "Pyra", content.Audit)
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
		fmt.Fprintln(writer, "Settings already match Pyra.")
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

func fetchBundle(host string) (Bundle, error) {
	if strings.ToLower(host) != CanonicalHost {
		return Bundle{}, fmt.Errorf("pull source must be Pyra")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, "/Users/tombell/.local/bin/codex-sync", "export")
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Bundle{}, fmt.Errorf("prepare SSH to Pyra: %w", err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return Bundle{}, fmt.Errorf("could not start SSH to Pyra: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, MaxBundleBytes+1))
	if len(data) > MaxBundleBytes {
		_ = command.Process.Kill()
		_ = command.Wait()
		return Bundle{}, fmt.Errorf("Pyra export exceeded the maximum safe bundle size")
	}
	waitErr := command.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		return Bundle{}, fmt.Errorf("Pyra export timed out")
	}
	if readErr != nil {
		return Bundle{}, fmt.Errorf("read Pyra export: %w", readErr)
	}
	if waitErr != nil {
		return Bundle{}, fmt.Errorf("Pyra export failed over SSH")
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

func chatgptIsRunning() bool {
	command := exec.Command("pgrep", "-x", "ChatGPT")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err == nil {
		return true
	} else if _, ok := err.(*exec.ExitError); ok {
		return false
	}
	return true
}

func localHostname() string {
	for _, command := range [][]string{{"scutil", "--get", "LocalHostName"}, {"hostname", "-s"}} {
		output, err := exec.Command(command[0], command[1:]...).Output()
		if err == nil && strings.TrimSpace(string(output)) != "" {
			return strings.ToLower(strings.TrimSpace(string(output)))
		}
	}
	return ""
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
  codex-sync pull pyra [--dry-run]
  codex-sync status pyra
  codex-sync audit
  codex-sync rollback`)
}
