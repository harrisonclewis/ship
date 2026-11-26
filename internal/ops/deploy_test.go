package ops

import (
	"archive/tar"
	"bytes"
	"context"
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
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestBuildImageDryRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &config.Server{
		Name: "prod",
		Arch: "linux/amd64",
		Apps: map[string]*config.App{
			"app": {Source: config.Source{Type: "image", Path: dir}},
		},
	}
	var out bytes.Buffer
	s := remote.NewSession("ops@host", true, &out, io.Discard)
	version, err := buildImage(context.Background(), s, srv, "app", false)
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

func TestUploadAppFilesDryRunUsesOneTransfer(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "apps", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apps", "app", "compose.yaml"), []byte("name: app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &config.Server{
		Name: "prod",
		Dir:  dir,
		Apps: map[string]*config.App{
			"app": {Domains: []string{"app.example.com"}, Upstream: "app:8080",
				Source: config.Source{Type: "image", Path: dir}},
		},
	}
	var out bytes.Buffer
	s := remote.NewSession("ops@host", true, &out, io.Discard)
	if err := uploadAppFiles(s, srv, "app"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if n := strings.Count(got, "$ 'ssh'"); n != 1 {
		t.Fatalf("want one ssh transfer, got %d:\n%s", n, got)
	}
	for _, want := range []string{"app-files", "stdin:"} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, got)
		}
	}
}

func TestDeployImageDryRun(t *testing.T) {
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
		"buildx", "--load", "ship/app:dry-run",
		"app-files", "docker", "save", "gzip", "image-load", "'deploy'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "app-files"); n != 1 {
		t.Errorf("app files were transferred %d times, want 1:\n%s", n, got)
	}
}

func TestDeployAllContinuesAndSummarises(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "apps", "zzz"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apps", "zzz", "compose.yaml"), []byte("name: zzz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apps", "zzz", "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      aaa:
        domains: [aaa.example.com]
        upstream: aaa:8080
        source:
          type: image
          path: ` + t.TempDir() + `
        backup: {}
      zzz:
        domains: [zzz.example.com]
        upstream: zzz:8080
        source:
          type: github-release
          repo: example/zzz
        backup: {}
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
		Name: "prod",
		Arch: "linux/amd64",
		Apps: map[string]*config.App{
			"app": {Source: config.Source{Type: "image", Path: t.TempDir()}},
		},
	}
	s := remote.NewSession("ops@host", true, io.Discard, io.Discard)
	if _, err := buildImage(context.Background(), s, srv, "app", false); err == nil || !strings.Contains(err.Error(), "Dockerfile") {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionPassesIdentityFile(t *testing.T) {
	key := filepath.Join(t.TempDir(), "prod")
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Servers: map[string]*config.Server{
		"prod": {Name: "prod", Host: "ops@host", IdentityFile: key, Apps: map[string]*config.App{}},
	}}
	var out bytes.Buffer
	o := New(cfg, Options{DryRun: true, Out: &out, ErrOut: io.Discard})
	if err := o.ProxyUpdate(context.Background(), "prod"); err != nil {
		t.Fatal(err)
	}
	if want := "'-i' '" + key + "' '-o' 'IdentitiesOnly=yes'"; !strings.Contains(out.String(), want) {
		t.Fatalf("ssh argv missing %q:\n%s", want, out.String())
	}

	missing := New(&config.Config{Servers: map[string]*config.Server{
		"prod": {Name: "prod", Host: "ops@host", IdentityFile: key + "-gone", Apps: map[string]*config.App{}},
	}}, Options{Out: io.Discard, ErrOut: io.Discard})
	if err := missing.ProxyUpdate(context.Background(), "prod"); err == nil || !strings.Contains(err.Error(), "servers.prod.identity_file") {
		t.Fatalf("missing key: want a servers.prod.identity_file error, got %v", err)
	}
}
