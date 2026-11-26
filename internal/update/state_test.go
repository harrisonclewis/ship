package update

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBackgroundCheckThrottlesAndCaches(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("x"))
	client, _ := f.start(t)
	client.StateDir = t.TempDir()
	env := map[string]string{}
	client.Getenv = func(key string) string { return env[key] }

	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	notice := client.BackgroundCheck(context.Background(), "0.1.0", now)
	if notice == nil || notice.LatestVersion != "0.2.0" {
		t.Fatalf("notice = %+v", notice)
	}
	if api, _ := f.hits(); api != 1 {
		t.Fatalf("api hits = %d, want 1", api)
	}

	if notice := client.BackgroundCheck(context.Background(), "0.1.0", now.Add(10*time.Hour)); notice == nil {
		t.Fatal("cached notice missing later the same day")
	}
	if api, _ := f.hits(); api != 1 {
		t.Fatalf("api hits after 10h = %d, want 1", api)
	}

	if notice := client.BackgroundCheck(context.Background(), "0.1.0", now.Add(25*time.Hour)); notice == nil {
		t.Fatal("notice missing after the cache expired")
	}
	if api, _ := f.hits(); api != 2 {
		t.Fatalf("api hits after 25h = %d, want 2", api)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(client.StateDir, stateFileName))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("state mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestBackgroundCheckFallsBackToCacheWhenOffline(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("x"))
	client, server := f.start(t)
	client.StateDir = t.TempDir()
	client.Getenv = func(string) string { return "" }

	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	if notice := client.BackgroundCheck(context.Background(), "0.1.0", now); notice == nil {
		t.Fatal("first check returned no notice")
	}
	server.Close()

	notice := client.BackgroundCheck(context.Background(), "0.1.0", now.Add(25*time.Hour))
	if notice == nil || notice.LatestVersion != "0.2.0" {
		t.Fatalf("offline notice = %+v", notice)
	}
}

func TestBackgroundCheckCanBeDisabled(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("x"))
	client, _ := f.start(t)
	client.StateDir = t.TempDir()
	client.Getenv = func(key string) string {
		if key == "SHIP_NO_UPDATE_CHECK" {
			return "1"
		}
		return ""
	}
	if notice := client.BackgroundCheck(context.Background(), "0.1.0", time.Now()); notice != nil {
		t.Fatalf("notice = %+v, want nil", notice)
	}
	if api, _ := f.hits(); api != 0 {
		t.Fatalf("api hits = %d, want 0", api)
	}
}

func TestBackgroundCheckSkipsDevelopmentBuilds(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("x"))
	client, _ := f.start(t)
	client.StateDir = t.TempDir()
	client.Getenv = func(string) string { return "" }
	if notice := client.BackgroundCheck(context.Background(), "dev", time.Now()); notice != nil {
		t.Fatalf("notice = %+v, want nil", notice)
	}
	if api, _ := f.hits(); api != 0 {
		t.Fatalf("api hits = %d, want 0", api)
	}
}

func TestStartupPrintsCachedNotice(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("x"))
	client, _ := f.start(t)
	client.StateDir = t.TempDir()
	client.Getenv = func(string) string { return "" }
	if err := client.writeState(updateState{
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
		LatestVersion: "0.2.0",
	}); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	startup(client, "0.1.0", []string{"status"}, true, &out)
	if got := out.String(); !strings.Contains(got, "ship 0.2.0 is available") {
		t.Fatalf("output = %q", got)
	}
	if api, _ := f.hits(); api != 0 {
		t.Fatalf("fresh cache still contacted GitHub (%d hits)", api)
	}
}

func TestStartupSkips(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("x"))
	client, _ := f.start(t)
	client.StateDir = t.TempDir()
	client.Getenv = func(string) string { return "" }
	if err := client.writeState(updateState{
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
		LatestVersion: "0.2.0",
	}); err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		current string
		args    []string
		tty     bool
	}{
		"not a tty":   {"0.1.0", []string{"status"}, false},
		"update":      {"0.1.0", []string{"update"}, true},
		"version":     {"0.1.0", []string{"version"}, true},
		"development": {"dev", []string{"status"}, true},
	} {
		var out strings.Builder
		startup(client, test.current, test.args, test.tty, &out)
		if out.Len() != 0 {
			t.Errorf("%s: output = %q", name, out.String())
		}
	}
}
