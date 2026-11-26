// Package cli implements the ship command line. It parses arguments, asks
// for confirmation and calls internal/ops for the actual work.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
		DryRun: o.dryRun,
		Out:    o.out,
		ErrOut: o.errOut,
		In:     o.in,
		Agent:  o.agent,
		Runner: o.runner,
	})
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
	case "backup":
		return cmdBackup(o, cfg, server, rest)
	case "backups":
		return cmdBackups(o, cfg, server, rest)
	case "restore":
		return cmdRestore(o, cfg, server, rest)
	case "diff":
		return cmdDiff(o, cfg, server, rest)
	case "import":
		return cmdImport(o, cfg, server, rest)
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

type configEditKind int

const (
	editShipConfig configEditKind = iota
	editCompose
)

type configEditTarget struct {
	path      string
	label     string
	validator localedit.Validator
	kind      configEditKind
}

func cmdConfig(o *options, args []string) error {
	if len(args) == 0 || args[0] != "edit" || len(args) > 2 {
		return usageErr("usage: ship config edit [app]")
	}
	path, err := config.Find()
	if err != nil {
		return err
	}
	app := ""
	if len(args) == 2 {
		app = args[1]
	}
	target, err := resolveConfigEditTarget(path, app)
	if err != nil {
		return err
	}
	return editConfigFile(o, target)
}

func resolveConfigEditTarget(path, app string) (configEditTarget, error) {
	if app == "" {
		return configEditTarget{
			path:      path,
			label:     path,
			validator: localedit.ValidateShipYAML,
			kind:      editShipConfig,
		}, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return configEditTarget{}, err
	}
	srv, ok := cfg.ServerForApp(app)
	if !ok {
		return configEditTarget{}, fmt.Errorf("app %q is not in %s on any server", app, path)
	}
	return configEditTarget{
		path:      srv.ComposePath(app),
		label:     "apps/" + app + "/compose.yaml",
		validator: localedit.ValidateCompose,
		kind:      editCompose,
	}, nil
}

func editConfigFile(o *options, target configEditTarget) error {
	editor, err := localedit.Resolve()
	if err != nil {
		return err
	}
	session, err := localedit.Begin(target.path)
	if err != nil {
		return err
	}
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
			fmt.Fprintf(o.out, "No changes to %s.\n", target.label)
			return nil
		}

		if err := session.Validate(target.validator); err != nil {
			fmt.Fprintf(o.errOut, "%s is not valid:\n%v\n\n", target.label, err)
			if !askReopen(reader, o.out) {
				return fmt.Errorf("discarded changes to %s; the file was not changed", target.label)
			}
			continue
		}

		if err := session.Save(); err != nil {
			keepTemp = true
			if errors.Is(err, localedit.ErrChanged) {
				fmt.Fprintf(o.errOut, "%s changed on disk while you were editing; not saved.\nYour edits are in %s\n", target.label, session.TempPath())
			} else {
				fmt.Fprintf(o.errOut, "Could not save %s; your edits are in %s\n", target.label, session.TempPath())
			}
			return err
		}
		if session.BackupWritten() {
			fmt.Fprintf(o.out, "Saved %s (previous kept as %s.bak).\n", target.label, target.path)
		} else {
			fmt.Fprintf(o.out, "Saved %s.\n", target.label)
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
	srv, err := cfg.Server(server)
	if err != nil {
		return err
	}
	if err := confirm(o, "Disable password and root SSH logins? Make sure "+srv.AdminUser+" can log in with a key first."); err != nil {
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
	if len(args) != 1 || args[0] != "update" {
		return usageErr("usage: ship <server> proxy update")
	}
	return o.ops(cfg).ProxyUpdate(context.Background(), server)
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
		return usageErr("usage: ship <server> deploy <app|all> [version] [--allow-dirty]")
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
	srv, err := cfg.Server(server)
	if err != nil {
		return err
	}
	if _, err := srv.App(args[0]); err != nil {
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
	srv, err := cfg.Server(server)
	if err != nil {
		return err
	}
	if _, err := srv.App(app); err != nil {
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
	diffs, err := o.ops(cfg).Compare(context.Background(), server)
	if err != nil {
		return err
	}
	found, printed := false, false
	for _, d := range diffs {
		if filter != "" && d.App != filter {
			continue
		}
		found = true
		switch {
		case d.Local && d.Server && !d.HasRecord:
			if d.RecordErr != "" {
				fmt.Fprintf(o.out, "%s: record unreadable (newer ship?)\n", d.App)
			} else {
				fmt.Fprintf(o.out, "%s: no record (older ship)\n", d.App)
			}
			printed = true
			if d.Diff != "" {
				fmt.Fprint(o.out, d.Diff)
			}
		case d.Local && d.Server && d.Changed():
			fmt.Fprintf(o.out, "%s: differs\n", d.App)
			fmt.Fprint(o.out, d.Diff)
			printed = true
		case d.Local && d.Server:
			if filter != "" {
				fmt.Fprintf(o.out, "%s: up to date\n", d.App)
				printed = true
			}
		case d.Local:
			fmt.Fprintf(o.out, "%s: only local\n", d.App)
			printed = true
		case d.Server:
			if d.RecordErr != "" {
				fmt.Fprintf(o.out, "%s: record unreadable (newer ship?), only on server\n", d.App)
			} else if !d.HasRecord {
				fmt.Fprintf(o.out, "%s: no record (older ship), only on server\n", d.App)
			} else {
				fmt.Fprintf(o.out, "%s: only on server (run ship %s import %s)\n", d.App, server, d.App)
			}
			printed = true
		}
	}
	if filter != "" && !found {
		return fmt.Errorf("app %q is neither in ship.yaml nor on server %s", filter, server)
	}
	if !printed && filter == "" {
		fmt.Fprintln(o.out, "No differences.")
	}
	return nil
}

func cmdImport(o *options, cfg *config.Config, server string, args []string) error {
	all, overwrite := false, false
	var apps []string
	for _, a := range args {
		switch {
		case a == "--all":
			all = true
		case a == "--overwrite":
			overwrite = true
		case strings.HasPrefix(a, "-"):
			return usageErr("unknown import flag %q", a)
		default:
			apps = append(apps, a)
		}
	}
	if all == (len(apps) > 0) {
		return usageErr("usage: ship <server> import <app...>|--all [--overwrite]")
	}
	summary, err := o.ops(cfg).Import(context.Background(), ops.ImportRequest{
		Server:    server,
		Apps:      apps,
		All:       all,
		Overwrite: overwrite,
	})
	if err != nil {
		var conflict *ops.ConflictError
		if errors.As(err, &conflict) {
			return fmt.Errorf("%w; pass --overwrite to replace it", err)
		}
		return err
	}
	for _, skip := range summary.Skipped {
		fmt.Fprintf(o.out, "skipped %s: %s\n", skip.App, skip.Reason)
	}
	if len(summary.Imported) == 0 {
		fmt.Fprintln(o.out, "Nothing to import.")
		return nil
	}
	fmt.Fprintf(o.out, "Imported %s.\n", strings.Join(summary.Imported, ", "))
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
  proxy update                  render and activate the Caddyfile
  deploy <app|all> [version]    deploy one app, or every app in name order
                                (--allow-dirty permits an uncommitted build)
  rollback <app>                previous image (database is not restored)
  status [app]                  version, previous version, health, proxy state
  logs <app> [service]          follow logs
  restart <app>                 recreate the app container
  maintenance <on|off> <app>    serve or stop serving the maintenance page
  env edit <app>                sudoedit the app's secret .env
  backup [app|all] [--no-pull]  back up now; pull archives locally
  backups <app>                 list server-side archives
  restore <app> <archive>       restore an archive (local path is uploaded)
  diff [app]                    compare local apps with the server's records
  import <app...>|--all         copy server apps into ship.yaml and apps/
                                (--overwrite replaces existing apps/files)
  config edit [app]             edit ship.yaml, or apps/<app>/compose.yaml,
                                in $VISUAL/$EDITOR (no server contact)
  version                       print the ship version
  update [--check]              update ship to the latest release

Global flags:
  --dry-run   print ssh/docker commands instead of running them
  --yes       skip confirmations (required outside a TTY)
  --version   print the ship version
`)
}
