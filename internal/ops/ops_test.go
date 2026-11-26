package ops

import (
	"io"
	"strings"
	"testing"
)

func TestListsServersAndApps(t *testing.T) {
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      beta:
        domains: [beta.example.com]
        upstream: beta:8080
        source: {type: github-release, repo: example/beta}
        backup: {}
      alpha:
        domains: [alpha.example.com]
        upstream: alpha:8080
        source: {type: github-release, repo: example/alpha}
        backup: {}
  staging:
    host: ops@203.0.113.11
    apps:
      app:
        domains: [staging.example.com]
        upstream: app:8080
        source: {type: github-release, repo: example/app}
        backup: {}
`
	op := New(loadConfig(t, doc), Options{Out: io.Discard})
	if got := strings.Join(op.Servers(), ","); got != "prod,staging" {
		t.Fatalf("Servers = %q", got)
	}
	apps, err := op.Apps("prod")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(apps, ","); got != "alpha,beta" {
		t.Fatalf("Apps = %q", got)
	}
	if _, err := op.Apps("nope"); err == nil {
		t.Fatal("unknown server accepted")
	}
}
