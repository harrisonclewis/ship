package tui

import (
	"context"
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/ops"
)

type pageKind int

const (
	pageServers pageKind = iota
	pageActivity
	pageServer
	pageServerActions
	pageAppActions
	pageOutput
	pageRestorePick
)

type serverAction int

const (
	saDeployAll serverAction = iota
	saBackupAll
	saProxyUpdate
	saSetup
	saLockSSH
	saVPSUpdate
	saReboot
	saAddApp
	saEditServer
	saDeleteServer
)

type serverActionDef struct {
	key    string
	label  string
	desc   string
	action serverAction
	danger bool
}

var serverActionDefs = []serverActionDef{
	{"d", "Deploy all", "Deploy every app in name order", saDeployAll, false},
	{"b", "Backup all", "Archive and download every app", saBackupAll, false},
	{"p", "Proxy update", "Render and activate the Caddyfile", saProxyUpdate, false},
	{"s", "Setup", "Prepare packages, users, Docker and the backup timer", saSetup, false},
	{"l", "Lock SSH", "Disable password and root SSH logins", saLockSSH, true},
	{"u", "VPS update", "apt update, full upgrade and autoremove", saVPSUpdate, false},
	{"R", "Reboot", "Reboot the server", saReboot, true},
	{"a", "Add app", "Add an application to ship.yaml", saAddApp, false},
	{"e", "Edit server", "Change host, admin user, arch or Caddy image", saEditServer, false},
	{"x", "Delete server", "Remove it from ship.yaml only", saDeleteServer, true},
}

type appAction int

const (
	aaDeployLatest appAction = iota
	aaDeployVersion
	aaRollback
	aaRestart
	aaMaintenance
	aaBackup
	aaBackups
	aaRestore
	aaLogs
	aaEnvEdit
	aaEdit
	aaDelete
)

type appActionDef struct {
	key    string
	label  string
	desc   string
	action appAction
	danger bool
}

var appActionDefs = []appActionDef{
	{"d", "Deploy latest/build", "Release latest, or build the current git tree", aaDeployLatest, false},
	{"v", "Deploy version…", "Deploy a release tag or loaded image", aaDeployVersion, false},
	{"o", "Rollback", "Previous image; the database is not restored", aaRollback, true},
	{"s", "Restart", "Recreate the app containers", aaRestart, false},
	{"m", "Maintenance on/off", "Toggle the maintenance page", aaMaintenance, false},
	{"b", "Backup", "Archive now and download", aaBackup, false},
	{"L", "Backups", "List archives stored on the server", aaBackups, false},
	{"t", "Restore", "Restore a server archive or a local file", aaRestore, true},
	{"g", "Logs", "Follow logs in the terminal", aaLogs, false},
	{"e", "Edit env", "sudoedit the app's secret .env", aaEnvEdit, false},
	{"i", "Edit app", "Change the app in ship.yaml", aaEdit, false},
	{"x", "Delete app", "Remove it from ship.yaml only", aaDelete, true},
}

type confirmDialog struct {
	title string
	body  string
	yes   func() tea.Cmd
}

type model struct {
	width  int
	height int
	dryRun bool

	backend Backend
	cfg     *config.Config
	doc     *yamlDoc
	path    string

	page          pageKind
	tab           int
	serverCursor  int
	activityCur   int
	appCursor     int
	serverActCur  int
	appActCur     int
	backupCursor  int
	serverName    string
	appName       string
	outputRun     int
	outputBack    pageKind
	lastServerTab int

	activities []*activity
	byID       map[int]*activity
	runs       map[string]int
	runSeq     int

	statuses      map[string]ops.StatusReport
	statusErr     map[string]string
	statusLoading map[string]bool

	backups        []BackupEntry
	backupsLoading bool
	backupsErr     string
	backupsServer  string
	backupsApp     string

	vp            viewport.Model
	spinnerFrame  int
	tickScheduled bool

	form          *formHost
	confirm       *confirmDialog
	toast         *toast
	toastSet      time.Time
	pendingReboot string
}

type statusMsg struct {
	server string
	report ops.StatusReport
	err    error
}

type backupsMsg struct {
	server string
	app    string
	items  []BackupEntry
	err    error
}

type terminalReadyMsg struct {
	req     TerminalRequest
	cmd     *exec.Cmd
	display string
	err     error
}

func newModel(cfg *config.Config, backend Backend, doc *yamlDoc, path string, dryRun bool) *model {
	m := &model{
		width:         80,
		height:        24,
		dryRun:        dryRun,
		backend:       backend,
		cfg:           cfg,
		doc:           doc,
		path:          path,
		page:          pageServers,
		activityCur:   0,
		byID:          map[int]*activity{},
		runs:          map[string]int{},
		statuses:      map[string]ops.StatusReport{},
		statusErr:     map[string]string{},
		statusLoading: map[string]bool{},
	}
	m.vp = viewport.New(76, 16)
	m.resizeViewport()
	return m
}

func (m *model) Init() tea.Cmd {
	return m.refreshAllStatuses()
}

func (m *model) resizeViewport() {
	w := m.width - 4
	if w < 10 {
		w = 10
	}
	h := m.bodyHeight() - 2
	if h < 3 {
		h = 3
	}
	m.vp.Width = w
	m.vp.Height = h
	if m.page == pageOutput {
		// Re-wrap the output for the new width.
		if entry := m.outputActivity(); entry != nil {
			atBottom := m.vp.AtBottom()
			m.vp.SetContent(ansi.Wrap(entry.out.String(), w, " /,'"))
			if atBottom {
				m.vp.GotoBottom()
			}
		}
	}
}

func (m *model) bodyHeight() int {
	h := m.height - 1 - 2
	if m.page == pageServers || m.page == pageActivity {
		h--
	}
	if m.toast != nil {
		h -= 2
	}
	if h < 3 {
		h = 3
	}
	return h
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	// A huh form draws nothing until Init has run, so initialise every form
	// that was opened or rebuilt during this update.
	if m.form != nil && m.form.form != nil && m.form.initialised != m.form.form {
		m.form.initialised = m.form.form
		cmd = tea.Batch(cmd, m.form.form.Init())
	}
	return model, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewport()
		if m.form != nil && m.form.form != nil {
			updated, cmd := m.form.form.Update(msg)
			if f, ok := updated.(*huh.Form); ok {
				m.form.form = f
			}
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case statusMsg:
		delete(m.statusLoading, msg.server)
		if msg.err != nil {
			m.statusErr[msg.server] = msg.err.Error()
			delete(m.statuses, msg.server)
			return m, nil
		}
		delete(m.statusErr, msg.server)
		m.statuses[msg.server] = msg.report
		return m, nil
	case backupsMsg:
		if m.page != pageRestorePick || msg.server != m.backupsServer || msg.app != m.backupsApp {
			return m, nil
		}
		m.backupsLoading = false
		m.backups = msg.items
		if msg.err != nil {
			m.backupsErr = msg.err.Error()
		} else {
			m.backupsErr = ""
		}
		return m, nil
	case runDoneMsg:
		return m, m.finishRun(msg)
	case runTickMsg:
		m.tickScheduled = false
		m.spinnerFrame++
		if m.page == pageOutput {
			m.syncOutput(false)
		}
		if m.runningCount() > 0 {
			return m, m.ensureTick()
		}
		return m, nil
	case terminalReadyMsg:
		return m, m.terminalReady(msg)
	case terminalDoneMsg:
		if msg.err != nil {
			m.showToast(toastError, msg.err.Error())
		} else {
			m.showToast(toastInfo, "Terminal closed.")
		}
		return m, nil
	}
	// huh moves between fields and groups with its own messages (and blinks
	// the cursor), so everything else goes to the open form.
	if m.form != nil {
		return m, m.updateForm(msg)
	}
	return m, nil
}

// updateForm passes msg to the open form and handles its completion.
func (m *model) updateForm(msg tea.Msg) tea.Cmd {
	if m.form == nil || m.form.form == nil {
		return nil
	}
	updated, cmd := m.form.form.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		m.form.form = f
	}
	switch m.form.form.State {
	case huh.StateCompleted:
		return m.submitForm()
	case huh.StateAborted:
		m.cancelForm()
		return nil
	}
	return cmd
}

type terminalDoneMsg struct {
	err error
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.form != nil {
		if key == "esc" {
			m.cancelForm()
			return m, nil
		}
		return m, m.updateForm(msg)
	}

	if m.confirm != nil {
		switch key {
		case "y", "enter":
			yes := m.confirm.yes
			m.confirm = nil
			if yes != nil {
				cmd := yes()
				m.offerPendingReboot()
				return m, cmd
			}
			m.offerPendingReboot()
		case "n", "esc", "q":
			m.confirm = nil
			m.offerPendingReboot()
		}
		return m, nil
	}

	if key == "ctrl+c" {
		if m.page == pageOutput && m.outputActivity() != nil && m.outputActivity().state == runRunning {
			m.askCancelRun()
			return m, nil
		}
		return m, m.requestQuit()
	}
	if key == "q" {
		return m, m.requestQuit()
	}

	switch m.page {
	case pageServers, pageActivity:
		return m.handleTabKey(msg, key)
	case pageServer:
		return m.handleServerKey(key)
	case pageServerActions:
		return m.handleServerActionsKey(key)
	case pageAppActions:
		return m.handleAppActionsKey(key)
	case pageOutput:
		return m.handleOutputKey(msg, key)
	case pageRestorePick:
		return m.handleRestoreKey(key)
	}
	return m, nil
}

func (m *model) handleTabKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	if key == "tab" || key == "shift+tab" || key == "1" || key == "2" {
		m.toast = nil
		if key == "1" {
			m.tab = 0
		} else if key == "2" {
			m.tab = 1
		} else {
			m.tab = 1 - m.tab
		}
		if m.tab == 0 {
			m.page = pageServers
		} else {
			m.page = pageActivity
		}
		return m, nil
	}
	switch key {
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "enter":
		if m.page == pageServers {
			name := m.selectedServerName()
			if name != "" {
				return m, m.openServer(name)
			}
		} else {
			return m, m.openSelectedActivity()
		}
	case "a":
		if m.page == pageServers {
			m.openServerForm("", true)
		}
	case "r":
		if m.page == pageServers {
			return m, m.refreshAllStatuses()
		}
	}
	return m, nil
}

func (m *model) moveCursor(delta int) {
	if m.page == pageActivity {
		m.activityCur = clampIndex(m.activityCur+delta, len(m.activities))
		return
	}
	m.serverCursor = clampIndex(m.serverCursor+delta, len(m.backend.Servers()))
}

func (m *model) handleServerKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.toast = nil
		m.page = pageServers
		return m, nil
	case "up", "k":
		m.appCursor = clampIndex(m.appCursor-1, len(m.appNames()))
	case "down", "j":
		m.appCursor = clampIndex(m.appCursor+1, len(m.appNames()))
	case "enter":
		if len(m.appNames()) > 0 {
			if m.appCursor >= len(m.appNames()) {
				m.appCursor = len(m.appNames()) - 1
			}
			m.appName = m.appNames()[m.appCursor]
			m.page = pageAppActions
			m.appActCur = 0
		}
	case "a":
		m.openAppForm("", true)
	case "e":
		m.openServerForm(m.serverName, false)
	case "m":
		m.page = pageServerActions
		m.serverActCur = 0
	case "d":
		return m, m.runServerAction(saDeployAll)
	case "b":
		return m, m.runServerAction(saBackupAll)
	case "p":
		return m, m.runServerAction(saProxyUpdate)
	case "s":
		return m, m.runServerAction(saSetup)
	case "l":
		return m, m.runServerAction(saLockSSH)
	case "u":
		return m, m.runServerAction(saVPSUpdate)
	case "R":
		return m, m.runServerAction(saReboot)
	case "x":
		return m, m.runServerAction(saDeleteServer)
	case "r":
		return m, m.loadStatus(m.serverName)
	}
	return m, nil
}

func (m *model) handleServerActionsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.page = pageServer
		return m, nil
	case "up", "k":
		m.serverActCur = clampIndex(m.serverActCur-1, len(serverActionDefs))
		return m, nil
	case "down", "j":
		m.serverActCur = clampIndex(m.serverActCur+1, len(serverActionDefs))
		return m, nil
	case "enter":
		return m, m.runServerAction(serverActionDefs[m.serverActCur].action)
	}
	for i, def := range serverActionDefs {
		if key == def.key {
			m.serverActCur = i
			return m, m.runServerAction(def.action)
		}
	}
	return m, nil
}

func (m *model) handleAppActionsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.page = pageServer
		return m, nil
	case "up", "k":
		m.appActCur = clampIndex(m.appActCur-1, len(appActionDefs))
		return m, nil
	case "down", "j":
		m.appActCur = clampIndex(m.appActCur+1, len(appActionDefs))
		return m, nil
	case "enter":
		return m, m.runAppAction(appActionDefs[m.appActCur].action)
	}
	for i, def := range appActionDefs {
		if key == def.key {
			m.appActCur = i
			return m, m.runAppAction(def.action)
		}
	}
	return m, nil
}

func (m *model) handleOutputKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.toast = nil
		if m.outputBack == pageActivity {
			m.page = pageActivity
		} else {
			m.page = pageServer
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *model) handleRestoreKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.page = pageServer
		return m, nil
	case "up", "k":
		m.backupCursor = clampIndex(m.backupCursor-1, len(m.backups)+1)
	case "down", "j":
		m.backupCursor = clampIndex(m.backupCursor+1, len(m.backups)+1)
	case "p":
		m.openRestorePathForm()
	case "enter":
		if m.backupsLoading {
			return m, nil
		}
		if m.backupCursor >= len(m.backups) {
			m.openRestorePathForm()
			return m, nil
		}
		entry := m.backups[m.backupCursor]
		app := m.backupsApp
		server := m.backupsServer
		m.openConfirm("Restore backup", "Restore "+app+" from "+filepath.Base(entry.Path)+"? A pre-restore backup is taken first; the app is stopped while files are replaced.", func() tea.Cmd {
			return m.startRun(Request{Action: ActRestore, Server: server, App: app, Archive: entry.Path})
		})
	case "r":
		return m, m.loadBackups(m.backupsServer, m.backupsApp)
	}
	return m, nil
}

func clampIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

func (m *model) selectedServerName() string {
	names := m.backend.Servers()
	if len(names) == 0 {
		return ""
	}
	m.serverCursor = clampIndex(m.serverCursor, len(names))
	return names[m.serverCursor]
}

func (m *model) appNames() []string {
	srv, err := m.backend.Server(m.serverName)
	if err != nil {
		return nil
	}
	return srv.AppNames()
}

func (m *model) currentServer() *config.Server {
	srv, err := m.backend.Server(m.serverName)
	if err != nil {
		return nil
	}
	return srv
}

func (m *model) currentApp() *config.App {
	srv := m.currentServer()
	if srv == nil {
		return nil
	}
	app, err := srv.App(m.appName)
	if err != nil {
		return nil
	}
	return app
}

func (m *model) openServer(name string) tea.Cmd {
	m.page = pageServer
	m.serverName = name
	m.appCursor = 0
	m.toast = nil
	return m.loadStatus(name)
}

func (m *model) openSelectedActivity() tea.Cmd {
	if len(m.activities) == 0 {
		return nil
	}
	m.activityCur = clampIndex(m.activityCur, len(m.activities))
	entry := m.activities[m.activityCur]
	m.outputRun = entry.id
	m.outputBack = pageActivity
	m.page = pageOutput
	m.showOutput(entry)
	if entry.state == runRunning {
		return m.ensureTick()
	}
	return nil
}

func (m *model) outputActivity() *activity {
	return m.byID[m.outputRun]
}

func (m *model) showOutput(entry *activity) {
	if entry == nil {
		m.vp.SetContent("")
		return
	}
	entry.shown = entry.out.Len()
	m.vp.SetContent(entry.out.String())
	m.vp.GotoBottom()
	m.resizeViewport()
}

func (m *model) syncOutput(force bool) {
	entry := m.outputActivity()
	if entry == nil {
		return
	}
	data := entry.out.String()
	if !force && len(data) == entry.shown {
		return
	}
	atBottom := m.vp.AtBottom()
	entry.shown = len(data)
	// Wrap rather than clip: ssh and docker lines are often wider than the pane.
	m.vp.SetContent(ansi.Wrap(data, m.vp.Width, " /,'"))
	if force || atBottom {
		m.vp.GotoBottom()
	}
}

func (m *model) refreshAllStatuses() tea.Cmd {
	var cmds []tea.Cmd
	for _, name := range m.backend.Servers() {
		if cmd := m.loadStatus(name); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *model) loadStatus(server string) tea.Cmd {
	if server == "" || m.statusLoading[server] {
		return nil
	}
	m.statusLoading[server] = true
	delete(m.statuses, server)
	delete(m.statusErr, server)
	backend := m.backend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		report, err := backend.Status(ctx, server, "")
		return statusMsg{server: server, report: report, err: err}
	}
}

func (m *model) startRun(req Request) tea.Cmd {
	if req.Server == "" {
		m.showToast(toastError, "No server selected.")
		return nil
	}
	if _, err := m.backend.Server(req.Server); err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	if m.runs[req.Server] != 0 {
		m.showToast(toastWarning, "Another run is already active on "+req.Server+"; wait for it to finish.")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.runSeq++
	entry := &activity{
		id:     m.runSeq,
		req:    req,
		label:  req.actionLabel(),
		start:  time.Now(),
		state:  runRunning,
		out:    &syncBuffer{},
		ctx:    ctx,
		cancel: cancel,
	}
	m.activities = append(m.activities, entry)
	m.byID[entry.id] = entry
	m.runs[req.Server] = entry.id
	m.toast = nil
	m.outputRun = entry.id
	m.outputBack = pageServer
	m.page = pageOutput
	m.showOutput(entry)

	backend := m.backend
	id := entry.id
	out := entry.out
	runCmd := func() tea.Msg {
		result, err := backend.Run(ctx, req, out)
		return runDoneMsg{id: id, result: result, err: err}
	}
	return tea.Batch(runCmd, m.ensureTick())
}

func (m *model) ensureTick() tea.Cmd {
	if m.tickScheduled {
		return nil
	}
	m.tickScheduled = true
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return runTickMsg{} })
}

func (m *model) finishRun(msg runDoneMsg) tea.Cmd {
	entry := m.byID[msg.id]
	if entry == nil {
		return nil
	}
	if m.page == pageOutput && m.outputRun == entry.id {
		m.syncOutput(true)
	}
	entry.result = msg.result
	entry.err = msg.err
	entry.endTime = time.Now()
	entry.cancel()
	if msg.err != nil {
		entry.state = runFailed
		if errors.Is(msg.err, context.Canceled) {
			m.showToast(toastWarning, entry.label+" on "+entry.req.Server+" was cancelled.")
		} else {
			m.showToast(toastError, entry.label+" on "+entry.req.Server+" failed: "+msg.err.Error())
		}
	} else {
		entry.state = runSucceeded
		m.showToast(toastSuccess, entry.label+" on "+entry.req.Server+" finished.")
	}
	if m.runs[entry.req.Server] == entry.id {
		delete(m.runs, entry.req.Server)
	}
	if msg.err == nil && entry.req.Action == ActVPSUpdate && msg.result.RebootRequired {
		m.pendingReboot = entry.req.Server
		m.offerPendingReboot()
	}
	var cmds []tea.Cmd
	if cmd := m.loadStatus(entry.req.Server); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.runningCount() > 0 {
		cmds = append(cmds, m.ensureTick())
	}
	return tea.Batch(cmds...)
}

func (m *model) runningCount() int {
	n := 0
	for _, entry := range m.activities {
		if entry.state == runRunning {
			n++
		}
	}
	return n
}

func (m *model) runningEntries() []*activity {
	var out []*activity
	for _, entry := range m.activities {
		if entry.state == runRunning {
			out = append(out, entry)
		}
	}
	return out
}

func (m *model) cancelAll() {
	for _, entry := range m.runningEntries() {
		entry.cancel()
	}
}

func (m *model) requestQuit() tea.Cmd {
	if m.runningCount() == 0 {
		return tea.Quit
	}
	m.openConfirm("Quit with runs in progress?", "Operations are still running. Quitting cancels them.", func() tea.Cmd {
		m.cancelAll()
		return tea.Quit
	})
	return nil
}

func (m *model) askCancelRun() {
	entry := m.outputActivity()
	if entry == nil {
		return
	}
	m.openConfirm("Cancel this run?", "Stop "+entry.label+" on "+entry.req.Server+"?", func() tea.Cmd {
		entry.cancel()
		m.showToast(toastInfo, "Cancelling "+entry.label+"…")
		return nil
	})
}

func (m *model) openConfirm(title, body string, yes func() tea.Cmd) {
	m.confirm = &confirmDialog{title: title, body: body, yes: yes}
}

func (m *model) showToast(kind toastKind, msg string) {
	m.toast = &toast{kind: kind, msg: msg}
	m.resizeViewport()
}

func (m *model) offerPendingReboot() {
	if m.pendingReboot == "" || m.form != nil || m.confirm != nil {
		return
	}
	server := m.pendingReboot
	m.pendingReboot = ""
	m.openConfirm("Reboot required", "VPS update finished and needs a reboot. Reboot "+server+" now?", func() tea.Cmd {
		return m.startRun(Request{Action: ActVPSReboot, Server: server})
	})
}

func (m *model) runServerAction(action serverAction) tea.Cmd {
	switch action {
	case saDeployAll:
		server := m.serverName
		m.openConfirm("Deploy all", "Deploy every app on "+server+"?", func() tea.Cmd {
			return m.startRun(Request{Action: ActDeployAll, Server: server})
		})
	case saBackupAll:
		return m.startRun(Request{Action: ActBackup, Server: m.serverName, App: "all", Pull: true})
	case saProxyUpdate:
		return m.startRun(Request{Action: ActProxyUpdate, Server: m.serverName})
	case saSetup:
		return m.startRun(Request{Action: ActSetup, Server: m.serverName})
	case saLockSSH:
		server := m.serverName
		m.openConfirm("Lock SSH", "Disable password and root SSH logins on "+server+"? Make sure the admin user can log in with a key first.", func() tea.Cmd {
			return m.startRun(Request{Action: ActLockSSH, Server: server})
		})
	case saVPSUpdate:
		return m.startRun(Request{Action: ActVPSUpdate, Server: m.serverName})
	case saReboot:
		server := m.serverName
		m.openConfirm("Reboot VPS", "Reboot "+server+" now? The ssh connection will drop.", func() tea.Cmd {
			return m.startRun(Request{Action: ActVPSReboot, Server: server})
		})
	case saAddApp:
		m.openAppForm("", true)
	case saEditServer:
		m.openServerForm(m.serverName, false)
	case saDeleteServer:
		server := m.serverName
		if m.runs[server] != 0 {
			m.showToast(toastWarning, "Wait for the run on "+server+" to finish before deleting it.")
			return nil
		}
		m.openConfirm("Delete server", "Remove "+server+" from ship.yaml? This only edits the file; the server and its apps are not changed.", func() tea.Cmd {
			return m.deleteServer(server)
		})
	}
	return nil
}

func (m *model) runAppAction(action appAction) tea.Cmd {
	server, app := m.serverName, m.appName
	switch action {
	case aaDeployLatest:
		return m.startRun(Request{Action: ActDeploy, Server: server, App: app})
	case aaDeployVersion:
		f := &formHost{purpose: formDeployVersion, serverName: server, appName: app}
		f.form = m.buildForm(f)
		m.form = f
	case aaRollback:
		m.openConfirm("Roll back", "Roll back "+app+" to the previous image? The database is not restored.", func() tea.Cmd {
			return m.startRun(Request{Action: ActRollback, Server: server, App: app})
		})
	case aaRestart:
		return m.startRun(Request{Action: ActRestart, Server: server, App: app})
	case aaMaintenance:
		on := false
		if report, ok := m.statuses[server]; ok {
			for _, st := range report.Apps {
				if st.App == app {
					on = st.Maintenance
					break
				}
			}
		}
		return m.startRun(Request{Action: ActMaintenance, Server: server, App: app, MaintenanceOn: !on})
	case aaBackup:
		return m.startRun(Request{Action: ActBackup, Server: server, App: app, Pull: true})
	case aaBackups:
		return m.startRun(Request{Action: ActBackups, Server: server, App: app})
	case aaRestore:
		return m.loadBackups(server, app)
	case aaLogs:
		f := &formHost{purpose: formLogsService, serverName: server, appName: app}
		f.form = m.buildForm(f)
		m.form = f
	case aaEnvEdit:
		return m.prepareTerminal(TerminalRequest{Kind: TerminalEnvEdit, Server: server, App: app})
	case aaEdit:
		m.openAppForm(app, false)
	case aaDelete:
		m.openConfirm("Delete app", "Remove "+app+" from ship.yaml? This only edits the file; the app's compose files, data and deployed containers are not changed.", func() tea.Cmd {
			return m.deleteApp(server, app)
		})
	}
	return nil
}

func (m *model) prepareTerminal(req TerminalRequest) tea.Cmd {
	backend := m.backend
	return func() tea.Msg {
		cmd, display, err := backend.TerminalCommand(context.Background(), req)
		return terminalReadyMsg{req: req, cmd: cmd, display: display, err: err}
	}
}

func (m *model) terminalReady(msg terminalReadyMsg) tea.Cmd {
	if msg.err != nil {
		m.showToast(toastError, msg.err.Error())
		return nil
	}
	if msg.cmd == nil {
		// Dry run: record the command as a finished activity entry.
		m.runSeq++
		entry := &activity{
			id:      m.runSeq,
			req:     Request{Server: msg.req.Server, App: msg.req.App},
			label:   terminalLabel(msg.req),
			start:   time.Now(),
			endTime: time.Now(),
			state:   runSucceeded,
			out:     &syncBuffer{},
		}
		_, _ = entry.out.Write([]byte(dryRunTerminalCommand(msg.display)))
		m.activities = append(m.activities, entry)
		m.byID[entry.id] = entry
		m.outputRun = entry.id
		m.outputBack = pageServer
		m.page = pageOutput
		m.showOutput(entry)
		return nil
	}
	return tea.ExecProcess(msg.cmd, func(err error) tea.Msg {
		return terminalDoneMsg{err: err}
	})
}

func terminalLabel(req TerminalRequest) string {
	if req.Kind == TerminalLogs {
		if req.Service != "" {
			return "Logs " + req.App + " " + req.Service
		}
		return "Logs " + req.App
	}
	return "Edit env " + req.App
}

func (m *model) loadBackups(server, app string) tea.Cmd {
	m.page = pageRestorePick
	m.serverName = server
	m.appName = app
	m.backupsServer = server
	m.backupsApp = app
	m.backups = nil
	m.backupsErr = ""
	m.backupsLoading = true
	m.backupCursor = 0
	m.toast = nil
	return m.backupsCmd(server, app)
}

func (m *model) backupsCmd(server, app string) tea.Cmd {
	backend := m.backend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := backend.ListBackups(ctx, server, app)
		return backupsMsg{server: server, app: app, items: items, err: err}
	}
}

func (m *model) openServerForm(name string, isNew bool) {
	var srv *config.Server
	if !isNew {
		var err error
		srv, err = m.backend.Server(name)
		if err != nil {
			m.showToast(toastError, err.Error())
			return
		}
	}
	v := newServerFormValues(srv, isNew)
	f := &formHost{purpose: formAddServer, server: v}
	if !isNew {
		f.purpose = formEditServer
	}
	f.form = m.buildForm(f)
	m.form = f
}

func (m *model) openAppForm(name string, isNew bool) {
	if m.serverName == "" {
		m.showToast(toastWarning, "Open a server first.")
		return
	}
	v := m.newAppFormValues(m.serverName, name, isNew)
	f := &formHost{purpose: formAddApp, app: v}
	if !isNew {
		f.purpose = formEditApp
	}
	f.form = m.buildForm(f)
	m.form = f
}

func (m *model) openRestorePathForm() {
	f := &formHost{purpose: formRestorePath, serverName: m.backupsServer, appName: m.backupsApp}
	f.form = m.buildForm(f)
	m.form = f
}

func (m *model) cancelForm() {
	m.form = nil
	m.offerPendingReboot()
}

func (m *model) submitForm() tea.Cmd {
	f := m.form
	if f == nil {
		return nil
	}
	switch f.purpose {
	case formAddServer, formEditServer:
		tmp := m.doc.clone()
		if err := tmp.applyServer(*f.server); err != nil {
			f.err = err.Error()
			f.form = m.buildForm(f)
			return nil
		}
		if err := tmp.save(); err != nil {
			f.err = err.Error()
			f.form = m.buildForm(f)
			return nil
		}
		m.doc = tmp
		if err := m.reloadConfig(); err != nil {
			f.err = err.Error()
			f.form = m.buildForm(f)
			return nil
		}
		name := f.server.Name
		m.form = nil
		m.page = pageServers
		for i, n := range m.backend.Servers() {
			if n == name {
				m.serverCursor = i
				break
			}
		}
		m.showToast(toastSuccess, "Saved "+name+".")
		return m.refreshAllStatuses()
	case formAddApp, formEditApp:
		f.submitValues()
		tmp := m.doc.clone()
		if err := tmp.applyApp(*f.app); err != nil {
			f.err = err.Error()
			f.form = m.buildForm(f)
			return nil
		}
		if err := tmp.save(); err != nil {
			f.err = err.Error()
			f.form = m.buildForm(f)
			return nil
		}
		m.doc = tmp
		if err := m.reloadConfig(); err != nil {
			f.err = err.Error()
			f.form = m.buildForm(f)
			return nil
		}
		name := f.app.Name
		m.form = nil
		m.page = pageServer
		m.serverName = f.app.Server
		for i, n := range m.appNames() {
			if n == name {
				m.appCursor = i
				break
			}
		}
		m.showToast(toastSuccess, "Saved "+name+".")
		return m.loadStatus(m.serverName)
	case formDeployVersion:
		version := formText(f.text)
		if version != "" {
			srv, err := m.backend.Server(f.serverName)
			if err != nil {
				f.err = err.Error()
				f.form = m.buildForm(f)
				return nil
			}
			app, err := srv.App(f.appName)
			if err != nil {
				f.err = err.Error()
				f.form = m.buildForm(f)
				return nil
			}
			var verify error
			if app.Source.Type == "image" {
				verify = config.ValidateImageVersion(version)
			} else {
				verify = config.ValidateReleaseTag(version)
			}
			if verify != nil {
				f.err = verify.Error()
				f.form = m.buildForm(f)
				return nil
			}
		}
		m.form = nil
		return m.startRun(Request{Action: ActDeploy, Server: f.serverName, App: f.appName, Version: version})
	case formRestorePath:
		archive := formText(f.text)
		if archive == "" {
			f.err = "An archive path is required."
			f.form = m.buildForm(f)
			return nil
		}
		server := f.serverName
		app := f.appName
		m.form = nil
		m.openConfirm("Restore backup", "Restore "+app+" from "+archive+"? A pre-restore backup is taken first; the app is stopped while files are replaced.", func() tea.Cmd {
			return m.startRun(Request{Action: ActRestore, Server: server, App: app, Archive: archive})
		})
		return nil
	case formLogsService:
		service := formText(f.text)
		if service != "" {
			if err := config.ValidateService(service); err != nil {
				f.err = err.Error()
				f.form = m.buildForm(f)
				return nil
			}
		}
		server := f.serverName
		app := f.appName
		m.form = nil
		return m.prepareTerminal(TerminalRequest{Kind: TerminalLogs, Server: server, App: app, Service: service})
	}
	return nil
}

func (m *model) deleteServer(name string) tea.Cmd {
	tmp := m.doc.clone()
	if err := tmp.deleteServer(name); err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	if err := tmp.save(); err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	m.doc = tmp
	if err := m.reloadConfig(); err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	m.page = pageServers
	m.serverCursor = clampIndex(m.serverCursor, len(m.backend.Servers()))
	m.showToast(toastSuccess, "Removed "+name+" from ship.yaml.")
	return m.refreshAllStatuses()
}

func (m *model) deleteApp(server, app string) tea.Cmd {
	tmp := m.doc.clone()
	if err := tmp.deleteApp(server, app); err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	if err := tmp.save(); err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	m.doc = tmp
	if err := m.reloadConfig(); err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	m.page = pageServer
	m.serverName = server
	m.appCursor = clampIndex(m.appCursor, len(m.appNames()))
	m.showToast(toastSuccess, "Removed "+app+" from ship.yaml.")
	return m.loadStatus(server)
}

func (m *model) reloadConfig() error {
	cfg, err := config.Load(m.path)
	if err != nil {
		return err
	}
	m.cfg = cfg
	m.backend.SetConfig(cfg)
	m.serverCursor = clampIndex(m.serverCursor, len(cfg.ServerNames()))
	return nil
}

func (m *model) formWidth() int {
	w := m.width - 8
	if w > 72 {
		w = 72
	}
	if w < 36 {
		w = 36
	}
	return w
}

func (m *model) configPathLabel() string {
	if m.path == "" {
		return ""
	}
	return filepath.Base(m.path)
}

func (m *model) composeMissing(name string) bool {
	srv := m.currentServer()
	if srv == nil {
		return false
	}
	fi, err := os.Stat(srv.ComposePath(name))
	return err != nil || fi.IsDir()
}

func (m *model) appStatus(server, app string) (ops.AppStatus, bool) {
	report, ok := m.statuses[server]
	if !ok {
		return ops.AppStatus{}, false
	}
	for _, st := range report.Apps {
		if st.App == app {
			return st, true
		}
	}
	return ops.AppStatus{}, false
}

func (m *model) statusText(server string) (string, string) {
	report, ok := m.statuses[server]
	if m.statusLoading[server] && !ok {
		return "…", "muted"
	}
	if errText := m.statusErr[server]; errText != "" {
		return "unreachable", "error"
	}
	if !ok {
		return "unknown", "muted"
	}
	text := report.Proxy
	if text == "" {
		text = "unknown"
	}
	kind := "muted"
	switch text {
	case "running":
		kind = "success"
	case "missing", "stopped":
		kind = "error"
	}
	if report.RebootRequired {
		text += " · reboot"
		if kind == "success" {
			kind = "warning"
		}
	}
	return text, kind
}

func (m *model) serverHost(server string) string {
	srv, err := m.backend.Server(server)
	if err != nil {
		return server
	}
	if i := strings.LastIndex(srv.Host, "@"); i >= 0 {
		return srv.Host[i+1:]
	}
	return srv.Host
}

func (m *model) activityByID(id int) *activity { return m.byID[id] }

func (m *model) selectedActivity() *activity {
	if len(m.activities) == 0 {
		return nil
	}
	m.activityCur = clampIndex(m.activityCur, len(m.activities))
	return m.activities[m.activityCur]
}

func (m *model) appDomains(name string) string {
	srv := m.currentServer()
	if srv == nil {
		return ""
	}
	app, err := srv.App(name)
	if err != nil {
		return ""
	}
	return strings.Join(app.Domains, ", ")
}

func (m *model) appType(name string) string {
	srv := m.currentServer()
	if srv == nil {
		return ""
	}
	app, err := srv.App(name)
	if err != nil {
		return ""
	}
	if app.Source.Type == "image" {
		return "image"
	}
	return "release"
}

func (m *model) activityTime(a *activity) string {
	if a == nil {
		return ""
	}
	return a.start.Format("15:04:05")
}

func (m *model) summaryLine() string {
	return fmt.Sprintf("%d servers · %s", len(m.backend.Servers()), m.configPathLabel())
}
