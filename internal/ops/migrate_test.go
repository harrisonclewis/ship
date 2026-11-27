package ops

import (
	"context"
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

// putRunnerFull serves config-export and captures app-put/server-put uploads.
type putRunnerFull struct {
	puts   []string
	export []byte
	inputs []string
}

func (r *putRunnerFull) Output(c remote.Cmd) ([]byte, error) {
	sum := sha256.Sum256(nil)
	return []byte("version=" + strconv.Itoa(remote.AgentProtocol) + "\nsha256=" + hex.EncodeToString(sum[:]) + "\n"), nil
}

func (r *putRunnerFull) Run(c remote.Cmd) error {
	joined := strings.Join(c.Args, " ")
	r.puts = append(r.puts, joined)
	switch {
	case strings.Contains(joined, "config-export") && c.Stdout != nil:
		_, err := c.Stdout.Write(r.export)
		return err
	case strings.Contains(joined, "app-put"), strings.Contains(joined, "server-put"):
		if c.Stdin != nil {
			data, _ := io.ReadAll(c.Stdin)
			r.inputs = append(r.inputs, string(data))
		}
		if c.Stdout != nil {
			_, _ = c.Stdout.Write([]byte("hash=" + strings.Repeat("b", 64) + "\n"))
		}
		return nil
	default:
		if c.Stdin != nil {
			_, _ = io.Copy(io.Discard, c.Stdin)
		}
		return nil
	}
}

func TestConfigPushUploadsDesiredAndRewritesLocal(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "apps", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	compose := "name: web\nservices:\n  web:\n    image: ${IMAGE}\n"
	if err := os.WriteFile(filepath.Join(dir, "apps", "web", "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	old := `servers:
  prod:
    host: ops@203.0.113.10
    admin_user: ops
    arch: linux/amd64
    caddy_image: caddy:2.10-alpine
    apps:
      web:
        domains: [web.example.com]
        upstream: web:3000
        source:
          type: image
          path: ` + filepath.Join(dir, "src") + `
        backup: {}
`
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	export := buildConfigExport(t, map[string]string{"config/arch": "x86_64\n"})
	runner := &putRunnerFull{export: export}
	var out strings.Builder
	op := New(cfg, Options{Agent: testAgent, Runner: runner, Out: &out, ErrOut: io.Discard})
	if err := op.ConfigPush(context.Background(), "prod"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 2 {
		t.Fatalf("puts = %d, want server-put + app-put", len(runner.inputs))
	}
	if !strings.Contains(strings.Join(runner.puts, " "), "setup-record") {
		t.Fatalf("config push did not record setup state: %v", runner.puts)
	}
	if !strings.Contains(out.String(), "pushed desired configuration for web") {
		t.Fatalf("output = %q", out.String())
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("rewritten file does not load in the new format: %v", err)
	}
	srv := loaded.Servers["prod"]
	if srv.IsOldFormat() {
		t.Fatalf("old fields remain: %+v", srv)
	}
	if got := srv.Sources["web"]; got != filepath.Join(dir, "src") {
		t.Fatalf("sources.web = %q", got)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("missing .bak: %v", err)
	}
}

func TestConfigPushTwoServersOneAtATime(t *testing.T) {
	dir := t.TempDir()
	for _, app := range []string{"one", "two"} {
		if err := os.MkdirAll(filepath.Join(dir, "apps", app), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "apps", app, "compose.yaml"), []byte("name: "+app+"\nservices:\n  "+app+":\n    image: ${IMAGE}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srcOne := filepath.Join(dir, "src-one")
	srcTwo := filepath.Join(dir, "src-two")
	old := `servers:
  prod:
    host: ops@203.0.113.10
    admin_user: ops
    apps:
      one:
        domains: [one.example.com]
        upstream: one:8080
        source:
          type: image
          path: ` + srcOne + `
        backup: {}
  staging:
    host: ops@203.0.113.11
    admin_user: ops
    apps:
      two:
        domains: [two.example.com]
        upstream: two:8080
        source:
          type: image
          path: ` + srcTwo + `
        backup: {}
`
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	export := buildConfigExport(t, map[string]string{"config/arch": "x86_64\n"})
	for _, server := range []string{"prod", "staging"} {
		cfg, err := config.LoadOld(path)
		if err != nil {
			t.Fatalf("%s: reload old: %v", server, err)
		}
		op := New(cfg, Options{Agent: testAgent, Runner: &putRunnerFull{export: export}, Out: io.Discard, ErrOut: io.Discard})
		if err := op.ConfigPush(context.Background(), server); err != nil {
			t.Fatalf("%s: %v", server, err)
		}
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("both servers did not load in the new format: %v", err)
	}
	if loaded.Servers["prod"].Sources["one"] != srcOne || loaded.Servers["staging"].Sources["two"] != srcTwo {
		t.Fatalf("sources = %+v / %+v", loaded.Servers["prod"].Sources, loaded.Servers["staging"].Sources)
	}
}

func TestConfigPushRefusesAlreadyNewFormat(t *testing.T) {
	cfg := loadConfig(t, `servers:
  prod:
    host: ops@203.0.113.10
`)
	op := New(cfg, Options{Agent: testAgent, Out: io.Discard})
	err := op.ConfigPush(context.Background(), "prod")
	if err == nil || !strings.Contains(err.Error(), "already in the new local format") {
		t.Fatalf("err = %v", err)
	}
}
