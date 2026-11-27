package yamldoc

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
)

func TestYAMLEditKeepsCommentsAndWritesBuildDeployFields(t *testing.T) {
	path, _, doc := writeTestConfig(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	v := AppValues{
		Server:            "prod",
		Name:              "app",
		OriginalName:      "app",
		Domains:           []string{"app.example.com"},
		Upstream:          "app:8080",
		SourceType:        "image",
		SourcePath:        "~/app",
		Env:               "FOO=bar",
		BuildCommands:     "npm ci\nnpm run build",
		DeployCommands:    "php artisan migrate --force",
		DeployMaintenance: false,
	}
	tmp := doc.Clone()
	if err := tmp.ApplyApp(v); err != nil {
		t.Fatalf("applyApp: %v", err)
	}
	if err := tmp.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{
		"# keep this comment",
		"# prod is the primary server",
		"build:",
		"commands: |",
		"npm ci",
		"deploy:",
		"maintenance: false",
		"FOO: bar",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("saved YAML missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "domains: [app.example.com]") > strings.Index(text, "upstream: app:8080") {
		t.Errorf("existing key order changed:\n%s", text)
	}

	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if !bytes.Equal(bak, original) {
		t.Fatal("ship.yaml.bak does not contain the previous file")
	}

	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	app, err := cfg.Servers["prod"].App("app")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if app.Source.Type != "image" || app.Source.Path != filepath.Join(home, "app") {
		t.Fatalf("source = %+v", app.Source)
	}
	if !strings.Contains(app.Build.Commands, "npm ci") {
		t.Fatalf("build commands = %q", app.Build.Commands)
	}
	if !strings.Contains(app.Deploy.Commands, "php artisan migrate --force") {
		t.Fatalf("deploy commands = %q", app.Deploy.Commands)
	}
	if app.MaintenanceEnabled() {
		t.Fatal("maintenance should be disabled")
	}
}

func TestYAMLRejectsInvalidConfigWithoutWriting(t *testing.T) {
	path, _, doc := writeTestConfig(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v := AppValues{
		Server:       "prod",
		Name:         "app",
		OriginalName: "app",
		// No domains: config validation must reject this.
		Upstream:   "app:8080",
		SourceType: "github-release",
		SourceRepo: "example/app",
	}
	tmp := doc.Clone()
	if err := tmp.ApplyApp(v); err != nil {
		t.Fatalf("applyApp: %v", err)
	}
	err = tmp.Save()
	if err == nil {
		t.Fatal("invalid config was saved")
	}
	if !strings.Contains(err.Error(), "domains") {
		t.Fatalf("error does not mention domains: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatal("the file was changed despite an invalid config")
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a .bak file was written despite validation failing")
	}
}

func TestYAMLRefusesWhenFileChangedOnDisk(t *testing.T) {
	path, _, doc := writeTestConfig(t)
	external := strings.Replace(testConfig, "domains: [app.example.com]", "domains: [changed.example.com]", 1)
	if err := os.WriteFile(path, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	v := AppValues{
		Server:       "prod",
		Name:         "app",
		OriginalName: "app",
		Domains:      []string{"app.example.com"},
		Upstream:     "app:8080",
		SourceType:   "github-release",
		SourceRepo:   "example/app",
	}
	tmp := doc.Clone()
	if err := tmp.ApplyApp(v); err != nil {
		t.Fatalf("applyApp: %v", err)
	}
	err := tmp.Save()
	if !errors.Is(err, ErrConfigChanged) {
		t.Fatalf("save error = %v, want ErrConfigChanged", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != external {
		t.Fatal("the file changed on disk was overwritten")
	}
}

func TestYAMLSaveCreatesPrivateConfigAndEmptyServer(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ship")
	path := filepath.Join(dir, "ship.yaml")
	doc := New(path)
	v := ServerValues{
		Name:       "prod",
		Host:       "ops@203.0.113.10",
		AdminUser:  "ops",
		Arch:       "linux/amd64",
		CaddyImage: "caddy:2.10-alpine",
		IsNew:      true,
	}
	if err := doc.ApplyServer(v); err != nil {
		t.Fatalf("applyServer: %v", err)
	}
	if err := doc.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no Unix permission bits; the mode check only means something elsewhere.
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 600", fi.Mode().Perm())
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && di.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o, want 700", di.Mode().Perm())
	}

	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	srv, err := cfg.Server("prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(srv.AppNames()) != 0 {
		t.Fatalf("apps = %v, want none", srv.AppNames())
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a .bak file was written for a brand new config")
	}
}

func TestApplyLocalServerWritesConnectionAndRemovesSSH(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte("servers:\n  prod:\n    host: ops@203.0.113.10\n    identity_file: ~/.ssh/prod\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	v := ServerValues{
		Name:         "prod",
		OriginalName: "prod",
		Connection:   config.ConnectionLocal,
		User:         "ops",
	}
	tmp := doc.Clone()
	if err := tmp.ApplyServer(v); err != nil {
		t.Fatalf("applyServer: %v", err)
	}
	if err := tmp.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	srv, err := cfg.Server("prod")
	if err != nil {
		t.Fatal(err)
	}
	if !srv.IsLocal() || srv.Host != "" || srv.IdentityFile != "" || srv.User != "ops" {
		t.Fatalf("saved local server = %+v", srv)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"connection: local", "user: ops"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("saved YAML missing %q:\n%s", want, text)
		}
	}
	for _, bad := range []string{"host:", "identity_file:"} {
		if strings.Contains(string(text), bad) {
			t.Errorf("saved YAML kept %q:\n%s", bad, text)
		}
	}
}

func TestYAMLServerRenameKeepsAppsAndComments(t *testing.T) {
	path, _, doc := writeTestConfig(t)
	v := ServerValues{
		Name:         "production",
		OriginalName: "prod",
		Host:         "ops@203.0.113.10",
		AdminUser:    "ops",
		Arch:         "linux/amd64",
		CaddyImage:   "caddy:2.10-alpine",
	}
	tmp := doc.Clone()
	if err := tmp.ApplyServer(v); err != nil {
		t.Fatalf("applyServer: %v", err)
	}
	if err := tmp.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Server("prod"); err == nil {
		t.Fatal("old server name still present")
	}
	srv, err := cfg.Server("production")
	if err != nil {
		t.Fatal(err)
	}
	if got := srv.AppNames(); len(got) != 1 {
		t.Fatalf("renamed server apps = %v", got)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "# prod is the primary server") {
		t.Fatalf("comment lost on rename:\n%s", text)
	}
}

func TestRestoreBlankLines(t *testing.T) {
	original := []byte("# top\n\nbackup_dir_local: ~/b\n\nservers:\n  prod:\n    host: a@b\n\n    apps: {}\n")
	rendered := []byte("# top\nbackup_dir_local: ~/b\nservers:\n  prod:\n    host: a@c\n    apps: {}\n")
	got := string(restoreBlankLines(original, rendered))
	want := "# top\n\nbackup_dir_local: ~/b\n\nservers:\n  prod:\n    host: a@c\n\n    apps: {}\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRestoreBlankLinesNeverChangesBlockScalars(t *testing.T) {
	// The user removed the blank line inside the script; it must stay removed.
	original := []byte("cmd: |\n  one\n\n  two\n")
	rendered := []byte("cmd: |\n  one\n  two\n")
	if got := string(restoreBlankLines(original, rendered)); got != string(rendered) {
		t.Fatalf("block scalar changed:\n%s", got)
	}
}

const testConfig = `# keep this comment
servers:
  prod:
    # prod is the primary server
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

func writeTestConfig(t *testing.T) (string, *config.Config, *Doc) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(testConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, cfg, doc
}

func TestPutAppKeepsCommentsAndLocalSourcePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	docText := `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      # keep this comment
      web:
        domains: [web.example.com]
        upstream: web:3000
        source:
          type: image
          path: ~/src/web
`
	if err := os.WriteFile(path, []byte(docText), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	record := []byte(`domains: [new.example.com]
upstream: web:3000
source:
  type: image
`)
	if err := doc.PutApp("prod", "web", record, true); err != nil {
		t.Fatal(err)
	}
	if err := doc.PutApp("prod", "api", []byte("domains: [api.example.com]\nupstream: api:8080\nsource: {type: github-release, repo: example/api}\n"), true); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(saved)
	for _, want := range []string{"# keep this comment", "new.example.com", "path: ~/src/web", "api:"} {
		if !strings.Contains(text, want) {
			t.Errorf("saved YAML missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "web.example.com") {
		t.Errorf("old app definition survived:\n%s", text)
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := cfg.Servers["prod"].Apps["web"].Source.Path; got == "" {
		t.Fatal("source.path was not kept")
	}
}

func TestPutAppMergesAndKeepsCommentsAndFlowStyle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	docText := `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      web:
        # domains comment
        domains: [old.example.com, www.old.example.com] # inline domains
        upstream: web:3000
        source:
          type: image
          path: ~/src/web # local path
        uid: 1000
        data: [/srv/web] # data comment
        health:
          http_path: /old
        backup: {}
        env:
          KEEP: one # keep env
`
	if err := os.WriteFile(path, []byte(docText), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	record := []byte(`domains: [new.example.com, www.new.example.com]
upstream: web:3000
source:
  type: image
env:
  KEEP: one
  ADDED: two
`)
	if err := doc.PutApp("prod", "web", record, true); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(saved)
	for _, want := range []string{
		"# domains comment",
		"domains: [new.example.com, www.new.example.com]",
		"# inline domains",
		"upstream: web:3000",
		"path: ~/src/web",
		"# local path",
		"# keep env",
		"ADDED: two",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("saved YAML missing %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"old.example.com", "uid:", "data:", "health:", "backup:"} {
		if strings.Contains(text, gone) {
			t.Errorf("saved YAML still contains %q:\n%s", gone, text)
		}
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	app := cfg.Servers["prod"].Apps["web"]
	if got := app.Source.Path; got == "" {
		t.Fatal("source.path was not kept")
	}
	if app.Env["KEEP"] != "one" || app.Env["ADDED"] != "two" {
		t.Fatalf("env = %v", app.Env)
	}
}
