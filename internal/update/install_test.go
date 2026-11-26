package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstallReplacesThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevated privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "ship-real")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "ship")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := Install([]byte("new"), link, "linux")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new" {
		t.Fatalf("target content = %q, err %v", data, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o, want 755", info.Mode().Perm())
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was replaced: info=%v err=%v", info, err)
	}
}

func TestInstallWindowsParksOldBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ship.exe")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Install([]byte("new"), target, "windows"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new" {
		t.Fatalf("target content = %q, err %v", data, err)
	}
	if data, err := os.ReadFile(previousBinaryPath(target)); err != nil || string(data) != "old" {
		t.Fatalf("previous content = %q, err %v", data, err)
	}

	CleanupPreviousBinary(target)
	if _, err := os.Stat(previousBinaryPath(target)); !os.IsNotExist(err) {
		t.Fatalf("previous binary still present: err = %v", err)
	}
}
