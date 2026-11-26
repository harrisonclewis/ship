package ops

import (
	"strings"
	"testing"
)

func TestParseBackupOutput(t *testing.T) {
	out := `app=lewbox archive=/var/backups/ship/lewbox/manual-20260101T000000Z.tar.gz sha256=` + strings.Repeat("a", 64) + "\n" +
		`app=commerce archive=/var/backups/ship/commerce/manual-20260101T000001Z.tar.gz sha256=` + strings.Repeat("b", 64) + "\n" +
		"some progress line\n"
	got := parseBackupOutput(out)
	if len(got) != 2 {
		t.Fatalf("got %d results: %#v", len(got), got)
	}
	if got[0].App != "lewbox" || got[1].App != "commerce" {
		t.Fatalf("results = %#v", got)
	}
	if len(got[0].SHA256) != 64 {
		t.Fatalf("sha = %q", got[0].SHA256)
	}
}

func TestValidArchivePath(t *testing.T) {
	ok := "/var/backups/ship/lewbox/pre-v1.2.3-20260101T000000Z.tar.gz"
	if err := validArchivePath("lewbox", ok); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		"/var/backups/ship/other/pre-v1.tar.gz",
		"/var/backups/ship/lewbox/../../etc/passwd",
		"/var/backups/ship/lewbox/not-a-tar",
		"/etc/passwd",
	}
	for _, p := range bad {
		if err := validArchivePath("lewbox", p); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
}

func TestValidRemoteArchive(t *testing.T) {
	if !validRemoteArchive("lewbox", "/var/backups/ship/lewbox/manual-20260101T000000Z.tar.gz") {
		t.Fatal("valid server archive rejected")
	}
	if !validRemoteArchive("lewbox", "/tmp/ship-restore-lewbox-1767225600.tar.gz") {
		t.Fatal("valid uploaded archive rejected")
	}
	for _, p := range []string{
		"/tmp/ship-restore-commerce-1.tar.gz",
		"/tmp/ship-restore-lewbox-1;rm.tar.gz",
		"/var/backups/ship/lewbox/../../etc/foo.tar.gz",
		"/var/backups/ship/lewbox/" + strings.Repeat("x", 300) + ".tar.gz",
	} {
		if validRemoteArchive("lewbox", p) {
			t.Errorf("accepted %q", p)
		}
	}
}
