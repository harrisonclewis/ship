package localedit

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolvePrefersVisualThenEditor(t *testing.T) {
	t.Setenv("VISUAL", "code --wait")
	t.Setenv("EDITOR", "nano")
	ed, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if ed.Name != "code" || len(ed.Args) != 1 || ed.Args[0] != "--wait" {
		t.Fatalf("editor = %#v, want code --wait", ed)
	}

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "code --wait")
	ed, err = Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if ed.Name != "code" || len(ed.Args) != 1 || ed.Args[0] != "--wait" {
		t.Fatalf("editor = %#v, want code --wait", ed)
	}

	t.Setenv("EDITOR", "")
	ed, err = Resolve()
	if err != nil {
		t.Fatal(err)
	}
	want := "vi"
	if runtime.GOOS == "windows" {
		want = "notepad"
	}
	if ed.Name != want || len(ed.Args) != 0 {
		t.Fatalf("default editor = %#v, want %s", ed, want)
	}
}

func TestSessionSavesWithBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ship.yaml")
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	got, err := os.ReadFile(s.TempPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old\n" {
		t.Fatalf("temp copy = %q", got)
	}
	if changed, err := s.Changed(); err != nil || changed {
		t.Fatalf("Changed = %v, %v; want false", changed, err)
	}

	if err := os.WriteFile(s.TempPath(), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.Changed(); err != nil || !changed {
		t.Fatalf("Changed = %v, %v; want true", changed, err)
	}
	if err := s.Validate(func(data []byte) error {
		if string(data) != "new\n" {
			t.Fatalf("validator saw %q", data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if !s.BackupWritten() {
		t.Fatal("save did not report a backup")
	}

	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new\n" {
		t.Fatalf("saved file = %q", got)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != "old\n" {
		t.Fatalf("backup = %q", bak)
	}
}

func TestSessionValidationFailureDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ship.yaml")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.WriteFile(s.TempPath(), []byte("bad\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(func([]byte) error { return errors.New("nope") }); err == nil {
		t.Fatal("validator error was lost")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old\n" {
		t.Fatalf("file changed despite a validation failure: %q", got)
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a .bak was written despite a validation failure")
	}
}

func TestSessionRefusesChangedFileAndKeepsTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.WriteFile(s.TempPath(), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("theirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); !errors.Is(err, ErrChanged) {
		t.Fatalf("Save = %v, want ErrChanged", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "theirs\n" {
		t.Fatalf("the file changed on disk was overwritten: %q", got)
	}
	temp, err := os.ReadFile(s.TempPath())
	if err != nil {
		t.Fatalf("temporary edits were lost: %v", err)
	}
	if string(temp) != "mine\n" {
		t.Fatalf("temporary edits = %q", temp)
	}
}

func TestSessionCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps", "app", "compose.yaml")
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Existed() {
		t.Fatal("a missing file was reported as existing")
	}
	if changed, err := s.Changed(); err != nil || changed {
		t.Fatalf("Changed = %v, %v; want false", changed, err)
	}
	if err := os.WriteFile(s.TempPath(), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if s.BackupWritten() {
		t.Fatal("a new file got a backup")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "services: {}\n" {
		t.Fatalf("created file = %q", got)
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a .bak was written for a new file")
	}
}
