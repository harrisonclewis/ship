package ops

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/remote"
)

type fakeRunner struct {
	cmds     []remote.Cmd
	onRun    func(remote.Cmd) error
	onOutput func(remote.Cmd) ([]byte, error)
}

func (f *fakeRunner) Run(c remote.Cmd) error {
	f.cmds = append(f.cmds, c)
	if f.onRun != nil {
		return f.onRun(c)
	}
	return nil
}

func (f *fakeRunner) Output(c remote.Cmd) ([]byte, error) {
	f.cmds = append(f.cmds, c)
	if f.onOutput != nil {
		return f.onOutput(c)
	}
	return nil, nil
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func buildTestApp(t *testing.T, commands string) (*config.Server, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &config.Server{
		Name: "prod",
		Arch: "linux/amd64",
		Apps: map[string]*config.App{
			"app": {
				Source: config.Source{Type: "image", Path: dir},
				Build:  config.BuildSpec{Commands: commands},
			},
		},
	}
	return srv, dir
}

func TestBuildCommandsRunBeforeBuildx(t *testing.T) {
	srv, dir := buildTestApp(t, "echo one\necho two\n")
	r := &fakeRunner{onOutput: func(c remote.Cmd) ([]byte, error) {
		if hasArg(c.Args, "--porcelain") {
			return nil, nil
		}
		if c.Path == "git" {
			return []byte("v1.2.3\n"), nil
		}
		return nil, nil
	}}
	var out bytes.Buffer
	s := &remote.Session{Host: "ops@host", Out: &out, Err: io.Discard, Runner: r}

	if _, err := buildImage(context.Background(), s, srv, "app", false); err != nil {
		t.Fatal(err)
	}
	shAt, buildxAt := -1, -1
	for i, c := range r.cmds {
		if c.Path == "sh" {
			shAt = i
			script := c.Args[len(c.Args)-1]
			for _, want := range []string{"set -e", "cd " + remote.ShellQuote(dir), "echo one", "echo two"} {
				if !strings.Contains(script, want) {
					t.Errorf("build script missing %q:\n%s", want, script)
				}
			}
		}
		if c.Path == "docker" && len(c.Args) > 0 && c.Args[0] == "buildx" {
			buildxAt = i
		}
	}
	if shAt < 0 || buildxAt < 0 {
		t.Fatalf("commands = %#v", r.cmds)
	}
	if shAt > buildxAt {
		t.Fatalf("build commands ran after docker buildx: sh at %d, buildx at %d", shAt, buildxAt)
	}
}

func TestBuildCommandsFailureStopsBeforeBuildx(t *testing.T) {
	srv, _ := buildTestApp(t, "false\n")
	r := &fakeRunner{
		onOutput: func(c remote.Cmd) ([]byte, error) {
			if hasArg(c.Args, "--porcelain") {
				return nil, nil
			}
			return []byte("v1.2.3\n"), nil
		},
		onRun: func(c remote.Cmd) error {
			if c.Path == "sh" {
				return errors.New("exit status 1")
			}
			return nil
		},
	}
	s := &remote.Session{Host: "ops@host", Out: io.Discard, Err: io.Discard, Runner: r}

	_, err := buildImage(context.Background(), s, srv, "app", false)
	if err == nil || !strings.Contains(err.Error(), "build commands") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range r.cmds {
		if c.Path == "docker" {
			t.Fatalf("docker ran after a failed build command: %#v", c)
		}
	}
}

func TestDeployDryRunShowsBothScripts(t *testing.T) {
	imageDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(imageDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "apps", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apps", "app", "compose.yaml"), []byte("name: app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      app:
        domains: [app.example.com]
        upstream: app:8080
        source:
          type: image
          path: ` + imageDir + `
        build:
          commands: |
            npm ci
            npm run build
        deploy:
          maintenance: true
          commands: |
            # migrate first
            ./migrate
        backup: {}
        env: {}
`
	cfgPath := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(cfgPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	op := New(cfg, Options{DryRun: true, Out: &out, ErrOut: io.Discard})
	if err := op.Deploy(context.Background(), "prod", "app", "", false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"maintenance mount",
		"npm run build",
		"set -e",
		"deploy commands will run",
		"# migrate first",
		"./migrate",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, got)
		}
	}
}

func TestAppFilesTarAllowsDeployScript(t *testing.T) {
	blob, err := appFilesTar([]appFile{
		{Name: "compose.yaml", Data: []byte("name: app\n")},
		{Name: ".ship-meta", Data: []byte("app=app\n")},
		{Name: ".ship-env", Data: []byte("A=B\n")},
		{Name: ".ship-deploy.sh", Data: []byte("# migrate\n./migrate\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(blob))
	got := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, tr); err != nil {
			t.Fatal(err)
		}
		got[hdr.Name] = buf.String()
	}
	if got[".ship-deploy.sh"] != "# migrate\n./migrate\n" {
		t.Fatalf("deploy script member = %q (all %#v)", got[".ship-deploy.sh"], got)
	}
}
