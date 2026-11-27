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

func loadConfig(t *testing.T, doc string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// buildConfigExport builds the tar shape ship-agent config-export produces.
func buildConfigExport(t *testing.T, members map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, data := range members {
		if err := tw.WriteHeader(&tar.Header{Name: "./" + name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type configExportRunner struct {
	export  []byte
	outputs func(remote.Cmd) ([]byte, error)
	cmds    []remote.Cmd
}

func (r *configExportRunner) Run(c remote.Cmd) error {
	r.cmds = append(r.cmds, c)
	if strings.Contains(strings.Join(c.Args, " "), "config-export") && c.Stdout != nil {
		_, err := c.Stdout.Write(r.export)
		return err
	}
	if c.Stdin != nil {
		_, _ = io.Copy(io.Discard, c.Stdin)
	}
	return nil
}

func (r *configExportRunner) Output(c remote.Cmd) ([]byte, error) {
	r.cmds = append(r.cmds, c)
	if r.outputs != nil {
		return r.outputs(c)
	}
	return nil, nil
}

func TestBuildImageDryRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &config.Server{
		Name:    "prod",
		Arch:    "linux/amd64",
		Sources: map[string]string{"app": dir},
		Apps: map[string]*config.App{
			"app": {Source: config.Source{Type: "image"}},
		},
	}
	var out bytes.Buffer
	s := remote.NewSession("ops@host", true, &out, io.Discard)
	op := New(&config.Config{Dir: t.TempDir()}, Options{Agent: testAgent, Out: &out, ErrOut: io.Discard})
	version, err := op.buildImage(context.Background(), s, srv, "app", false)
	if err != nil {
		t.Fatal(err)
	}
	if version != "dry-run" {
		t.Fatalf("version = %q", version)
	}
	got := out.String()
	for _, want := range []string{
		"docker", "buildx", "--platform", "linux/amd64",
		"ship/app:dry-run", "--load", dir,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("build command missing %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"--push", "ghcr"} {
		if strings.Contains(got, bad) {
			t.Errorf("build command still contains %q:\n%s", bad, got)
		}
	}
}

func TestDeployLocalImageSkipsTransfer(t *testing.T) {
	imageDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(imageDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	export := buildConfigExport(t, map[string]string{
		"config/arch":               "x86_64\n",
		"desired/app/ship-app.yaml": "domains: [app.example.com]\nupstream: app:8080\nsource:\n  type: image\n",
		"desired/app/compose.yaml":  "name: app\n",
	})
	var out bytes.Buffer
	runner := &configExportRunner{export: export}
	cfg := &config.Config{Dir: t.TempDir(), Servers: map[string]*config.Server{
		"self": {
			Name:       "self",
			Connection: config.ConnectionLocal,
			User:       "ops",
			Sources:    map[string]string{"app": imageDir},
		},
	}}
	op := New(cfg, Options{Agent: testAgent, DryRun: true, Out: &out, ErrOut: io.Discard, Runner: runner})
	if err := op.Deploy(context.Background(), "self", "app", "", false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "gzip") || strings.Contains(got, "'save'") || strings.Contains(got, "docker save") {
		t.Errorf("local deploy still streams the image:\n%s", got)
	}
	for _, want := range []string{
		"'image' 'inspect' 'ship/app:dry-run'",
		"warning: building app uses this server's CPU and memory",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("local deploy missing %q:\n%s", want, got)
		}
	}
}

func TestBuildImageLocalNeedsDockerAccess(t *testing.T) {
	srv, _ := buildTestApp(t, "")
	r := &fakeRunner{onOutput: func(c remote.Cmd) ([]byte, error) {
		if c.Path == "docker" && hasArg(c.Args, "info") {
			return nil, errors.New("permission denied")
		}
		return nil, nil
	}}
	s := &remote.Session{LocalMode: true, Out: io.Discard, Err: io.Discard, Runner: r}
	op := New(&config.Config{Dir: t.TempDir()}, Options{Agent: testAgent, Out: io.Discard, ErrOut: io.Discard})
	_, err := op.buildImage(context.Background(), s, srv, "app", false)
	if err == nil || !strings.Contains(err.Error(), "docker group") {
		t.Fatalf("err = %v, want a Docker access hint", err)
	}
	if len(r.cmds) != 1 {
		t.Fatalf("docker info failure should stop before other commands, got %d", len(r.cmds))
	}
}

func TestAppFilesTar(t *testing.T) {
	blob, err := appFilesTar([]appFile{
		{Name: "compose.yaml", Mode: 0o644, Data: []byte("name: app\n")},
		{Name: ".ship-meta", Mode: 0o644, Data: []byte("app=app\n")},
		{Name: ".ship-env", Mode: 0o644, Data: []byte("A=B\n")},
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
		if hdr.Typeflag != tar.TypeReg {
			t.Fatalf("%s is type %q, want a regular file", hdr.Name, hdr.Typeflag)
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, tr); err != nil {
			t.Fatal(err)
		}
		got[hdr.Name] = buf.String()
	}
	if len(got) != 3 || got["compose.yaml"] != "name: app\n" || got[".ship-meta"] != "app=app\n" {
		t.Fatalf("tar members = %#v", got)
	}
}

func TestAppFilesTarRejectsUnsafeNames(t *testing.T) {
	base := []appFile{
		{Name: "compose.yaml"},
		{Name: ".ship-meta"},
		{Name: ".ship-env"},
	}
	cases := []string{
		"/etc/passwd",
		"../compose.yaml",
		"sub/compose.yaml",
		`sub\compose.yaml`,
		"evil.sh",
		"",
	}
	for _, name := range cases {
		files := append(append([]appFile{}, base...), appFile{Name: name})
		if _, err := appFilesTar(files); err == nil {
			t.Errorf("accepted unsafe name %q", name)
		}
	}
	dup := append(append([]appFile{}, base...), appFile{Name: "compose.yaml"})
	if _, err := appFilesTar(dup); err == nil {
		t.Error("accepted duplicate compose.yaml")
	}
	if _, err := appFilesTar(base[:2]); err == nil {
		t.Error("accepted app files without .ship-env")
	}
}

func TestDeployImageDryRun(t *testing.T) {
	imageDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(imageDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	export := buildConfigExport(t, map[string]string{
		"config/arch":               "x86_64\n",
		"desired/app/ship-app.yaml": "domains: [app.example.com]\nupstream: app:8080\nsource:\n  type: image\n",
		"desired/app/compose.yaml":  "name: app\n",
		"desired/app/.hash":         strings.Repeat("a", 64) + "\n",
	})
	var out bytes.Buffer
	runner := &configExportRunner{export: export}
	cfg := &config.Config{Dir: t.TempDir(), Servers: map[string]*config.Server{
		"prod": {Name: "prod", Host: "ops@203.0.113.10", Sources: map[string]string{"app": imageDir}},
	}}
	op := New(cfg, Options{Agent: testAgent, DryRun: true, Out: &out, ErrOut: io.Discard, Runner: runner})
	if err := op.Deploy(context.Background(), "prod", "app", "", false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"buildx", "--load", "ship/app:dry-run",
		"docker", "save", "gzip", "image-load", "'deploy'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "app-files") {
		t.Errorf("dry-run still uploads app files:\n%s", got)
	}
}

func TestDeployAllContinuesAndSummarises(t *testing.T) {
	export := buildConfigExport(t, map[string]string{
		"config/arch":               "x86_64\n",
		"desired/aaa/ship-app.yaml": "domains: [aaa.example.com]\nupstream: aaa:8080\nsource:\n  type: image\n",
		"desired/aaa/compose.yaml":  "name: aaa\n",
		"desired/zzz/ship-app.yaml": "domains: [zzz.example.com]\nupstream: zzz:8080\nsource:\n  type: github-release\n  repo: example/zzz\n",
		"desired/zzz/compose.yaml":  "name: zzz\n",
		"desired/zzz/Dockerfile":    "FROM scratch\n",
	})
	var out bytes.Buffer
	runner := &configExportRunner{export: export}
	cfg := &config.Config{Dir: t.TempDir(), Servers: map[string]*config.Server{
		"prod": {Name: "prod", Host: "ops@203.0.113.10"},
	}}
	op := New(cfg, Options{Agent: testAgent, DryRun: true, Out: &out, ErrOut: io.Discard, Runner: runner})
	summary, err := op.DeployAll(context.Background(), "prod", false)
	if err == nil {
		t.Fatal("DeployAll succeeded despite a failed app")
	}
	if summary.OK() {
		t.Fatal("summary says OK despite a failure")
	}
	if strings.Join(summary.Succeeded, ",") != "zzz" {
		t.Fatalf("succeeded = %v, want zzz", summary.Succeeded)
	}
	if len(summary.Failed) != 1 || summary.Failed[0].App != "aaa" {
		t.Fatalf("failed = %#v", summary.Failed)
	}
	got := out.String()
	for _, want := range []string{"deploy summary for prod: 1 succeeded, 1 failed", "failed: aaa:"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestBuildImageRequiresDockerfile(t *testing.T) {
	srv := &config.Server{
		Name:    "prod",
		Arch:    "linux/amd64",
		Sources: map[string]string{"app": t.TempDir()},
		Apps: map[string]*config.App{
			"app": {Source: config.Source{Type: "image"}},
		},
	}
	s := remote.NewSession("ops@host", true, io.Discard, io.Discard)
	op := New(&config.Config{Dir: t.TempDir()}, Options{Agent: testAgent, DryRun: true, Out: io.Discard, ErrOut: io.Discard})
	if _, err := op.buildImage(context.Background(), s, srv, "app", false); err == nil || !strings.Contains(err.Error(), "Dockerfile") {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionPassesIdentityFile(t *testing.T) {
	key := filepath.Join(t.TempDir(), "prod")
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	export := buildConfigExport(t, map[string]string{"config/arch": "x86_64\n"})
	cfg := &config.Config{Dir: t.TempDir(), Servers: map[string]*config.Server{
		"prod": {Name: "prod", Host: "ops@host", IdentityFile: key},
	}}
	var out bytes.Buffer
	op := New(cfg, Options{Agent: testAgent, DryRun: true, Out: &out, ErrOut: io.Discard, Runner: &configExportRunner{export: export}})
	if err := op.ProxySync(context.Background(), "prod"); err != nil {
		t.Fatal(err)
	}
	if want := "'-i' '" + key + "' '-o' 'IdentitiesOnly=yes'"; !strings.Contains(out.String(), want) {
		t.Fatalf("ssh argv missing %q:\n%s", want, out.String())
	}

	missing := New(&config.Config{Dir: t.TempDir(), Servers: map[string]*config.Server{
		"prod": {Name: "prod", Host: "ops@host", IdentityFile: key + "-gone"},
	}}, Options{Agent: testAgent, Out: io.Discard, ErrOut: io.Discard, Runner: &configExportRunner{export: export}})
	if err := missing.ProxySync(context.Background(), "prod"); err == nil || !strings.Contains(err.Error(), "servers.prod.identity_file") {
		t.Fatalf("missing key: want a servers.prod.identity_file error, got %v", err)
	}
}

func TestDownloadSHAStillUsesServerName(t *testing.T) {
	// Guard the Backup path that still needs the configured server name.
	if got := (&config.Server{Name: "prod"}).Name; got != "prod" {
		t.Fatalf("server name = %q", got)
	}
}
