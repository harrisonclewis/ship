package render

import (
	"strings"
	"testing"
)

func TestCaddyfileMaintenanceForDomainsAndRedirects(t *testing.T) {
	got := CaddyfileFor(goldenApps())
	for _, want := range []string{
		"@maintenance file {",
		"root /etc/caddy/maintenance",
		"try_files lewbox",
		"try_files lewbox-commerce",
		"header Retry-After 120",
		`respond "Maintenance in progress. Try again soon." 503`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Caddyfile missing %q", want)
		}
	}
	// The redirect block must check the same app flag before redirecting.
	redirect := got[strings.LastIndex(got, "www.lewbox.com {"):]
	if !strings.Contains(redirect, "@maintenance file") || !strings.Contains(redirect, "try_files lewbox-commerce") {
		t.Errorf("redirect block has no maintenance guard:\n%s", redirect)
	}
	if strings.Index(redirect, "handle @maintenance") > strings.Index(redirect, "redir https://") {
		t.Errorf("redirect runs before the maintenance handler:\n%s", redirect)
	}
}
