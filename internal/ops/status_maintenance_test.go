package ops

import "testing"

func TestParseStatusKVMaintenance(t *testing.T) {
	out := `server proxy=running reboot_required=no
app name=app current=v1.2.3 previous=- health=healthy maintenance=on
app name=other current=v1.0.0 previous=v0.9.0 health=healthy maintenance=off
`
	report, err := ParseStatusKV([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Apps) != 2 {
		t.Fatalf("apps = %#v", report.Apps)
	}
	if !report.Apps[0].Maintenance {
		t.Errorf("first app maintenance = false, want true")
	}
	if report.Apps[1].Maintenance {
		t.Errorf("second app maintenance = true, want false")
	}
}
