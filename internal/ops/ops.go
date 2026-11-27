// Package ops implements the operations behind the ship CLI and menu.
//
// Everything here works on one named server from ship.yaml, writes its output
// to an io.Writer supplied by the caller and never prompts. Confirmation is an
// explicit choice made by internal/cli (or, later, the TUI).
package ops

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/remote"
)

// Options configures an Ops.
type Options struct {
	DryRun bool
	Out    io.Writer
	ErrOut io.Writer
	In     io.Reader
	Agent  []byte
	// Runner overrides the command runner (tests).
	Runner remote.Runner
	// ApproveBuild asks the user to approve an unapproved build script. When
	// nil, deploy returns *BuildApprovalError instead of prompting.
	ApproveBuild func(*BuildApprovalError) error
}

// Ops runs ship operations for one loaded config.
type Ops struct {
	Config *config.Config
	opts   Options
}

// New returns operations for cfg.
func New(cfg *config.Config, opts Options) *Ops {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.ErrOut == nil {
		opts.ErrOut = io.Discard
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}
	return &Ops{Config: cfg, opts: opts}
}

// Load finds and loads ship.yaml, then returns operations for it.
func Load(opts Options) (*Ops, error) {
	cfg, err := config.LoadDefault()
	if err != nil {
		return nil, err
	}
	return New(cfg, opts), nil
}

// Out is where operation output is written.
func (o *Ops) Out() io.Writer { return o.opts.Out }

// Servers lists configured server names in sorted order.
func (o *Ops) Servers() []string { return o.Config.ServerNames() }

// Server returns one server's configuration.
func (o *Ops) Server(name string) (*config.Server, error) {
	return o.Config.Server(name)
}

// Apps lists app names for a server in sorted order. It returns the names
// only; use Server for the full app definitions.
func (o *Ops) Apps(server string) ([]string, error) {
	srv, err := o.Config.Server(server)
	if err != nil {
		return nil, err
	}
	return srv.AppNames(), nil
}

func (o *Ops) session(ctx context.Context, server string) (*remote.Session, *config.Server, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	srv, err := o.Server(server)
	if err != nil {
		return nil, nil, err
	}
	if !o.opts.DryRun && !srv.IsLocal() {
		if err := remote.CheckIdentityFile(srv.IdentityFile); err != nil {
			return nil, nil, fmt.Errorf("servers.%s.%w", srv.Name, err)
		}
	}
	s := o.configureSession(ctx, srv, o.opts.DryRun)
	// A dry run always prints commands; a runner seam only replaces the real
	// executor.
	if o.opts.Runner != nil && !o.opts.DryRun {
		s.Runner = o.opts.Runner
	}
	return s, srv, nil
}

// readSession is a real (non-dry-run) ssh session for read-only operations
// such as fetching app records. --dry-run still reads the server; it only
// fakes commands that would change something.
func (o *Ops) readSession(ctx context.Context, server string) (*remote.Session, *config.Server, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	srv, err := o.Server(server)
	if err != nil {
		return nil, nil, err
	}
	if !srv.IsLocal() {
		if err := remote.CheckIdentityFile(srv.IdentityFile); err != nil {
			return nil, nil, fmt.Errorf("servers.%s.%w", srv.Name, err)
		}
	}
	s := o.configureSession(ctx, srv, false)
	if o.opts.Runner != nil {
		s.Runner = o.opts.Runner
	}
	return s, srv, nil
}

// configureSession builds an ssh session or a local one for srv.
func (o *Ops) configureSession(ctx context.Context, srv *config.Server, dryRun bool) *remote.Session {
	if srv.IsLocal() {
		s := remote.NewLocalSessionContext(dryRun, o.opts.Out, o.opts.ErrOut, ctx)
		s.User = srv.User
		return s
	}
	s := remote.NewSessionContext(srv.Host, dryRun, o.opts.Out, o.opts.ErrOut, ctx)
	s.IdentityFile = srv.IdentityFile
	return s
}

func (o *Ops) ensureAgent(s *remote.Session) error {
	return s.EnsureAgent(o.opts.Agent)
}

// Setup prepares or re-runs the server using its desired settings and app
// records.
func (o *Ops) Setup(ctx context.Context, server string) error {
	state, err := o.FetchConfig(ctx, server)
	if err != nil {
		return err
	}
	records, err := state.DesiredRecords()
	if err != nil {
		return err
	}
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("setup", state.Settings.AdminUser, state.Settings.CaddyImage, AppsSpecFor(records))
}

// RecordSetup marks the current desired admin user and app:uid list as already
// applied, without running setup. config push uses it for legacy servers.
func (o *Ops) RecordSetup(ctx context.Context, server, adminUser, appsSpec string) error {
	if err := config.ValidateServerSettings(adminUser, config.DefaultCaddyImage); err != nil {
		return err
	}
	if o.opts.DryRun {
		fmt.Fprintf(o.opts.Out, "# would record setup as applied for %s\n", adminUser)
		return nil
	}
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("setup-record", adminUser, appsSpec)
}

// LockSSH disables password and root SSH logins.
func (o *Ops) LockSSH(ctx context.Context, server string) error {
	state, err := o.FetchConfig(ctx, server)
	if err != nil {
		return err
	}
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("lock-ssh", state.Settings.AdminUser)
}

// VPSUpdate upgrades packages and reports whether a reboot is required.
func (o *Ops) VPSUpdate(ctx context.Context, server string) (bool, error) {
	s, _, err := o.session(ctx, server)
	if err != nil {
		return false, err
	}
	if err := o.ensureAgent(s); err != nil {
		return false, err
	}
	out, err := s.OutputAgent("vps-update")
	if len(out) > 0 {
		fmt.Fprint(o.opts.Out, string(out))
	}
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "reboot_required=yes" {
			return true, nil
		}
	}
	return false, nil
}

// VPSReboot reboots the server. Callers confirm first.
func (o *Ops) VPSReboot(ctx context.Context, server string) error {
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("reboot")
}

// ProxySync renders the Caddyfile from server records and reloads Caddy.
func (o *Ops) ProxySync(ctx context.Context, server string) error {
	return o.proxyApply(ctx, server, false)
}

// ProxyRestart renders the Caddyfile from server records, activates it with
// the desired image and recreates the Caddy container so pending changes apply.
func (o *Ops) ProxyRestart(ctx context.Context, server string) error {
	return o.proxyApply(ctx, server, true)
}

// ProxyUpdate is the old name for ProxySync, kept for the TUI.
func (o *Ops) ProxyUpdate(ctx context.Context, server string) error {
	return o.ProxySync(ctx, server)
}

func (o *Ops) proxyApply(ctx context.Context, server string, restart bool) error {
	state, err := o.FetchConfig(ctx, server)
	if err != nil {
		return err
	}
	if missing := state.RunningWithoutDesired(); len(missing) > 0 {
		return fmt.Errorf("%s run on this server but have no desired config; run `ship %s config push` first", strings.Join(missing, ", "), server)
	}
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	caddyfile := state.CaddyfileFor()
	if strings.TrimSpace(caddyfile) == "" {
		return fmt.Errorf("rendered Caddyfile is empty")
	}
	action := "proxy-sync"
	if restart {
		action = "proxy-restart"
	}
	return s.InputAgent([]byte(caddyfile), action, state.Settings.CaddyImage)
}

// ServerSet writes one desired server-wide setting.
func (o *Ops) ServerSet(ctx context.Context, server, key, value string) error {
	state, err := o.FetchConfig(ctx, server)
	if err != nil {
		return err
	}
	admin, caddy := state.Settings.AdminUser, state.Settings.CaddyImage
	switch key {
	case "admin_user":
		admin = value
	case "caddy_image":
		caddy = value
	default:
		return fmt.Errorf("unknown server setting %q (want admin_user or caddy_image)", key)
	}
	newHash, err := o.PutServerSettings(ctx, server, admin, caddy, state.Settings.Hash)
	if err != nil {
		return err
	}
	state.Settings.Hash = newHash
	if key == "caddy_image" {
		fmt.Fprintf(o.opts.Out, "Saved caddy_image. Run `ship %s proxy restart` to apply it.\n", server)
	} else {
		fmt.Fprintf(o.opts.Out, "Saved admin_user. Run `ship %s setup` to apply it.\n", server)
	}
	return nil
}

// Rollback switches an app back to its previous image.
func (o *Ops) Rollback(ctx context.Context, server, app string) error {
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("rollback", app)
}

// Restart recreates an app's containers and waits for health.
func (o *Ops) Restart(ctx context.Context, server, app string) error {
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("restart", app)
}

// Maintenance turns an app's maintenance page on or off.
func (o *Ops) Maintenance(ctx context.Context, server, app string, on bool) error {
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	state := "off"
	if on {
		state = "on"
	}
	return s.RunAgent("maintenance", app, state)
}

// Logs follows an app's logs on a TTY.
func (o *Ops) Logs(ctx context.Context, server, app, service string) error {
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	if service != "" {
		if err := config.ValidateService(service); err != nil {
			return err
		}
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	args := []string{"logs", app}
	if service != "" {
		args = append(args, service)
	}
	return s.RunAgentTTY(args...)
}

// EnvEdit opens the app's secret .env in sudoedit.
func (o *Ops) EnvEdit(ctx context.Context, server, app string) error {
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	env := "/opt/apps/" + app + "/.env"
	if err := s.RunEnvEdit(env); err != nil {
		return err
	}
	if !o.opts.DryRun {
		fmt.Fprintf(o.opts.Out, "Saved. Run: ship %s restart %s\n", server, app)
	}
	return nil
}

// Backups lists an app's archives on the server.
func (o *Ops) Backups(ctx context.Context, server, app string) error {
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("backups", app)
}
