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
// tree for image apps, or means latest for release apps.
func (o *Ops) Deploy(ctx context.Context, server, app, version string, allowDirty bool) error {
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}
	return o.deployApp(ctx, s, srv, app, version, allowDirty)
}

// DeployAll deploys every app on a server in name order. A failed app is
// recorded and the remaining apps still run. It prints a summary and returns
// an error when anything failed.
func (o *Ops) DeployAll(ctx context.Context, server string, allowDirty bool) (DeploySummary, error) {
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return DeploySummary{Server: server}, err
	}
	summary := DeploySummary{Server: srv.Name}
	if err := o.ensureAgent(s); err != nil {
		return summary, err
	}
	for _, name := range srv.AppNames() {
		if err := ctx.Err(); err != nil {
			summary.Failed = append(summary.Failed, DeployFailure{App: name, Err: err})
			return summary, err
		}
		fmt.Fprintf(o.opts.Out, "\n== deploy %s\n", name)
		if err := o.deployApp(ctx, s, srv, name, "", allowDirty); err != nil {
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

func (o *Ops) deployApp(ctx context.Context, s *remote.Session, srv *config.Server, name, version string, allowDirty bool) error {
	app, err := srv.App(name)
	if err != nil {
		return err
	}
	if app.MaintenanceEnabled() {
		if err := checkMaintenanceSupport(s, srv); err != nil {
			return err
		}
	}

	built := false
	switch app.Source.Type {
	case "github-release":
		if version == "" {
			version = "latest"
		} else if err := config.ValidateReleaseTag(version); err != nil {
			return err
		}
	case "image":
		if version == "" {
			version, err = buildImage(ctx, s, srv, name, allowDirty)
			if err != nil {
				return err
			}
			built = true
		} else if err := config.ValidateImageVersion(version); err != nil {
			return err
		}
	}

	// Load the image first so a failed transfer leaves the app's files alone.
	if built {
		ref := srv.ImageRef(name, version)
		if err := s.ShipImage(ref, "image-load", name, version); err != nil {
			return err
		}
	}
	if err := uploadAppFiles(s, srv, name); err != nil {
		return err
	}
	if s.DryRun && strings.TrimSpace(app.Deploy.Commands) != "" {
		service := app.Upstream[:strings.LastIndex(app.Upstream, ":")]
		fmt.Fprintf(s.Out, "# deploy commands will run with sh -e in a one-off %s container of %s:\n%s\n",
			service, srv.ImageRef(name, version), app.Deploy.Commands)
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
		return fmt.Errorf("the proxy on %s has no maintenance mount; run `ship %s setup` and `ship %s proxy update` first",
			srv.Name, srv.Name, srv.Name)
	}
	return nil
}

func buildImage(ctx context.Context, s *remote.Session, srv *config.Server, name string, allowDirty bool) (string, error) {
	app := srv.Apps[name]
	dir := app.Source.Path
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s.source.path: %q is not a directory", name, dir)
	}
	if fi, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil || fi.IsDir() {
		return "", fmt.Errorf("image app %s has no Dockerfile in %s (see docs/deploying.md)", name, dir)
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
	if err := runBuildCommands(s, app); err != nil {
		return "", err
	}
	ref := srv.ImageRef(name, version)
	if err := s.Local("docker", "buildx", "build", "--platform", srv.Arch, "--tag", ref, "--load", dir); err != nil {
		return "", fmt.Errorf("docker buildx: %w", err)
	}
	fmt.Fprintf(s.Out, "built %s\n", ref)
	return version, nil
}

// runBuildCommands runs an image app's build script locally in its source
// directory, as one `sh` process with set -e, before docker buildx runs.
func runBuildCommands(s *remote.Session, app *config.App) error {
	if strings.TrimSpace(app.Build.Commands) == "" {
		return nil
	}
	if runtime.GOOS == "windows" && !s.DryRun {
		if _, err := exec.LookPath("sh"); err != nil {
			return fmt.Errorf("build commands need sh on PATH; install Git for Windows and run ship from Git Bash")
		}
	}
	script := "set -e\ncd " + remote.ShellQuote(app.Source.Path) + "\n" + app.Build.Commands
	fmt.Fprintf(s.Out, "running build commands in %s\n", app.Source.Path)
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

func uploadAppFiles(s *remote.Session, srv *config.Server, name string) error {
	app := srv.Apps[name]
	composePath := srv.ComposePath(name)
	compose, err := os.ReadFile(composePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", composePath, err)
	}
	files := []appFile{{Name: "compose.yaml", Mode: 0o644, Data: compose}}
	if app.Source.Type == "github-release" {
		dockerfilePath := srv.DockerfilePath(name)
		dockerfile, err := os.ReadFile(dockerfilePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", dockerfilePath, err)
		}
		files = append(files, appFile{Name: "Dockerfile", Mode: 0o644, Data: dockerfile})
	}
	files = append(files,
		appFile{Name: ".ship-meta", Mode: 0o644, Data: []byte(srv.Meta(name))},
		appFile{Name: ".ship-env", Mode: 0o644, Data: []byte(srv.ShipEnv(name))},
		appFile{Name: ".ship-deploy.sh", Mode: 0o644, Data: []byte(app.Deploy.Commands)},
	)
	blob, err := appFilesTar(files)
	if err != nil {
		return err
	}
	return s.InputAgent(blob, "app-files", name)
}
