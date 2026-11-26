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
	"github.com/harrisonclewis/ship/internal/render"
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
	if !o.opts.DryRun {
		if err := remote.CheckIdentityFile(srv.IdentityFile); err != nil {
			return nil, nil, fmt.Errorf("servers.%s.%w", srv.Name, err)
		}
	}
	s := remote.NewSessionContext(srv.Host, o.opts.DryRun, o.opts.Out, o.opts.ErrOut, ctx)
	s.IdentityFile = srv.IdentityFile
	if o.opts.Runner != nil {
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
	if err := remote.CheckIdentityFile(srv.IdentityFile); err != nil {
		return nil, nil, fmt.Errorf("servers.%s.%w", srv.Name, err)
	}
	s := remote.NewSessionContext(srv.Host, false, o.opts.Out, o.opts.ErrOut, ctx)
	s.IdentityFile = srv.IdentityFile
	if o.opts.Runner != nil {
		s.Runner = o.opts.Runner
	}
	return s, srv, nil
}

func (o *Ops) ensureAgent(s *remote.Session) error {
	return s.EnsureAgent(o.opts.Agent)
}

// Setup prepares or re-runs the server.
func (o *Ops) Setup(ctx context.Context, server string) error {
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("setup", srv.AdminUser, srv.CaddyImage, srv.AppsSpec())
}

// LockSSH disables password and root SSH logins.
func (o *Ops) LockSSH(ctx context.Context, server string) error {
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("lock-ssh", srv.AdminUser)
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

// ProxyUpdate renders this server's Caddyfile and activates it.
func (o *Ops) ProxyUpdate(ctx context.Context, server string) error {
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	caddyfile := render.Caddyfile(srv)
	if strings.TrimSpace(caddyfile) == "" {
		return fmt.Errorf("rendered Caddyfile is empty")
	}
	return s.InputAgent([]byte(caddyfile), "proxy-update", srv.CaddyImage)
}

// Rollback switches an app back to its previous image.
func (o *Ops) Rollback(ctx context.Context, server, app string) error {
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if _, err := srv.App(app); err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("rollback", app)
}

// Restart recreates an app's containers and waits for health.
func (o *Ops) Restart(ctx context.Context, server, app string) error {
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if _, err := srv.App(app); err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("restart", app)
}

// Maintenance turns an app's maintenance page on or off.
func (o *Ops) Maintenance(ctx context.Context, server, app string, on bool) error {
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if _, err := srv.App(app); err != nil {
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
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if _, err := srv.App(app); err != nil {
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
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if _, err := srv.App(app); err != nil {
		return err
	}
	env := "/opt/apps/" + app + "/.env"
	dir := "/opt/apps/" + app
	remoteCmd := fmt.Sprintf("sudo -n install -d -m 0755 %s; sudo -n test -f %s || sudo -n install -m 0600 /dev/null %s; sudo -n chmod 0600 %s; sudo -n -e %s && echo 'Saved. Run: ship %s restart %s'",
		remote.ShellQuote(dir), remote.ShellQuote(env), remote.ShellQuote(env), remote.ShellQuote(env), remote.ShellQuote(env), srv.Name, app)
	return s.RunSSH(remoteCmd, true)
}

// Backups lists an app's archives on the server.
func (o *Ops) Backups(ctx context.Context, server, app string) error {
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if _, err := srv.App(app); err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return s.RunAgent("backups", app)
}
