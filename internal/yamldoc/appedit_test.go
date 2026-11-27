package yamldoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
)

const appEditDoc = `# top comment
servers:
  prod:
    host: ops@203.0.113.10
    apps:
      app:
        # keep this app comment
        domains: [app.example.com]
        redirects: [www.app.example.com]
        upstream: app:8080
        source:
          type: image
          path: ~/app
        deploy:
          commands: |
            echo deploy
      other:
        domains: [other.example.com]
        upstream: other:8080
        source:
          type: github-release
          repo: example/other
`

func loadAppEditDoc(t *testing.T) (string, *Doc) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ship.yaml")
	if err := os.WriteFile(path, []byte(appEditDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, doc
}

func TestAppFieldSettersSaveAndKeepComments(t *testing.T) {
	path, doc := loadAppEditDoc(t)
	if err := doc.SetAppSourcePath("prod", "app", "~/new-app"); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetAppDomains("prod", "app", []string{"new.example.com", "alt.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetAppRedirects("prod", "app", nil); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetAppBuildCommands("prod", "app", "npm ci\nnpm run build\n"); err != nil {
		t.Fatal(err)
	}
	if err := doc.SetAppDeployCommands("prod", "app", "php artisan migrate --force\n"); err != nil {
		t.Fatal(err)
	}
	maintenance := false
	if err := doc.SetAppDeployMaintenance("prod", "app", &maintenance); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	app := cfg.Servers["prod"].Apps["app"]
	if got := strings.Join(app.Domains, ","); got != "new.example.com,alt.example.com" {
		t.Fatalf("domains = %q", got)
	}
	if len(app.Redirects) != 0 {
		t.Fatalf("redirects = %v", app.Redirects)
	}
	if app.Source.Path != filepath.Join(home(t), "new-app") {
		t.Fatalf("source.path = %q", app.Source.Path)
	}
	if app.Build.Commands != "npm ci\nnpm run build\n" {
		t.Fatalf("build.commands = %q", app.Build.Commands)
	}
	if app.Deploy.Commands != "php artisan migrate --force\n" {
		t.Fatalf("deploy.commands = %q", app.Deploy.Commands)
	}
	if app.MaintenanceEnabled() {
		t.Fatal("maintenance should be off")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# top comment", "# keep this app comment"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("saved file lost %q:\n%s", want, data)
		}
	}
}

func home(t *testing.T) string {
	t.Helper()
	h, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestAppYAMLRoundTripSplicesOnlyThatApp(t *testing.T) {
	path, doc := loadAppEditDoc(t)
	data, err := doc.AppYAML("prod", "app")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "server prod, app app") {
		t.Fatalf("header missing:\n%s", text)
	}
	if !strings.Contains(text, "# keep this app comment") {
		t.Fatalf("app comment missing:\n%s", text)
	}
	if strings.Contains(text, "other.example.com") {
		t.Fatalf("another app leaked into the app YAML:\n%s", text)
	}
	text = strings.Replace(text, "app.example.com", "changed.example.com", 1)
	text = strings.Replace(text, "echo deploy", "echo changed", 1)
	tmp := doc.Clone()
	if err := tmp.ReplaceAppYAML("prod", "app", []byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Save(); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Servers["prod"].Apps["app"].Domains[0]; got != "changed.example.com" {
		t.Fatalf("app domain = %q", got)
	}
	if got := cfg.Servers["prod"].Apps["other"].Domains[0]; got != "other.example.com" {
		t.Fatalf("other app changed: %q", got)
	}
	if got := cfg.Servers["prod"].Apps["app"].Deploy.Commands; got != "echo changed\n" {
		t.Fatalf("deploy.commands = %q", got)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "Edit only this app's settings") {
		t.Fatalf("the editor header was saved into ship.yaml:\n%s", saved)
	}
}

func TestReplaceAppYAMLRejectsBadDocuments(t *testing.T) {
	_, doc := loadAppEditDoc(t)
	cases := []struct {
		name string
		data string
		want string
	}{
		{"empty", "", "empty"},
		{"sequence", "- one\n- two\n", "mapping"},
		{"two documents", "domains: [a.example.com]\n---\ndomains: [b.example.com]\n", "exactly one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := doc.Clone().ReplaceAppYAML("prod", "app", []byte(tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
