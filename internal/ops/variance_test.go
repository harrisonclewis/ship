package ops

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
)

func testApp(t *testing.T, doc string) *config.App {
	t.Helper()
	app, err := config.DecodeAppRecord(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestParseConfigExport(t *testing.T) {
	blob := buildConfigExport(t, map[string]string{
		"config/server.yaml":        "admin_user: ops\ncaddy_image: caddy:2.10-alpine\n",
		"config/applied-setup.yaml": "admin_user=ops\napps=app:1000\n",
		"config/applied-proxy.yaml": "caddyfile_sha256=abc\ncaddy_image=caddy:2.10-alpine\nmode=sync\n",
		"config/arch":               "x86_64\n",
		"proxy/Caddyfile":           "active\n",
		"proxy/desired-image":       "caddy:2.10-alpine\n",
		"proxy/running-image":       "caddy:2.10-alpine\n",
		"desired/app/ship-app.yaml": "domains: [app.example.com]\nupstream: app:8080\nsource:\n  type: image\n",
		"desired/app/compose.yaml":  "name: app\n",
		"desired/app/.hash":         strings.Repeat("a", 64),
		"running/app/ship-app.yaml": "domains: [app.example.com]\nupstream: app:8080\nsource:\n  type: image\n",
		"running/app/compose.yaml":  "name: app\n",
		"running/app/.ship-meta":    "app=app\n",
		"running/app/.ship-state":   "current=v1\nprevious=\n",
	})
	state, err := parseConfigExport(blob)
	if err != nil {
		t.Fatal(err)
	}
	state.applyDefaults()
	if state.Settings.AdminUser != "ops" || state.Settings.CaddyImage != "caddy:2.10-alpine" {
		t.Fatalf("settings = %+v", state.Settings)
	}
	if state.Arch != "x86_64" {
		t.Fatalf("arch = %q", state.Arch)
	}
	if state.Apps["app"] == nil || state.Apps["app"].Record == nil || state.Apps["app"].Hash == "" {
		t.Fatalf("desired app = %+v", state.Apps["app"])
	}
	if state.Running["app"] == nil || state.Running["app"].Current != "v1" {
		t.Fatalf("running app = %+v", state.Running["app"])
	}
	if !state.AppliedSetup.Present || state.AppliedSetup.Apps != "app:1000" {
		t.Fatalf("applied setup = %+v", state.AppliedSetup)
	}
	if !state.AppliedProxy.Present || state.AppliedProxy.Mode != "sync" {
		t.Fatalf("applied proxy = %+v", state.AppliedProxy)
	}
	// A duplicate or unsafe member must be rejected.
	bad := buildConfigExport(t, map[string]string{"desired/app/notes.txt": "x"})
	if _, err := parseConfigExport(bad); err == nil {
		t.Fatal("unexpected desired file accepted")
	}
}

func TestProxyRefusesRunningAppsWithoutDesiredConfig(t *testing.T) {
	export := buildConfigExport(t, map[string]string{
		"config/arch":                  "x86_64\n",
		"running/legacy/ship-app.yaml": "domains: [legacy.example.com]\nupstream: legacy:8080\nsource:\n  type: image\n",
		"running/legacy/.ship-meta":    "app=legacy\nsource=image\n",
	})
	cfg := &config.Config{Dir: t.TempDir(), Servers: map[string]*config.Server{
		"prod": {Name: "prod", Host: "ops@203.0.113.10"},
	}}
	op := New(cfg, Options{Agent: testAgent, DryRun: true, Out: &strings.Builder{}, ErrOut: io.Discard, Runner: &configExportRunner{export: export}})
	err := op.ProxySync(context.Background(), "prod")
	if err == nil {
		t.Fatal("proxy sync accepted a running app with no desired config")
	}
	for _, want := range []string{"legacy", "run on this server but have no desired config", "config push"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestRunningWithoutDesired(t *testing.T) {
	state := &ServerConfig{
		Apps: map[string]*DesiredApp{"has": {Name: "has", Record: testApp(t, "domains: [has.example.com]\nupstream: has:1\nsource:\n  type: image\n")}},
		Running: map[string]*RunningApp{
			"has":    {Name: "has", Record: testApp(t, "domains: [has.example.com]\nupstream: has:1\nsource:\n  type: image\n"), Meta: map[string]string{"app": "has"}},
			"legacy": {Name: "legacy", Meta: map[string]string{"app": "legacy"}},
		},
	}
	if got := strings.Join(state.RunningWithoutDesired(), ","); got != "legacy" {
		t.Fatalf("RunningWithoutDesired = %q", got)
	}
}

func TestComputeVariances(t *testing.T) {
	desiredWithLiveDomain := testApp(t, "domains: [new.example.com]\nupstream: app:8080\nsource:\n  type: image\n")
	running := testApp(t, "domains: [app.example.com]\nupstream: app:8080\nsource:\n  type: image\n")
	pending := testApp(t, "domains: [pending.example.com]\nupstream: pending:9000\nsource:\n  type: image\n")
	pendingRunning := testApp(t, "domains: [pending.example.com]\nupstream: pending:8000\nsource:\n  type: image\n")
	never := testApp(t, "domains: [never.example.com]\nupstream: never:8080\nsource:\n  type: image\n")

	state := &ServerConfig{
		Server: "prod",
		Settings: ServerSettings{
			AdminUser:  "ops",
			CaddyImage: "caddy:2.10-alpine",
			Present:    true,
		},
		Apps: map[string]*DesiredApp{
			"applied": {Name: "applied", Record: desiredWithLiveDomain, Compose: []byte("name: applied\n")},
			"pending": {Name: "pending", Record: pending, Compose: []byte("name: pending\n"), Dockerfile: []byte("FROM new\n"), HasDockerfile: true},
			"never":   {Name: "never", Record: never, Compose: []byte("name: never\n")},
		},
		Running: map[string]*RunningApp{
			"applied": {Name: "applied", Record: running, Compose: []byte("name: applied\n"), Meta: map[string]string{"app": "applied"}},
			"pending": {Name: "pending", Record: pendingRunning, Compose: []byte("name: old\n"), Dockerfile: []byte("FROM old\n"), HasDockerfile: true, Meta: map[string]string{"app": "pending"}},
		},
		AppliedSetup: AppliedSetup{Present: true, AdminUser: "ops", Apps: ""},
	}
	rendered := state.CaddyfileFor()
	state.ActiveCaddyfile = []byte(rendered)
	state.RunningImage = "caddy:2.10-alpine"

	report := ComputeVariances(state)
	if report.ProxyOutOfSync {
		t.Fatalf("proxy reported out of sync: %s", report.ProxyReason)
	}
	if report.SetupPending {
		t.Fatalf("setup reported pending: %s", report.SetupReason)
	}
	byApp := map[string]AppVariance{}
	for _, v := range report.Apps {
		byApp[v.App] = v
	}
	if got := byApp["applied"].State; got != VarianceApplied {
		t.Fatalf("applied state = %q (diff: %s)", got, byApp["applied"].Diff)
	}
	if got := byApp["pending"].State; got != VariancePending || !byApp["pending"].ComposeChanged || !byApp["pending"].RecordChanged || !byApp["pending"].DockerfileChanged {
		t.Fatalf("pending state = %+v", byApp["pending"])
	}
	for _, want := range []string{
		"--- running/ship-app.yaml", "+++ desired/ship-app.yaml",
		"--- running/compose.yaml", "+++ desired/compose.yaml",
		"--- running/Dockerfile", "+++ desired/Dockerfile",
	} {
		if !strings.Contains(byApp["pending"].Diff, want) {
			t.Errorf("diff missing %q:\n%s", want, byApp["pending"].Diff)
		}
	}
	if got := byApp["never"].State; got != VarianceNotDeployed {
		t.Fatalf("never state = %q", got)
	}
	if !strings.Contains(state.CaddyfileFor(), "reverse_proxy app:8080") {
		t.Fatal("Caddyfile did not use the running upstream")
	}
	if strings.Contains(state.CaddyfileFor(), "never.example.com") {
		t.Fatal("Caddyfile included a never-deployed app")
	}

	state.ActiveCaddyfile = []byte("something else")
	state.RunningImage = "caddy:2.9"
	state.AppliedSetup = AppliedSetup{Present: true, AdminUser: "other", Apps: ""}
	report = ComputeVariances(state)
	if !report.ProxyOutOfSync || !report.SetupPending {
		t.Fatalf("server-level variance = %+v", report)
	}
}

func TestCaddyfileHeaderChangeIsNotOutOfSync(t *testing.T) {
	a := []byte("# old header\n\nexample.com {\n\treverse_proxy web:3000\n}\n")
	b := []byte("# new header, reworded\n\nexample.com {\n\treverse_proxy web:3000\n}\n")
	if !bytes.Equal(caddyfileWithoutComments(a), caddyfileWithoutComments(b)) {
		t.Fatal("header-only change should compare equal")
	}
	c := []byte("# new header\n\nexample.org {\n\treverse_proxy web:3000\n}\n")
	if bytes.Equal(caddyfileWithoutComments(a), caddyfileWithoutComments(c)) {
		t.Fatal("a routing change must not compare equal")
	}
}
