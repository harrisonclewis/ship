// Package localedit edits a small local file through a temporary copy. The
// caller validates the copy and only then replaces the original, so a bad edit
// never reaches the file.
package localedit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrChanged means the file on disk no longer matches the version copied when
// editing began.
var ErrChanged = errors.New("file changed on disk since editing began; reload it and try again")

// Validator checks edited contents before they replace the original.
type Validator func([]byte) error

// Editor is the program that opens a file for the user.
type Editor struct {
	Name string
	Args []string
}

// Resolve picks the editor from $VISUAL, then $EDITOR, then the platform
// default. Values may include arguments, for example "code --wait".
func Resolve() (Editor, error) {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		fields := strings.Fields(os.Getenv(key))
		if len(fields) == 0 {
			continue
		}
		return Editor{Name: fields[0], Args: fields[1:]}, nil
	}
	if runtime.GOOS == "windows" {
		return Editor{Name: "notepad"}, nil
	}
	return Editor{Name: "vi"}, nil
}

// Command builds the editor process for one file.
func (e Editor) Command(file string) *exec.Cmd {
	args := append(append([]string(nil), e.Args...), file)
	return exec.Command(e.Name, args...)
}

// Session is one file being edited through a temporary copy.
type Session struct {
	path     string
	original []byte
	exists   bool
	mode     os.FileMode

	dir         string
	temp        string
	wroteBackup bool
}

// Begin copies path into a private temporary directory. A missing file starts
// empty, so the caller can create it on save.
func Begin(path string) (*Session, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	s := &Session{path: abs, mode: 0o600}
	data, readErr := os.ReadFile(abs)
	switch {
	case readErr == nil:
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if fi.IsDir() {
			return nil, fmt.Errorf("%s is a directory", abs)
		}
		s.original = data
		s.exists = true
		s.mode = fi.Mode().Perm()
	case errors.Is(readErr, os.ErrNotExist):
	default:
		return nil, readErr
	}

	dir, err := os.MkdirTemp("", "ship-edit-")
	if err != nil {
		return nil, err
	}
	s.dir = dir
	s.temp = filepath.Join(dir, filepath.Base(abs))
	if err := os.WriteFile(s.temp, s.original, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return s, nil
}

// Path is the real file this session edits.
func (s *Session) Path() string { return s.path }

// TempPath is the copy handed to the editor. It is kept when a save is refused
// so the user's edits are not lost.
func (s *Session) TempPath() string { return s.temp }

// Existed reports whether the real file existed when the session began.
func (s *Session) Existed() bool { return s.exists }

// BackupWritten reports whether the successful save kept a .bak copy.
func (s *Session) BackupWritten() bool { return s.wroteBackup }

// Changed reports whether the editor changed the temporary copy.
func (s *Session) Changed() (bool, error) {
	data, err := os.ReadFile(s.temp)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(data, s.original), nil
}

// Validate reads the temporary copy and runs the caller's validator.
func (s *Session) Validate(validate Validator) error {
	data, err := os.ReadFile(s.temp)
	if err != nil {
		return err
	}
	return validate(data)
}

// Save replaces the real file with the temporary copy after checking that the
// real file has not changed since Begin. The previous file is kept as .bak.
func (s *Session) Save() error {
	data, err := os.ReadFile(s.temp)
	if err != nil {
		return err
	}
	wroteBackup, err := ReplaceFile(s.path, s.original, data, s.exists, s.mode)
	if err != nil {
		return err
	}
	s.original = data
	s.exists = true
	s.wroteBackup = wroteBackup
	return nil
}

// Close removes the temporary copy.
func (s *Session) Close() error {
	if s.dir == "" {
		return nil
	}
	err := os.RemoveAll(s.dir)
	s.dir, s.temp = "", ""
	return err
}

// ReplaceFile atomically replaces path with data. It refuses when the file no
// longer matches original and keeps original as path+".bak" when it was not
// empty. It returns whether a backup was written.
func ReplaceFile(path string, original, data []byte, exists bool, mode os.FileMode) (bool, error) {
	if exists {
		current, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false, ErrChanged
			}
			return false, err
		}
		if !bytes.Equal(current, original) {
			return false, ErrChanged
		}
	} else if _, err := os.Stat(path); err == nil {
		return false, ErrChanged
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	if mode == 0 || !exists {
		mode = 0o600
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, err
	}
	tmp := f.Name()
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}
	if err := f.Chmod(mode); err != nil {
		cleanup()
		return false, err
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return false, err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return false, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}

	wroteBackup := false
	if exists && len(original) > 0 {
		bak := path + ".bak"
		if err := os.WriteFile(bak, original, mode); err != nil {
			_ = os.Remove(tmp)
			return false, fmt.Errorf("write %s: %w", bak, err)
		}
		wroteBackup = true
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return wroteBackup, nil
}
