package cli

import (
	"archive/tar"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
    sources:
      app: ~/src/app
`

const twoServerDoc = `servers:
  prod:
    host: ops@203.0.113.10
  staging:
    host: ops@203.0.113.11
`

type exportRunner struct {
	export []byte
}

func (r *exportRunner) Run(c remote.Cmd) error {
	if strings.Contains(strings.Join(c.Args, " "), "config-export") && c.Stdout != nil {
		_, err := c.Stdout.Write(r.export)
		return err
	}
	if c.Stdin != nil {
		_, _ = io.Copy(io.Discard, c.Stdin)
	}
	return nil
}

func (r *exportRunner) Output(c remote.Cmd) ([]byte, error) {
	sum := sha256.Sum256(nil)
	return []byte("version=" + strconv.Itoa(remote.AgentProtocol) + "\nsha256=" + hex.EncodeToString(sum[:]) + "\n"), nil
}

func configExport(t *testing.T, members map[string]string) []byte {
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

func desiredExport(t *testing.T, app, record, compose string) []byte {
	t.Helper()
	return configExport(t, map[string]string{
		"config/arch":                       "x86_64\n",
		"desired/" + app + "/ship-app.yaml": record,
		"desired/" + app + "/compose.yaml":  compose,
		"desired/" + app + "/.hash":         strings.Repeat("a", 64) + "\n",
	})
}

const validCompose = "name: app\nservices:\n  app:\n    image: ${IMAGE}\n"

const desiredRecord = `domains: [one.example.com, two.example.com]
upstream: app:8080
source:
  type: github-release
  repo: example/app
`

func writeConfig(t *testing.T, doc string) (*config.Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ship.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, path
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
	path := filepath.Join(t.TempDir(), "ship.yaml")
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

func TestRunRejectsOldFormatWithConfigPushHint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ship.yaml")
	old := `servers:
  prod:
    host: ops@203.0.113.10
    admin_user: ops
    apps:
      app:
        domains: [app.example.com]
        upstream: app:8080
        source: {type: github-release, repo: example/app}
`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIP_CONFIG", path)
	var out bytes.Buffer
	err := Run([]string{"prod", "status"}, strings.NewReader(""), &out, io.Discard, nil)
	if err == nil || !strings.Contains(err.Error(), "config push") {
		t.Fatalf("err = %v, want a config push hint", err)
	}
}

func TestConfigPushServerDetection(t *testing.T) {
	cases := []struct {
		args   []string
		server string
		ok     bool
	}{
		{[]string{"config", "push"}, "", true},
		{[]string{"config", "push", "prod"}, "prod", true},
		{[]string{"prod", "config", "push"}, "prod", true},
		{[]string{"config", "edit"}, "", false},
		{[]string{"prod", "deploy", "app"}, "", false},
	}
	for _, tc := range cases {
		server, ok := configPushServer(tc.args)
		if ok != tc.ok || server != tc.server {
			t.Errorf("configPushServer(%v) = %q, %v; want %q, %v", tc.args, server, ok, tc.server, tc.ok)
		}
	}
}

func TestDeployDryRunUsesServerConfig(t *testing.T) {
	imageDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(imageDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := writeConfig(t, `servers:
  prod:
    host: ops@203.0.113.10
    sources:
      app: `+imageDir+`
`)
	export := desiredExport(t, "app", `domains: [app.example.com]
upstream: app:8080
source:
  type: image
`, "name: app\n")
	var out bytes.Buffer
	o := &options{in: strings.NewReader(""), out: &out, errOut: io.Discard, dryRun: true, runner: &exportRunner{export: export}}
	if err := cmdDeploy(o, cfg, "prod", []string{"app"}); err != nil {
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

func TestAppSetUpdatesDesiredAndSaysRestartCaddy(t *testing.T) {
	cfg, _ := writeConfig(t, oneServerDoc)
	export := desiredExport(t, "app", desiredRecord, validCompose)
	o := &options{in: strings.NewReader(""), errOut: io.Discard, dryRun: true, runner: &exportRunner{export: export}}
	got, err := runAppCommand(o, cfg, "set", "app", "domains", "a.example.com", "b.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Saved.", "Restart Caddy to apply: ship prod proxy restart", "would put desired configuration"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestAppAddIsIdempotent(t *testing.T) {
	cfg, _ := writeConfig(t, oneServerDoc)
	export := desiredExport(t, "app", desiredRecord, validCompose)
	o := &options{in: strings.NewReader(""), errOut: io.Discard, dryRun: true, runner: &exportRunner{export: export}}
	got, err := runAppCommand(o, cfg, "add", "app", "domains", "one.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "already contains") {
		t.Fatalf("no-op add output = %q", got)
	}
}

func TestAppSetErrors(t *testing.T) {
	cfg, _ := writeConfig(t, oneServerDoc)
	export := desiredExport(t, "app", desiredRecord, validCompose)
	o := &options{in: strings.NewReader(""), out: io.Discard, errOut: io.Discard, dryRun: true, runner: &exportRunner{export: export}}

	if _, err := runAppCommand(o, cfg, "set", "app", "nope", "x"); err == nil || !strings.Contains(err.Error(), "valid keys") {
		t.Fatalf("unknown key error = %v", err)
	}
	if _, err := runAppCommand(o, cfg, "add", "app", "source.repo", "x/y"); err == nil || !strings.Contains(err.Error(), "not a list") {
		t.Fatalf("add non-list error = %v", err)
	}
	if _, err := runAppCommand(o, cfg, "set", "app", "deploy.maintenance", "maybe"); err == nil || !strings.Contains(err.Error(), "true or false") {
		t.Fatalf("bad bool error = %v", err)
	}
	if _, err := runAppCommand(o, cfg, "remove", "app", "domains", "one.example.com", "two.example.com"); err == nil || !strings.Contains(err.Error(), "last domain") {
		t.Fatalf("remove all error = %v", err)
	}
}

func TestAppDeleteKeepsDataWarning(t *testing.T) {
	cfg, _ := writeConfig(t, oneServerDoc)
	export := desiredExport(t, "app", desiredRecord, validCompose)
	var out bytes.Buffer
	o := &options{in: strings.NewReader(""), out: &out, errOut: io.Discard, dryRun: true, yes: true, runner: &exportRunner{export: export}}
	if err := cmdApp(o, cfg, "prod", []string{"delete", "app"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Data directories and .env were kept") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestAppCreateRejectsBadNames(t *testing.T) {
	cfg, _ := writeConfig(t, oneServerDoc)
	o := &options{in: strings.NewReader(""), out: io.Discard, errOut: io.Discard}
	if err := cmdApp(o, cfg, "prod", []string{"create", "Bad"}); err == nil {
		t.Fatal("bad app name accepted")
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

func runAppCommand(o *options, cfg *config.Config, args ...string) (string, error) {
	var out bytes.Buffer
	o.out = &out
	err := cmdApp(o, cfg, "prod", args)
	return out.String(), err
}

func TestServerConfigEditIsDispatched(t *testing.T) {
	cfg, _ := writeConfig(t, oneServerDoc)
	o := &options{in: strings.NewReader(""), out: io.Discard, errOut: io.Discard}
	// An invalid file name fails in cmdConfigServer before any server call,
	// so reaching that error proves `ship prod config edit` is routed there.
	err := dispatchCommand(o, cfg, "prod", []string{"config", "edit", "app", "bogus"})
	if err == nil || !strings.Contains(err.Error(), "edit either compose or Dockerfile") {
		t.Fatalf("dispatchCommand(prod config edit app bogus) = %v", err)
	}
}
