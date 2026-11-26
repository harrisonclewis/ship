package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const previousSuffix = ".old"

// Install replaces targetPath (following symlinks) with binary. The replacement
// is written beside the target first, so a failure leaves the old binary in
// place. On Unix the new file is renamed over the target atomically. Windows
// cannot overwrite a running .exe, so the old one is renamed to <target>.old
// (removed by CleanupPreviousBinary on a later start) and put back if the swap
// fails.
func Install(binary []byte, targetPath, goos string) (string, error) {
	if targetPath == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("find the running executable: %w", err)
		}
		targetPath = executable
	}
	target, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", targetPath, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".update-*")
	if err != nil {
		return "", installError(err, goos, target)
	}
	tmpPath := tmp.Name()
	keepTmp := true
	defer func() {
		if keepTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write update: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("flush update: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close update: %w", err)
	}
	if goos != "windows" {
		if err := os.Chmod(tmpPath, 0o755); err != nil {
			return "", fmt.Errorf("make update executable: %w", err)
		}
	}

	if goos == "windows" {
		previous := previousBinaryPath(target)
		_ = os.Remove(previous)
		if err := os.Rename(target, previous); err != nil {
			return "", installError(err, goos, target)
		}
		if err := os.Rename(tmpPath, target); err != nil {
			_ = os.Rename(previous, target)
			return "", installError(err, goos, target)
		}
		keepTmp = false
		return target, nil
	}

	if err := os.Rename(tmpPath, target); err != nil {
		return "", installError(err, goos, target)
	}
	keepTmp = false
	return target, nil
}

func previousBinaryPath(target string) string {
	return target + previousSuffix
}

// CleanupPreviousBinary removes the executable a Windows update renamed aside.
// It is best effort and safe to call on every platform.
func CleanupPreviousBinary(targetPath string) {
	if targetPath == "" {
		executable, err := os.Executable()
		if err != nil {
			return
		}
		targetPath = executable
	}
	target, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		target = targetPath
	}
	_ = os.Remove(previousBinaryPath(target))
}

func installError(err error, goos, target string) error {
	if errors.Is(err, fs.ErrPermission) || isReadOnlyError(err) {
		hint := "Run `sudo ship update`, or reinstall ship to a directory you own."
		if goos == "windows" {
			hint = "Run it from an elevated terminal, or reinstall ship to a folder you own."
		}
		return fmt.Errorf("no permission to replace %s. %s: %w", target, hint, err)
	}
	return fmt.Errorf("could not replace %s: %w", target, err)
}
