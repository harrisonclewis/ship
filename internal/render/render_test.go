package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func goldenApps() []App {
	return []App{
		{Name: "lewbox", Domains: []string{"cloud.lewbox.com", "dev.lewbox.com"}, Upstream: "lewbox:8080", Deployed: true},
		{Name: "lewbox-commerce", Domains: []string{"lewbox.com"}, Redirects: []string{"www.lewbox.com"}, Upstream: "lewbox-commerce:3000", Deployed: true},
		{Name: "never", Domains: []string{"never.example.com"}, Upstream: "never:8080", Deployed: false},
	}
}

func TestCaddyfileGolden(t *testing.T) {
	got := CaddyfileFor(goldenApps())
	want, err := os.ReadFile(filepath.Join("testdata", "Caddyfile.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("Caddyfile mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestCaddyfileUsesRunningUpstreamAndSkipsNeverDeployed(t *testing.T) {
	apps := []App{
		{Name: "app", Domains: []string{"app.example.com"}, Upstream: "app:9000", Deployed: true},
		{Name: "never", Domains: []string{"never.example.com"}, Upstream: "never:8080", Deployed: false},
	}
	got := CaddyfileFor(apps)
	if !strings.Contains(got, "reverse_proxy app:9000") {
		t.Fatalf("running upstream missing:\n%s", got)
	}
	if strings.Contains(got, "never.example.com") || strings.Contains(got, "never:8080") {
		t.Fatalf("never-deployed app was rendered:\n%s", got)
	}
}

func TestCaddyfileIncludesRedirects(t *testing.T) {
	got := CaddyfileFor(goldenApps())
	for _, want := range []string{
		"cloud.lewbox.com, dev.lewbox.com {",
		"reverse_proxy lewbox:8080",
		"www.lewbox.com {",
		"redir https://lewbox.com{uri} permanent",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Caddyfile missing %q", want)
		}
	}
}

func TestCaddyfileIsDeterministic(t *testing.T) {
	a := CaddyfileFor([]App{
		{Name: "b", Domains: []string{"b.example.com"}, Upstream: "b:1", Deployed: true},
		{Name: "a", Domains: []string{"a.example.com"}, Upstream: "a:1", Deployed: true},
	})
	b := CaddyfileFor([]App{
		{Name: "a", Domains: []string{"a.example.com"}, Upstream: "a:1", Deployed: true},
		{Name: "b", Domains: []string{"b.example.com"}, Upstream: "b:1", Deployed: true},
	})
	if a != b {
		t.Fatal("render depends on input order")
	}
	if strings.Index(a, "a.example.com") > strings.Index(a, "b.example.com") {
		t.Fatal("apps are not sorted by name")
	}
}
