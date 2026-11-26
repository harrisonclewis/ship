package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/ops"
	"github.com/harrisonclewis/ship/internal/remote"
)

// Action identifies an operation the TUI can run.
type Action int

const (
	ActDeploy Action = iota
	ActDeployAll
	ActRollback
	ActRestart
	ActBackup
	ActBackups
	ActRestore
	ActProxyUpdate
	ActSetup
	ActLockSSH
	ActVPSUpdate
	ActVPSReboot
	ActMaintenance
)

// Request is one operation to run against a server.
type Request struct {
	Action        Action
	Server        string
	App           string
	Version       string
	Archive       string
	Pull          bool
	AllowDirty    bool
	MaintenanceOn bool
}

// Result carries the small amount of structured data some operations return.
type Result struct {
	Summary        *ops.DeploySummary
	RebootRequired bool
}

// BackupEntry is one archive on the server.
type BackupEntry struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// TerminalKind selects an interactive operation.
type TerminalKind int

const (
	TerminalLogs TerminalKind = iota
	TerminalEnvEdit
)

// TerminalRequest describes an interactive ssh handoff.
type TerminalRequest struct {
	Kind    TerminalKind
	Server  string
	App     string
	Service string
}

// Backend is the small surface the TUI needs from internal/ops. Tests provide
// a fake implementation.
type Backend interface {
	Config() *config.Config
	SetConfig(*config.Config)
	Servers() []string
	Server(name string) (*config.Server, error)
	Status(ctx context.Context, server, app string) (ops.StatusReport, error)
	ListBackups(ctx context.Context, server, app string) ([]BackupEntry, error)
	Run(ctx context.Context, req Request, out io.Writer) (Result, error)
	TerminalCommand(ctx context.Context, req TerminalRequest) (cmd *exec.Cmd, display string, err error)
}

type realBackend struct {
	cfg    *config.Config
	dryRun bool
	agent  []byte
	in     io.Reader
}

func newRealBackend(cfg *config.Config, dryRun bool, agent []byte, in io.Reader) *realBackend {
	return &realBackend{cfg: cfg, dryRun: dryRun, agent: agent, in: in}
}

func (b *realBackend) Config() *config.Config { return b.cfg }

func (b *realBackend) SetConfig(cfg *config.Config) { b.cfg = cfg }

func (b *realBackend) Servers() []string { return b.cfg.ServerNames() }

func (b *realBackend) Server(name string) (*config.Server, error) { return b.cfg.Server(name) }

func (b *realBackend) ops(out io.Writer) *ops.Ops {
	return ops.New(b.cfg, ops.Options{
		DryRun: b.dryRun,
		Out:    out,
		ErrOut: out,
		In:     b.in,
		Agent:  b.agent,
	})
}

func (b *realBackend) Status(ctx context.Context, server, app string) (ops.StatusReport, error) {
	return b.ops(io.Discard).Status(ctx, server, app)
}

func (b *realBackend) ListBackups(ctx context.Context, server, app string) ([]BackupEntry, error) {
	var buf bytes.Buffer
	if err := b.ops(&buf).Backups(ctx, server, app); err != nil {
		return nil, err
	}
	return parseBackupEntries(buf.String()), nil
}

func parseBackupEntries(out string) []BackupEntry {
	var entries []BackupEntry
	for _, line := range strings.Split(out, "\n") {
		fields := map[string]string{}
		for _, f := range strings.Fields(line) {
			if k, v, ok := strings.Cut(f, "="); ok {
				fields[k] = v
			}
		}
		p := fields["path"]
		if p == "" {
			continue
		}
		entry := BackupEntry{Path: p}
		if n, err := strconv.ParseInt(fields["size"], 10, 64); err == nil {
			entry.Size = n
		}
		if n, err := strconv.ParseInt(fields["mtime"], 10, 64); err == nil {
			entry.ModTime = time.Unix(n, 0)
		}
		entries = append(entries, entry)
	}
	return entries
}

func (b *realBackend) Run(ctx context.Context, req Request, out io.Writer) (Result, error) {
	op := b.ops(out)
	var result Result
	var err error
	switch req.Action {
	case ActDeploy:
		err = op.Deploy(ctx, req.Server, req.App, req.Version, req.AllowDirty)
	case ActDeployAll:
		summary, e := op.DeployAll(ctx, req.Server, req.AllowDirty)
		result.Summary = &summary
		err = e
	case ActRollback:
		err = op.Rollback(ctx, req.Server, req.App)
	case ActRestart:
		err = op.Restart(ctx, req.Server, req.App)
	case ActBackup:
		target := req.App
		if target == "" {
			target = "all"
		}
		_, err = op.Backup(ctx, req.Server, target, req.Pull)
	case ActBackups:
		err = op.Backups(ctx, req.Server, req.App)
	case ActRestore:
		err = op.Restore(ctx, req.Server, req.App, req.Archive)
	case ActProxyUpdate:
		err = op.ProxyUpdate(ctx, req.Server)
	case ActSetup:
		err = op.Setup(ctx, req.Server)
	case ActLockSSH:
		err = op.LockSSH(ctx, req.Server)
	case ActVPSUpdate:
		result.RebootRequired, err = op.VPSUpdate(ctx, req.Server)
	case ActVPSReboot:
		_ = op.VPSReboot(ctx, req.Server)
	case ActMaintenance:
		err = op.Maintenance(ctx, req.Server, req.App, req.MaintenanceOn)
	default:
		err = fmt.Errorf("unknown action")
	}
	return result, err
}

func (b *realBackend) TerminalCommand(ctx context.Context, req TerminalRequest) (*exec.Cmd, string, error) {
	srv, err := b.cfg.Server(req.Server)
	if err != nil {
		return nil, "", err
	}
	if _, err := srv.App(req.App); err != nil {
		return nil, "", err
	}
	if !b.dryRun {
		if err := remote.CheckIdentityFile(srv.IdentityFile); err != nil {
			return nil, "", fmt.Errorf("servers.%s.%w", srv.Name, err)
		}
	}
	switch req.Kind {
	case TerminalLogs:
		if req.Service != "" {
			if err := config.ValidateService(req.Service); err != nil {
				return nil, "", err
			}
		}
		sess := remote.NewSessionContext(srv.Host, b.dryRun, io.Discard, io.Discard, ctx)
		sess.IdentityFile = srv.IdentityFile
		if !b.dryRun {
			if err := sess.EnsureAgent(b.agent); err != nil {
				return nil, "", err
			}
		}
		args := []string{"logs", req.App}
		if req.Service != "" {
			args = append(args, req.Service)
		}
		argv := sess.AgentArgv(true, args...)
		display := "$ " + remote.ShellJoin(append([]string{"ssh"}, argv...))
		if b.dryRun {
			return nil, display, nil
		}
		return exec.CommandContext(ctx, "ssh", argv...), display, nil
	case TerminalEnvEdit:
		envPath := "/opt/apps/" + req.App + "/.env"
		dir := path.Dir(envPath)
		remoteCmd := fmt.Sprintf("sudo -n install -d -m 0755 %s; sudo -n test -f %s || sudo -n install -m 0600 /dev/null %s; sudo -n chmod 0600 %s; sudo -n -e %s && echo 'Saved. Run: ship %s restart %s'",
			remote.ShellQuote(dir), remote.ShellQuote(envPath), remote.ShellQuote(envPath),
			remote.ShellQuote(envPath), remote.ShellQuote(envPath), srv.Name, req.App)
		argv := remote.SSHArgv(srv.Host, srv.IdentityFile, remoteCmd, true)
		display := "$ " + remote.ShellJoin(append([]string{"ssh"}, argv...))
		if b.dryRun {
			return nil, display, nil
		}
		return exec.CommandContext(ctx, "ssh", argv...), display, nil
	default:
		return nil, "", fmt.Errorf("unknown terminal action")
	}
}

func (r Request) actionLabel() string {
	switch r.Action {
	case ActDeploy:
		label := "Deploy " + r.App
		if r.Version != "" {
			label += " " + r.Version
		}
		return label
	case ActDeployAll:
		return "Deploy all"
	case ActRollback:
		return "Rollback " + r.App
	case ActRestart:
		return "Restart " + r.App
	case ActBackup:
		target := r.App
		if target == "" {
			target = "all"
		}
		return "Backup " + target
	case ActBackups:
		return "List backups " + r.App
	case ActRestore:
		return "Restore " + r.App
	case ActProxyUpdate:
		return "Proxy update"
	case ActSetup:
		return "Setup"
	case ActLockSSH:
		return "Lock SSH"
	case ActVPSUpdate:
		return "VPS update"
	case ActVPSReboot:
		return "Reboot"
	case ActMaintenance:
		if r.MaintenanceOn {
			return "Maintenance on"
		}
		return "Maintenance off"
	default:
		return "Operation"
	}
}

func (r Request) target() string {
	if r.App != "" {
		return r.App
	}
	return "-"
}

// dryRunTerminalCommand records a terminal command as activity output.
func dryRunTerminalCommand(display string) string {
	if display == "" {
		return ""
	}
	return "dry run: the TUI would hand the terminal to:\n" + display + "\n"
}
