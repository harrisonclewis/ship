package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
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
