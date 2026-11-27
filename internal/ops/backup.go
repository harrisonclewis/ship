package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/remote"
)

// BackupResult is one archive reported by the agent.
type BackupResult struct {
	App     string
	Archive string
	SHA256  string
}

func parseBackupOutput(out string) []BackupResult {
	var results []BackupResult
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := map[string]string{}
		for _, f := range strings.Fields(line) {
			if k, v, ok := strings.Cut(f, "="); ok {
				fields[k] = v
			}
		}
		if fields["app"] != "" && fields["archive"] != "" {
			results = append(results, BackupResult{
				App:     fields["app"],
				Archive: fields["archive"],
				SHA256:  fields["sha256"],
			})
		}
	}
	return results
}

// Backup archives one app, or every app when target is "all" or empty. When
// pull is true it downloads each new archive to
// <backup_dir_local>/<server>/<app>/.
func (o *Ops) Backup(ctx context.Context, server, target string, pull bool) ([]BackupResult, error) {
	if target == "" {
		target = "all"
	}
	s, srv, err := o.session(ctx, server)
	if err != nil {
		return nil, err
	}
	if target != "all" {
		if err := config.ValidateAppName(target); err != nil {
			return nil, err
		}
	}
	if err := o.ensureAgent(s); err != nil {
		return nil, err
	}
	out, err := s.OutputAgent("backup", target, "manual")
	if len(out) > 0 {
		fmt.Fprint(o.opts.Out, string(out))
	}
	if err != nil {
		return nil, err
	}
	results := parseBackupOutput(string(out))
	if !pull || o.opts.DryRun {
		return results, nil
	}
	for _, r := range results {
		if err := validArchivePath(r.App, r.Archive); err != nil {
			return results, err
		}
		if len(r.SHA256) != 64 {
			return results, fmt.Errorf("agent returned an invalid sha256 for %s", r.Archive)
		}
		dst, err := downloadSHA(s, filepath.Join(o.Config.BackupDirLocal, srv.Name, r.App), r.Archive, r.SHA256)
		if err != nil {
			return results, err
		}
		fmt.Fprintf(o.opts.Out, "saved %s\n", dst)
	}
	return results, nil
}

var archiveNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,150}\.tar\.gz$`)

func validArchivePath(app, p string) error {
	if !strings.HasPrefix(p, "/var/backups/ship/"+app+"/") ||
		strings.Contains(p, "..") || path.Clean(p) != p ||
		!archiveNameRE.MatchString(path.Base(p)) {
		return fmt.Errorf("unexpected archive path %q for %s", p, app)
	}
	return nil
}

func validRemoteArchive(app, p string) bool {
	if strings.HasPrefix(p, "/var/backups/ship/"+app+"/") &&
		!strings.Contains(p, "..") && path.Clean(p) == p &&
		archiveNameRE.MatchString(path.Base(p)) {
		return true
	}
	return regexp.MustCompile(`^/tmp/ship-restore-` + regexp.QuoteMeta(app) + `-[0-9]+\.tar\.gz$`).MatchString(p)
}

// Restore restores an app from a server archive or a local file, which is
// uploaded first. Callers confirm first.
func (o *Ops) Restore(ctx context.Context, server, app, archive string) error {
	s, _, err := o.session(ctx, server)
	if err != nil {
		return err
	}
	if err := config.ValidateAppName(app); err != nil {
		return err
	}
	if err := o.ensureAgent(s); err != nil {
		return err
	}

	remotePath := archive
	if fi, err := os.Stat(archive); err == nil && fi.Mode().IsRegular() {
		if s.LocalMode {
			abs, err := filepath.Abs(archive)
			if err != nil {
				return err
			}
			if validRemoteArchive(app, abs) {
				remotePath = abs
			} else {
				// The agent only accepts its own archive directory or a
				// /tmp/ship-restore-* path. Copy a local archive there instead
				// of uploading it over ssh.
				remotePath = fmt.Sprintf("/tmp/ship-restore-%s-%d.tar.gz", app, time.Now().UnixNano())
				if o.opts.DryRun {
					fmt.Fprintf(o.opts.Out, "# would copy %s to %s\n", abs, remotePath)
				} else if err := copyLocalFile(abs, remotePath); err != nil {
					return fmt.Errorf("copy archive: %w", err)
				}
			}
		} else {
			f, err := os.Open(archive)
			if err != nil {
				return err
			}
			defer f.Close()
			remotePath = fmt.Sprintf("/tmp/ship-restore-%s-%d.tar.gz", app, time.Now().Unix())
			if err := s.UploadStream(remotePath, 0o600, f, fi.Size()); err != nil {
				return fmt.Errorf("upload archive: %w", err)
			}
		}
	} else if !validRemoteArchive(app, archive) {
		return fmt.Errorf("archive %q is neither a local file nor a ship archive for %s", archive, app)
	}
	return s.RunAgent("restore", app, remotePath, "--yes")
}

// downloadSHA verifies and stores a downloaded archive.
func downloadSHA(s *remote.Session, localDir, remotePath, wantSHA string) (string, error) {
	if err := os.MkdirAll(localDir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(localDir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(localDir, ".download-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return "", err
	}
	if err := s.DownloadFile(remotePath, f); err != nil {
		f.Close()
		return "", fmt.Errorf("download %s: %w", remotePath, err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	sum, err := fileSHA(tmp)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(sum, wantSHA) {
		return "", fmt.Errorf("checksum mismatch for %s: want %s, got %s", remotePath, wantSHA, sum)
	}
	// remotePath is a server (POSIX) path; localDir is a local one.
	dst := filepath.Join(localDir, path.Base(remotePath))
	if existing, err := fileSHA(dst); err == nil {
		if existing == sum {
			return dst, nil
		}
		return "", fmt.Errorf("%s already exists with a different checksum", dst)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	if err := os.Chmod(dst, 0o600); err != nil {
		return "", err
	}
	return dst, nil
}

func copyLocalFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
