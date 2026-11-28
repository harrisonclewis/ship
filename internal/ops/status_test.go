package ops

import (
	"bytes"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
)

func TestParseStatusKV(t *testing.T) {
	out := `server proxy=running reboot_required=no
app name=lewbox current=v1.2.3 previous=v1.2.2 health=healthy
app name=commerce current=v2.0.0 previous=- health=starting
`
	report, err := ParseStatusKV([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if report.Proxy != "running" || report.RebootRequired {
		t.Fatalf("server record = %+v", report)
	}
	if len(report.Apps) != 2 {
		t.Fatalf("apps = %#v", report.Apps)
	}
	first := report.Apps[0]
	if first.App != "lewbox" || first.Current != "v1.2.3" || first.Previous != "v1.2.2" || first.Health != "healthy" {
		t.Fatalf("first app = %+v", first)
	}
	second := report.Apps[1]
	if second.App != "commerce" || second.Current != "v2.0.0" || second.Previous != "" || second.Health != "starting" {
		t.Fatalf("second app = %+v", second)
	}
}

func TestParseStatusKVLastDeployed(t *testing.T) {
	out := `server proxy=running reboot_required=no
app name=new current=v1.2.3 previous=v1.2.2 health=healthy maintenance=off last_deployed=2026-11-27T12:34:56Z
app name=missing current=v2.0.0 previous=- health=healthy maintenance=off last_deployed=-
app name=old current=v3.0.0 previous=v2.9.0 health=healthy maintenance=off
`
	report, err := ParseStatusKV([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Apps) != 3 {
		t.Fatalf("apps = %#v", report.Apps)
	}
	if got := report.Apps[0].LastDeployed; got != "2026-11-27T12:34:56Z" {
		t.Errorf("new app last deployed = %q", got)
	}
	if got := report.Apps[1].LastDeployed; got != "" {
		t.Errorf("missing timestamp = %q, want empty", got)
	}
	if got := report.Apps[2].LastDeployed; got != "" {
		t.Errorf("old output timestamp = %q, want empty", got)
	}
}

func TestPrintStatusLastDeployed(t *testing.T) {
	var out bytes.Buffer
	o := New(&config.Config{}, Options{Out: &out})
	o.printStatus(StatusReport{
		Server: "prod",
		Proxy:  "running",
		Apps: []AppStatus{
			{App: "new", Current: "v1", Health: "healthy", Config: "applied", LastDeployed: "2026-11-27T12:34:56Z"},
			{App: "old", Current: "v2", Health: "healthy", Config: "applied"},
		},
	})
	got := out.String()
	if !strings.Contains(got, "last_deployed=2026-11-27T12:34:56Z") {
		t.Errorf("status output missing timestamp:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "old") && !strings.Contains(line, "last_deployed=-") {
			t.Errorf("old app line = %q, want last_deployed=-", line)
		}
	}
}

func TestParseStatusKVReboot(t *testing.T) {
	report, err := ParseStatusKV([]byte("server proxy=missing reboot_required=yes\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.RebootRequired || report.Proxy != "missing" {
		t.Fatalf("report = %+v", report)
	}
}

func TestParseStatusKVRejectsGarbage(t *testing.T) {
	for _, out := range []string{
		"nonsense record\n",
		"app current=v1\n",
		"server proxy=running extra\n",
	} {
		if _, err := ParseStatusKV([]byte(out)); err == nil {
			t.Errorf("accepted %q", strings.TrimSpace(out))
		}
	}
}
