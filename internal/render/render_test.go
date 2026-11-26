package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
)

func TestCaddyfileGolden(t *testing.T) {
	c, err := config.Load(filepath.Join("testdata", "ship.yaml"))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	srv, err := c.Server("prod")
	if err != nil {
		t.Fatal(err)
	}
	got := Caddyfile(srv)
	want, err := os.ReadFile(filepath.Join("testdata", "Caddyfile.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("Caddyfile mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestCaddyfileIncludesRedirects(t *testing.T) {
	c, err := config.Load(filepath.Join("testdata", "ship.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := c.Server("prod")
	if err != nil {
		t.Fatal(err)
	}
	got := Caddyfile(srv)
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
