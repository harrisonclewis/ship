package ops

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/remote"
)

// DeployFailure is one app that failed during DeployAll.
type DeployFailure struct {
	App string
	Err error
}

// DeploySummary is the outcome of DeployAll.
type DeploySummary struct {
	Server    string
	Succeeded []string
	Failed    []DeployFailure
}

// OK reports whether every app deployed.
func (s DeploySummary) OK() bool { return len(s.Failed) == 0 }

func (s DeploySummary) summaryError() error {
	if s.OK() {
		return nil
	}
	parts := make([]string, 0, len(s.Failed))
	for _, f := range s.Failed {
		parts = append(parts, f.App+": "+f.Err.Error())
	}
	return fmt.Errorf("%d of %d deploys failed on %s: %s",
		len(s.Failed), len(s.Succeeded)+len(s.Failed), s.Server, strings.Join(parts, "; "))
}

// Deploy deploys one app on a server. An empty version builds the current git
// tree for image apps, or means latest for release apps. The app's record and
// files are read from the server, never from the local file.
func (o *Ops) Deploy(ctx context.Context, server, app, version string, allowDirty bool) error {
	state, err := o.FetchConfig(ctx, server)
	if err != nil {
		return err
	}
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	o.applyServerConfig(srv, state)
	return o.deployApp(ctx, s, srv, state, app, version, allowDirty)
}

// DeployAll deploys every desired app on a server in name order. A failed app
// is recorded and the remaining apps still run.
func (o *Ops) DeployAll(ctx context.Context, server string, allowDirty bool) (DeploySummary, error) {
	state, err := o.FetchConfig(ctx, server)
	if err != nil {
		return DeploySummary{Server: server}, err
	}
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return DeploySummary{Server: server}, err
	}
	summary := DeploySummary{Server: srv.Name}
	if err := o.ensureAgent(s); err != nil {
		return summary, err
	}
	o.applyServerConfig(srv, state)
	for _, name := range state.AppNames() {
		if err := ctx.Err(); err != nil {
			summary.Failed = append(summary.Failed, DeployFailure{App: name, Err: err})
			return summary, err
		}
		fmt.Fprintf(o.opts.Out, "\n== deploy %s\n", name)
		if err := o.deployApp(ctx, s, srv, state, name, "", allowDirty); err != nil {
			summary.Failed = append(summary.Failed, DeployFailure{App: name, Err: err})
			continue
		}
		summary.Succeeded = append(summary.Succeeded, name)
	}
	fmt.Fprintf(o.opts.Out, "\ndeploy summary for %s: %d succeeded, %d failed\n",
		srv.Name, len(summary.Succeeded), len(summary.Failed))
	for _, f := range summary.Failed {
		fmt.Fprintf(o.opts.Out, "  failed: %s: %v\n", f.App, f.Err)
	}
	return summary, summary.summaryError()
}

// applyServerConfig copies desired records and the detected architecture onto
// the runtime server value so the existing meta/image helpers keep working.
func (o *Ops) applyServerConfig(srv *config.Server, state *ServerConfig) {
	apps := make(map[string]*config.App, len(state.Apps))
	for name, da := range state.Apps {
		if da.Record != nil {
			apps[name] = da.Record
		}
	}
	srv.Apps = apps
	arch, err := config.NormalizeArch(state.Arch)
	if err != nil {
		arch = config.DefaultArch
	}
	srv.Arch = arch
}

func (o *Ops) deployApp(ctx context.Context, s *remote.Session, srv *config.Server, state *ServerConfig, name, version string, allowDirty bool) error {
	da := state.Apps[name]
	if da == nil {
		return fmt.Errorf("app %q is not configured on %s; run `ship %s config push` or `ship %s app create %s`", name, srv.Name, srv.Name, srv.Name, name)
	}
	if da.Record == nil {
		reason := "the record is missing"
		if da.RecordErr != "" {
			reason = da.RecordErr
		}
		return fmt.Errorf("app %s on %s cannot be deployed: %s", name, srv.Name, reason)
	}
	app := da.Record
	if app.MaintenanceEnabled() {
		if err := checkMaintenanceSupport(s, srv); err != nil {
			return err
		}
	}

	built := false
	sourceRef := ""
	switch app.Source.Type {
	case "github-release":
		if version == "" {
			version = "latest"
		} else if err := config.ValidateReleaseTag(version); err != nil {
			return err
		}
	case "image":
		repo, gitSource, err := o.gitSource(srv.Name, name)
		if err != nil {
			return err
		}
		if gitSource {
			// Reject a bad selector before touching the cache or network.
			sel, err := parseGitSelector(version)
			if err != nil {
				return err
			}
			resolved := gitResolution{Commit: "dry-run", Ref: dryRunGitRef(sel), ImageVersion: "dry-run"}
			if sel.kind == "release-asset" {
				resolved.Tag = sel.value
				resolved.Asset = sel.asset
				resolved.ImageVersion = "asset-dry-run"
			}
			cache, err := o.ensureGitCache(ctx, s, srv.Name, repo)
			if err != nil {
				return err
			}
			if !s.DryRun {
				resolved, err = o.resolveGitRevision(ctx, cache, repo, version)
				if err != nil {
					return err
				}
			}
			builtVersion := resolved.ImageVersion
			if resolved.Asset != "" {
				builtVersion, err = o.buildReleaseAssetImage(ctx, s, srv, name, resolved, repo, cache)
				if err != nil {
					return err
				}
			} else {
				builtVersion, err = o.buildGitImage(ctx, s, srv, name, app, resolved, cache)
				if err != nil {
					return err
				}
			}
			version = builtVersion
			sourceRef = resolved.Ref
			built = true
		} else if version == "" {
			builtVersion, err := o.buildImage(ctx, s, srv, name, allowDirty)
			if err != nil {
				return err
			}
			version = builtVersion
			built = true
		} else if err := config.ValidateImageVersion(version); err != nil {
			return err
		}
	default:
		return fmt.Errorf("app %s has unsupported source type %q", name, app.Source.Type)
	}

	// Load the image first so a failed transfer leaves the running app alone.
	if built {
		ref := srv.ImageRef(name, version)
		if err := s.ShipImage(ref, "image-load", name, version); err != nil {
			return err
		}
	}
	if s.DryRun && strings.TrimSpace(app.Deploy.Commands) != "" {
		service := app.Upstream[:strings.LastIndex(app.Upstream, ":")]
		fmt.Fprintf(s.Out, "# deploy commands will run with sh -e in a one-off %s container of %s:\n%s\n",
			service, srv.ImageRef(name, version), app.Deploy.Commands)
	}
	if sourceRef != "" {
		return s.RunAgent("deploy", name, version, sourceRef)
	}
	return s.RunAgent("deploy", name, version)
}

// checkMaintenanceSupport fails before a build or upload when the proxy on the
// server cannot serve the maintenance page yet.
func checkMaintenanceSupport(s *remote.Session, srv *config.Server) error {
	if s.DryRun {
		fmt.Fprintln(s.Out, "# would check that the proxy has the maintenance mount")
		return nil
	}
	out, err := s.OutputAgent("maintenance-check")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "maintenance=ok" {
		return fmt.Errorf("the proxy on %s has no maintenance mount; run `ship %s setup` and `ship %s proxy sync` first",
			srv.Name, srv.Name, srv.Name)
	}
	return nil
}

// checkLocalBuild keeps the local-connection warning and Docker access check
// in one place so directory and Git builds behave the same.
func checkLocalBuild(s *remote.Session, name string) error {
	if !s.LocalMode {
		return nil
	}
	// Building on the server needs Docker access as well as CPU and memory
	// that the running apps share.
	if _, err := s.OutputLocal("docker", "info"); err != nil {
		return fmt.Errorf("cannot reach the local Docker daemon; add your user to the docker group or run ship as root: %w", err)
	}
	fmt.Fprintf(s.Out, "warning: building %s uses this server's CPU and memory\n", name)
	return nil
}

func (o *Ops) buildImage(ctx context.Context, s *remote.Session, srv *config.Server, name string, allowDirty bool) (string, error) {
	app := srv.Apps[name]
	dir := srv.Sources[name]
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("set sources.%s in ship.yaml to build it", name)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("sources.%s: %q is not a directory", name, dir)
	}
	if fi, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil || fi.IsDir() {
		return "", fmt.Errorf("image app %s has no Dockerfile in %s (see docs/guides/deploy-an-app.md)", name, dir)
	}
	if err := checkLocalBuild(s, name); err != nil {
		return "", err
	}

	version := "dry-run"
	if !s.DryRun {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		status, err := s.OutputLocal("git", "-C", dir, "status", "--porcelain")
		if err != nil {
			return "", fmt.Errorf("git status %s: %w", dir, err)
		}
		dirty := strings.TrimSpace(string(status)) != ""
		if dirty && !allowDirty {
			return "", fmt.Errorf("%s has uncommitted changes; commit them or pass --allow-dirty", dir)
		}
		desc, err := s.OutputLocal("git", "-C", dir, "describe", "--tags", "--always")
		if err != nil {
			return "", fmt.Errorf("git describe %s: %w", dir, err)
		}
		version = strings.TrimSpace(string(desc))
		if dirty {
			version += "-dirty-" + time.Now().UTC().Format("20060102T150405Z")
		}
		if err := config.ValidateImageVersion(version); err != nil {
			return "", err
		}
	}
	if err := o.runBuildCommands(s, srv, name, app, dir); err != nil {
		return "", err
	}
	ref := srv.ImageRef(name, version)
	arch := srv.Arch
	if arch == "" {
		arch = config.DefaultArch
	}
	if err := s.Local("docker", "buildx", "build", "--platform", arch, "--tag", ref, "--load", dir); err != nil {
		return "", fmt.Errorf("docker buildx: %w", err)
	}
	fmt.Fprintf(s.Out, "built %s\n", ref)
	return version, nil
}

// runBuildCommands runs an image app's build script locally in its source
// directory, as one `sh` process with set -e, before docker buildx runs. The
// script comes from the server, so a new or changed one needs this machine's
// approval first.
func (o *Ops) runBuildCommands(s *remote.Session, srv *config.Server, name string, app *config.App, dir string) error {
	if strings.TrimSpace(app.Build.Commands) == "" {
		return nil
	}
	if err := o.ensureBuildApproval(srv.Name, name, app.Build.Commands); err != nil {
		return err
	}
	if runtime.GOOS == "windows" && !s.DryRun {
		if _, err := exec.LookPath("sh"); err != nil {
			return fmt.Errorf("build commands need sh on PATH; install Git for Windows and run ship from Git Bash")
		}
	}
	script := "set -e\ncd " + remote.ShellQuote(dir) + "\n" + app.Build.Commands
	fmt.Fprintf(s.Out, "running build commands in %s\n", dir)
	if err := s.Local("sh", "-c", script); err != nil {
		return fmt.Errorf("build commands: %w", err)
	}
	return nil
}

type appFile struct {
	Name string
	Mode int64
	Data []byte
}

var appFileNames = map[string]bool{
	"compose.yaml":    true,
	"Dockerfile":      true,
	"ship-app.yaml":   true,
	".ship-meta":      true,
	".ship-env":       true,
	".ship-deploy.sh": true,
}

// appFilesTar packs the files sent to the agent in one upload. Only plain
// basenames from the allowed set are accepted; the agent validates them again.
func appFilesTar(files []appFile) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	seen := map[string]bool{}
	for _, f := range files {
		if !appFileNames[f.Name] || f.Name != filepath.Base(f.Name) ||
			strings.ContainsAny(f.Name, `/\`) {
			return nil, fmt.Errorf("invalid app file name %q", f.Name)
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("duplicate app file %q", f.Name)
		}
		seen[f.Name] = true
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{
			Name:     f.Name,
			Mode:     mode,
			Size:     int64(len(f.Data)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(f.Data); err != nil {
			return nil, err
		}
	}
	for _, want := range []string{"compose.yaml", ".ship-meta", ".ship-env"} {
		if !seen[want] {
			return nil, fmt.Errorf("app files are missing %s", want)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
