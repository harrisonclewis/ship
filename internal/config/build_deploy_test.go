package config

import (
	"strings"
	"testing"
)

func imageApp(extra string) string {
	return `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      web:
        domains: [example.com]
        upstream: web:3000
        source:
          type: image
          path: ~/web
` + extra
}

func releaseApp(extra string) string {
	return `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      hello:
        domains: [hello.example.com]
        upstream: hello:8080
        source:
          type: github-release
          repo: example/hello
` + extra
}

func TestDeployMaintenanceDefaultsToTrue(t *testing.T) {
	c, err := decode(t, imageApp(""))
	if err != nil {
		t.Fatal(err)
	}
	if !server(t, c, "prod").Apps["web"].MaintenanceEnabled() {
		t.Fatal("maintenance should default to true")
	}
}

func TestDeployMaintenanceCanBeDisabled(t *testing.T) {
	c, err := decode(t, imageApp("        deploy:\n          maintenance: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if server(t, c, "prod").Apps["web"].MaintenanceEnabled() {
		t.Fatal("maintenance: false was not honoured")
	}
}

func TestDeployCommandsAllowBlocksAndComments(t *testing.T) {
	doc := imageApp(`        deploy:
          maintenance: true
          commands: |
            # migrate first
            ./migrate

            ./warm-cache
`)
	c, err := decode(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	got := server(t, c, "prod").Apps["web"].Deploy.Commands
	for _, want := range []string{"# migrate first", "./migrate", "./warm-cache"} {
		if !strings.Contains(got, want) {
			t.Errorf("deploy commands missing %q:\n%s", want, got)
		}
	}
}

func TestBuildCommandsOnlyForImageApps(t *testing.T) {
	doc := releaseApp(`        build:
          commands: |
            make linux
`)
	if _, err := decode(t, doc); err == nil || !strings.Contains(err.Error(), "build.commands") {
		t.Fatalf("release app build commands: err = %v", err)
	}
}

func TestBuildCommandsForImageApps(t *testing.T) {
	doc := imageApp(`        build:
          commands: |
            npm ci
            npm run build
`)
	c, err := decode(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := server(t, c, "prod").Apps["web"].Build.Commands; !strings.Contains(got, "npm run build") {
		t.Fatalf("build commands = %q", got)
	}
}

func TestScriptsRejectNULAndNormaliseCRLF(t *testing.T) {
	doc := imageApp("        build:\n          commands: \"npm ci\\r\\nnpm run build\\r\\n\"\n")
	c, err := decode(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := server(t, c, "prod").Apps["web"].Build.Commands; strings.Contains(got, "\r") {
		t.Fatalf("CRLF was not normalised: %q", got)
	}
	bad := imageApp("        deploy:\n          commands: \"echo \\u0000\"\n")
	if _, err := decode(t, bad); err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("NUL script: err = %v", err)
	}
}

func TestMetaCarriesServerAndMaintenance(t *testing.T) {
	c, err := decode(t, imageApp("        deploy:\n          maintenance: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	meta := server(t, c, "prod").Meta("web")
	for _, want := range []string{"server=prod\n", "maintenance=off\n"} {
		if !strings.Contains(meta, want) {
			t.Errorf("meta missing %q:\n%s", want, meta)
		}
	}
}
