package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/localedit"
	"github.com/harrisonclewis/ship/internal/ops"
	"github.com/harrisonclewis/ship/internal/yamldoc"
)

const tuiTestDoc = `# keep this comment
servers:
  prod:
    # prod is the primary server
    host: ops@203.0.113.10
    apps:
      app:
        domains: [app.example.com]
        upstream: app:8080
        source:
          type: github-release
          repo: example/app
        backup: {}
  staging:
    host: ops@203.0.113.11
    apps:
      web:
        domains: [web.example.com]
        upstream: web:3000
        source:
          type: image
          path: ~/web
        backup: {}
`

const testConfig = tuiTestDoc

func writeTestConfig(t *testing.T) (string, *config.Config, *yamldoc.Doc) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(testConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := yamldoc.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, cfg, doc
}

type fakeBackend struct {
	cfg          *config.Config
	statusFn     func(server string) (ops.StatusReport, error)
	serverApps   []ops.ServerApp
	serverAppsFn func(server string) ([]ops.ServerApp, error)
	backups      []BackupEntry
	runFn        func(context.Context, Request, io.Writer) (Result, error)
	runCalls     []Request
	importFn     func(ops.ImportRequest) (ops.ImportSummary, error)
	importCalls  []ops.ImportRequest

	terminalCmd     *exec.Cmd
	terminalDisplay string
	terminalErr     error
}

func (f *fakeBackend) Config() *config.Config     { return f.cfg }
func (f *fakeBackend) SetConfig(c *config.Config) { f.cfg = c }
func (f *fakeBackend) Servers() []string          { return f.cfg.ServerNames() }
func (f *fakeBackend) Server(name string) (*config.Server, error) {
	return f.cfg.Server(name)
}

func (f *fakeBackend) Status(ctx context.Context, server, app string) (ops.StatusReport, error) {
	if f.statusFn != nil {
		return f.statusFn(server)
	}
	return ops.StatusReport{Server: server, Proxy: "running"}, nil
}

func (f *fakeBackend) ServerApps(ctx context.Context, server string) ([]ops.ServerApp, error) {
	if f.serverAppsFn != nil {
		return f.serverAppsFn(server)
	}
	return f.serverApps, nil
}

func (f *fakeBackend) ListBackups(ctx context.Context, server, app string) ([]BackupEntry, error) {
	return f.backups, nil
}

func (f *fakeBackend) Import(ctx context.Context, req ops.ImportRequest) (ops.ImportSummary, error) {
	f.importCalls = append(f.importCalls, req)
	if f.importFn != nil {
		return f.importFn(req)
	}
	return ops.ImportSummary{Imported: req.Apps}, nil
}

func (f *fakeBackend) Run(ctx context.Context, req Request, out io.Writer) (Result, error) {
	f.runCalls = append(f.runCalls, req)
	if f.runFn != nil {
		return f.runFn(ctx, req, out)
	}
	return Result{}, nil
}

func (f *fakeBackend) TerminalCommand(ctx context.Context, req TerminalRequest) (*exec.Cmd, string, error) {
	return f.terminalCmd, f.terminalDisplay, f.terminalErr
}

func testModel(t *testing.T, doc string) (*model, *fakeBackend) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ship.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	yd, err := yamldoc.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeBackend{cfg: cfg}
	m := newModel(cfg, fake, yd, path, false)
	m.width = 80
	m.height = 24
	m.resizeViewport()
	return m, fake
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func press(t *testing.T, m *model, key string) (*model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(keyMsg(key))
	got, ok := next.(*model)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	return got, cmd
}

func statusFor(server string, apps ...ops.AppStatus) statusMsg {
	return statusMsg{server: server, report: ops.StatusReport{Server: server, Proxy: "running", Apps: apps}}
}

func TestEditShipConfigFromServers(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m, cmd := press(t, m, "e")
	if cmd == nil || m.edit == nil {
		t.Fatal("e did not start a local edit session")
	}
	if m.edit.target.path != m.path || m.edit.target.kind != editShipConfig {
		t.Fatalf("edit target = %#v, want %s", m.edit.target, m.path)
	}
	_ = m.edit.session.Close()
	m.edit = nil
}

func TestEditComposeFromAppActions(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m, _ = press(t, m, "enter")
	m, _ = press(t, m, "enter")
	if m.page != pageAppActions {
		t.Fatalf("page = %v, want app actions", m.page)
	}
	m, cmd := press(t, m, "f")
	if cmd == nil || m.edit == nil {
		t.Fatal("f did not start a local edit session")
	}
	want := filepath.Join(filepath.Dir(m.path), "apps", "app", "compose.yaml")
	if m.edit.target.path != want || m.edit.target.kind != editCompose {
		t.Fatalf("edit target = %#v, want %s", m.edit.target, want)
	}
	_ = m.edit.session.Close()
	m.edit = nil
}

func TestInvalidShipEditOffersReopenOrDiscard(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	session, err := localedit.Begin(m.path)
	if err != nil {
		t.Fatal(err)
	}
	m.edit = &editSession{
		target:  editTarget{path: m.path, label: m.path, validator: localedit.ValidateShipYAML, kind: editShipConfig},
		session: session,
		editor:  localedit.Editor{Name: "true"},
	}
	if err := os.WriteFile(session.TempPath(), []byte("servers: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.finishLocalEdit(editDoneMsg{})
	if m.confirm == nil || m.confirm.yesHint != "reopen" || m.confirm.noHint != "discard" {
		t.Fatalf("confirm = %#v, want a reopen/discard choice", m.confirm)
	}
	before, err := os.ReadFile(m.path)
	if err != nil {
		t.Fatal(err)
	}
	m, _ = press(t, m, "n")
	if m.edit != nil || m.confirm != nil {
		t.Fatalf("discard left edit=%v confirm=%v", m.edit, m.confirm)
	}
	after, err := os.ReadFile(m.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("an invalid edit changed ship.yaml")
	}
}

func TestTabsAndOpenServer(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m.Update(statusFor("prod", ops.AppStatus{App: "app", Current: "v1.2.3", Health: "healthy"}))
	m.Update(statusFor("staging"))
	if m.page != pageServers || m.tab != 0 {
		t.Fatalf("started on page=%v tab=%d", m.page, m.tab)
	}
	if view := m.View(); !strings.Contains(view, "Servers (2)") || !strings.Contains(view, "prod") {
		t.Fatalf("servers view missing content:\n%s", view)
	}

	m, _ = press(t, m, "2")
	if m.page != pageActivity || m.tab != 1 {
		t.Fatalf("2 did not switch to Activity: page=%v tab=%d", m.page, m.tab)
	}
	if view := m.View(); !strings.Contains(view, "Activity (0)") {
		t.Fatalf("activity view missing tab:\n%s", view)
	}

	m, _ = press(t, m, "tab")
	if m.page != pageServers || m.tab != 0 {
		t.Fatalf("tab did not return to Servers: page=%v tab=%d", m.page, m.tab)
	}

	m, _ = press(t, m, "enter")
	if m.page != pageServer || m.serverName != "prod" {
		t.Fatalf("enter did not open prod: page=%v server=%q", m.page, m.serverName)
	}
	m.Update(statusFor("prod", ops.AppStatus{App: "app", Current: "v1.2.3", Health: "healthy"}))
	view := m.View()
	for _, want := range []string{"Server", "Applications (1)", "app", "v1.2.3", "healthy"} {
		if !strings.Contains(view, want) {
			t.Errorf("server view missing %q:\n%s", want, view)
		}
	}

	m, _ = press(t, m, "esc")
	if m.page != pageServers {
		t.Fatalf("esc did not go back to Servers: %v", m.page)
	}
}

func TestConfirmGatesDeployAll(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m, _ = press(t, m, "enter")
	m, _ = press(t, m, "m")
	if m.page != pageServerActions {
		t.Fatalf("server actions not open: %v", m.page)
	}
	m.serverActCur = 0
	m, _ = press(t, m, "enter")
	if m.confirm == nil {
		t.Fatal("deploy all did not ask for confirmation")
	}
	if len(m.activities) != 0 {
		t.Fatalf("an operation started before confirmation: %d", len(m.activities))
	}
	m, _ = press(t, m, "y")
	if m.confirm != nil {
		t.Fatal("confirmation stayed open")
	}
	if len(m.activities) != 1 {
		t.Fatalf("activities = %d, want 1", len(m.activities))
	}
	entry := m.activities[0]
	if entry.req.Action != ActDeployAll || entry.req.Server != "prod" || entry.state != runRunning {
		t.Fatalf("unexpected activity: %+v", entry)
	}
	if m.page != pageOutput {
		t.Fatalf("starting a run did not open its output: %v", m.page)
	}
}

func TestRunStreamsOutputAndFinishes(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	fake.runFn = func(ctx context.Context, req Request, out io.Writer) (Result, error) {
		fmt.Fprintln(out, "line one")
		fmt.Fprintln(out, "line two")
		return Result{}, nil
	}

	cmd := m.startRun(Request{Action: ActDeploy, Server: "prod", App: "app"})
	if cmd == nil {
		t.Fatal("startRun returned no command")
	}
	if len(m.activities) != 1 {
		t.Fatalf("activities = %d", len(m.activities))
	}
	entry := m.activities[0]

	// Live output between ticks.
	entry.out.Write([]byte("early output\n"))
	m.Update(runTickMsg{})
	if view := m.View(); !strings.Contains(view, "early output") {
		t.Fatalf("live output not shown:\n%s", view)
	}
	if entry.state != runRunning {
		t.Fatalf("entry finished too early: %v", entry.state)
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("startRun command did not batch: %T", msg)
	}
	doneMsg := batch[0]()
	done, ok := doneMsg.(runDoneMsg)
	if !ok {
		t.Fatalf("run command returned %T", doneMsg)
	}
	m.Update(done)

	if entry.state != runSucceeded {
		t.Fatalf("state = %v, want succeeded", entry.state)
	}
	if len(fake.runCalls) != 1 || fake.runCalls[0].Action != ActDeploy {
		t.Fatalf("run calls = %+v", fake.runCalls)
	}
	view := m.View()
	for _, want := range []string{"line one", "line two", "succeeded"} {
		if !strings.Contains(view, want) {
			t.Errorf("output view missing %q:\n%s", want, view)
		}
	}
	if m.toast == nil || m.toast.kind != toastSuccess {
		t.Fatalf("no success toast: %+v", m.toast)
	}

	m, _ = press(t, m, "esc")
	m, _ = press(t, m, "esc")
	m, _ = press(t, m, "2")
	if view := m.View(); !strings.Contains(view, "Deploy app") || !strings.Contains(view, "succeeded") {
		t.Fatalf("activity tab missing the run:\n%s", view)
	}
}

func TestSameServerRunsAreRefused(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m.startRun(Request{Action: ActRestart, Server: "prod", App: "app"})
	if len(m.activities) != 1 {
		t.Fatalf("first run did not start: %d", len(m.activities))
	}
	m.startRun(Request{Action: ActRestart, Server: "prod", App: "app"})
	if len(m.activities) != 1 {
		t.Fatalf("second run on the same server started: %d", len(m.activities))
	}
	if m.toast == nil || m.toast.kind != toastWarning {
		t.Fatalf("no warning toast: %+v", m.toast)
	}
	m.startRun(Request{Action: ActRestart, Server: "staging", App: "web"})
	if len(m.activities) != 2 {
		t.Fatalf("run on another server was refused: %d", len(m.activities))
	}
}

func TestMaintenanceBadgeAndToggle(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m.Update(statusFor("prod", ops.AppStatus{
		App:         "app",
		Current:     "v1.2.3",
		Health:      "healthy",
		Maintenance: true,
	}))
	m, _ = press(t, m, "enter")
	m.Update(statusFor("prod", ops.AppStatus{
		App:         "app",
		Current:     "v1.2.3",
		Health:      "healthy",
		Maintenance: true,
	}))
	if view := m.View(); !strings.Contains(view, "MAINT") {
		t.Fatalf("maintenance badge missing:\n%s", view)
	}
	m, _ = press(t, m, "enter") // open app actions
	m, _ = press(t, m, "m")
	if len(m.activities) != 1 {
		t.Fatalf("maintenance toggle did not start: %d", len(m.activities))
	}
	entry := m.activities[0]
	if entry.req.Action != ActMaintenance || entry.req.MaintenanceOn {
		t.Fatalf("expected maintenance off, got %+v", entry.req)
	}
}

func TestViewsFitEightyByTwentyFour(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m.Update(statusFor("prod", ops.AppStatus{App: "app", Current: "v1.2.3", Health: "healthy"}))
	check := func(name string) {
		t.Helper()
		lines := strings.Split(m.View(), "\n")
		if len(lines) > 24 {
			t.Errorf("%s view has %d lines, want <= 24", name, len(lines))
		}
		for i, line := range lines {
			if ansi.StringWidth(line) > 80 {
				t.Errorf("%s view line %d is %d cells wide, want <= 80", name, i, ansi.StringWidth(line))
			}
		}
	}
	check("servers")
	press(t, m, "2")
	check("activity")
	press(t, m, "1")
	press(t, m, "enter")
	check("server")
	press(t, m, "m")
	check("server actions")
	press(t, m, "esc")
	press(t, m, "enter")
	check("app actions")
	press(t, m, "esc")
	press(t, m, "a")
	check("form")
	press(t, m, "esc")
	press(t, m, "l")
	check("confirm")
	press(t, m, "n")
}

func TestAppFormLoadsSourceAndDeployValues(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m.serverName = "staging"
	m.appName = "web"
	v := m.newAppFormValues("staging", "web", false)
	if v.SourceType != "image" || v.SourcePath != "~/web" {
		t.Fatalf("image source not loaded: %+v", v)
	}
	if !v.DeployMaintenance {
		t.Fatal("maintenance should default to on")
	}
	if form := m.buildForm(&formHost{purpose: formEditApp, app: v}); form == nil {
		t.Fatal("no app form")
	}
}

func decodedRecord(t *testing.T, doc string) *config.App {
	t.Helper()
	record, err := config.DecodeAppRecord(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func setServerApps(m *model, fake *fakeBackend, server string, apps []ops.ServerApp) {
	m.serverName = server
	m.page = pageServer
	m.serverApps[server] = apps
	fake.serverApps = apps
	m.recomputeDiffs(server)
}

func TestServerRowsShowDiffBadgeAndImportAction(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	srv := m.cfg.Servers["prod"]
	dir := filepath.Join(srv.Dir, "apps", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("name: app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	apps := []ops.ServerApp{
		{
			Name: "app",
			Record: decodedRecord(t, `domains: [app.example.com]
upstream: app:8080
source: {type: github-release, repo: example/app}
env:
  REMOTE: one
`),
			Compose: []byte("name: app\n"),
		},
		{
			Name: "zzz",
			Record: decodedRecord(t, `domains: [zzz.example.com]
upstream: zzz:8080
source: {type: github-release, repo: example/zzz}
`),
			Compose: []byte("name: zzz\n"),
		},
	}
	setServerApps(m, fake, "prod", apps)
	view := m.View()
	for _, want := range []string{"DIFF", "zzz", "on server"} {
		if !strings.Contains(view, want) {
			t.Errorf("server view missing %q:\n%s", want, view)
		}
	}

	for i, e := range m.appEntries() {
		if e.Name == "zzz" {
			m.appCursor = i
		}
	}
	m, _ = press(t, m, "enter")
	if m.page != pageAppActions || m.appName != "zzz" {
		t.Fatalf("page=%v app=%q", m.page, m.appName)
	}
	if view := m.View(); !strings.Contains(view, "Import app") {
		t.Fatalf("server-only app has no import action:\n%s", view)
	}
	_, cmd := press(t, m, "i")
	if cmd == nil {
		t.Fatal("import action returned no command")
	}
	done := cmd()
	m.Update(done)
	if len(fake.importCalls) != 1 || len(fake.importCalls[0].Apps) != 1 || fake.importCalls[0].Apps[0] != "zzz" {
		t.Fatalf("import calls = %+v", fake.importCalls)
	}
	if m.page != pageServer {
		t.Fatalf("after import page = %v", m.page)
	}
}

func TestNoRecordAppOffersComposeImportOnly(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	setServerApps(m, fake, "prod", []ops.ServerApp{{Name: "old", Compose: []byte("name: old\n")}})
	if view := m.View(); !strings.Contains(view, "no record") || !strings.Contains(view, "older ship") {
		t.Fatalf("no-record marker missing:\n%s", view)
	}
	for i, e := range m.appEntries() {
		if e.Name == "old" {
			m.appCursor = i
		}
	}
	m, _ = press(t, m, "enter")
	if actions := m.appActions(); len(actions) != 1 || actions[0].label != "Import compose file" {
		t.Fatalf("actions = %+v", actions)
	}
	_, cmd := press(t, m, "i")
	if cmd == nil {
		t.Fatal("compose import returned no command")
	}
	done := cmd()
	m.Update(done)
	if len(fake.importCalls) != 1 || !fake.importCalls[0].FilesOnly {
		t.Fatalf("import calls = %+v", fake.importCalls)
	}
}

func TestAddServerOffersMultiSelectImport(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	values := &serverFormValues{
		Name:       "new",
		Host:       "ops@203.0.113.12",
		AdminUser:  "ops",
		Arch:       "linux/amd64",
		CaddyImage: "caddy:2.10-alpine",
		IsNew:      true,
	}
	m.form = &formHost{purpose: formAddServer, server: values}
	if cmd := m.submitForm(); cmd == nil {
		t.Fatal("add server returned no follow-up command")
	}
	if m.form != nil {
		t.Fatal("form stayed open after saving")
	}

	apps := []ops.ServerApp{
		{Name: "api", Record: decodedRecord(t, "domains: [api.example.com]\nupstream: api:8080\nsource: {type: github-release, repo: example/api}\n")},
		{Name: "worker", Record: decodedRecord(t, "domains: [worker.example.com]\nupstream: worker:8080\nsource: {type: github-release, repo: example/worker}\n")},
	}
	m.Update(serverAppsMsg{server: "new", apps: apps, forImport: true})
	if m.form == nil || m.form.purpose != formImportApps {
		t.Fatalf("import dialog did not open: %+v", m.form)
	}
	if got := strings.Join(m.form.importSelected, ","); got != "api,worker" {
		t.Fatalf("preselected = %q", got)
	}
	cmd := m.submitForm()
	if cmd == nil {
		t.Fatal("import dialog returned no command")
	}
	done := cmd()
	m.Update(done)
	if len(fake.importCalls) != 1 {
		t.Fatalf("import calls = %+v", fake.importCalls)
	}
	if got := strings.Join(fake.importCalls[0].Apps, ","); got != "api,worker" {
		t.Fatalf("imported apps = %q", got)
	}
	if m.page != pageServer || m.serverName != "new" {
		t.Fatalf("page=%v server=%q", m.page, m.serverName)
	}
}

func TestCompareCanUseServerVersion(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	srv := m.cfg.Servers["prod"]
	dir := filepath.Join(srv.Dir, "apps", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("name: app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	apps := []ops.ServerApp{{
		Name: "app",
		Record: decodedRecord(t, `domains: [app.example.com]
upstream: app:8080
source: {type: github-release, repo: example/app}
env:
  REMOTE: one
`),
		Compose: []byte("name: app\n"),
	}}
	setServerApps(m, fake, "prod", apps)
	m.appName = "app"
	m.openCompare()
	if m.page != pageCompare {
		t.Fatalf("page = %v", m.page)
	}
	if !strings.Contains(m.View(), "Compare") || !strings.Contains(m.compare.Diff, "@@") {
		t.Fatalf("compare view missing:\n%s\ndiff:\n%s", m.View(), m.compare.Diff)
	}

	m, _ = press(t, m, "u")
	if m.confirm == nil {
		t.Fatal("use server version did not ask for confirmation")
	}
	_, cmd := press(t, m, "y")
	if cmd == nil {
		t.Fatal("use server version returned no command")
	}
	done := cmd()
	m.Update(done)
	if len(fake.importCalls) != 1 || !fake.importCalls[0].Overwrite {
		t.Fatalf("import calls = %+v", fake.importCalls)
	}
	if m.page != pageServer {
		t.Fatalf("page after use server = %v", m.page)
	}

	// Keep mine just closes the comparison.
	m.page = pageCompare
	m, _ = press(t, m, "k")
	if m.page != pageServer || m.toast == nil {
		t.Fatalf("keep mine: page=%v toast=%+v", m.page, m.toast)
	}
}

func TestImportConfirmsDifferingFiles(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	srv := m.cfg.Servers["prod"]
	dir := filepath.Join(srv.Dir, "apps", "blog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("name: local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setServerApps(m, fake, "prod", []ops.ServerApp{{
		Name: "blog",
		Record: decodedRecord(t, `domains: [blog.example.com]
upstream: blog:8080
source: {type: github-release, repo: example/blog}
`),
		Compose: []byte("name: server\n"),
	}})
	for i, e := range m.appEntries() {
		if e.Name == "blog" {
			m.appCursor = i
		}
	}
	m, _ = press(t, m, "enter")
	_, cmd := press(t, m, "i")
	if cmd != nil {
		t.Fatal("differing file import started without confirmation")
	}
	if m.confirm == nil {
		t.Fatal("no confirmation dialog opened")
	}
	for _, want := range []string{"apps/blog/compose.yaml differs from the server's", ".bak"} {
		if !strings.Contains(m.confirm.body, want) {
			t.Errorf("confirmation missing %q: %q", want, m.confirm.body)
		}
	}
	m, _ = press(t, m, "n")
	if m.confirm != nil || len(fake.importCalls) != 0 {
		t.Fatalf("cancel still imported: confirm=%v calls=%+v", m.confirm, fake.importCalls)
	}

	m, _ = press(t, m, "i")
	if m.confirm == nil {
		t.Fatal("confirmation did not reopen")
	}
	_, cmd = press(t, m, "y")
	if cmd == nil {
		t.Fatal("confirming returned no import command")
	}
	done := cmd()
	m.Update(done)
	if len(fake.importCalls) != 1 || !fake.importCalls[0].Overwrite {
		t.Fatalf("import calls = %+v", fake.importCalls)
	}
}

func TestImportIdenticalFileNeedsNoConfirmation(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	srv := m.cfg.Servers["prod"]
	dir := filepath.Join(srv.Dir, "apps", "blog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("name: same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setServerApps(m, fake, "prod", []ops.ServerApp{{
		Name: "blog",
		Record: decodedRecord(t, `domains: [blog.example.com]
upstream: blog:8080
source: {type: github-release, repo: example/blog}
`),
		Compose: []byte("name: same\n"),
	}})
	for i, e := range m.appEntries() {
		if e.Name == "blog" {
			m.appCursor = i
		}
	}
	m, _ = press(t, m, "enter")
	_, cmd := press(t, m, "i")
	if m.confirm != nil {
		t.Fatal("identical file opened an overwrite confirmation")
	}
	if cmd == nil {
		t.Fatal("identical file import returned no command")
	}
	done := cmd()
	m.Update(done)
	if len(fake.importCalls) != 1 || fake.importCalls[0].Overwrite {
		t.Fatalf("import calls = %+v", fake.importCalls)
	}
}
