// Package cli implements the ship command line. It parses arguments, asks
// for confirmation and calls internal/ops for the actual work.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/localedit"
	"github.com/harrisonclewis/ship/internal/ops"
	"github.com/harrisonclewis/ship/internal/remote"
	"github.com/harrisonclewis/ship/internal/tui"
	"github.com/harrisonclewis/ship/internal/update"
	"github.com/harrisonclewis/ship/internal/version"
)

// ErrUsage marks a command-line usage problem (exit status 2).
var ErrUsage = errors.New("usage")

// IsUsage reports whether err is a usage error.
func IsUsage(err error) bool { return errors.Is(err, ErrUsage) }

func usageErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUsage, fmt.Sprintf(format, args...))
}

type options struct {
	dryRun bool
	yes    bool
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	agent  []byte
	// runner is a test seam for commands that need ssh.
	runner remote.Runner
}

func (o *options) ops(cfg *config.Config) *ops.Ops {
	return ops.New(cfg, ops.Options{
		DryRun:       o.dryRun,
		Out:          o.out,
		ErrOut:       o.errOut,
		In:           o.in,
		Agent:        o.agent,
		Runner:       o.runner,
		ApproveBuild: o.approveBuild,
	})
}

// approveBuild shows a server-provided build script and asks for approval.
// --yes approves without a prompt; no TTY and no --yes refuses.
func (o *options) approveBuild(e *ops.BuildApprovalError) error {
	fmt.Fprintf(o.errOut, "The build script for %s on %s comes from the server and is not yet approved on this machine:\n\n%s\n", e.App, e.Server, e.Script)
	if o.yes {
		return nil
	}
	if !isTTY() {
		return fmt.Errorf("refusing to run an unapproved build script without --yes: %s on %s", e.App, e.Server)
	}
	var ok bool
	if err := huh.NewConfirm().Title("Run this build script on this machine?").Affirmative("Yes").Negative("No").Value(&ok).Run(); err != nil {
		return err
	}
	if !ok {
		return errors.New("aborted")
	}
	return nil
}

// Run executes ship with the given arguments.
func Run(args []string, in io.Reader, out, errOut io.Writer, agentScript []byte) error {
	o := &options{in: in, out: out, errOut: errOut, agent: agentScript}
	// Global flags may appear before the command. Help exits here.
	for len(args) > 0 {
		switch args[0] {
		case "--dry-run":
			o.dryRun = true
		case "--yes", "-y":
			o.yes = true
		case "-h", "--help", "help":
			printUsage(out)
			return nil
		case "--version":
			fmt.Fprintln(out, "ship "+version.Version)
			return nil
		default:
			goto doneFlags
		}
		args = args[1:]
	}
doneFlags:
	// Also accept the global flags after the command, e.g.
	// `ship prod rollback hello --yes`.
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--dry-run":
			o.dryRun = true
		case "--yes", "-y":
			o.yes = true
		default:
			filtered = append(filtered, a)
		}
	}
	args = filtered

	update.Startup(version.Version, args, isTTY(), o.errOut)

	if len(args) == 0 {
		if !isTTY() {
			printUsage(out)
			return fmt.Errorf("%w: no command", ErrUsage)
		}
		return runMenu(o)
	}

	// config push must read the old format, which normal loading rejects.
	if server, ok := configPushServer(args); ok {
		return cmdConfigPush(o, server)
	}

	// These commands belong to ship itself, not to a server.
	switch args[0] {
	case "version":
		return requireNoArgs("version", args[1:], func() error {
			fmt.Fprintln(o.out, "ship "+version.Version)
			return nil
		})
	case "update":
		return update.Run(args[1:], o.out)
	case "config":
		return cmdConfig(o, args[1:])
	case "help":
		printUsage(out)
		return nil
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	server, rest, err := resolveServer(cfg, args)
	if err != nil {
		return err
	}
	return dispatchCommand(o, cfg, server, rest)
}

// resolveServer splits an explicit server name off the arguments, or applies
// the only configured server. Several servers and none named is a usage error
// that lists them.
func resolveServer(cfg *config.Config, args []string) (string, []string, error) {
	names := cfg.ServerNames()
	if len(args) > 0 {
		if _, ok := cfg.Servers[args[0]]; ok {
			return args[0], args[1:], nil
		}
	}
	if len(names) == 1 {
		return names[0], args, nil
	}
	return "", nil, usageErr("specify a server first: %s (usage: ship <server> <command>)", strings.Join(names, ", "))
}

func dispatchCommand(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) == 0 {
		return usageErr("no command for server %q (usage: ship <server> <command>)", server)
	}
	cmd := args[0]
	rest := args[1:]
	switch cmd {
	case "setup":
		return requireNoArgs(cmd, rest, func() error {
			return o.ops(cfg).Setup(context.Background(), server)
		})
	case "lock-ssh":
		return requireNoArgs(cmd, rest, func() error { return cmdLockSSH(o, cfg, server) })
	case "vps":
		return cmdVPS(o, cfg, server, rest)
	case "proxy":
		return cmdProxy(o, cfg, server, rest)
	case "deploy":
		return cmdDeploy(o, cfg, server, rest)
	case "rollback":
		return cmdRollback(o, cfg, server, rest)
	case "status":
		return cmdStatus(o, cfg, server, rest)
	case "logs":
		return cmdLogs(o, cfg, server, rest)
	case "restart":
		return cmdRestart(o, cfg, server, rest)
	case "maintenance":
		return cmdMaintenance(o, cfg, server, rest)
	case "env":
		return cmdEnv(o, cfg, server, rest)
	case "app":
		return cmdApp(o, cfg, server, rest)
	case "backup":
		return cmdBackup(o, cfg, server, rest)
	case "backups":
		return cmdBackups(o, cfg, server, rest)
	case "restore":
		return cmdRestore(o, cfg, server, rest)
	case "diff":
		return cmdDiff(o, cfg, server, rest)
	case "server":
		return cmdServerSettings(o, cfg, server, rest)
	case "config":
		return cmdConfigServer(o, cfg, server, rest)
	case "version":
		return requireNoArgs(cmd, rest, func() error {
			fmt.Fprintln(o.out, "ship "+version.Version)
			return nil
		})
	case "update":
		return update.Run(rest, o.out)
	case "help":
		printUsage(o.out)
		return nil
	default:
		printUsage(o.errOut)
		return usageErr("unknown command %q", cmd)
	}
}

func requireNoArgs(name string, args []string, fn func() error) error {
	if len(args) != 0 {
		return usageErr("%s takes no arguments", name)
	}
	return fn()
}

func loadConfig() (*config.Config, error) {
	return config.LoadDefault()
}

func cmdConfig(o *options, args []string) error {
	if len(args) == 0 || args[0] != "edit" {
		return usageErr("usage: ship config edit | ship <server> config edit <app> [compose|Dockerfile] | ship <server> config push")
	}
	if len(args) == 1 {
		path, err := config.Find()
		if err != nil {
			return err
		}
		return editLocalConfig(o, path)
	}
	if len(args) != 2 {
		return usageErr("usage: ship config edit | ship <server> config edit <app> [compose|Dockerfile]")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	names := cfg.ServerNames()
	if len(names) != 1 {
		return usageErr("specify the server: ship <server> config edit %s", args[1])
	}
	return cmdConfigServer(o, cfg, names[0], []string{"edit", args[1]})
}

func editLocalConfig(o *options, path string) error {
	editor, err := localedit.Resolve()
	if err != nil {
		return err
	}
	session, err := localedit.Begin(path)
	if err != nil {
		return err
	}
	saved := func() string {
		if session.BackupWritten() {
			return fmt.Sprintf("Saved %s (previous kept as %s.bak).", path, path)
		}
		return "Saved " + path + "."
	}
	return runEditorLoop(o, editor, session, path, localedit.ValidateShipYAML, func([]byte) error {
		return session.Save()
	}, saved)
}

// cmdConfigServer edits one desired compose or Dockerfile on the server.
func cmdConfigServer(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) < 2 || args[0] != "edit" {
		return usageErr("usage: ship <server> config edit <app> [compose|Dockerfile]")
	}
	if len(args) > 3 {
		return usageErr("usage: ship <server> config edit <app> [compose|Dockerfile]")
	}
	app := args[1]
	file := "compose"
	if len(args) == 3 {
		file = args[2]
	}
	if file != "compose" && file != "Dockerfile" {
		return usageErr("edit either compose or Dockerfile, got %q", file)
	}
	op := o.ops(cfg)
	ctx := context.Background()
	state, err := op.FetchConfig(ctx, server)
	if err != nil {
		return err
	}
	da := state.Apps[app]
	if da == nil || da.Record == nil {
		return fmt.Errorf("app %q is not configured on %s; run `ship %s app create %s`", app, server, server, app)
	}
	data := da.Compose
	label := "apps/" + app + "/compose.yaml"
	validator := localedit.Validator(localedit.ValidateCompose)
	if file == "Dockerfile" {
		data = da.Dockerfile
		label = "apps/" + app + "/Dockerfile"
		validator = nil
	}
	editor, err := localedit.Resolve()
	if err != nil {
		return err
	}
	buffer, err := localedit.BeginBuffer(label, data)
	if err != nil {
		return err
	}
	commit := func(data []byte) error {
		compose, dockerfile := da.Compose, da.Dockerfile
		if file == "compose" {
			compose = data
		} else {
			dockerfile = data
		}
		_, err := op.PutDesiredApp(ctx, server, app, da.Record, compose, dockerfile, state.Arch, da.Hash)
		return err
	}
	saved := func() string {
		return fmt.Sprintf("Saved %s.\nDeploy to apply: ship %s deploy %s\n", label, server, app)
	}
	return runEditorLoop(o, editor, buffer, label, validator, commit, saved)
}

func configPushServer(args []string) (string, bool) {
	if len(args) >= 2 && args[0] == "config" && args[1] == "push" {
		if len(args) == 2 {
			return "", true
		}
		if len(args) == 3 {
			return args[2], true
		}
	}
	if len(args) >= 3 && args[1] == "config" && args[2] == "push" {
		if len(args) == 3 {
			return args[0], true
		}
	}
	return "", false
}

func cmdConfigPush(o *options, server string) error {
	path, err := config.Find()
	if err != nil {
		return err
	}
	old, err := config.LoadOld(path)
	if err != nil {
		return err
	}
	if server == "" {
		names := old.ServerNames()
		if len(names) != 1 {
			return usageErr("specify the server: ship <server> config push (configured: %s)", strings.Join(names, ", "))
		}
		server = names[0]
	}
	if _, err := old.Server(server); err != nil {
		return err
	}
	return o.ops(old).ConfigPush(context.Background(), server)
}

type tempEditor interface {
	TempPath() string
	Changed() (bool, error)
	Validate(localedit.Validator) error
	Read() ([]byte, error)
	Close() error
}

func runEditorLoop(o *options, editor localedit.Editor, session tempEditor, label string, validate localedit.Validator, commit func([]byte) error, saved func() string) error {
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = session.Close()
		}
	}()
	in := o.in
	if in == nil {
		in = os.Stdin
	}
	reader := bufio.NewReader(in)

	for {
		cmd := editor.Command(session.TempPath())
		cmd.Stdin, cmd.Stdout, cmd.Stderr = in, o.out, o.errOut
		if err := cmd.Run(); err != nil {
			keepTemp = true
			fmt.Fprintf(o.errOut, "Your edits are in %s\n", session.TempPath())
			return fmt.Errorf("editor %s: %w", editor.Name, err)
		}

		changed, err := session.Changed()
		if err != nil {
			keepTemp = true
			fmt.Fprintf(o.errOut, "Your edits are in %s\n", session.TempPath())
			return err
		}
		if !changed {
			fmt.Fprintf(o.out, "No changes to %s.\n", label)
			return nil
		}

		if err := session.Validate(validate); err != nil {
			fmt.Fprintf(o.errOut, "%s is not valid:\n%v\n\n", label, err)
			if !askReopen(reader, o.out) {
				return fmt.Errorf("discarded changes to %s; the file was not changed", label)
			}
			continue
		}

		data, err := session.Read()
		if err != nil {
			keepTemp = true
			fmt.Fprintf(o.errOut, "Could not read your edits; they are in %s\n", session.TempPath())
			return err
		}
		if err := commit(data); err != nil {
			keepTemp = true
			if errors.Is(err, localedit.ErrChanged) {
				fmt.Fprintf(o.errOut, "%s changed on disk while you were editing; not saved.\nYour edits are in %s\n", label, session.TempPath())
			} else {
				fmt.Fprintf(o.errOut, "Could not save %s; your edits are in %s\n", label, session.TempPath())
			}
			return err
		}
		if saved != nil {
			fmt.Fprintln(o.out, saved())
		} else {
			fmt.Fprintf(o.out, "Saved %s.\n", label)
		}
		return nil
	}
}

// askReopen reads a y/n answer, defaulting to yes on an empty line.
func askReopen(reader *bufio.Reader, out io.Writer) bool {
	for {
		fmt.Fprint(out, "Reopen editor? [Y/n] ")
		line, err := reader.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "y", "yes":
			if err == nil || strings.TrimSpace(line) != "" {
				return true
			}
			return false
		case "n", "no":
			return false
		}
		if err != nil {
			return false
		}
		fmt.Fprintln(out, "Please answer y or n.")
	}
}

func confirm(o *options, title string) error {
	if o.yes || o.dryRun {
		return nil
	}
	if !isTTY() {
		return fmt.Errorf("refusing without confirmation; re-run with --yes: %s", title)
	}
	var ok bool
	if err := huh.NewConfirm().Title(title).Affirmative("Yes").Negative("No").Value(&ok).Run(); err != nil {
		return err
	}
	if !ok {
		return errors.New("aborted")
	}
	return nil
}

func ask(o *options, title string) bool {
	if o.yes {
		return true
	}
	if !isTTY() {
		return false
	}
	var ok bool
	if err := huh.NewConfirm().Title(title).Affirmative("Yes").Negative("No").Value(&ok).Run(); err != nil {
		return false
	}
	return ok
}

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func cmdLockSSH(o *options, cfg *config.Config, server string) error {
	if _, err := cfg.Server(server); err != nil {
		return err
	}
	if err := confirm(o, "Disable password and root SSH logins? Make sure the server's admin user can log in with a key first."); err != nil {
		return err
	}
	return o.ops(cfg).LockSSH(context.Background(), server)
}

func cmdVPS(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) == 0 || args[0] != "update" && args[0] != "reboot" {
		return usageErr("usage: ship <server> vps <update|reboot>")
	}
	op := o.ops(cfg)
	switch args[0] {
	case "update":
		if len(args) != 1 {
			return usageErr("usage: ship <server> vps update")
		}
		reboot, err := op.VPSUpdate(context.Background(), server)
		if err != nil {
			return err
		}
		if reboot {
			if o.yes || ask(o, "A reboot is required. Reboot now?") {
				fmt.Fprintln(o.out, "rebooting the VPS; the ssh connection will drop")
				_ = op.VPSReboot(context.Background(), server)
				return nil
			}
			fmt.Fprintln(o.out, "reboot deferred; run `ship "+server+" vps reboot` when ready")
		}
		return nil
	case "reboot":
		if len(args) != 1 {
			return usageErr("usage: ship <server> vps reboot")
		}
		if err := confirm(o, "Reboot the VPS now?"); err != nil {
			return err
		}
		_ = op.VPSReboot(context.Background(), server)
		return nil
	}
	return nil
}

func cmdProxy(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) != 1 {
		return usageErr("usage: ship <server> proxy sync|update|restart")
	}
	switch args[0] {
	case "sync", "update":
		return o.ops(cfg).ProxySync(context.Background(), server)
	case "restart":
		return o.ops(cfg).ProxyRestart(context.Background(), server)
	default:
		return usageErr("usage: ship <server> proxy sync|update|restart")
	}
}

func cmdServerSettings(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) < 3 || args[0] != "set" || (args[1] != "admin_user" && args[1] != "caddy_image") {
		return usageErr("usage: ship <server> server set admin_user|caddy_image <value>")
	}
	return o.ops(cfg).ServerSet(context.Background(), server, args[1], strings.Join(args[2:], " "))
}

func cmdDeploy(o *options, cfg *config.Config, server string, args []string) error {
	var positional []string
	allowDirty := false
	for _, a := range args {
		if a == "--allow-dirty" {
			allowDirty = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			return usageErr("unknown deploy flag %q", a)
		}
		positional = append(positional, a)
	}
	if len(positional) < 1 || len(positional) > 2 {
		return usageErr("usage: ship <server> deploy <app|all> [version | commit:<sha> | release:<tag> | release:<tag>:<asset>] [--allow-dirty]")
	}
	name := positional[0]
	version := ""
	if len(positional) == 2 {
		version = positional[1]
	}
	op := o.ops(cfg)
	if name == "all" {
		if version != "" {
			return usageErr("deploy all takes no version")
		}
		_, err := op.DeployAll(context.Background(), server, allowDirty)
		return err
	}
	return op.Deploy(context.Background(), server, name, version, allowDirty)
}

func cmdRollback(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) != 1 {
		return usageErr("usage: ship <server> rollback <app>")
	}
	if _, err := cfg.Server(server); err != nil {
		return err
	}
	if err := config.ValidateAppName(args[0]); err != nil {
		return err
	}
	if err := confirm(o, "Roll back "+args[0]+" to the previous image? The database is not restored."); err != nil {
		return err
	}
	return o.ops(cfg).Rollback(context.Background(), server, args[0])
}

func cmdStatus(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) > 1 {
		return usageErr("usage: ship <server> status [app]")
	}
	app := ""
	if len(args) == 1 {
		app = args[0]
	}
	_, err := o.ops(cfg).Status(context.Background(), server, app)
	return err
}

func cmdLogs(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return usageErr("usage: ship <server> logs <app> [service]")
	}
	app := args[0]
	service := ""
	if len(args) == 2 {
		service = args[1]
	}
	return o.ops(cfg).Logs(context.Background(), server, app, service)
}

func cmdRestart(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) != 1 {
		return usageErr("usage: ship <server> restart <app>")
	}
	return o.ops(cfg).Restart(context.Background(), server, args[0])
}

func cmdMaintenance(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) != 2 || (args[0] != "on" && args[0] != "off") {
		return usageErr("usage: ship <server> maintenance <on|off> <app>")
	}
	return o.ops(cfg).Maintenance(context.Background(), server, args[1], args[0] == "on")
}

func cmdEnv(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) != 2 || args[0] != "edit" {
		return usageErr("usage: ship <server> env edit <app>")
	}
	return o.ops(cfg).EnvEdit(context.Background(), server, args[1])
}

var appSetKeys = []string{
	"source.type", "source.path", "source.build_dir", "source.repo", "source.asset", "domains", "redirects",
	"build.commands", "deploy.commands", "deploy.maintenance",
}

func appSetKeyHelp() string { return strings.Join(appSetKeys, ", ") }

func isAppListKey(key string) bool { return key == "domains" || key == "redirects" }

func validAppSetKey(key string) bool { return slices.Contains(appSetKeys, key) }

func appUsage(server string) error {
	return usageErr("usage: ship %s app set|add|remove <app> <key> <value...> | app edit|create|delete <app>", server)
}

func localConfigPath(cfg *config.Config) (string, error) {
	if cfg.Path != "" {
		return cfg.Path, nil
	}
	return config.Find()
}

func cmdApp(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) < 2 {
		return appUsage(server)
	}
	sub, app := args[0], args[1]
	switch sub {
	case "set", "add", "remove":
		if len(args) < 4 {
			return appUsage(server)
		}
		return cmdAppSet(o, cfg, server, sub, app, args[2], args[3:])
	case "edit":
		if len(args) != 2 {
			return appUsage(server)
		}
		return cmdAppEdit(o, cfg, server, app)
	case "create":
		if len(args) != 2 {
			return appUsage(server)
		}
		return cmdAppCreate(o, cfg, server, app)
	case "delete":
		if len(args) != 2 {
			return appUsage(server)
		}
		return cmdAppDelete(o, cfg, server, app)
	default:
		return appUsage(server)
	}
}

func cmdAppSet(o *options, cfg *config.Config, server, sub, app, key string, values []string) error {
	if !validAppSetKey(key) {
		return usageErr("unknown app key %q; valid keys: %s", key, appSetKeyHelp())
	}
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	if key == "source.path" {
		if sub != "set" {
			return usageErr("source.path is local; use `ship %s app set %s source.path <directory|git-url>`", server, app)
		}
		value := strings.Join(values, " ")
		path, err := localConfigPath(cfg)
		if err != nil {
			return err
		}
		if strings.TrimSpace(value) == "" {
			if err := config.SetLocalSourceEntry(path, server, app, "", "", true); err != nil {
				return err
			}
			fmt.Fprintf(o.out, "sources.%s cleared.\n", app)
		} else {
			source, err := config.ParseLocalSource(value)
			if err != nil {
				return fmt.Errorf("source.path: %w", err)
			}
			// A Git URL keeps an existing build directory; switching to a
			// local directory clears it so it cannot be used by accident.
			buildDir := ""
			if source.IsGit {
				current, err := config.Load(path)
				if err != nil {
					return err
				}
				srv, err := current.Server(server)
				if err != nil {
					return err
				}
				buildDir = srv.BuildDirs[app]
			}
			if err := config.SetLocalSourceEntry(path, server, app, value, buildDir, false); err != nil {
				return err
			}
			fmt.Fprintf(o.out, "sources.%s = %s.\n", app, value)
		}
		if _, err := config.Load(path); err != nil {
			return err
		}
		return nil
	}
	if key == "source.build_dir" {
		if sub != "set" {
			return usageErr("source.build_dir is local; use `ship %s app set %s source.build_dir <directory>`", server, app)
		}
		value := strings.Join(values, " ")
		dir, err := config.NormalizeBuildDir(value)
		if err != nil {
			return fmt.Errorf("source.build_dir: %w", err)
		}
		path, err := localConfigPath(cfg)
		if err != nil {
			return err
		}
		current, err := config.Load(path)
		if err != nil {
			return err
		}
		srv, err := current.Server(server)
		if err != nil {
			return err
		}
		if dir != "" {
			if _, ok := config.GitSource(srv.Sources[app]); !ok {
				return fmt.Errorf("source.build_dir: sources.%s is not a Git repository URL; set source.path to a Git URL first", app)
			}
		}
		if err := config.SetLocalBuildDir(path, server, app, dir); err != nil {
			return err
		}
		if dir == "" {
			fmt.Fprintf(o.out, "build_dirs.%s cleared.\n", app)
		} else {
			fmt.Fprintf(o.out, "build_dirs.%s = %s.\n", app, dir)
		}
		if _, err := config.Load(path); err != nil {
			return err
		}
		return nil
	}
	if sub != "set" && !isAppListKey(key) {
		return usageErr("%s %s is not a list; use `ship %s app set %s %s <value...>`", sub, key, server, app, key)
	}
	if len(values) == 0 {
		return usageErr("app %s needs at least one value", sub)
	}

	op := o.ops(cfg)
	ctx := context.Background()
	state, err := op.FetchConfig(ctx, server)
	if err != nil {
		return err
	}
	da := state.Apps[app]
	if da == nil || da.Record == nil {
		return fmt.Errorf("app %q is not configured on %s; run `ship %s app create %s`", app, server, server, app)
	}
	record := cloneApp(da.Record)

	oldCanonical := ""
	if len(record.Domains) > 0 {
		oldCanonical = record.Domains[0]
	}
	var next []string
	switch sub {
	case "add", "remove":
		var changed bool
		var already []string
		var err error
		next, changed, already, err = mutateList(record, key, values, sub)
		if err != nil {
			return err
		}
		if !changed {
			fmt.Fprintf(o.out, "%s already contains %s; no changes.\n", key, strings.Join(already, ", "))
			return nil
		}
		if len(already) > 0 {
			fmt.Fprintf(o.out, "Already present: %s.\n", strings.Join(already, ", "))
		}
		if err := setRecordList(record, key, next); err != nil {
			return err
		}
	case "set":
		if isAppListKey(key) {
			var list []string
			if !(len(values) == 1 && values[0] == "") {
				list = dedupeValues(values)
			}
			if err := setRecordList(record, key, list); err != nil {
				return err
			}
		} else {
			value := strings.Join(values, " ")
			if (key == "build.commands" || key == "deploy.commands") && len(values) == 1 && values[0] == "-" {
				in := o.in
				if in == nil {
					in = os.Stdin
				}
				data, err := io.ReadAll(in)
				if err != nil {
					return err
				}
				value = string(data)
			}
			if err := setRecordScalar(record, key, value); err != nil {
				return err
			}
		}
	}

	if _, err := recordsWith(state, app, record); err != nil {
		return err
	}
	_, err = op.PutDesiredApp(ctx, server, app, record, da.Compose, da.Dockerfile, state.Arch, da.Hash)
	if err != nil {
		return err
	}
	printSavedAppChanges(o.out, server, app, da.Record, record)
	if key == "domains" && oldCanonical != "" && len(record.Domains) > 0 && record.Domains[0] != oldCanonical {
		fmt.Fprintf(o.out, "Canonical domain is now %s.\n", record.Domains[0])
	}
	return nil
}

func mutateList(record *config.App, key string, values []string, sub string) ([]string, bool, []string, error) {
	current := recordList(record, key)
	have := make(map[string]bool, len(current))
	for _, v := range current {
		have[v] = true
	}
	if sub == "add" {
		next := append([]string(nil), current...)
		var already []string
		for _, v := range dedupeValues(values) {
			if have[v] {
				already = append(already, v)
				continue
			}
			have[v] = true
			next = append(next, v)
		}
		return next, len(next) != len(current), already, nil
	}
	for _, v := range dedupeValues(values) {
		if !have[v] {
			return nil, false, nil, fmt.Errorf("%s does not contain %s; nothing was changed", key, v)
		}
	}
	remove := map[string]bool{}
	for _, v := range values {
		remove[v] = true
	}
	var next []string
	for _, v := range current {
		if !remove[v] {
			next = append(next, v)
		}
	}
	if key == "domains" && len(next) == 0 {
		return nil, false, nil, errors.New("cannot remove the last domain; domains are required")
	}
	return next, true, nil, nil
}

func recordList(record *config.App, key string) []string {
	if key == "domains" {
		return append([]string(nil), record.Domains...)
	}
	return append([]string(nil), record.Redirects...)
}

func setRecordList(record *config.App, key string, values []string) error {
	switch key {
	case "domains":
		record.Domains = values
	case "redirects":
		record.Redirects = values
	default:
		return fmt.Errorf("%s is not a list", key)
	}
	return nil
}

func setRecordScalar(record *config.App, key, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "source.type":
		record.Source.Type = value
		if value == "image" {
			record.Source.Repo = ""
			record.Source.Asset = ""
		}
	case "source.repo":
		record.Source.Repo = value
	case "source.asset":
		record.Source.Asset = value
	case "build.commands":
		record.Build.Commands = value
	case "deploy.commands":
		record.Deploy.Commands = value
	case "deploy.maintenance":
		switch strings.ToLower(value) {
		case "":
			record.Deploy.Maintenance = nil
		case "true":
			v := true
			record.Deploy.Maintenance = &v
		case "false":
			v := false
			record.Deploy.Maintenance = &v
		default:
			return usageErr("deploy.maintenance wants true or false, got %q", value)
		}
	default:
		return fmt.Errorf("unknown app key %q", key)
	}
	return nil
}

func recordsWith(state *ops.ServerConfig, name string, record *config.App) (map[string]*config.App, error) {
	records := make(map[string]*config.App, len(state.Apps)+1)
	for n, da := range state.Apps {
		if da.Record != nil && n != name {
			records[n] = da.Record
		}
	}
	records[name] = record
	if err := config.ValidateRecords(records); err != nil {
		return nil, err
	}
	return records, nil
}

func printSavedAppChanges(out io.Writer, server, app string, old, next *config.App) {
	fmt.Fprint(out, savedAppMessage(server, app, old, next))
}

func savedAppMessage(server, app string, old, next *config.App) string {
	msg := "Saved.\n"
	routesChanged := !slices.Equal(old.Domains, next.Domains) || !slices.Equal(old.Redirects, next.Redirects)
	if routesChanged {
		return msg + fmt.Sprintf("Restart Caddy to apply: ship %s proxy restart\n", server)
	}
	return msg + fmt.Sprintf("Deploy to apply: ship %s deploy %s\n", server, app)
}

func cmdAppEdit(o *options, cfg *config.Config, server, app string) error {
	op := o.ops(cfg)
	ctx := context.Background()
	state, err := op.FetchConfig(ctx, server)
	if err != nil {
		return err
	}
	da := state.Apps[app]
	if da == nil || da.Record == nil {
		return fmt.Errorf("app %q is not configured on %s; run `ship %s app create %s`", app, server, server, app)
	}
	data, err := da.Record.RecordYAML()
	if err != nil {
		return err
	}
	editor, err := localedit.Resolve()
	if err != nil {
		return err
	}
	buffer, err := localedit.BeginBuffer(app+".yaml", data)
	if err != nil {
		return err
	}
	validate := func(data []byte) error {
		record, err := config.DecodeAppRecord(bytes.NewReader(data))
		if err != nil {
			return err
		}
		_, err = recordsWith(state, app, record)
		return err
	}
	var next *config.App
	commit := func(data []byte) error {
		record, err := config.DecodeAppRecord(bytes.NewReader(data))
		if err != nil {
			return err
		}
		if _, err := op.PutDesiredApp(ctx, server, app, record, da.Compose, da.Dockerfile, state.Arch, da.Hash); err != nil {
			return err
		}
		next = record
		return nil
	}
	saved := func() string {
		if next == nil {
			return ""
		}
		return savedAppMessage(server, app, da.Record, next)
	}
	return runEditorLoop(o, editor, buffer, "app "+server+"/"+app, validate, commit, saved)
}

func cmdAppCreate(o *options, cfg *config.Config, server, app string) error {
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	op := o.ops(cfg)
	ctx := context.Background()
	state, err := op.FetchConfig(ctx, server)
	if err != nil {
		return err
	}
	if state.Apps[app] != nil {
		return fmt.Errorf("app %q already exists on %s", app, server)
	}
	template := fmt.Sprintf(`domains: [%s.example.com]
upstream: %s:8080
source:
  type: image
backup: {}
`, app, app)
	editor, err := localedit.Resolve()
	if err != nil {
		return err
	}
	buffer, err := localedit.BeginBuffer(app+".yaml", []byte(template))
	if err != nil {
		return err
	}
	validate := func(data []byte) error {
		record, err := config.DecodeAppRecord(bytes.NewReader(data))
		if err != nil {
			return err
		}
		_, err = recordsWith(state, app, record)
		return err
	}
	commit := func(data []byte) error {
		record, err := config.DecodeAppRecord(bytes.NewReader(data))
		if err != nil {
			return err
		}
		compose := []byte("name: " + app + "\nservices:\n  " + app + ":\n    image: \"${IMAGE:?IMAGE is set by ship}\"\n")
		_, err = op.PutDesiredApp(ctx, server, app, record, compose, nil, state.Arch, "")
		return err
	}
	if err := runEditorLoop(o, editor, buffer, "new app "+server+"/"+app, validate, commit, nil); err != nil {
		return err
	}
	fmt.Fprintf(o.out, "Created %s. Edit its compose file with: ship %s config edit %s\n", app, server, app)
	return nil
}

func cmdAppDelete(o *options, cfg *config.Config, server, app string) error {
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	op := o.ops(cfg)
	ctx := context.Background()
	state, err := op.FetchConfig(ctx, server)
	if err != nil {
		return err
	}
	da := state.Apps[app]
	if da == nil {
		return fmt.Errorf("app %q is not configured on %s", app, server)
	}
	if err := confirm(o, "Delete "+app+" on "+server+"? Its desired configuration is removed and, if deployed, its stack is stopped. Data directories and .env are kept."); err != nil {
		return err
	}
	if err := op.RemoveDesiredApp(ctx, server, app, da.Hash); err != nil {
		return err
	}
	fmt.Fprintf(o.out, "Deleted %s. Data directories and .env were kept.\n", app)
	fmt.Fprintf(o.out, "Restart Caddy to apply: ship %s proxy restart\n", server)
	return nil
}

func cloneApp(a *config.App) *config.App {
	b := *a
	b.Domains = append([]string(nil), a.Domains...)
	b.Redirects = append([]string(nil), a.Redirects...)
	b.Data = append([]string(nil), a.Data...)
	b.Backup.SQLite = append([]string(nil), a.Backup.SQLite...)
	b.Backup.Files = append([]string(nil), a.Backup.Files...)
	if a.Backup.Postgres != nil {
		pg := *a.Backup.Postgres
		b.Backup.Postgres = &pg
	}
	if a.UID != nil {
		v := *a.UID
		b.UID = &v
	}
	if a.Deploy.Maintenance != nil {
		v := *a.Deploy.Maintenance
		b.Deploy.Maintenance = &v
	}
	if a.Env != nil {
		b.Env = make(map[string]string, len(a.Env))
		for k, v := range a.Env {
			b.Env[k] = v
		}
	}
	return &b
}

func dedupeValues(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func cmdBackups(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) != 1 {
		return usageErr("usage: ship <server> backups <app>")
	}
	return o.ops(cfg).Backups(context.Background(), server, args[0])
}

func cmdBackup(o *options, cfg *config.Config, server string, args []string) error {
	target := "all"
	noPull := false
	for _, a := range args {
		switch {
		case a == "--no-pull":
			noPull = true
		case strings.HasPrefix(a, "-"):
			return usageErr("unknown backup flag %q", a)
		default:
			if target != "all" {
				return usageErr("usage: ship <server> backup [app|all] [--no-pull]")
			}
			target = a
		}
	}
	_, err := o.ops(cfg).Backup(context.Background(), server, target, !noPull)
	return err
}

func cmdRestore(o *options, cfg *config.Config, server string, args []string) error {
	var positional []string
	for _, a := range args {
		switch {
		case a == "--yes" || a == "-y":
			o.yes = true
		case strings.HasPrefix(a, "-"):
			return usageErr("unknown restore flag %q", a)
		default:
			positional = append(positional, a)
		}
	}
	if len(positional) != 2 {
		return usageErr("usage: ship <server> restore <app> <archive> [--yes]")
	}
	app, archive := positional[0], positional[1]
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	if _, err := cfg.Server(server); err != nil {
		return err
	}
	if err := confirm(o, "Restore "+app+" from "+archive+"? A pre-restore backup is taken first; the app is stopped while files are replaced."); err != nil {
		return err
	}
	return o.ops(cfg).Restore(context.Background(), server, app, archive)
}

func cmdDiff(o *options, cfg *config.Config, server string, args []string) error {
	if len(args) > 1 {
		return usageErr("usage: ship <server> diff [app]")
	}
	filter := ""
	if len(args) == 1 {
		if strings.HasPrefix(args[0], "-") {
			return usageErr("unknown diff flag %q", args[0])
		}
		filter = args[0]
		if err := config.ValidateAppName(filter); err != nil {
			return err
		}
	}
	report, err := o.ops(cfg).Variances(context.Background(), server)
	if err != nil {
		return err
	}
	found := false
	for _, v := range report.Apps {
		if filter != "" && v.App != filter {
			continue
		}
		found = true
		switch v.State {
		case ops.VarianceNotDeployed:
			fmt.Fprintf(o.out, "%s: not deployed\n", v.App)
		case ops.VariancePending:
			fmt.Fprintf(o.out, "%s: pending deploy\n", v.App)
			if v.Diff != "" {
				fmt.Fprint(o.out, v.Diff)
			}
		case ops.VarianceApplied:
			if filter != "" {
				fmt.Fprintf(o.out, "%s: applied\n", v.App)
			}
		}
	}
	if filter != "" && !found {
		return fmt.Errorf("app %q is not configured on server %s", filter, server)
	}
	printedServer := false
	if report.ProxyOutOfSync {
		fmt.Fprintf(o.out, "proxy: out of sync (%s); run `ship %s proxy restart`\n", report.ProxyReason, server)
		printedServer = true
		if report.ProxyDiff != "" {
			fmt.Fprint(o.out, report.ProxyDiff)
		}
	}
	if report.SetupPending {
		fmt.Fprintf(o.out, "setup: pending (%s); run `ship %s setup`\n", report.SetupReason, server)
		printedServer = true
	}
	if filter == "" && !printedServer {
		any := false
		for _, v := range report.Apps {
			if v.State != ops.VarianceApplied {
				any = true
			}
		}
		if !any {
			fmt.Fprintln(o.out, "No differences.")
		}
	}
	return nil
}

func runMenu(o *options) error {
	return tui.Run(tui.Options{
		DryRun: o.dryRun,
		Agent:  o.agent,
		In:     o.in,
		Out:    o.out,
		ErrOut: o.errOut,
	})
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `ship — deploy and operate apps on personal VPSes

Usage:
  ship [--dry-run] [--yes] <server> <command> [arguments]
  ship [--dry-run] [--yes] <command> [arguments]   (one configured server)
  ship [--dry-run] [--yes]                         interactive menu (TTY only)

Servers:
  Every server in ship.yaml is named under servers:. Pass its name first, for
  example: ship prod deploy hello. With exactly one server it is optional.

Commands:
  setup                         prepare the VPS (re-runnable)
  lock-ssh                      key-only SSH, no root login
  vps update                    apt update, full upgrade, autoremove
  vps reboot                    reboot the VPS
  proxy sync|update             render the Caddyfile and reload Caddy
  proxy restart                 render the Caddyfile and recreate Caddy
  deploy <app|all> [version]    deploy one app, or every app in name order
                                Git sources: commit:<sha>, release:<tag>,
                                release:<tag>:<asset>, or empty for HEAD
                                (--allow-dirty permits an uncommitted build)
  rollback <app>                previous image (database is not restored)
  status [app]                  version, previous version, health, config state
  logs <app> [service]          follow logs
  restart <app>                 recreate the app container
  maintenance <on|off> <app>    serve or stop serving the maintenance page
  env edit <app>                sudoedit the app's secret .env
  app set|add|remove <app> <key> <value...>
                                change desired app configuration on the server
  app edit <app>                edit one app's record in $VISUAL/$EDITOR
  app create <app>              create a desired app from a template
  app delete <app>              remove desired config and stop the stack
  server set admin_user|caddy_image <value>
                                change desired server-wide settings
  backup [app|all] [--no-pull]  back up now; pull archives locally
  backups <app>                 list server-side archives
  restore <app> <archive>       restore an archive (local path is uploaded)
  diff [app]                    desired vs running configuration
  config edit [app] [compose|Dockerfile]
                                edit local ship.yaml, or a desired server file
  config push                   migrate an old-format local file to the server
                                (from the file's server block, then rewrite it)
  version                       print the ship version
  update [--check]              update ship to the latest release

Global flags:
  --dry-run   print ssh/docker commands instead of running them
  --yes       skip confirmations (required outside a TTY)
  --version   print the ship version
`)
}
