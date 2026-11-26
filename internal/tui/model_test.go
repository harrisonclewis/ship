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
	"github.com/harrisonclewis/ship/internal/ops"
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
          path: /tmp/web
        backup: {}
`

const testConfig = tuiTestDoc

func writeTestConfig(t *testing.T) (string, *config.Config, *yamlDoc) {
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
	doc, err := loadYAMLDoc(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, cfg, doc
}

type fakeBackend struct {
	cfg      *config.Config
	statusFn func(server string) (ops.StatusReport, error)
	backups  []BackupEntry
	runFn    func(context.Context, Request, io.Writer) (Result, error)
	runCalls []Request

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
	yd, err := loadYAMLDoc(path)
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
	if v.SourceType != "image" || v.SourcePath != "/tmp/web" {
		t.Fatalf("image source not loaded: %+v", v)
	}
	if !v.DeployMaintenance {
		t.Fatal("maintenance should default to on")
	}
	if form := m.buildForm(&formHost{purpose: formEditApp, app: v}); form == nil {
		t.Fatal("no app form")
	}
}
