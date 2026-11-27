package tui

import (
	"context"
	"errors"
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
backup_dir_local: ~/backups
servers:
  prod:
    # prod is the primary server
    host: ops@203.0.113.10
  staging:
    host: ops@203.0.113.11
    sources:
      web: ~/web
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

type putCall struct {
	server, app         string
	record              *config.App
	compose, dockerfile []byte
	arch, expectedHash  string
}

type serverPutCall struct {
	server, admin, caddy, expectedHash string
}

type removeCall struct {
	server, app, expectedHash string
}

type approvalCall struct {
	server, app, script string
}

type fakeBackend struct {
	cfg           *config.Config
	statusFn      func(server string) (ops.StatusReport, error)
	backups       []BackupEntry
	runFn         func(context.Context, Request, io.Writer) (Result, error)
	runCalls      []Request
	putFn         func(putCall) (string, error)
	putCalls      []putCall
	serverPutFn   func(serverPutCall) (string, error)
	serverPuts    []serverPutCall
	removeFn      func(removeCall) error
	removeCalls   []removeCall
	approvalFn    func(approvalCall) error
	approvalCalls []approvalCall

	commitsFn       func(server, app string) ([]ops.GitCommit, error)
	releasesFn      func(server, app string) ([]ops.GitRelease, error)
	releaseAssetsFn func(server, app, tag string) ([]ops.GitAsset, error)

	variance      map[string]ops.VarianceReport
	varianceFn    func(server string) (ops.VarianceReport, error)
	varianceCalls int

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

func (f *fakeBackend) Variances(ctx context.Context, server string) (ops.VarianceReport, error) {
	f.varianceCalls++
	if f.varianceFn != nil {
		return f.varianceFn(server)
	}
	if f.variance != nil {
		if r, ok := f.variance[server]; ok {
			return r, nil
		}
	}
	return ops.VarianceReport{Server: server}, nil
}

func (f *fakeBackend) ListBackups(ctx context.Context, server, app string) ([]BackupEntry, error) {
	return f.backups, nil
}

func (f *fakeBackend) Run(ctx context.Context, req Request, out io.Writer) (Result, error) {
	f.runCalls = append(f.runCalls, req)
	if f.runFn != nil {
		return f.runFn(ctx, req, out)
	}
	return Result{}, nil
}

func (f *fakeBackend) PutDesiredApp(ctx context.Context, server, app string, record *config.App, compose, dockerfile []byte, arch, expectedHash string) (string, error) {
	call := putCall{server: server, app: app, record: record, compose: compose, dockerfile: dockerfile, arch: arch, expectedHash: expectedHash}
	f.putCalls = append(f.putCalls, call)
	if f.putFn != nil {
		return f.putFn(call)
	}
	return "newhash", nil
}

func (f *fakeBackend) PutServerSettings(ctx context.Context, server, adminUser, caddyImage, expectedHash string) (string, error) {
	call := serverPutCall{server: server, admin: adminUser, caddy: caddyImage, expectedHash: expectedHash}
	f.serverPuts = append(f.serverPuts, call)
	if f.serverPutFn != nil {
		return f.serverPutFn(call)
	}
	return "newhash", nil
}

func (f *fakeBackend) RemoveDesiredApp(ctx context.Context, server, app, expectedHash string) error {
	call := removeCall{server: server, app: app, expectedHash: expectedHash}
	f.removeCalls = append(f.removeCalls, call)
	if f.removeFn != nil {
		return f.removeFn(call)
	}
	return nil
}

func (f *fakeBackend) RecordBuildApproval(ctx context.Context, server, app, script string) error {
	call := approvalCall{server: server, app: app, script: script}
	f.approvalCalls = append(f.approvalCalls, call)
	if f.approvalFn != nil {
		return f.approvalFn(call)
	}
	return nil
}

func (f *fakeBackend) GitCommits(ctx context.Context, server, app string) ([]ops.GitCommit, error) {
	if f.commitsFn != nil {
		return f.commitsFn(server, app)
	}
	return nil, nil
}

func (f *fakeBackend) GitReleases(ctx context.Context, server, app string) ([]ops.GitRelease, error) {
	if f.releasesFn != nil {
		return f.releasesFn(server, app)
	}
	return nil, nil
}

func (f *fakeBackend) GitReleaseAssets(ctx context.Context, server, app, tag string) ([]ops.GitAsset, error) {
	if f.releaseAssetsFn != nil {
		return f.releaseAssetsFn(server, app, tag)
	}
	return nil, nil
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

func decodedRecord(t *testing.T, doc string) *config.App {
	t.Helper()
	record, err := config.DecodeAppRecord(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

const appRecord = `domains: [app.example.com]
upstream: app:8080
source: {type: github-release, repo: example/app}
`

func testState(server string, records map[string]*config.App) *ops.ServerConfig {
	state := &ops.ServerConfig{
		Server:  server,
		Apps:    map[string]*ops.DesiredApp{},
		Running: map[string]*ops.RunningApp{},
		Settings: ops.ServerSettings{
			AdminUser:  "ops",
			CaddyImage: config.DefaultCaddyImage,
			Present:    true,
			Hash:       "srvhash",
		},
		Arch: "amd64",
	}
	for name, rec := range records {
		state.Apps[name] = &ops.DesiredApp{
			Name:    name,
			Record:  rec,
			Compose: []byte("name: " + name + "\n"),
			Hash:    "hash-" + name,
		}
	}
	return state
}

func setVariances(m *model, fake *fakeBackend, server string, report ops.VarianceReport) {
	if report.Server == "" {
		report.Server = server
	}
	if fake.variance == nil {
		fake.variance = map[string]ops.VarianceReport{}
	}
	fake.variance[server] = report
	m.variances[server] = report
}

func statusFor(server string, apps ...ops.AppStatus) statusMsg {
	return statusMsg{server: server, report: ops.StatusReport{Server: server, Proxy: "running", Apps: apps}}
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

// runCmd runs a command and feeds any messages it produces back into the model.
func runCmd(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	var feed func(tea.Cmd)
	feed = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, inner := range msg {
				feed(inner)
			}
		default:
			next, nextCmd := m.Update(msg)
			if mm, ok := next.(*model); ok {
				*m = *mm
			}
			feed(nextCmd)
		}
	}
	feed(cmd)
}

func appWithState(t *testing.T, m *model, fake *fakeBackend) *ops.ServerConfig {
	t.Helper()
	state := testState("prod", map[string]*config.App{"app": decodedRecord(t, appRecord)})
	report := ops.VarianceReport{
		Server: "prod",
		State:  state,
		Apps:   []ops.AppVariance{{App: "app", State: ops.VarianceApplied}},
	}
	setVariances(m, fake, "prod", report)
	m.serverName = "prod"
	m.page = pageServer
	return state
}

func TestTabsAndOpenServer(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	m.page = pageServers
	m.tab = 0
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
	for _, want := range []string{"Server", "Applications (1)", "app", "v1.2.3", "healthy", "applied"} {
		if !strings.Contains(view, want) {
			t.Errorf("server view missing %q:\n%s", want, view)
		}
	}

	m, _ = press(t, m, "esc")
	if m.page != pageServers {
		t.Fatalf("esc did not go back to Servers: %v", m.page)
	}
}

func TestConfigBadgesOnAppRows(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	state := testState("prod", map[string]*config.App{
		"api": decodedRecord(t, "domains: [api.example.com]\nupstream: api:8080\nsource: {type: github-release, repo: example/api}\n"),
		"web": decodedRecord(t, "domains: [web.example.com]\nupstream: web:8080\nsource: {type: github-release, repo: example/web}\n"),
		"new": decodedRecord(t, "domains: [new.example.com]\nupstream: new:8080\nsource: {type: github-release, repo: example/new}\n"),
	})
	report := ops.VarianceReport{
		Server: "prod",
		State:  state,
		Apps: []ops.AppVariance{
			{App: "api", State: ops.VarianceApplied},
			{App: "new", State: ops.VarianceNotDeployed},
			{App: "web", State: ops.VariancePending},
		},
	}
	setVariances(m, fake, "prod", report)
	m.serverName = "prod"
	m.page = pageServer
	m.Update(statusFor("prod",
		ops.AppStatus{App: "api", Current: "v1", Health: "healthy"},
		ops.AppStatus{App: "web", Current: "v2", Health: "unhealthy"},
	))
	view := m.View()
	for _, want := range []string{"CONFIG", "applied", "pending deploy", "not deployed", "healthy"} {
		if !strings.Contains(view, want) {
			t.Errorf("app row missing %q:\n%s", want, view)
		}
	}
}

func TestServerAlertListsOutOfSyncItems(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	state := testState("prod", map[string]*config.App{"web": decodedRecord(t, "domains: [web.example.com]\nupstream: web:8080\nsource: {type: github-release, repo: example/web}\n")})
	report := ops.VarianceReport{
		Server:         "prod",
		State:          state,
		Apps:           []ops.AppVariance{{App: "web", State: ops.VariancePending}},
		ProxyOutOfSync: true,
		ProxyReason:    "the Caddyfile that would be rendered differs from the active one",
		SetupPending:   true,
		SetupReason:    "admin_user changed since the last setup",
	}
	setVariances(m, fake, "prod", report)
	m.serverName = "prod"
	m.page = pageServer
	view := m.View()
	for _, want := range []string{
		"web: config changed, pending deploy",
		"Caddy: domains changed, restart Caddy to apply (p)",
		"Server: admin_user changed, run setup (s)",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("alert missing %q:\n%s", want, view)
		}
	}

	report.ProxyReason = "the desired caddy_image differs from the running image"
	report.SetupReason = "app uids changed since the last setup"
	setVariances(m, fake, "prod", report)
	view = m.View()
	for _, want := range []string{
		"Caddy: caddy_image changed, restart Caddy to apply (p)",
		"Server: app uids changed, run setup (s)",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("alert missing %q:\n%s", want, view)
		}
	}

	report.Apps = []ops.AppVariance{{App: "web", State: ops.VarianceApplied}}
	report.ProxyOutOfSync = false
	report.SetupPending = false
	setVariances(m, fake, "prod", report)
	view = m.View()
	for _, unwanted := range []string{"pending deploy", "restart Caddy to apply", "run setup"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("in-sync server still shows %q:\n%s", unwanted, view)
		}
	}
}

func TestEditAppWritesServerRecordWithLoadedHash(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	m.appName = "app"
	m.openAppForm("app", false)
	if m.form == nil || m.form.purpose != formEditApp {
		t.Fatalf("form = %#v", m.form)
	}
	m.form.app.Upstream = "app:9090"
	cmd := m.submitForm()
	if cmd == nil {
		t.Fatal("submit returned no command")
	}
	runCmd(t, m, cmd)
	if len(fake.putCalls) != 1 {
		t.Fatalf("put calls = %+v", fake.putCalls)
	}
	call := fake.putCalls[0]
	if call.server != "prod" || call.app != "app" || call.expectedHash != "hash-app" {
		t.Fatalf("put call = %+v", call)
	}
	if call.record.Upstream != "app:9090" {
		t.Fatalf("record upstream = %q", call.record.Upstream)
	}
	if string(call.compose) != "name: app\n" {
		t.Fatalf("compose changed unexpectedly: %q", call.compose)
	}
	if m.form != nil || m.page != pageServer {
		t.Fatalf("after save form=%v page=%v", m.form, m.page)
	}
	if fake.varianceCalls == 0 {
		t.Fatal("variances were not refreshed after saving")
	}
}

func TestRenameIsRejected(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	m.appName = "app"
	m.openAppForm("app", false)
	m.form.app.Name = "renamed"
	runCmd(t, m, m.submitForm())
	if len(fake.putCalls) != 0 {
		t.Fatalf("rename wrote to the server: %+v", fake.putCalls)
	}
	if m.form == nil || !strings.Contains(m.form.err, "cannot be changed") {
		t.Fatalf("form error = %q", m.form.err)
	}
}

func TestHashConflictShowsMessageAndReloads(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	before := fake.varianceCalls
	fake.putFn = func(putCall) (string, error) {
		return "", fmt.Errorf("%w: app 'app' changed on the server since you loaded it", ops.ErrConfigConflict)
	}
	m.appName = "app"
	m.openAppForm("app", false)
	m.form.app.Upstream = "app:9090"
	runCmd(t, m, m.submitForm())
	if m.form != nil {
		t.Fatal("form stayed open after a conflict")
	}
	if m.confirm == nil || m.confirm.title != "Changed on the server" {
		t.Fatalf("confirm = %#v", m.confirm)
	}
	if !strings.Contains(m.confirm.body, "changed on the server since you loaded it") {
		t.Fatalf("conflict message = %q", m.confirm.body)
	}
	if fake.varianceCalls <= before {
		t.Fatal("variances were not reloaded after a conflict")
	}
}

func TestBuildApprovalDialogAndRetry(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	approval := &ops.BuildApprovalError{Server: "prod", App: "app", Script: "#!/bin/sh\necho one\necho two\n"}
	calls := 0
	fake.runFn = func(context.Context, Request, io.Writer) (Result, error) {
		calls++
		if calls == 1 {
			return Result{}, approval
		}
		return Result{}, nil
	}
	runCmd(t, m, m.startRun(Request{Action: ActDeploy, Server: "prod", App: "app"}))
	if m.confirm == nil || m.confirm.title != "Approve build script" || !m.confirm.scrollable {
		t.Fatalf("approval dialog = %#v", m.confirm)
	}
	if !strings.Contains(m.confirm.script, "echo one") || m.confirm.yesHint != "approve and deploy" {
		t.Fatalf("approval dialog content = %#v", m.confirm)
	}
	view := m.View()
	if !strings.Contains(view, "echo one") || !strings.Contains(view, "approve and deploy") {
		t.Fatalf("approval view missing script or action:\n%s", view)
	}
	m, _ = press(t, m, "down")
	if m.confirm.scroll == 0 {
		t.Fatal("scroll did not move")
	}
	_, cmd := press(t, m, "y")
	if cmd == nil {
		t.Fatal("approve returned no command")
	}
	msg := cmd()
	_, retry := m.Update(msg)
	if retry == nil {
		t.Fatal("approval did not retry the deploy")
	}
	runCmd(t, m, retry)
	if len(fake.approvalCalls) != 1 || fake.approvalCalls[0].script != approval.Script {
		t.Fatalf("approval calls = %+v", fake.approvalCalls)
	}
	if len(fake.runCalls) != 2 || fake.runCalls[1].Action != ActDeploy {
		t.Fatalf("run calls = %+v", fake.runCalls)
	}
	if m.confirm != nil {
		t.Fatal("approval dialog stayed open")
	}
}

func TestCompareShowsVarianceDiff(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	newRecord := decodedRecord(t, appRecord)
	oldRecord := decodedRecord(t, strings.Replace(appRecord, "app:8080", "app:8080\nenv:\n  OLD: one", 1))
	state := testState("prod", map[string]*config.App{"app": newRecord})
	state.Running["app"] = &ops.RunningApp{Name: "app", Record: oldRecord, Compose: []byte("name: old\n")}
	report := ops.ComputeVariances(state)
	setVariances(m, fake, "prod", report)
	m.serverName = "prod"
	m.appName = "app"
	m.page = pageServer
	m.openCompare()
	if m.page != pageCompare {
		t.Fatalf("page = %v", m.page)
	}
	if !strings.Contains(m.compareText, "@@") {
		t.Fatalf("compare text has no diff:\n%s", m.compareText)
	}
	view := m.View()
	for _, want := range []string{"Compare", "running → desired", "@@"} {
		if !strings.Contains(view, want) {
			t.Errorf("compare view missing %q:\n%s", want, view)
		}
	}
	m, _ = press(t, m, "esc")
	if m.page != pageServer {
		t.Fatalf("esc left page = %v", m.page)
	}
}

func TestServerSettingsFormWritesWithHash(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	state := appWithState(t, m, fake)
	m.serverName = "prod"
	m.openServerSettingsForm("prod")
	if m.form == nil || m.form.purpose != formServerSettings {
		t.Fatalf("form = %#v", m.form)
	}
	if m.form.server.AdminUser != state.Settings.AdminUser {
		t.Fatalf("admin field = %q", m.form.server.AdminUser)
	}
	m.form.server.AdminUser = "admin2"
	m.form.server.CaddyImage = "caddy:2.9-alpine"
	runCmd(t, m, m.submitForm())
	if len(fake.serverPuts) != 1 {
		t.Fatalf("server puts = %+v", fake.serverPuts)
	}
	call := fake.serverPuts[0]
	if call.server != "prod" || call.admin != "admin2" || call.caddy != "caddy:2.9-alpine" || call.expectedHash != "srvhash" {
		t.Fatalf("server put = %+v", call)
	}
	if m.form != nil || m.page != pageServer {
		t.Fatalf("after save form=%v page=%v", m.form, m.page)
	}
}

func TestImageSourcePathOnlyWritesLocalFile(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	state := testState("staging", map[string]*config.App{
		"web": decodedRecord(t, "domains: [web.example.com]\nupstream: web:8080\nsource: {type: image}\n"),
	})
	setVariances(m, fake, "staging", ops.VarianceReport{
		Server: "staging",
		State:  state,
		Apps:   []ops.AppVariance{{App: "web", State: ops.VarianceApplied}},
	})
	m.serverName = "staging"
	m.appName = "web"
	m.page = pageAppEdit
	m.openAppQuickForm(eaSource)
	if m.form == nil || m.form.purpose != formEditAppSource {
		t.Fatalf("form = %#v", m.form)
	}
	m.form.app.SourcePath = "~/code/web"
	runCmd(t, m, m.submitForm())
	if len(fake.putCalls) != 0 {
		t.Fatalf("local source path wrote to the server: %+v", fake.putCalls)
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "web: ~/code/web") {
		t.Fatalf("local sources not updated:\n%s", data)
	}
	cfg, err := config.Load(m.path)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := cfg.Server("staging")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Sources["web"] == "" {
		t.Fatalf("sources = %+v", srv.Sources)
	}
}

func TestGitSourceURLOnlyWritesLocalFile(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	state := testState("staging", map[string]*config.App{
		"web": decodedRecord(t, "domains: [web.example.com]\nupstream: web:8080\nsource: {type: image}\n"),
	})
	setVariances(m, fake, "staging", ops.VarianceReport{
		Server: "staging",
		State:  state,
		Apps:   []ops.AppVariance{{App: "web", State: ops.VarianceApplied}},
	})
	m.serverName = "staging"
	m.appName = "web"
	m.page = pageAppEdit
	m.openAppQuickForm(eaSource)
	m.form.app.SourceKind = "git"
	m.form.app.SourceURL = "https://github.com/acme/web.git"
	m.form.app.SourcePath = ""
	runCmd(t, m, m.submitForm())
	if len(fake.putCalls) != 0 {
		t.Fatalf("local Git URL wrote to the server: %+v", fake.putCalls)
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "web: https://github.com/acme/web.git") {
		t.Fatalf("local sources not updated:\n%s", data)
	}
}

func TestGitReleasePickerBuildsSourceOrAsset(t *testing.T) {
	doc := strings.Replace(tuiTestDoc, "web: ~/web", "web: https://github.com/acme/web", 1)
	m, fake := testModel(t, doc)
	state := testState("staging", map[string]*config.App{
		"web": decodedRecord(t, "domains: [web.example.com]\nupstream: web:8080\nsource: {type: image}\n"),
	})
	setVariances(m, fake, "staging", ops.VarianceReport{
		Server: "staging",
		State:  state,
		Apps:   []ops.AppVariance{{App: "web", State: ops.VarianceApplied}},
	})
	fake.commitsFn = func(server, app string) ([]ops.GitCommit, error) {
		return []ops.GitCommit{{SHA: strings.Repeat("a", 40), Short: "aaaaaaa", Subject: "latest"}}, nil
	}
	fake.releasesFn = func(server, app string) ([]ops.GitRelease, error) {
		return []ops.GitRelease{{Tag: "v1.2.3", Name: "Release"}}, nil
	}
	fake.releaseAssetsFn = func(server, app, tag string) ([]ops.GitAsset, error) {
		return []ops.GitAsset{{Name: "web_linux_amd64", Size: 10, Digest: "sha256:" + strings.Repeat("b", 64)}}, nil
	}
	m.serverName = "staging"
	m.appName = "web"
	m.page = pageAppActions

	runCmd(t, m, m.openDeployPicker())
	if m.page != pageDeployPick || len(m.pickCommits) != 1 {
		t.Fatalf("commit picker page=%v commits=%+v", m.page, m.pickCommits)
	}
	var cmd tea.Cmd
	m, cmd = press(t, m, "tab")
	runCmd(t, m, cmd)
	if len(m.pickReleases) != 1 {
		t.Fatalf("releases = %+v", m.pickReleases)
	}
	m, cmd = press(t, m, "enter")
	runCmd(t, m, cmd)
	if m.pickReleaseTag != "v1.2.3" || len(m.pickAssets) != 1 {
		t.Fatalf("release choice tag=%q assets=%+v", m.pickReleaseTag, m.pickAssets)
	}
	m.width, m.height = 64, 24
	m.resizeViewport()
	view := m.View()
	for _, want := range []string{"Build source", "web_linux_amd64"} {
		if !strings.Contains(view, want) {
			t.Fatalf("choice view missing %q:\n%s", want, view)
		}
	}
	m, cmd = press(t, m, "down")
	_ = cmd
	m, cmd = press(t, m, "enter")
	runCmd(t, m, cmd)
	if len(fake.runCalls) != 1 {
		t.Fatalf("run calls = %+v", fake.runCalls)
	}
	if got := fake.runCalls[0].Version; got != "release:v1.2.3:web_linux_amd64" {
		t.Fatalf("deploy version = %q", got)
	}
}

func TestRestartAndSyncCaddyActions(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	m.runServerAction(saProxyRestart)
	if len(m.activities) != 1 || m.activities[0].req.Action != ActProxyRestart {
		t.Fatalf("restart activities = %+v", m.activities)
	}
	m2, _ := testModel(t, tuiTestDoc)
	m2.serverName = "prod"
	m2.runServerAction(saProxySync)
	if len(m2.activities) != 1 || m2.activities[0].req.Action != ActProxySync {
		t.Fatalf("sync activities = %+v", m2.activities)
	}
	_ = fake
}

func TestOldFormatFilePointsAtConfigPush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ship.yaml")
	old := "servers:\n  prod:\n    host: ops@203.0.113.10\n    apps:\n      app:\n        domains: [app.example.com]\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIP_CONFIG", path)
	_, _, _, err := loadForTUI(io.Discard)
	if err == nil {
		t.Fatal("old-format file loaded")
	}
	if !strings.Contains(err.Error(), "config push") || !strings.Contains(err.Error(), "old full-configuration format") {
		t.Fatalf("error does not point at config push: %v", err)
	}
}

func TestAddServerWritesNewLocalFormat(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m.openServerForm("", true)
	m.form.server.Name = "new"
	m.form.server.Host = "ops@203.0.113.12"
	runCmd(t, m, m.submitForm())
	data, err := os.ReadFile(m.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "admin_user") || strings.Contains(string(data), "apps:") {
		t.Fatalf("new server wrote old-format keys:\n%s", data)
	}
	cfg, err := config.Load(m.path)
	if err != nil {
		t.Fatalf("new local file is not valid: %v\n%s", err, data)
	}
	if _, err := cfg.Server("new"); err != nil {
		t.Fatal(err)
	}
}

func TestServerFormSwitchesConnectionFields(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m.openServerForm("", true)
	v := m.form.server
	if v.Connection != config.ConnectionRemote {
		t.Fatalf("new server connection = %q, want remote", v.Connection)
	}
	if serverRemoteHidden(v) || !serverLocalHidden(v) {
		t.Fatal("remote server form does not show the SSH fields")
	}
	v.Connection = config.ConnectionLocal
	if !serverRemoteHidden(v) || serverLocalHidden(v) {
		t.Fatal("local server form does not show the user field")
	}
}

func TestAddLocalServerWritesConnection(t *testing.T) {
	m, _ := testModel(t, tuiTestDoc)
	m.openServerForm("", true)
	m.form.server.Name = "self"
	m.form.server.Connection = config.ConnectionLocal
	m.form.server.Host = "ops@203.0.113.99"
	m.form.server.IdentityFile = "~/.ssh/prod"
	m.form.server.User = "ops"
	runCmd(t, m, m.submitForm())

	cfg, err := config.Load(m.path)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := cfg.Server("self")
	if err != nil {
		t.Fatal(err)
	}
	if !srv.IsLocal() || srv.Host != "" || srv.IdentityFile != "" || srv.User != "ops" {
		t.Fatalf("saved local server = %+v", srv)
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ops@203.0.113.99") {
		t.Fatalf("local server kept a host:\n%s", data)
	}
}

func TestMaintenanceBadgeAndToggle(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	m.Update(statusFor("prod", ops.AppStatus{
		App:         "app",
		Current:     "v1.2.3",
		Health:      "healthy",
		Maintenance: true,
	}))
	m.page = pageServer
	if view := m.View(); !strings.Contains(view, "MAINT") {
		t.Fatalf("maintenance badge missing:\n%s", view)
	}
	m.appName = "app"
	m.page = pageAppActions
	m, _ = press(t, m, "m")
	if len(m.activities) != 1 {
		t.Fatalf("maintenance toggle did not start: %d", len(m.activities))
	}
	entry := m.activities[0]
	if entry.req.Action != ActMaintenance || entry.req.MaintenanceOn {
		t.Fatalf("expected maintenance off, got %+v", entry.req)
	}
}

func TestConfirmGatesDeployAll(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	m.page = pageServerActions
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
	appWithState(t, m, fake)
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
	entry.out.Write([]byte("early output\n"))
	m.Update(runTickMsg{})
	if view := m.View(); !strings.Contains(view, "early output") {
		t.Fatalf("live output not shown:\n%s", view)
	}
	runCmd(t, m, cmd)
	if entry.state != runSucceeded {
		t.Fatalf("state = %v, want succeeded", entry.state)
	}
	view := m.View()
	for _, want := range []string{"line one", "line two", "succeeded"} {
		if !strings.Contains(view, want) {
			t.Errorf("output view missing %q:\n%s", want, view)
		}
	}
	if fake.varianceCalls == 0 {
		t.Fatal("deploy did not refresh variances")
	}
}

func TestSameServerRunsAreRefused(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
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

func TestViewsFitEightyByTwentyFour(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	state := testState("prod", map[string]*config.App{"app": decodedRecord(t, appRecord)})
	setVariances(m, fake, "prod", ops.VarianceReport{
		Server:         "prod",
		State:          state,
		Apps:           []ops.AppVariance{{App: "app", State: ops.VariancePending}},
		ProxyOutOfSync: true,
		ProxyReason:    "the Caddyfile that would be rendered differs from the active one",
		SetupPending:   true,
		SetupReason:    "admin_user changed since the last setup",
	})
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
	m.appName = "app"
	m.page = pageAppActions
	check("app actions")
	press(t, m, "esc")
	press(t, m, "a")
	check("form")
}

func TestEditComposeUsesServerDesiredFile(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	m.appName = "app"
	m.page = pageAppActions
	m, cmd := press(t, m, "f")
	if cmd == nil || m.edit == nil {
		t.Fatal("f did not start an editor")
	}
	if m.edit.target.kind != editDesiredCompose || m.edit.target.label != "apps/app/compose.yaml" {
		t.Fatalf("edit target = %#v", m.edit.target)
	}
	data, err := m.edit.session.Read()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "name: app\n" {
		t.Fatalf("editor data = %q", data)
	}
	_ = m.edit.session.Close()
	m.edit = nil
	_ = fake
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

func TestAppDeleteUsesLoadedHash(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	m.appName = "app"
	m.page = pageAppActions
	m.runAppAction(aaDelete)
	if m.confirm == nil {
		t.Fatal("delete did not ask for confirmation")
	}
	_, cmd := press(t, m, "y")
	runCmd(t, m, cmd)
	if len(fake.removeCalls) != 1 || fake.removeCalls[0].expectedHash != "hash-app" {
		t.Fatalf("remove calls = %+v", fake.removeCalls)
	}
	if m.page != pageServer {
		t.Fatalf("page = %v", m.page)
	}
}

func TestDeleteHashConflictReloads(t *testing.T) {
	m, fake := testModel(t, tuiTestDoc)
	appWithState(t, m, fake)
	fake.removeFn = func(removeCall) error {
		return fmt.Errorf("%w: app changed on the server since you loaded it", ops.ErrConfigConflict)
	}
	m.appName = "app"
	m.page = pageAppActions
	m.runAppAction(aaDelete)
	_, cmd := press(t, m, "y")
	runCmd(t, m, cmd)
	if m.confirm == nil || m.confirm.title != "Changed on the server" {
		t.Fatalf("confirm = %#v", m.confirm)
	}
	if !strings.Contains(m.confirm.body, "changed on the server since you loaded it") {
		t.Fatalf("conflict message = %q", m.confirm.body)
	}
}

func TestErrorsIsConflictSentinel(t *testing.T) {
	if !errors.Is(fmt.Errorf("wrap: %w", ops.ErrConfigConflict), ops.ErrConfigConflict) {
		t.Fatal("ErrConfigConflict is not usable with errors.Is")
	}
}
