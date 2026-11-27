package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
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
	ActProxySync
	ActProxyRestart
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
	Variances(ctx context.Context, server string) (ops.VarianceReport, error)
	ListBackups(ctx context.Context, server, app string) ([]BackupEntry, error)
	Run(ctx context.Context, req Request, out io.Writer) (Result, error)
	PutDesiredApp(ctx context.Context, server, app string, record *config.App, compose, dockerfile []byte, arch, expectedHash string) (string, error)
	PutServerSettings(ctx context.Context, server, adminUser, caddyImage, expectedHash string) (string, error)
	RemoveDesiredApp(ctx context.Context, server, app, expectedHash string) error
	RecordBuildApproval(ctx context.Context, server, app, script string) error
	GitCommits(ctx context.Context, server, app string) ([]ops.GitCommit, error)
	GitReleases(ctx context.Context, server, app string) ([]ops.GitRelease, error)
	GitReleaseAssets(ctx context.Context, server, app, tag string) ([]ops.GitAsset, error)
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

func (b *realBackend) Variances(ctx context.Context, server string) (ops.VarianceReport, error) {
	return b.ops(io.Discard).Variances(ctx, server)
}

func (b *realBackend) PutDesiredApp(ctx context.Context, server, app string, record *config.App, compose, dockerfile []byte, arch, expectedHash string) (string, error) {
	return b.ops(io.Discard).PutDesiredApp(ctx, server, app, record, compose, dockerfile, arch, expectedHash)
}

func (b *realBackend) PutServerSettings(ctx context.Context, server, adminUser, caddyImage, expectedHash string) (string, error) {
	return b.ops(io.Discard).PutServerSettings(ctx, server, adminUser, caddyImage, expectedHash)
}

func (b *realBackend) RemoveDesiredApp(ctx context.Context, server, app, expectedHash string) error {
	return b.ops(io.Discard).RemoveDesiredApp(ctx, server, app, expectedHash)
}

func (b *realBackend) RecordBuildApproval(ctx context.Context, server, app, script string) error {
	return b.ops(io.Discard).RecordBuildApproval(server, app, script)
}

func (b *realBackend) GitCommits(ctx context.Context, server, app string) ([]ops.GitCommit, error) {
	return b.ops(io.Discard).GitCommits(ctx, server, app)
}

func (b *realBackend) GitReleases(ctx context.Context, server, app string) ([]ops.GitRelease, error) {
	return b.ops(io.Discard).GitReleases(ctx, server, app)
}

func (b *realBackend) GitReleaseAssets(ctx context.Context, server, app, tag string) ([]ops.GitAsset, error) {
	return b.ops(io.Discard).GitReleaseAssets(ctx, server, app, tag)
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
	case ActProxySync:
		err = op.ProxySync(ctx, req.Server)
	case ActProxyRestart:
		err = op.ProxyRestart(ctx, req.Server)
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
	if err := config.ValidateAppName(req.App); err != nil {
		return nil, "", err
	}
	if !b.dryRun && !srv.IsLocal() {
		if err := remote.CheckIdentityFile(srv.IdentityFile); err != nil {
			return nil, "", fmt.Errorf("servers.%s.%w", srv.Name, err)
		}
	}
	sess := b.session(ctx, srv)
	if !b.dryRun {
		if err := sess.EnsureAgent(b.agent); err != nil {
			return nil, "", err
		}
	}
	switch req.Kind {
	case TerminalLogs:
		if req.Service != "" {
			if err := config.ValidateService(req.Service); err != nil {
				return nil, "", err
			}
		}
		args := []string{"logs", req.App}
		if req.Service != "" {
			args = append(args, req.Service)
		}
		program, argv := sess.AgentCommand(true, args...)
		display := "$ " + remote.ShellJoin(append([]string{program}, argv...))
		if b.dryRun {
			return nil, display, nil
		}
		return exec.CommandContext(ctx, program, argv...), display, nil
	case TerminalEnvEdit:
		envPath := "/opt/apps/" + req.App + "/.env"
		program, argv := sess.EnvEditCommand(envPath, true)
		display := "$ " + remote.ShellJoin(append([]string{program}, argv...))
		if b.dryRun {
			return nil, display, nil
		}
		return exec.CommandContext(ctx, program, argv...), display, nil
	default:
		return nil, "", fmt.Errorf("unknown terminal action")
	}
}

// session builds an ssh session or a local one for srv.
func (b *realBackend) session(ctx context.Context, srv *config.Server) *remote.Session {
	if srv.IsLocal() {
		s := remote.NewLocalSessionContext(b.dryRun, io.Discard, io.Discard, ctx)
		s.User = srv.User
		return s
	}
	s := remote.NewSessionContext(srv.Host, b.dryRun, io.Discard, io.Discard, ctx)
	s.IdentityFile = srv.IdentityFile
	return s
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
	case ActProxySync:
		return "Sync Caddy"
	case ActProxyRestart:
		return "Restart Caddy"
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
