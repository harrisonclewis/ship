package config

import (
	"strings"
	"testing"
)

func imageRecord(extra string) string {
	return `domains: [example.com]
upstream: web:3000
source:
  type: image
` + extra
}

func releaseRecord(extra string) string {
	return `domains: [hello.example.com]
upstream: hello:8080
source:
  type: github-release
  repo: example/hello
` + extra
}

func recordFrom(t *testing.T, doc string) *App {
	t.Helper()
	app, err := DecodeAppRecord(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestDeployMaintenanceDefaultsToTrue(t *testing.T) {
	if !recordFrom(t, imageRecord("")).MaintenanceEnabled() {
		t.Fatal("maintenance should default to true")
	}
}

func TestDeployMaintenanceCanBeDisabled(t *testing.T) {
	if recordFrom(t, imageRecord("deploy:\n  maintenance: false\n")).MaintenanceEnabled() {
		t.Fatal("maintenance: false was not honoured")
	}
}

func TestDeployCommandsAllowBlocksAndComments(t *testing.T) {
	doc := imageRecord(`deploy:
  maintenance: true
  commands: |
    # migrate first
    ./migrate

    ./warm-cache
`)
	got := recordFrom(t, doc).Deploy.Commands
	for _, want := range []string{"# migrate first", "./migrate", "./warm-cache"} {
		if !strings.Contains(got, want) {
			t.Errorf("deploy commands missing %q:\n%s", want, got)
		}
	}
}

func TestBuildCommandsOnlyForImageApps(t *testing.T) {
	doc := releaseRecord(`build:
  commands: |
    make linux
`)
	if _, err := DecodeAppRecord(strings.NewReader(doc)); err == nil || !strings.Contains(err.Error(), "build.commands") {
		t.Fatalf("release app build commands: err = %v", err)
	}
}

func TestBuildCommandsForImageApps(t *testing.T) {
	doc := imageRecord(`build:
  commands: |
    npm ci
    npm run build
`)
	if got := recordFrom(t, doc).Build.Commands; !strings.Contains(got, "npm run build") {
		t.Fatalf("build commands = %q", got)
	}
}

func TestScriptsRejectNULAndNormaliseCRLF(t *testing.T) {
	doc := imageRecord("build:\n  commands: \"npm ci\\r\\nnpm run build\\r\\n\"\n")
	if got := recordFrom(t, doc).Build.Commands; strings.Contains(got, "\r") {
		t.Fatalf("CRLF was not normalised: %q", got)
	}
	bad := imageRecord("deploy:\n  commands: \"echo \\u0000\"\n")
	if _, err := DecodeAppRecord(strings.NewReader(bad)); err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("NUL script: err = %v", err)
	}
}

func TestMetaCarriesServerAndMaintenance(t *testing.T) {
	app := recordFrom(t, "domains: [example.com]\nupstream: web:3000\nsource:\n  type: image\ndeploy:\n  maintenance: false\n")
	srv := &Server{Name: "prod", Arch: "linux/amd64", Apps: map[string]*App{"web": app}}
	meta := srv.Meta("web")
	for _, want := range []string{"server=prod\n", "maintenance=off\n"} {
		if !strings.Contains(meta, want) {
			t.Errorf("meta missing %q:\n%s", want, meta)
		}
	}
}
