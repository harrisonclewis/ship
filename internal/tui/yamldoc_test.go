package tui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
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

	v := appFormValues{
		Server:            "prod",
		Name:              "app",
		OriginalName:      "app",
		Domains:           []string{"app.example.com"},
		Upstream:          "app:8080",
		SourceType:        "image",
		SourcePath:        "/tmp/app",
		Env:               "FOO=bar",
		BuildCommands:     "npm ci\nnpm run build",
		DeployCommands:    "php artisan migrate --force",
		DeployMaintenance: false,
	}
	tmp := doc.clone()
	if err := tmp.applyApp(v); err != nil {
		t.Fatalf("applyApp: %v", err)
	}
	if err := tmp.save(); err != nil {
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

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	app, err := cfg.Servers["prod"].App("app")
	if err != nil {
		t.Fatal(err)
	}
	if app.Source.Type != "image" || app.Source.Path != "/tmp/app" {
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
	v := appFormValues{
		Server:       "prod",
		Name:         "app",
		OriginalName: "app",
		// No domains: config validation must reject this.
		Upstream:   "app:8080",
		SourceType: "github-release",
		SourceRepo: "example/app",
	}
	tmp := doc.clone()
	if err := tmp.applyApp(v); err != nil {
		t.Fatalf("applyApp: %v", err)
	}
	err = tmp.save()
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
	v := appFormValues{
		Server:       "prod",
		Name:         "app",
		OriginalName: "app",
		Domains:      []string{"app.example.com"},
		Upstream:     "app:8080",
		SourceType:   "github-release",
		SourceRepo:   "example/app",
	}
	tmp := doc.clone()
	if err := tmp.applyApp(v); err != nil {
		t.Fatalf("applyApp: %v", err)
	}
	err := tmp.save()
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
	doc := newEmptyYAMLDoc(path)
	v := serverFormValues{
		Name:       "prod",
		Host:       "ops@203.0.113.10",
		AdminUser:  "ops",
		Arch:       "linux/amd64",
		CaddyImage: "caddy:2.10-alpine",
		IsNew:      true,
	}
	if err := doc.applyServer(v); err != nil {
		t.Fatalf("applyServer: %v", err)
	}
	if err := doc.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 600", fi.Mode().Perm())
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o, want 700", di.Mode().Perm())
	}

	cfg, err := config.Load(path)
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

func TestYAMLServerRenameKeepsAppsAndComments(t *testing.T) {
	path, _, doc := writeTestConfig(t)
	v := serverFormValues{
		Name:         "production",
		OriginalName: "prod",
		Host:         "ops@203.0.113.10",
		AdminUser:    "ops",
		Arch:         "linux/amd64",
		CaddyImage:   "caddy:2.10-alpine",
	}
	tmp := doc.clone()
	if err := tmp.applyServer(v); err != nil {
		t.Fatalf("applyServer: %v", err)
	}
	if err := tmp.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	cfg, err := config.Load(path)
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
