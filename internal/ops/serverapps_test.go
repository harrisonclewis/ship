package ops

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/remote"
)

type tarMember struct {
	name string
	typ  byte
	data []byte
}

func buildExport(t *testing.T, members ...tarMember) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, m := range members {
		typ := m.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		if err := tw.WriteHeader(&tar.Header{Name: m.name, Typeflag: typ, Size: int64(len(m.data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(m.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func appRecordYAML(t *testing.T, env string) []byte {
	t.Helper()
	doc := `domains: [app.example.com]
upstream: app:8080
source:
  type: image
`
	if env != "" {
		doc += "env:\n  " + env + ": value\n"
	}
	record, err := config.DecodeAppRecord(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	out, err := record.RecordYAML()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestParseServerApps(t *testing.T) {
	blob := buildExport(t,
		tarMember{name: "app/ship-app.yaml", data: appRecordYAML(t, "A")},
		tarMember{name: "app/compose.yaml", data: []byte("name: app\n")},
		tarMember{name: "app/Dockerfile", data: []byte("FROM scratch\n")},
	)
	apps, err := parseServerApps(blob)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 {
		t.Fatalf("apps = %d", len(apps))
	}
	got := apps[0]
	if got.Name != "app" || got.Record == nil || !got.HasDockerfile || string(got.Compose) != "name: app\n" {
		t.Fatalf("app = %+v", got)
	}
	if got.Record.Source.Path != "" {
		t.Fatal("server record carries source.path")
	}
}

func TestParseServerAppsRejectsUnsafeOrUnexpectedMembers(t *testing.T) {
	cases := []struct {
		name    string
		members []tarMember
	}{
		{"dotenv", []tarMember{{name: "app/.env", data: []byte("SECRET=1\n")}}},
		{"ship-env", []tarMember{{name: "app/.ship-env", data: []byte("A=B\n")}}},
		{"ship.state", []tarMember{{name: "app/.ship-state", data: []byte("current=v1\n")}}},
		{"traversal", []tarMember{{name: "../app/compose.yaml", data: []byte("x")}}},
		{"nested", []tarMember{{name: "app/sub/compose.yaml", data: []byte("x")}}},
		{"absolute", []tarMember{{name: "/app/compose.yaml", data: []byte("x")}}},
		{"backslash", []tarMember{{name: `app\compose.yaml`, data: []byte("x")}}},
		{"bad app name", []tarMember{{name: "Bad/compose.yaml", data: []byte("x")}}},
		{"reserved app name", []tarMember{{name: "all/compose.yaml", data: []byte("x")}}},
		{"unknown file", []tarMember{{name: "app/notes.txt", data: []byte("x")}}},
		{"directory", []tarMember{{name: "app", typ: tar.TypeDir}}},
		{"symlink", []tarMember{{name: "app/compose.yaml", typ: tar.TypeSymlink}}},
		{"duplicate", []tarMember{{name: "app/compose.yaml", data: []byte("x")}, {name: "app/compose.yaml", data: []byte("y")}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseServerApps(buildExport(t, tc.members...)); err == nil {
				t.Fatalf("accepted %s", tc.name)
			}
		})
	}
}

func TestParseServerAppsRejectsOversizeFiles(t *testing.T) {
	big := bytes.Repeat([]byte("x"), exportFileLimit+1)
	blob := buildExport(t, tarMember{name: "app/compose.yaml", data: big})
	if _, err := parseServerApps(blob); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseServerAppsRejectsOversizeTotal(t *testing.T) {
	chunk := bytes.Repeat([]byte("x"), exportFileLimit)
	var members []tarMember
	for i := 0; i < exportTotalLimit/exportFileLimit+1; i++ {
		members = append(members, tarMember{name: fmt.Sprintf("app%02d/compose.yaml", i), data: chunk})
	}
	if _, err := parseServerApps(buildExport(t, members...)); err == nil {
		t.Fatal("oversize total export was accepted")
	}
}

func TestAppFilesTarAllowsShipAppRecord(t *testing.T) {
	blob, err := appFilesTar([]appFile{
		{Name: "compose.yaml", Data: []byte("name: app\n")},
		{Name: "ship-app.yaml", Data: []byte("domains: [app.example.com]\n")},
		{Name: ".ship-meta", Data: []byte("app=app\n")},
		{Name: ".ship-env", Data: []byte("A=B\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(blob))
	found := false
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "ship-app.yaml" {
			found = true
		}
	}
	if !found {
		t.Fatal("ship-app.yaml was not in the upload tar")
	}
}

type serverExportRunner struct {
	export []byte
}

func (r *serverExportRunner) Run(c remote.Cmd) error {
	if strings.Contains(strings.Join(c.Args, " "), "app-export") && c.Stdout != nil {
		_, err := c.Stdout.Write(r.export)
		return err
	}
	if c.Stdin != nil {
		_, _ = io.Copy(io.Discard, c.Stdin)
	}
	return nil
}

func (r *serverExportRunner) Output(c remote.Cmd) ([]byte, error) {
	// Agent probe: claim the current protocol version and the matching hash for
	// an empty agent so EnsureAgent does not need to upload anything.
	sum := sha256.Sum256(nil)
	return []byte("version=" + strconv.Itoa(remote.AgentProtocol) + "\nsha256=" + hex.EncodeToString(sum[:]) + "\n"), nil
}

func TestUnifiedDiff(t *testing.T) {
	if got := UnifiedDiff("a", "b", []byte("same\n"), []byte("same\n")); got != "" {
		t.Fatalf("identical files produced a diff: %q", got)
	}
	got := UnifiedDiff("local/x", "server/x", []byte("one\ntwo\nthree\n"), []byte("one\nTWO\nthree\n"))
	for _, want := range []string{"--- local/x", "+++ server/x", "@@", "-two", "+TWO"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff missing %q:\n%s", want, got)
		}
	}
}

func TestCompareServerApps(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "apps", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	compose := []byte("name: app\n")
	if err := os.WriteFile(filepath.Join(dir, "apps", "app", "compose.yaml"), compose, 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      app:
        domains: [app.example.com]
        upstream: app:8080
        source:
          type: image
          path: ` + filepath.Join(dir, "src") + `
        backup: {}
      localonly:
        domains: [local.example.com]
        upstream: local:8080
        source: {type: github-release, repo: example/local}
        backup: {}
`
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := cfg.Servers["prod"]

	same := ServerApp{
		Name:    "app",
		Record:  srv.Apps["app"],
		Compose: compose,
	}
	d, err := CompareApp(srv, same)
	if err != nil {
		t.Fatal(err)
	}
	if d.Changed() || !d.Local || !d.Server || !d.HasRecord {
		t.Fatalf("identical app = %+v", d)
	}

	changedRecord, err := srv.Apps["app"].RecordYAML()
	if err != nil {
		t.Fatal(err)
	}
	changedRecord = bytes.Replace(changedRecord, []byte("app.example.com"), []byte("new.example.com"), 1)
	record, err := config.DecodeAppRecord(bytes.NewReader(changedRecord))
	if err != nil {
		t.Fatal(err)
	}
	changed := ServerApp{Name: "app", Record: record, Compose: []byte("name: changed\n")}
	d, err = CompareApp(srv, changed)
	if err != nil {
		t.Fatal(err)
	}
	if !d.RecordChanged || !d.ComposeChanged || !strings.Contains(d.Diff, "@@") {
		t.Fatalf("changed app = %+v\ndiff:\n%s", d, d.Diff)
	}

	all, err := CompareServerApps(srv, []ServerApp{
		same,
		{Name: "serveronly", Compose: []byte("x")},
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]AppDiff{}
	for _, d := range all {
		byName[d.App] = d
	}
	if !byName["localonly"].Local || byName["localonly"].Server {
		t.Fatalf("localonly = %+v", byName["localonly"])
	}
	if byName["serveronly"].Local || !byName["serveronly"].Server {
		t.Fatalf("serveronly = %+v", byName["serveronly"])
	}
}

func TestImportRefusesAndOverwritesWithBackups(t *testing.T) {
	dir := t.TempDir()
	doc := `servers:
  prod:
    host: ops@203.0.113.10
    apps:
      web:
        domains: [web.example.com]
        upstream: web:3000
        source:
          type: image
          path: ` + filepath.Join(dir, "src") + `
        backup: {}
`
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}

	record := appRecordYAML(t, "A")
	export := buildExport(t,
		tarMember{name: "web/ship-app.yaml", data: record},
		tarMember{name: "web/compose.yaml", data: []byte("name: web\n")},
		tarMember{name: "web/Dockerfile", data: []byte("FROM scratch\n")},
	)
	runner := &serverExportRunner{export: export}
	op := New(cfg, Options{Agent: testAgent,
		Runner: runner,
		Out:    io.Discard,
		ErrOut: io.Discard,
	})

	// The app is local, so a plain import must refuse to overwrite it.
	if _, err := op.Import(context.Background(), ImportRequest{Server: "prod", Apps: []string{"web"}}); err == nil {
		t.Fatal("import overwrote a local app without --overwrite")
	}

	// With overwrite it replaces the block, keeps source.path and backs up the
	// files it replaces.
	if err := os.MkdirAll(filepath.Join(dir, "apps", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldCompose := []byte("name: old\n")
	if err := os.WriteFile(filepath.Join(dir, "apps", "web", "compose.yaml"), oldCompose, 0o644); err != nil {
		t.Fatal(err)
	}
	summary, err := op.Import(context.Background(), ImportRequest{Server: "prod", Apps: []string{"web"}, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Imported) != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	loaded, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	app := loaded.Servers["prod"].Apps["web"]
	if app == nil {
		t.Fatal("imported app missing")
	}
	if app.Source.Path != filepath.Join(dir, "src") {
		t.Fatalf("source.path = %q, want the local path", app.Source.Path)
	}
	gotCompose, err := os.ReadFile(filepath.Join(dir, "apps", "web", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCompose) != "name: web\n" {
		t.Fatalf("compose = %q", gotCompose)
	}
	bak, err := os.ReadFile(filepath.Join(dir, "apps", "web", "compose.yaml.bak"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bak, oldCompose) {
		t.Fatal("compose .bak does not hold the old file")
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("ship.yaml .bak: %v", err)
	}

	// A second plain import is still refused.
	if _, err := op.Import(context.Background(), ImportRequest{Server: "prod", Apps: []string{"web"}}); err == nil {
		t.Fatal("second import overwrote without --overwrite")
	}
}

type oldAgentRunner struct {
	serverExportRunner
}

func (r *oldAgentRunner) Run(c remote.Cmd) error {
	if strings.Contains(strings.Join(c.Args, " "), "app-export") {
		if c.Stdout != nil {
			_, _ = c.Stdout.Write([]byte("# ship-agent: server-side operations for the `ship` CLI.\n\nusage: ship-agent <action>\n"))
		}
		return errors.New("exit status 2")
	}
	return r.serverExportRunner.Run(c)
}

func TestServerAppsExplainsOlderAgent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(`servers:
  prod:
    host: ops@203.0.113.10
    apps: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	op := New(cfg, Options{Agent: testAgent, Runner: &oldAgentRunner{}, Out: io.Discard, ErrOut: io.Discard})
	_, err = op.ServerApps(context.Background(), "prod")
	if err == nil {
		t.Fatal("older agent did not fail")
	}
	for _, want := range []string{"ship-agent on prod is older", "without --dry-run"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestServerAppsReadsUnderDryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(`servers:
  prod:
    host: ops@203.0.113.10
    apps: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	export := buildExport(t, tarMember{name: "app/compose.yaml", data: []byte("name: app\n")})
	op := New(cfg, Options{Agent: testAgent, DryRun: true, Runner: &serverExportRunner{export: export}, Out: io.Discard, ErrOut: io.Discard})
	apps, err := op.ServerApps(context.Background(), "prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].Name != "app" {
		t.Fatalf("apps = %+v", apps)
	}
}

func TestParseServerAppsKeepsGoingPastAnUnreadableRecord(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range map[string]string{
		"new/ship-app.yaml": "future_field: true\n",
		"new/compose.yaml":  "name: new\n",
		"old/compose.yaml":  "name: old\n",
	} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	apps, err := parseServerApps(buf.Bytes())
	if err != nil {
		t.Fatalf("an unreadable record failed the whole export: %v", err)
	}
	if len(apps) != 2 || apps[0].Name != "new" || apps[0].RecordErr == "" || apps[0].Record != nil || apps[1].Name != "old" {
		t.Fatalf("apps = %+v", apps)
	}
}

func TestParseServerAppsMarksAnInvalidRecord(t *testing.T) {
	apps, err := parseServerApps(buildExport(t, tarMember{name: "app/ship-app.yaml", data: []byte("domains: []\n")}))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].Record != nil || apps[0].RecordErr == "" {
		t.Fatalf("invalid record not marked: %+v", apps)
	}
}

func TestImportSkipsIdenticalFilesAndOnlyConflictsOnDifferentOnes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(`servers:
  prod:
    host: ops@203.0.113.10
    apps: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadOld(path)
	if err != nil {
		t.Fatal(err)
	}
	sameCompose := []byte("name: same\n")
	sameDockerfile := []byte("FROM scratch\n")
	for _, app := range []string{"same", "diff"} {
		if err := os.MkdirAll(filepath.Join(dir, "apps", app), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "apps", "same", "compose.yaml"), sameCompose, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apps", "same", "Dockerfile"), sameDockerfile, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apps", "diff", "compose.yaml"), []byte("name: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	export := buildExport(t,
		tarMember{name: "same/ship-app.yaml", data: appRecordYAML(t, "")},
		tarMember{name: "same/compose.yaml", data: sameCompose},
		tarMember{name: "same/Dockerfile", data: sameDockerfile},
		tarMember{name: "diff/ship-app.yaml", data: appRecordYAML(t, "")},
		tarMember{name: "diff/compose.yaml", data: []byte("name: new\n")},
	)
	op := New(cfg, Options{Agent: testAgent, Runner: &serverExportRunner{export: export}, Out: io.Discard, ErrOut: io.Discard})

	summary, err := op.Import(context.Background(), ImportRequest{Server: "prod", Apps: []string{"same"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Imported) != 1 || len(summary.Files) != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	for _, p := range []string{
		filepath.Join(dir, "apps", "same", "compose.yaml.bak"),
		filepath.Join(dir, "apps", "same", "Dockerfile.bak"),
	} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("identical file kept a .bak: %s", p)
		}
	}

	// Even use-server-version (overwrite) must not back up identical bytes.
	summary, err = op.Import(context.Background(), ImportRequest{Server: "prod", Apps: []string{"same"}, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Files) != 0 {
		t.Fatalf("identical files were rewritten: %+v", summary.Files)
	}
	if _, err := os.Stat(filepath.Join(dir, "apps", "same", "compose.yaml.bak")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("overwrite wrote a .bak for an identical file")
	}

	// A differing file is a ConflictError whose base message has no CLI flag.
	_, err = op.Import(context.Background(), ImportRequest{Server: "prod", Apps: []string{"diff"}})
	if err == nil {
		t.Fatal("differing compose file was imported without overwrite")
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error is %T, want *ConflictError: %v", err, err)
	}
	for _, want := range []string{"apps/diff/compose.yaml differs from the server's"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "--overwrite") {
		t.Errorf("ops error leaks CLI wording: %v", err)
	}
}
