package ops

import (
	"strings"
	"testing"
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
