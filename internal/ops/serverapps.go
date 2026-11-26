package ops

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/yamldoc"
)

const (
	exportFileLimit  = 1 << 20  // 1 MiB for one record, compose file or Dockerfile
	exportTotalLimit = 16 << 20 // 16 MiB for the whole export
	exportAppLimit   = 500
)

// ServerApp is one app's last-deployed record and files on a server. Record
// is nil when the app was deployed by an older ship that did not store one,
// or when the record can't be read (RecordErr says why).
type ServerApp struct {
	Name   string
	Record *config.App
	// RecordErr is set when ship-app.yaml exists but doesn't decode, for
	// example because a newer ship wrote fields this one doesn't know.
	RecordErr     string
	Compose       []byte
	Dockerfile    []byte
	HasDockerfile bool
}

// ServerApps fetches the records and compose files of every app deployed on a
// server. The export is read over ssh, even under --dry-run. The agent is not
// updated in dry-run mode, because that would change the server.
func (o *Ops) ServerApps(ctx context.Context, server string) ([]ServerApp, error) {
	s, srv, err := o.readSession(ctx, server)
	if err != nil {
		return nil, err
	}
	if !o.opts.DryRun {
		if err := o.ensureAgent(s); err != nil {
			return nil, err
		}
	}
	var out limitBuffer
	out.limit = exportTotalLimit
	if err := s.StreamAgent(&out, "app-export"); err != nil {
		if looksLikeAgentUsage(out.Bytes()) {
			return nil, olderAgentError(srv.Name)
		}
		return nil, fmt.Errorf("read app records from %s: %w", srv.Name, err)
	}
	if looksLikeAgentUsage(out.Bytes()) {
		return nil, olderAgentError(srv.Name)
	}
	if out.overflow {
		return nil, fmt.Errorf("app export from %s is larger than %d bytes", srv.Name, exportTotalLimit)
	}
	return parseServerApps(out.Bytes())
}

func olderAgentError(server string) error {
	return fmt.Errorf("the ship-agent on %s is older; run any ship command for it without --dry-run to update it", server)
}

func looksLikeAgentUsage(out []byte) bool {
	return bytes.Contains(out, []byte("ship-agent: server-side operations")) ||
		bytes.Contains(out, []byte("usage: ship-agent"))
}

type limitBuffer struct {
	buf      bytes.Buffer
	limit    int64
	overflow bool
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - int64(b.buf.Len())
	if int64(len(p)) > remaining {
		if remaining > 0 {
			_, _ = b.buf.Write(p[:remaining])
		}
		b.overflow = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitBuffer) Bytes() []byte { return b.buf.Bytes() }

// parseServerApps enforces the export contract: exactly two path components
// per member, a valid app name, one of three known files, regular files only,
// no duplicates and bounded sizes. Nothing else can be imported.
func parseServerApps(data []byte) ([]ServerApp, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	apps := map[string]*ServerApp{}
	seen := map[string]bool{}
	var total int64
	members := 0

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("app export: %w", err)
		}
		members++
		if members > exportAppLimit*3 {
			return nil, fmt.Errorf("app export has more than %d files", exportAppLimit*3)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("app export: %q is not a regular file", hdr.Name)
		}
		app, file, err := splitExportName(hdr.Name)
		if err != nil {
			return nil, err
		}
		if seen[hdr.Name] {
			return nil, fmt.Errorf("app export: duplicate member %q", hdr.Name)
		}
		seen[hdr.Name] = true
		if hdr.Size < 0 || hdr.Size > exportFileLimit {
			return nil, fmt.Errorf("app export: %q is larger than %d bytes", hdr.Name, exportFileLimit)
		}
		total += hdr.Size
		if total > exportTotalLimit {
			return nil, fmt.Errorf("app export is larger than %d bytes", exportTotalLimit)
		}
		body, err := io.ReadAll(io.LimitReader(tr, exportFileLimit+1))
		if err != nil {
			return nil, fmt.Errorf("app export: read %q: %w", hdr.Name, err)
		}
		if int64(len(body)) != hdr.Size {
			return nil, fmt.Errorf("app export: %q has the wrong size", hdr.Name)
		}

		sa := apps[app]
		if sa == nil {
			if len(apps) >= exportAppLimit {
				return nil, fmt.Errorf("app export has more than %d apps", exportAppLimit)
			}
			sa = &ServerApp{Name: app}
			apps[app] = sa
		}
		switch file {
		case "ship-app.yaml":
			// One unreadable record must not hide every other app.
			record, err := config.DecodeAppRecord(bytes.NewReader(body))
			if err != nil {
				sa.RecordErr = err.Error()
				continue
			}
			sa.Record = record
		case "compose.yaml":
			sa.Compose = body
		case "Dockerfile":
			sa.Dockerfile = body
			sa.HasDockerfile = true
		}
	}

	out := make([]ServerApp, 0, len(apps))
	for _, sa := range apps {
		out = append(out, *sa)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func splitExportName(name string) (string, string, error) {
	if strings.ContainsAny(name, `\`) {
		return "", "", fmt.Errorf("app export: invalid member name %q", name)
	}
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("app export: invalid member name %q", name)
	}
	if err := config.ValidateAppName(parts[0]); err != nil {
		return "", "", fmt.Errorf("app export: invalid app directory %q", parts[0])
	}
	switch parts[1] {
	case "ship-app.yaml", "compose.yaml", "Dockerfile":
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("app export: unexpected file %q", name)
	}
}

// Compare fetches a server's app records and compares them with the local
// configuration and files.
func (o *Ops) Compare(ctx context.Context, server string) ([]AppDiff, error) {
	srv, err := o.Server(server)
	if err != nil {
		return nil, err
	}
	serverApps, err := o.ServerApps(ctx, server)
	if err != nil {
		return nil, err
	}
	return CompareServerApps(srv, serverApps)
}

// AppDiff is the comparison between the local app and its server record.
type AppDiff struct {
	App               string
	Local             bool
	Server            bool
	HasRecord         bool
	RecordErr         string
	RecordChanged     bool
	ComposeChanged    bool
	DockerfileChanged bool
	Diff              string
}

// Changed reports whether the app's record or files differ. A server-only app
// is not "changed"; it is missing locally.
func (d AppDiff) Changed() bool {
	return d.RecordChanged || d.ComposeChanged || d.DockerfileChanged
}

// CompareServerApps compares every local and server app on one server.
func CompareServerApps(srv *config.Server, serverApps []ServerApp) ([]AppDiff, error) {
	byName := make(map[string]ServerApp, len(serverApps))
	names := map[string]bool{}
	for _, sa := range serverApps {
		byName[sa.Name] = sa
		names[sa.Name] = true
	}
	for name := range srv.Apps {
		names[name] = true
	}
	var order []string
	for name := range names {
		order = append(order, name)
	}
	sort.Strings(order)

	out := make([]AppDiff, 0, len(order))
	for _, name := range order {
		sa, onServer := byName[name]
		if !onServer {
			out = append(out, AppDiff{App: name, Local: true})
			continue
		}
		diff, err := CompareApp(srv, sa)
		if err != nil {
			return nil, err
		}
		out = append(out, diff)
	}
	return out, nil
}

// CompareApp compares one server app with the local configuration and files.
func CompareApp(srv *config.Server, sa ServerApp) (AppDiff, error) {
	d := AppDiff{App: sa.Name, Server: true, HasRecord: sa.Record != nil, RecordErr: sa.RecordErr}
	local, err := srv.App(sa.Name)
	if err != nil {
		return d, nil
	}
	d.Local = true

	if sa.Record != nil {
		localRecord, err := local.RecordYAML()
		if err != nil {
			return d, err
		}
		serverRecord, err := sa.Record.RecordYAML()
		if err != nil {
			return d, err
		}
		if !bytes.Equal(localRecord, serverRecord) {
			d.RecordChanged = true
			d.Diff += UnifiedDiff("local/ship-app.yaml", "server/ship-app.yaml", localRecord, serverRecord)
		}
	}

	localCompose, err := os.ReadFile(srv.ComposePath(sa.Name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return d, err
	}
	if !bytes.Equal(localCompose, sa.Compose) {
		d.ComposeChanged = true
		d.Diff += UnifiedDiff("local/compose.yaml", "server/compose.yaml", localCompose, sa.Compose)
	}

	localDockerfile, err := os.ReadFile(srv.DockerfilePath(sa.Name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return d, err
	}
	if sa.HasDockerfile || localDockerfile != nil {
		serverDockerfile := sa.Dockerfile
		if !bytes.Equal(localDockerfile, serverDockerfile) {
			d.DockerfileChanged = true
			d.Diff += UnifiedDiff("local/Dockerfile", "server/Dockerfile", localDockerfile, serverDockerfile)
		}
	}
	return d, nil
}

// ConflictError reports a local app or file that a plain import would replace.
// Callers add their own "--overwrite" wording; the TUI asks first instead.
type ConflictError struct {
	Msg string
}

func (e *ConflictError) Error() string { return e.Msg }

// ImportRequest asks Import to copy server apps into the local configuration.
type ImportRequest struct {
	Server    string
	Apps      []string
	All       bool
	Overwrite bool
	FilesOnly bool
}

// ImportSkip explains why an app in an --all import was left alone.
type ImportSkip struct {
	App    string
	Reason string
}

// ImportSummary is what an import changed.
type ImportSummary struct {
	Imported []string
	Skipped  []ImportSkip
	Files    []string
}

// Import copies app records and files from a server into ship.yaml and
// apps/<name>. Existing local apps and files with different bytes are refused
// unless Overwrite is set, in which case each overwritten file keeps a .bak
// copy. Files that already match are skipped without a conflict or a .bak. An
// app's local source.path is always kept when its definition is replaced.
func (o *Ops) Import(ctx context.Context, req ImportRequest) (ImportSummary, error) {
	var summary ImportSummary
	srv, err := o.Config.Server(req.Server)
	if err != nil {
		return summary, err
	}
	if req.All == (len(req.Apps) > 0) {
		return summary, errors.New("choose app names or --all")
	}

	serverApps, err := o.ServerApps(ctx, req.Server)
	if err != nil {
		return summary, err
	}
	byName := make(map[string]ServerApp, len(serverApps))
	for _, sa := range serverApps {
		byName[sa.Name] = sa
	}

	names := append([]string(nil), req.Apps...)
	if req.All {
		names = names[:0]
		for name := range byName {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	if len(names) == 0 {
		return summary, nil
	}

	type target struct {
		sa          ServerApp
		writeRecord bool
	}
	seen := map[string]bool{}
	var targets []target
	for _, name := range names {
		if err := config.ValidateAppName(name); err != nil {
			return summary, err
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		sa, ok := byName[name]
		if !ok {
			return summary, fmt.Errorf("app %q is not on server %s", name, req.Server)
		}
		hasRecord := sa.Record != nil
		writeRecord := hasRecord && !req.FilesOnly
		local := srv.Apps[name] != nil

		if local && !req.Overwrite {
			if req.All {
				summary.Skipped = append(summary.Skipped, ImportSkip{App: name, Reason: "already in ship.yaml"})
				continue
			}
			return summary, &ConflictError{Msg: fmt.Sprintf("app %q already exists in ship.yaml", name)}
		}
		if !hasRecord && !req.FilesOnly {
			reason := "no record (older ship)"
			if sa.RecordErr != "" {
				reason = "an unreadable record (newer ship?)"
			}
			if req.All {
				summary.Skipped = append(summary.Skipped, ImportSkip{App: name, Reason: reason})
				continue
			}
			return summary, fmt.Errorf("app %q has %s; only its compose file can be imported", name, reason)
		}
		if writeRecord && len(sa.Compose) == 0 {
			return summary, fmt.Errorf("app %q has no compose.yaml on server %s", name, req.Server)
		}

		if len(sa.Compose) > 0 {
			p := srv.ComposePath(name)
			if err := checkImportFile(p, sa.Compose, req.Overwrite, displayImportPath(srv, p)); err != nil {
				if req.All {
					summary.Skipped = append(summary.Skipped, ImportSkip{App: name, Reason: err.Error()})
					continue
				}
				return summary, err
			}
		}
		if sa.HasDockerfile {
			p := srv.DockerfilePath(name)
			if err := checkImportFile(p, sa.Dockerfile, req.Overwrite, displayImportPath(srv, p)); err != nil {
				if req.All {
					summary.Skipped = append(summary.Skipped, ImportSkip{App: name, Reason: err.Error()})
					continue
				}
				return summary, err
			}
		}
		targets = append(targets, target{sa: sa, writeRecord: writeRecord})
	}

	if len(targets) == 0 {
		return summary, nil
	}

	path := o.Config.Path
	if path == "" {
		if o.Config.Dir == "" {
			return summary, errors.New("cannot import without a loaded ship.yaml path")
		}
		path = filepath.Join(o.Config.Dir, config.ConfigName)
	}
	doc, err := yamldoc.Load(path)
	if err != nil {
		return summary, err
	}
	writeYAML := false
	for _, t := range targets {
		if !t.writeRecord {
			continue
		}
		record, err := t.sa.Record.RecordYAML()
		if err != nil {
			return summary, fmt.Errorf("render %s record: %w", t.sa.Name, err)
		}
		if err := doc.PutApp(req.Server, t.sa.Name, record, true); err != nil {
			return summary, err
		}
		writeYAML = true
	}
	// Validate everything before touching any local file, so a domain clash
	// or invalid record leaves the checkout untouched.
	if writeYAML {
		if err := doc.Validate(); err != nil {
			return summary, err
		}
	}

	for _, t := range targets {
		if len(t.sa.Compose) > 0 {
			p := srv.ComposePath(t.sa.Name)
			written, err := writeImportFile(p, t.sa.Compose, req.Overwrite, displayImportPath(srv, p))
			if err != nil {
				return summary, err
			}
			if written {
				summary.Files = append(summary.Files, p)
			}
		}
		if t.sa.HasDockerfile {
			p := srv.DockerfilePath(t.sa.Name)
			written, err := writeImportFile(p, t.sa.Dockerfile, req.Overwrite, displayImportPath(srv, p))
			if err != nil {
				return summary, err
			}
			if written {
				summary.Files = append(summary.Files, p)
			}
		}
		summary.Imported = append(summary.Imported, t.sa.Name)
	}
	if writeYAML {
		if err := doc.Save(); err != nil {
			return summary, err
		}
	}
	return summary, nil
}

func displayImportPath(srv *config.Server, path string) string {
	if rel, err := filepath.Rel(srv.Dir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

// checkImportFile only reports a conflict when the local bytes differ. The
// same app name on another server shares apps/<name>, so an identical compose
// file or Dockerfile is not something to overwrite.
func checkImportFile(path string, data []byte, overwrite bool, display string) error {
	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("%s is a directory", display)
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Equal(existing, data) || overwrite {
		return nil
	}
	return &ConflictError{Msg: fmt.Sprintf("%s differs from the server's", display)}
}

// writeImportFile returns false when the file already has exactly the server's
// bytes. In that case it writes nothing and keeps no .bak.
func writeImportFile(path string, data []byte, overwrite bool, display string) (bool, error) {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		if fi.IsDir() {
			return false, fmt.Errorf("%s is a directory", display)
		}
		mode = fi.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		old, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		if bytes.Equal(old, data) {
			return false, nil
		}
		if !overwrite {
			return false, &ConflictError{Msg: fmt.Sprintf("%s differs from the server's", display)}
		}
		if err := os.WriteFile(path+".bak", old, mode); err != nil {
			return false, fmt.Errorf("write %s: %w", path+".bak", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
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
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}
