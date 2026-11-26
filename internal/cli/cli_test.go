package cli

import (
	"archive/tar"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/remote"
)

const oneServerDoc = `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      app:
        domains: [app.example.com]
        upstream: app:8080
        source:
          type: github-release
          repo: example/app
        backup: {}
`

const twoServerDoc = `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      app:
        domains: [app.example.com]
        upstream: app:8080
        source:
          type: github-release
          repo: example/app
        backup: {}
  staging:
    host: ops@203.0.113.11
    apps:
      app:
        domains: [staging.example.com]
        upstream: app:8080
        source:
          type: github-release
          repo: example/app
        backup: {}
`

const appSetDoc = `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      app:
        domains: [one.example.com, two.example.com]
        redirects: [www.example.com]
        upstream: app:8080
        source:
          type: github-release
          repo: example/app
        backup: {}
`

func appSetTestConfig(t *testing.T) (string, *config.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ship.yaml")
	if err := os.WriteFile(path, []byte(appSetDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, cfg
}

func runAppCommand(t *testing.T, cfg *config.Config, in io.Reader, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	o := &options{in: in, out: &out, errOut: io.Discard}
	err := cmdApp(o, cfg, "prod", args)
	return out.String(), err
}

func TestAppSetAndListCommands(t *testing.T) {
	path, cfg := appSetTestConfig(t)
	out, err := runAppCommand(t, cfg, nil, "set", "app", "domains", "a.example.com", "b.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "domains = a.example.com, b.example.com") {
		t.Fatalf("output = %q", out)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Servers["prod"].Apps["app"].Domains; len(got) != 2 || got[1] != "b.example.com" {
		t.Fatalf("domains = %v", got)
	}

	out, err = runAppCommand(t, cfg, nil, "add", "app", "domains", "b.example.com", "c.example.com", "c.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Already present") || !strings.Contains(out, "c.example.com") {
		t.Fatalf("add output = %q", out)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Servers["prod"].Apps["app"].Domains, ","); got != "a.example.com,b.example.com,c.example.com" {
		t.Fatalf("domains = %q", got)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, err = runAppCommand(t, cfg, nil, "add", "app", "domains", "c.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no changes") {
		t.Fatalf("no-op add output = %q", out)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("a no-op add wrote the file")
	}

	out, err = runAppCommand(t, cfg, nil, "remove", "app", "domains", "a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Canonical domain is now b.example.com") {
		t.Fatalf("remove output = %q", out)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Servers["prod"].Apps["app"].Domains[0]; got != "b.example.com" {
		t.Fatalf("canonical domain = %q", got)
	}
}

func TestAppListRemoveIsAtomicAndKeepsADomain(t *testing.T) {
	path, cfg := appSetTestConfig(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runAppCommand(t, cfg, nil, "remove", "app", "domains", "one.example.com", "missing.example.com"); err == nil {
		t.Fatal("removing a missing value succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("a partially invalid remove wrote the file")
	}
	if _, err := runAppCommand(t, cfg, nil, "remove", "app", "domains", "one.example.com", "two.example.com"); err == nil || !strings.Contains(err.Error(), "last domain") {
		t.Fatalf("remove all error = %v", err)
	}
	if _, err := runAppCommand(t, cfg, nil, "remove", "app", "redirects", "www.example.com"); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers["prod"].Apps["app"].Redirects) != 0 {
		t.Fatal("redirects were not cleared")
	}
}

func TestAppSetKeysAndErrors(t *testing.T) {
	path, cfg := appSetTestConfig(t)
	if _, err := runAppCommand(t, cfg, strings.NewReader("echo hi\n"), "set", "app", "deploy.commands", "-"); err != nil {
		t.Fatal(err)
	}
	if _, err := runAppCommand(t, cfg, nil, "set", "app", "deploy.maintenance", "false"); err != nil {
		t.Fatal(err)
	}
	if _, err := runAppCommand(t, cfg, nil, "set", "app", "source.asset", ""); err != nil {
		t.Fatal(err)
	}
	var err error
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	app := cfg.Servers["prod"].Apps["app"]
	if app.Deploy.Commands != "echo hi\n" || app.MaintenanceEnabled() {
		t.Fatalf("deploy = %+v", app.Deploy)
	}

	if _, err := runAppCommand(t, cfg, nil, "set", "app", "nope", "x"); err == nil || !strings.Contains(err.Error(), "valid keys") {
		t.Fatalf("unknown key error = %v", err)
	}
	if _, err := runAppCommand(t, cfg, nil, "add", "app", "source.repo", "x/y"); err == nil || !strings.Contains(err.Error(), "not a list") {
		t.Fatalf("add non-list error = %v", err)
	}
	if _, err := runAppCommand(t, cfg, nil, "set", "app", "deploy.maintenance", "maybe"); err == nil || !strings.Contains(err.Error(), "true or false") {
		t.Fatalf("bad bool error = %v", err)
	}
}

func TestConfigEditTargets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(oneServerDoc), 0o600); err != nil {
		t.Fatal(err)
	}

	ship, err := resolveConfigEditTarget(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if ship.path != path || ship.kind != editShipConfig {
		t.Fatalf("ship target = %#v", ship)
	}

	app, err := resolveConfigEditTarget(path, "app")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "apps", "app", "compose.yaml")
	if app.path != want || app.kind != editCompose {
		t.Fatalf("app target = %#v, want %s", app, want)
	}

	if _, err := resolveConfigEditTarget(path, "missing"); err == nil || !strings.Contains(err.Error(), "not in") {
		t.Fatalf("unknown app error = %v", err)
	}
}

func TestAskReopenDefaultsToYes(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"\n", true},
		{"y\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"no\n", false},
		{"maybe\nn\n", false},
		{"", false},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		if got := askReopen(bufio.NewReader(strings.NewReader(tc.in)), &out); got != tc.want {
			t.Errorf("askReopen(%q) = %v, want %v (output %q)", tc.in, got, tc.want, out.String())
		}
	}
}

func decodeConfig(t *testing.T, doc string) *config.Config {
	t.Helper()
	cfg, err := config.Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestResolveServer(t *testing.T) {
	one := decodeConfig(t, oneServerDoc)
	two := decodeConfig(t, twoServerDoc)

	cases := []struct {
		name       string
		cfg        *config.Config
		args       []string
		wantServer string
		wantRest   string
		wantErr    string
	}{
		{"explicit", two, []string{"staging", "deploy", "app"}, "staging", "deploy app", ""},
		{"single implicit", one, []string{"deploy", "app"}, "prod", "deploy app", ""},
		{"single explicit", one, []string{"prod", "deploy", "app"}, "prod", "deploy app", ""},
		{"several missing", two, []string{"deploy", "app"}, "", "", "prod, staging"},
		{"several unknown", two, []string{"nope", "deploy", "app"}, "", "", "prod, staging"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, rest, err := resolveServer(tc.cfg, tc.args)
			if tc.wantErr != "" {
				if err == nil || !IsUsage(err) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want usage mentioning %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if server != tc.wantServer || strings.Join(rest, " ") != tc.wantRest {
				t.Fatalf("got server=%q rest=%q", server, strings.Join(rest, " "))
			}
		})
	}
}

func TestSeveralServersRequireName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(twoServerDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIP_CONFIG", path)

	var out bytes.Buffer
	err := Run([]string{"--dry-run", "deploy", "app"}, strings.NewReader(""), &out, io.Discard, nil)
	if err == nil || !IsUsage(err) {
		t.Fatalf("err = %v, want usage", err)
	}
	for _, want := range []string{"prod", "staging", "specify a server"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestDeployAllDryRunSummaryAndExit(t *testing.T) {
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
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIP_CONFIG", path)

	var out bytes.Buffer
	err := Run([]string{"--dry-run", "prod", "deploy", "all"}, strings.NewReader(""), &out, io.Discard, nil)
	if err == nil {
		t.Fatal("deploy all succeeded despite a failed app")
	}
	if IsUsage(err) {
		t.Fatalf("deploy failure should not be a usage error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"deploy summary for prod: 1 succeeded, 1 failed", "failed: aaa:"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestDeployImageDryRunWithServer(t *testing.T) {
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
	t.Setenv("SHIP_CONFIG", cfgPath)

	var out bytes.Buffer
	if err := Run([]string{"--dry-run", "prod", "deploy", "app"}, strings.NewReader(""), &out, io.Discard, nil); err != nil {
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

type cliExportRunner struct {
	export []byte
}

func (r *cliExportRunner) Run(c remote.Cmd) error {
	if strings.Contains(strings.Join(c.Args, " "), "app-export") && c.Stdout != nil {
		_, err := c.Stdout.Write(r.export)
		return err
	}
	if c.Stdin != nil {
		_, _ = io.Copy(io.Discard, c.Stdin)
	}
	return nil
}

func (r *cliExportRunner) Output(c remote.Cmd) ([]byte, error) {
	sum := sha256.Sum256(nil)
	return []byte("version=" + strconv.Itoa(remote.AgentProtocol) + "\nsha256=" + hex.EncodeToString(sum[:]) + "\n"), nil
}

func exportTar(t *testing.T, record, compose string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, data := range map[string]string{
		"web/ship-app.yaml": record,
		"web/compose.yaml":  compose,
	} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
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

func TestImportCommandRefusesAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    apps: {}
`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	record := `domains: [web.example.com]
upstream: web:3000
source:
  type: image
`
	runner := &cliExportRunner{export: exportTar(t, record, "name: web\n")}
	var out bytes.Buffer
	o := &options{out: &out, errOut: io.Discard, in: strings.NewReader(""), runner: runner}

	if err := cmdImport(o, cfg, "prod", []string{"web"}); err != nil {
		t.Fatal(err)
	}
	result, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Servers["prod"].Apps["web"] == nil {
		t.Fatal("import did not write the app into ship.yaml")
	}
	compose := filepath.Join(dir, "apps", "web", "compose.yaml")
	if _, err := os.Stat(compose); err != nil {
		t.Fatal(err)
	}

	// A second import without --overwrite must refuse.
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	o.out = io.Discard
	if err := cmdImport(o, cfg, "prod", []string{"web"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want an already-exists error", err)
	} else if !strings.Contains(err.Error(), "--overwrite") {
		t.Fatalf("CLI error does not mention --overwrite: %v", err)
	}
	if _, err := os.Stat(compose + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused import wrote a .bak")
	}

	// With --overwrite it replaces the file and keeps a .bak.
	runner.export = exportTar(t, strings.Replace(record, "web.example.com", "new.example.com", 1), "name: changed\n")
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdImport(o, cfg, "prod", []string{"web", "--overwrite"}); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(compose + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != "name: web\n" {
		t.Fatalf("compose .bak = %q", bak)
	}
	changed, err := os.ReadFile(compose)
	if err != nil {
		t.Fatal(err)
	}
	if string(changed) != "name: changed\n" {
		t.Fatalf("compose = %q", changed)
	}
	result, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Servers["prod"].Apps["web"].Domains[0]; got != "new.example.com" {
		t.Fatalf("domain = %q", got)
	}
}
