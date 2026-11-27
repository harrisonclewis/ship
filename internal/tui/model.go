package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"

	"github.com/harrisonclewis/ship/internal/config"
	"github.com/harrisonclewis/ship/internal/localedit"
	"github.com/harrisonclewis/ship/internal/ops"
	"github.com/harrisonclewis/ship/internal/yamldoc"
)

type pageKind int

const (
	pageServers pageKind = iota
	pageActivity
	pageServer
	pageServerActions
	pageAppActions
	pageAppEdit
	pageOutput
	pageRestorePick
	pageCompare
	pageDeployPick
)

type serverAction int

const (
	saDeployAll serverAction = iota
	saBackupAll
	saProxyRestart
	saProxySync
	saSetup
	saServerSettings
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
	{"p", "Restart Caddy", "Render the Caddyfile and recreate Caddy so routing and image changes apply", saProxyRestart, false},
	{"P", "Sync Caddy (reload)", "Render the Caddyfile and reload Caddy without recreating it", saProxySync, false},
	{"s", "Setup", "Prepare packages, users, Docker and the backup timer", saSetup, false},
	{"g", "Server settings", "Change admin_user and caddy_image on the server", saServerSettings, false},
	{"l", "Lock SSH", "Disable password and root SSH logins", saLockSSH, true},
	{"u", "VPS update", "apt update, full upgrade and autoremove", saVPSUpdate, false},
	{"R", "Reboot", "Reboot the server", saReboot, true},
	{"a", "Add app", "Add an application to this server", saAddApp, false},
	{"e", "Edit server", "Change host or identity file in ship.yaml", saEditServer, false},
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
	aaEditCompose
	aaEditDockerfile
	aaDelete
	aaCompare
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
	{"v", "Deploy version…", "Git: pick a commit, release or asset; otherwise a release tag or loaded image", aaDeployVersion, false},
	{"o", "Rollback", "Previous image; the database is not restored", aaRollback, true},
	{"s", "Restart", "Recreate the app containers", aaRestart, false},
	{"m", "Maintenance on/off", "Toggle the maintenance page", aaMaintenance, false},
	{"b", "Backup", "Archive now and download", aaBackup, false},
	{"L", "Backups", "List archives stored on the server", aaBackups, false},
	{"t", "Restore", "Restore a server archive or a local file", aaRestore, true},
	{"g", "Logs", "Follow logs in the terminal", aaLogs, false},
	{"e", "Edit env", "sudoedit the app's secret .env", aaEnvEdit, false},
	{"i", "Edit…", "Source, domains, commands, YAML or all settings", aaEdit, false},
	{"f", "Edit compose.yaml", "Open the server's desired compose file", aaEditCompose, false},
	{"x", "Delete app", "Remove its desired configuration and stop its stack", aaDelete, true},
	{"c", "Compare with running", "Show the diff from running to desired", aaCompare, false},
}

var dockerfileAppAction = appActionDef{"F", "Edit Dockerfile", "Open the server's desired Dockerfile", aaEditDockerfile, false}

type appEditAction int

const (
	eaSource appEditAction = iota
	eaRouting
	eaCommands
	eaYAML
	eaAll
)

type appEditActionDef struct {
	key    string
	label  string
	desc   string
	action appEditAction
}

var appEditActionDefs = []appEditActionDef{
	{"s", "Source", "Image source path, or release repo and asset", eaSource},
	{"d", "Domains & redirects", "Change the app's public hostnames", eaRouting},
	{"c", "Build & deploy commands", "Build script, deploy script and maintenance page", eaCommands},
	{"y", "Edit app YAML", "Open only this app's mapping in your editor", eaYAML},
	{"a", "All settings", "Open the full app form", eaAll},
}

var compareAppAction = appActionDef{"c", "Compare with running", "Show the diff from running to desired", aaCompare, false}

type confirmDialog struct {
	title      string
	body       string
	script     string
	scroll     int
	scrollable bool
	yes        func() tea.Cmd
	no         func() tea.Cmd
	ack        bool
	yesHint    string
	noHint     string
}

type editKind int

const (
	editShipConfig editKind = iota
	editDesiredCompose
	editDesiredDockerfile
	editDesiredRecord
)

type editTarget struct {
	path      string
	label     string
	validator localedit.Validator
	kind      editKind
	server    string
}

type editBuffer interface {
	TempPath() string
	Changed() (bool, error)
	Validate(localedit.Validator) error
	Read() ([]byte, error)
	Close() error
}

type editSession struct {
	target  editTarget
	session editBuffer
	editor  localedit.Editor
	commit  func([]byte) error
}

type model struct {
	width  int
	height int
	dryRun bool

	backend Backend
	cfg     *config.Config
	doc     *yamldoc.Doc
	path    string

	page          pageKind
	tab           int
	serverCursor  int
	activityCur   int
	appCursor     int
	serverActCur  int
	appActCur     int
	appEditCur    int
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

	variances       map[string]ops.VarianceReport
	varianceErr     map[string]string
	varianceLoading map[string]bool

	compareApp  string
	compareText string

	backups        []BackupEntry
	backupsLoading bool
	backupsErr     string
	backupsServer  string
	backupsApp     string

	pickServer   string
	pickApp      string
	pickTab      int
	pickCursor   int
	pickCommits  []ops.GitCommit
	pickReleases []ops.GitRelease
	pickLoading  [2]bool
	pickLoaded   [2]bool
	pickErr      [2]string

	pickReleaseTag  string
	pickAssets      []ops.GitAsset
	pickAssetLoad   bool
	pickAssetErr    string
	pickChoiceCur   int
	pickAssetsReady bool

	vp            viewport.Model
	spinnerFrame  int
	tickScheduled bool

	form          *formHost
	confirm       *confirmDialog
	edit          *editSession
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

type varianceMsg struct {
	server string
	report ops.VarianceReport
	err    error
}

type saveDoneMsg struct {
	form    *formHost
	err     error
	warning string
}

type gitCommitsMsg struct {
	server string
	app    string
	items  []ops.GitCommit
	err    error
}

type gitReleasesMsg struct {
	server string
	app    string
	items  []ops.GitRelease
	err    error
}

type gitAssetsMsg struct {
	server string
	app    string
	tag    string
	items  []ops.GitAsset
	err    error
}

type deleteDoneMsg struct {
	server string
	app    string
	err    error
}

type buildApprovedMsg struct {
	req Request
	err error
}

// appEntry is one desired app shown on the server page.
type appEntry struct {
	Name string
}

type terminalReadyMsg struct {
	req     TerminalRequest
	cmd     *exec.Cmd
	display string
	err     error
}

type editDoneMsg struct {
	err error
}

func newModel(cfg *config.Config, backend Backend, doc *yamldoc.Doc, path string, dryRun bool) *model {
	m := &model{
		width:           80,
		height:          24,
		dryRun:          dryRun,
		backend:         backend,
		cfg:             cfg,
		doc:             doc,
		path:            path,
		page:            pageServers,
		activityCur:     0,
		byID:            map[int]*activity{},
		runs:            map[string]int{},
		statuses:        map[string]ops.StatusReport{},
		statusErr:       map[string]string{},
		statusLoading:   map[string]bool{},
		variances:       map[string]ops.VarianceReport{},
		varianceErr:     map[string]string{},
		varianceLoading: map[string]bool{},
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
	if m.page == pageCompare {
		atTop := m.vp.YOffset == 0
		m.vp.SetContent(ansi.Wrap(m.compareText, w, " /,'"))
		if atTop {
			m.vp.GotoTop()
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
	case varianceMsg:
		delete(m.varianceLoading, msg.server)
		if msg.err != nil {
			m.varianceErr[msg.server] = msg.err.Error()
			return m, nil
		}
		delete(m.varianceErr, msg.server)
		m.variances[msg.server] = msg.report
		return m, nil
	case saveDoneMsg:
		return m, m.finishSave(msg)
	case deleteDoneMsg:
		return m, m.finishDelete(msg)
	case buildApprovedMsg:
		if msg.err != nil {
			m.showToast(toastError, msg.err.Error())
			return m, nil
		}
		return m, m.startRun(msg.req)
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
	case gitCommitsMsg:
		if m.page != pageDeployPick || msg.server != m.pickServer || msg.app != m.pickApp {
			return m, nil
		}
		m.pickLoading[0] = false
		m.pickLoaded[0] = true
		m.pickCommits = msg.items
		if msg.err != nil {
			m.pickErr[0] = msg.err.Error()
		} else {
			m.pickErr[0] = ""
		}
		m.pickCursor = clampIndex(m.pickCursor, m.pickLength())
		return m, nil
	case gitReleasesMsg:
		if m.page != pageDeployPick || msg.server != m.pickServer || msg.app != m.pickApp {
			return m, nil
		}
		m.pickLoading[1] = false
		m.pickLoaded[1] = true
		m.pickReleases = msg.items
		if msg.err != nil {
			m.pickErr[1] = msg.err.Error()
		} else {
			m.pickErr[1] = ""
		}
		m.pickCursor = clampIndex(m.pickCursor, m.pickLength())
		return m, nil
	case gitAssetsMsg:
		if m.page != pageDeployPick || msg.server != m.pickServer || msg.app != m.pickApp || msg.tag != m.pickReleaseTag {
			return m, nil
		}
		m.pickAssetLoad = false
		m.pickAssetsReady = true
		m.pickAssets = msg.items
		if msg.err != nil {
			m.pickAssetErr = msg.err.Error()
		} else {
			m.pickAssetErr = ""
		}
		m.pickChoiceCur = 0
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
	case editDoneMsg:
		return m, m.finishLocalEdit(msg)
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
		if m.form.saving {
			return m, nil
		}
		if key == "esc" {
			m.cancelForm()
			return m, nil
		}
		return m, m.updateForm(msg)
	}

	if m.confirm != nil {
		if m.confirm.ack {
			switch key {
			case "y", "enter", "n", "esc", "q":
				m.confirm = nil
				m.offerPendingReboot()
			}
			return m, nil
		}
		if m.confirm.scrollable {
			lines := strings.Split(m.confirm.script, "\n")
			switch key {
			case "up", "k":
				if m.confirm.scroll > 0 {
					m.confirm.scroll--
				}
				return m, nil
			case "down", "j":
				if m.confirm.scroll < len(lines)-1 {
					m.confirm.scroll++
				}
				return m, nil
			case "pgup":
				m.confirm.scroll -= 5
				if m.confirm.scroll < 0 {
					m.confirm.scroll = 0
				}
				return m, nil
			case "pgdown":
				m.confirm.scroll += 5
				if m.confirm.scroll > len(lines)-1 {
					m.confirm.scroll = len(lines) - 1
				}
				return m, nil
			}
		}
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
			no := m.confirm.no
			m.confirm = nil
			if no != nil {
				cmd := no()
				m.offerPendingReboot()
				return m, cmd
			}
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
	case pageAppEdit:
		return m.handleAppEditKey(key)
	case pageOutput:
		return m.handleOutputKey(msg, key)
	case pageRestorePick:
		return m.handleRestoreKey(key)
	case pageCompare:
		return m.handleCompareKey(msg, key)
	case pageDeployPick:
		return m.handleDeployPickKey(key)
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
	case "e":
		if m.page == pageServers {
			return m, m.beginLocalEdit(editTarget{
				path:      m.path,
				label:     m.path,
				validator: localedit.ValidateShipYAML,
				kind:      editShipConfig,
			})
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
		m.appCursor = clampIndex(m.appCursor-1, len(m.appEntries()))
	case "down", "j":
		m.appCursor = clampIndex(m.appCursor+1, len(m.appEntries()))
	case "enter":
		entries := m.appEntries()
		if len(entries) > 0 {
			m.appCursor = clampIndex(m.appCursor, len(entries))
			m.appName = entries[m.appCursor].Name
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
		return m, m.runServerAction(saProxyRestart)
	case "P":
		return m, m.runServerAction(saProxySync)
	case "s":
		return m, m.runServerAction(saSetup)
	case "g":
		return m, m.runServerAction(saServerSettings)
	case "l":
		return m, m.runServerAction(saLockSSH)
	case "u":
		return m, m.runServerAction(saVPSUpdate)
	case "R":
		return m, m.runServerAction(saReboot)
	case "x":
		return m, m.runServerAction(saDeleteServer)
	case "r":
		return m, m.refreshServer(m.serverName)
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
	defs := m.appActions()
	switch key {
	case "esc":
		m.page = pageServer
		return m, nil
	case "up", "k":
		m.appActCur = clampIndex(m.appActCur-1, len(defs))
		return m, nil
	case "down", "j":
		m.appActCur = clampIndex(m.appActCur+1, len(defs))
		return m, nil
	case "enter":
		if len(defs) > 0 {
			m.appActCur = clampIndex(m.appActCur, len(defs))
			return m, m.runAppAction(defs[m.appActCur].action)
		}
	}
	for i, def := range defs {
		if key == def.key {
			m.appActCur = i
			return m, m.runAppAction(def.action)
		}
	}
	return m, nil
}

func (m *model) handleAppEditKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.page = pageAppActions
		return m, nil
	case "up", "k":
		m.appEditCur = clampIndex(m.appEditCur-1, len(appEditActionDefs))
		return m, nil
	case "down", "j":
		m.appEditCur = clampIndex(m.appEditCur+1, len(appEditActionDefs))
		return m, nil
	case "enter":
		m.appEditCur = clampIndex(m.appEditCur, len(appEditActionDefs))
		return m, m.runAppEditAction(appEditActionDefs[m.appEditCur].action)
	}
	for i, def := range appEditActionDefs {
		if key == def.key {
			m.appEditCur = i
			return m, m.runAppEditAction(def.action)
		}
	}
	return m, nil
}

func (m *model) runAppEditAction(action appEditAction) tea.Cmd {
	switch action {
	case eaSource, eaRouting, eaCommands:
		m.openAppQuickForm(action)
	case eaYAML:
		return m.beginAppYAMLEdit(m.serverName, m.appName)
	case eaAll:
		m.openAppForm(m.appName, false)
	}
	return nil
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

func (m *model) appGitRepo(name string) (config.GitRepo, bool) {
	rec := m.recordFor(m.serverName, name)
	if rec == nil || rec.Source.Type != "image" {
		return config.GitRepo{}, false
	}
	srv := m.currentServer()
	if srv == nil {
		return config.GitRepo{}, false
	}
	return config.GitSource(srv.Sources[name])
}

func (m *model) openDeployPicker() tea.Cmd {
	if _, ok := m.appGitRepo(m.appName); !ok {
		return nil
	}
	m.pickServer = m.serverName
	m.pickApp = m.appName
	m.pickTab = 0
	m.pickCursor = 0
	m.pickCommits = nil
	m.pickReleases = nil
	m.pickLoading = [2]bool{}
	m.pickLoaded = [2]bool{}
	m.pickErr = [2]string{}
	m.pickReleaseTag = ""
	m.pickAssets = nil
	m.pickAssetLoad = false
	m.pickAssetErr = ""
	m.pickChoiceCur = 0
	m.pickAssetsReady = false
	m.page = pageDeployPick
	return m.loadDeployTab(0, true)
}

func (m *model) loadDeployTab(tab int, force bool) tea.Cmd {
	if tab < 0 || tab > 1 {
		return nil
	}
	if !force && (m.pickLoaded[tab] || m.pickLoading[tab]) {
		return nil
	}
	m.pickLoading[tab] = true
	m.pickErr[tab] = ""
	backend := m.backend
	server, app := m.pickServer, m.pickApp
	if tab == 0 {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			items, err := backend.GitCommits(ctx, server, app)
			return gitCommitsMsg{server: server, app: app, items: items, err: err}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := backend.GitReleases(ctx, server, app)
		return gitReleasesMsg{server: server, app: app, items: items, err: err}
	}
}

func (m *model) pickLength() int {
	if m.pickTab == 0 {
		return len(m.pickCommits)
	}
	return len(m.pickReleases)
}

func (m *model) loadReleaseAssets(tag string) tea.Cmd {
	m.pickAssetLoad = true
	m.pickAssetsReady = false
	m.pickAssetErr = ""
	m.pickAssets = nil
	backend := m.backend
	server, app := m.pickServer, m.pickApp
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := backend.GitReleaseAssets(ctx, server, app, tag)
		return gitAssetsMsg{server: server, app: app, tag: tag, items: items, err: err}
	}
}

func (m *model) pickChoiceLength() int {
	if !m.pickAssetsReady {
		return 0
	}
	// Build source first, then assets.
	return 1 + len(m.pickAssets)
}

func (m *model) handleDeployPickKey(key string) (tea.Model, tea.Cmd) {
	if m.pickReleaseTag != "" {
		// Choice view for one selected release.
		switch key {
		case "esc":
			m.pickReleaseTag = ""
			m.pickAssets = nil
			m.pickAssetsReady = false
			m.pickAssetErr = ""
			m.pickChoiceCur = 0
			return m, nil
		case "up", "k":
			m.pickChoiceCur = clampIndex(m.pickChoiceCur-1, m.pickChoiceLength())
		case "down", "j":
			m.pickChoiceCur = clampIndex(m.pickChoiceCur+1, m.pickChoiceLength())
		case "r":
			return m, m.loadReleaseAssets(m.pickReleaseTag)
		case "enter":
			if m.pickAssetLoad || m.pickChoiceLength() == 0 {
				return m, nil
			}
			m.pickChoiceCur = clampIndex(m.pickChoiceCur, m.pickChoiceLength())
			version := "release:" + m.pickReleaseTag
			if m.pickChoiceCur > 0 {
				version += ":" + m.pickAssets[m.pickChoiceCur-1].Name
			}
			m.page = pageAppActions
			return m, m.startRun(Request{Action: ActDeploy, Server: m.pickServer, App: m.pickApp, Version: version})
		}
		return m, nil
	}
	switch key {
	case "esc":
		m.page = pageAppActions
		return m, nil
	case "tab", "right", "l", "1", "2":
		next := 1 - m.pickTab
		if key == "1" {
			next = 0
		} else if key == "2" {
			next = 1
		}
		m.pickTab = next
		m.pickCursor = 0
		return m, m.loadDeployTab(next, false)
	case "left", "h":
		if m.pickTab != 0 {
			m.pickTab = 0
			m.pickCursor = 0
			return m, m.loadDeployTab(0, false)
		}
	case "up", "k":
		m.pickCursor = clampIndex(m.pickCursor-1, m.pickLength())
	case "down", "j":
		m.pickCursor = clampIndex(m.pickCursor+1, m.pickLength())
	case "r":
		return m, m.loadDeployTab(m.pickTab, true)
	case "enter":
		if m.pickLoading[m.pickTab] || m.pickLength() == 0 {
			return m, nil
		}
		m.pickCursor = clampIndex(m.pickCursor, m.pickLength())
		if m.pickTab == 0 {
			version := "commit:" + m.pickCommits[m.pickCursor].SHA
			m.page = pageAppActions
			return m, m.startRun(Request{Action: ActDeploy, Server: m.pickServer, App: m.pickApp, Version: version})
		}
		m.pickReleaseTag = m.pickReleases[m.pickCursor].Tag
		m.pickChoiceCur = 0
		return m, m.loadReleaseAssets(m.pickReleaseTag)
	}
	return m, nil
}

func (m *model) handleCompareKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.page = pageServer
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
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

func (m *model) serverConfig(server string) *ops.ServerConfig {
	report, ok := m.variances[server]
	if !ok {
		return nil
	}
	return report.State
}

func (m *model) currentServer() *config.Server {
	srv, err := m.backend.Server(m.serverName)
	if err != nil {
		return nil
	}
	return srv
}

func (m *model) desiredApp(server, app string) *ops.DesiredApp {
	state := m.serverConfig(server)
	if state == nil {
		return nil
	}
	return state.Apps[app]
}

func (m *model) recordFor(server, app string) *config.App {
	da := m.desiredApp(server, app)
	if da == nil {
		return nil
	}
	return da.Record
}

func (m *model) currentApp() *config.App {
	return m.recordFor(m.serverName, m.appName)
}

// appEntries lists the apps in the server's desired configuration.
func (m *model) appEntries() []appEntry {
	state := m.serverConfig(m.serverName)
	if state == nil {
		return nil
	}
	names := state.AppNames()
	out := make([]appEntry, 0, len(names))
	for _, name := range names {
		out = append(out, appEntry{Name: name})
	}
	return out
}

func (m *model) varianceFor(server, app string) (ops.AppVariance, bool) {
	report, ok := m.variances[server]
	if !ok {
		return ops.AppVariance{}, false
	}
	for _, v := range report.Apps {
		if v.App == app {
			return v, true
		}
	}
	return ops.AppVariance{}, false
}

func (m *model) configState(server, app string) string {
	if v, ok := m.varianceFor(server, app); ok {
		return v.State
	}
	return ""
}

func (m *model) recordsFor(state *ops.ServerConfig, name string, record *config.App) (map[string]*config.App, error) {
	if state == nil {
		return nil, errors.New("server configuration is still loading; try again")
	}
	records := make(map[string]*config.App, len(state.Apps)+1)
	for n, da := range state.Apps {
		if n == name {
			continue
		}
		if da.Record != nil {
			records[n] = da.Record
		}
	}
	records[name] = record
	if err := config.ValidateRecords(records); err != nil {
		return nil, err
	}
	return records, nil
}

func (m *model) loadVariances(server string) tea.Cmd {
	if server == "" || m.varianceLoading[server] {
		return nil
	}
	m.varianceLoading[server] = true
	backend := m.backend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		report, err := backend.Variances(ctx, server)
		return varianceMsg{server: server, report: report, err: err}
	}
}

func (m *model) refreshServer(server string) tea.Cmd {
	if server == "" {
		return nil
	}
	return tea.Batch(m.loadStatus(server), m.loadVariances(server))
}

func (m *model) openCompare() {
	v, ok := m.varianceFor(m.serverName, m.appName)
	if !ok {
		m.showToast(toastWarning, "Configuration is still loading; try again.")
		return
	}
	m.compareApp = m.appName
	m.page = pageCompare
	switch v.State {
	case ops.VarianceNotDeployed:
		m.compareText = "Not deployed yet; deploying applies this configuration.\n\n"
	case ops.VarianceApplied:
		m.compareText = "Running matches desired.\n\n"
	default:
		m.compareText = "Running differs from desired. Deploy to apply it.\n\n"
	}
	m.compareText += v.Diff
	if strings.TrimSpace(v.Diff) == "" && v.State == ops.VariancePending {
		m.compareText += "The record differs from the running record."
	}
	m.showCompare()
}

func (m *model) showCompare() {
	m.vp.SetContent(ansi.Wrap(m.compareText, m.vp.Width, " /,'"))
	m.vp.GotoTop()
}

func (m *model) openServer(name string) tea.Cmd {
	m.page = pageServer
	m.serverName = name
	m.appCursor = 0
	m.toast = nil
	return m.refreshServer(name)
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
	var approval *ops.BuildApprovalError
	if msg.err != nil {
		entry.state = runFailed
		if errors.Is(msg.err, context.Canceled) {
			m.showToast(toastWarning, entry.label+" on "+entry.req.Server+" was cancelled.")
		} else if errors.As(msg.err, &approval) {
			m.showToast(toastWarning, entry.label+" needs build approval.")
			m.openBuildApproval(approval, entry.req)
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
	switch entry.req.Action {
	case ActDeploy, ActDeployAll, ActProxySync, ActProxyRestart, ActSetup:
		if cmd := m.loadVariances(entry.req.Server); cmd != nil {
			cmds = append(cmds, cmd)
		}
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

func (m *model) openConfirmChoice(title, body string, yes, no func() tea.Cmd) {
	m.confirm = &confirmDialog{title: title, body: body, yes: yes, no: no, yesHint: "reopen", noHint: "discard"}
}

func (m *model) openNotice(title, body string) {
	m.confirm = &confirmDialog{title: title, body: body, ack: true}
}

func (m *model) openBuildApproval(e *ops.BuildApprovalError, req Request) {
	m.confirm = &confirmDialog{
		title:      "Approve build script",
		body:       "The build script for " + e.App + " on " + e.Server + " comes from the server and is not approved on this machine.",
		script:     e.Script,
		scrollable: true,
		yesHint:    "approve and deploy",
		noHint:     "cancel",
		yes: func() tea.Cmd {
			return m.approveBuild(e, req)
		},
	}
}

func (m *model) approveBuild(e *ops.BuildApprovalError, req Request) tea.Cmd {
	backend := m.backend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := backend.RecordBuildApproval(ctx, e.Server, e.App, e.Script)
		return buildApprovedMsg{req: req, err: err}
	}
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
	case saProxyRestart:
		return m.startRun(Request{Action: ActProxyRestart, Server: m.serverName})
	case saProxySync:
		return m.startRun(Request{Action: ActProxySync, Server: m.serverName})
	case saSetup:
		return m.startRun(Request{Action: ActSetup, Server: m.serverName})
	case saServerSettings:
		m.openServerSettingsForm(m.serverName)
		return nil
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

func (m *model) appActions() []appActionDef {
	defs := append([]appActionDef(nil), appActionDefs...)
	if _, ok := m.appGitRepo(m.appName); ok {
		defs[aaDeployLatest].label = "Deploy latest commit"
		defs[aaDeployLatest].desc = "Build the head of the remote default branch"
		defs[aaDeployVersion].desc = "Choose a commit or a GitHub release"
	}
	if rec := m.currentApp(); rec != nil && rec.Source.Type != "image" {
		// Keep the Dockerfile action next to the compose action.
		out := make([]appActionDef, 0, len(defs)+1)
		for _, def := range defs {
			out = append(out, def)
			if def.action == aaEditCompose {
				out = append(out, dockerfileAppAction)
			}
		}
		return out
	}
	return defs
}

func (m *model) runAppAction(action appAction) tea.Cmd {
	server, app := m.serverName, m.appName
	switch action {
	case aaDeployLatest:
		return m.startRun(Request{Action: ActDeploy, Server: server, App: app})
	case aaDeployVersion:
		if _, ok := m.appGitRepo(app); ok {
			return m.openDeployPicker()
		}
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
		m.page = pageAppEdit
		m.appEditCur = 0
	case aaEditCompose:
		return m.beginDesiredFileEdit(server, app, "compose")
	case aaEditDockerfile:
		return m.beginDesiredFileEdit(server, app, "Dockerfile")
	case aaDelete:
		m.openConfirm("Delete app", "Remove "+app+"'s desired configuration from "+server+"? Its stack is stopped; data directories and .env are kept.", func() tea.Cmd {
			return m.deleteApp(server, app)
		})
	case aaCompare:
		m.openCompare()
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

func (m *model) beginLocalEdit(target editTarget) tea.Cmd {
	if m.edit != nil {
		m.showToast(toastWarning, "Finish the open editor first.")
		return nil
	}
	editor, err := localedit.Resolve()
	if err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	session, err := localedit.Begin(target.path)
	if err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	m.edit = &editSession{target: target, session: session, editor: editor, commit: func([]byte) error {
		return session.Save()
	}}
	return m.execEditor()
}

func (m *model) beginAppYAMLEdit(server, app string) tea.Cmd {
	da := m.desiredApp(server, app)
	if da == nil {
		m.showToast(toastWarning, "Configuration is still loading; try again.")
		return nil
	}
	if da.Record == nil {
		reason := da.RecordErr
		if reason == "" {
			reason = "the record is missing"
		}
		m.showToast(toastError, "The record for "+app+" is unreadable: "+reason)
		return nil
	}
	data, err := da.Record.RecordYAML()
	if err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	state := m.serverConfig(server)
	validate := func(data []byte) error {
		record, err := config.DecodeAppRecord(bytes.NewReader(data))
		if err != nil {
			return err
		}
		_, err = m.recordsFor(state, app, record)
		return err
	}
	commit := func(data []byte) error {
		record, err := config.DecodeAppRecord(bytes.NewReader(data))
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err = m.backend.PutDesiredApp(ctx, server, app, record, da.Compose, da.Dockerfile, state.Arch, da.Hash)
		return err
	}
	return m.beginBufferEdit(server, app+".yaml", "server "+server+"/"+app+" record", data, validate, editDesiredRecord, commit)
}

func (m *model) beginDesiredFileEdit(server, app, file string) tea.Cmd {
	da := m.desiredApp(server, app)
	if da == nil {
		m.showToast(toastWarning, "Configuration is still loading; try again.")
		return nil
	}
	if da.Record == nil {
		m.showToast(toastError, "The record for "+app+" is unreadable; edit the app record first.")
		return nil
	}
	state := m.serverConfig(server)
	data := da.Compose
	label := "apps/" + app + "/compose.yaml"
	name := "compose.yaml"
	var validator localedit.Validator = localedit.ValidateCompose
	kind := editDesiredCompose
	if file == "Dockerfile" {
		data = da.Dockerfile
		label = "apps/" + app + "/Dockerfile"
		name = "Dockerfile"
		validator = nil
		kind = editDesiredDockerfile
	}
	commit := func(edited []byte) error {
		compose, dockerfile := da.Compose, da.Dockerfile
		if file == "compose" {
			compose = edited
		} else {
			dockerfile = edited
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := m.backend.PutDesiredApp(ctx, server, app, da.Record, compose, dockerfile, state.Arch, da.Hash)
		return err
	}
	return m.beginBufferEdit(server, name, label, data, validator, kind, commit)
}

func (m *model) beginBufferEdit(server, name, label string, data []byte, validator localedit.Validator, kind editKind, commit func([]byte) error) tea.Cmd {
	if m.edit != nil {
		m.showToast(toastWarning, "Finish the open editor first.")
		return nil
	}
	editor, err := localedit.Resolve()
	if err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	buffer, err := localedit.BeginBuffer(name, data)
	if err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	m.edit = &editSession{
		target:  editTarget{label: label, validator: validator, kind: kind, server: server},
		session: buffer,
		editor:  editor,
		commit:  commit,
	}
	return m.execEditor()
}

func (m *model) execEditor() tea.Cmd {
	if m.edit == nil {
		return nil
	}
	cmd := m.edit.editor.Command(m.edit.session.TempPath())
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editDoneMsg{err: err}
	})
}

func (m *model) finishLocalEdit(msg editDoneMsg) tea.Cmd {
	e := m.edit
	if e == nil {
		return nil
	}
	if msg.err != nil {
		m.edit = nil
		m.openNotice("Editor failed", fmt.Sprintf("%v\n\nYour edits are in %s", msg.err, e.session.TempPath()))
		return nil
	}
	changed, err := e.session.Changed()
	if err != nil {
		m.edit = nil
		m.openNotice("Edit failed", fmt.Sprintf("%v\n\nYour edits are in %s", err, e.session.TempPath()))
		return nil
	}
	if !changed {
		_ = e.session.Close()
		m.edit = nil
		m.showToast(toastInfo, "No changes to "+e.target.label+".")
		return nil
	}
	if err := e.session.Validate(e.target.validator); err != nil {
		m.openConfirmChoice("Invalid "+e.target.label,
			fmt.Sprintf("%v\n\nYour edits are kept in %s.", err, e.session.TempPath()),
			func() tea.Cmd { return m.execEditor() },
			func() tea.Cmd {
				if m.edit != nil {
					_ = m.edit.session.Close()
					m.edit = nil
				}
				m.showToast(toastInfo, "Discarded changes to "+e.target.label+".")
				return nil
			})
		return nil
	}
	data, err := e.session.Read()
	if err != nil {
		m.edit = nil
		m.openNotice("Edit failed", fmt.Sprintf("%v\n\nYour edits are in %s", err, e.session.TempPath()))
		return nil
	}
	if err := e.commit(data); err != nil {
		m.edit = nil
		if errors.Is(err, localedit.ErrChanged) {
			m.openNotice("Not saved", fmt.Sprintf("%s changed on disk while you were editing; not saved.\n\nYour edits are in %s", e.target.label, e.session.TempPath()))
			return nil
		}
		if errors.Is(err, ops.ErrConfigConflict) {
			_ = e.session.Close()
			m.openNotice("Changed on the server", "This configuration changed on the server since you loaded it. Nothing was saved; reloading the latest version.")
			return m.refreshServer(e.target.server)
		}
		m.openNotice("Could not save", fmt.Sprintf("%v\n\nYour edits are in %s", err, e.session.TempPath()))
		return nil
	}
	_ = e.session.Close()
	m.edit = nil
	if e.target.kind == editShipConfig {
		if err := m.reloadConfig(); err != nil {
			m.showToast(toastError, err.Error())
			return nil
		}
		m.showToast(toastSuccess, "Saved "+e.target.label+".")
		return m.refreshAllStatuses()
	}
	m.showToast(toastSuccess, "Saved "+e.target.label+".")
	return m.refreshServer(e.target.server)
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

func (m *model) openAppQuickForm(action appEditAction) {
	if m.currentApp() == nil {
		m.showToast(toastWarning, "This app's configuration is not loaded yet.")
		return
	}
	v := m.newAppFormValues(m.serverName, m.appName, false)
	var purpose formPurpose
	switch action {
	case eaSource:
		purpose = formEditAppSource
	case eaRouting:
		purpose = formEditAppDomains
	default:
		purpose = formEditAppCommands
	}
	f := &formHost{purpose: purpose, app: v, serverName: m.serverName, appName: m.appName}
	f.form = m.buildForm(f)
	m.form = f
}

func (m *model) openServerSettingsForm(server string) {
	state := m.serverConfig(server)
	if state == nil {
		m.showToast(toastWarning, "Server settings are still loading; try again.")
		return
	}
	v := &serverFormValues{
		Name:       server,
		AdminUser:  state.Settings.AdminUser,
		CaddyImage: state.Settings.CaddyImage,
	}
	f := &formHost{purpose: formServerSettings, server: v, serverName: server}
	f.form = m.buildForm(f)
	m.form = f
}

func (m *model) openAppForm(name string, isNew bool) {
	if m.serverName == "" {
		m.showToast(toastWarning, "Open a server first.")
		return
	}
	if !isNew && m.recordFor(m.serverName, name) == nil {
		m.showToast(toastWarning, "This app's configuration is not loaded or its record is unreadable.")
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
	if f == nil || f.saving {
		return nil
	}
	switch f.purpose {
	case formAddServer, formEditServer:
		return m.submitServerForm(f)
	case formServerSettings:
		if m.serverConfig(f.serverName) == nil {
			m.showToast(toastWarning, "Server settings are still loading; try again.")
			return nil
		}
		f.saving = true
		return m.saveServerSettingsCmd(f)
	case formAddApp, formEditApp:
		f.submitValues()
		if m.serverConfig(f.app.Server) == nil {
			m.showToast(toastWarning, "Configuration is still loading; try again.")
			return nil
		}
		f.saving = true
		return m.saveAppFormCmd(f)
	case formEditAppSource, formEditAppDomains, formEditAppCommands:
		f.submitValues()
		if m.serverConfig(f.app.Server) == nil {
			m.showToast(toastWarning, "Configuration is still loading; try again.")
			return nil
		}
		f.saving = true
		return m.saveQuickAppCmd(f)
	case formDeployVersion:
		version := formText(f.text)
		if version != "" {
			rec := m.recordFor(f.serverName, f.appName)
			if rec == nil {
				f.err = "This app's configuration is not loaded."
				f.form = m.buildForm(f)
				return nil
			}
			var verify error
			if rec.Source.Type == "image" {
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

func (m *model) submitServerForm(f *formHost) tea.Cmd {
	v := *f.server
	v.Name = strings.TrimSpace(v.Name)
	v.Host = strings.TrimSpace(v.Host)
	v.IdentityFile = strings.TrimSpace(v.IdentityFile)
	v.User = strings.TrimSpace(v.User)
	if v.Connection == "" {
		v.Connection = config.ConnectionRemote
	}
	if v.Connection == config.ConnectionLocal {
		v.Host, v.IdentityFile = "", ""
	} else {
		v.User = ""
	}
	// Server-wide settings live on the server, not in the local file.
	v.AdminUser, v.Arch, v.CaddyImage = "", "", ""
	tmp := m.doc.Clone()
	if err := tmp.ApplyServer(v); err != nil {
		f.err = err.Error()
		f.form = m.buildForm(f)
		return nil
	}
	if err := tmp.Save(); err != nil {
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
	name := v.Name
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
}

func (m *model) saveServerSettingsCmd(f *formHost) tea.Cmd {
	server := f.serverName
	state := m.serverConfig(server)
	expected := ""
	if state != nil {
		expected = state.Settings.Hash
	}
	admin := strings.TrimSpace(f.server.AdminUser)
	caddy := strings.TrimSpace(f.server.CaddyImage)
	if admin == "" {
		admin = config.DefaultAdminUser
	}
	if caddy == "" {
		caddy = config.DefaultCaddyImage
	}
	backend := m.backend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := backend.PutServerSettings(ctx, server, admin, caddy, expected)
		return saveDoneMsg{form: f, err: err}
	}
}

func (m *model) saveAppFormCmd(f *formHost) tea.Cmd {
	v := *f.app
	v.Name = strings.TrimSpace(v.Name)
	v.Server = strings.TrimSpace(v.Server)
	server, name := v.Server, v.Name
	state := m.serverConfig(server)
	backend := m.backend
	path := m.path
	isNew := f.purpose == formAddApp
	var expected string
	var compose, dockerfile []byte
	if !isNew {
		da := m.desiredApp(server, v.OriginalName)
		if da == nil {
			return func() tea.Msg {
				return saveDoneMsg{form: f, err: fmt.Errorf("app %q is no longer configured on %s", v.OriginalName, server)}
			}
		}
		if name != v.OriginalName {
			return func() tea.Msg {
				return saveDoneMsg{form: f, err: errors.New("app names cannot be changed; create the new app and delete the old one")}
			}
		}
		expected = da.Hash
		compose = da.Compose
		dockerfile = da.Dockerfile
	} else {
		if state != nil && state.Apps[name] != nil {
			return func() tea.Msg {
				return saveDoneMsg{form: f, err: fmt.Errorf("app %q already exists on %s", name, server)}
			}
		}
		compose = []byte("name: " + name + "\nservices:\n  " + name + ":\n    image: \"${IMAGE:?IMAGE is set by ship}\"\n")
	}
	localValue, localBuildDir, keepLocal, localErr := localSourceFromForm(&v)
	if localErr != nil {
		return func() tea.Msg { return saveDoneMsg{form: f, err: localErr} }
	}
	hadLocal := false
	if srv, err := m.backend.Server(server); err == nil {
		_, hadLocal = srv.Sources[name]
		if !hadLocal {
			_, hadLocal = srv.BuildDirs[name]
		}
	}
	arch := config.DefaultArch
	if state != nil && state.Arch != "" {
		arch = state.Arch
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		record, err := appFromForm(&v)
		if err != nil {
			return saveDoneMsg{form: f, err: err}
		}
		if _, err := m.recordsFor(state, name, record); err != nil {
			return saveDoneMsg{form: f, err: err}
		}
		if _, err := backend.PutDesiredApp(ctx, server, name, record, compose, dockerfile, arch, expected); err != nil {
			return saveDoneMsg{form: f, err: err}
		}
		var warning string
		if keepLocal || hadLocal {
			if err := config.SetLocalSourceEntry(path, server, name, localValue, localBuildDir, !keepLocal); err != nil {
				warning = "Saved on the server, but could not update the local source: " + err.Error()
			}
		}
		return saveDoneMsg{form: f, warning: warning}
	}
}

func (m *model) saveQuickAppCmd(f *formHost) tea.Cmd {
	v := *f.app
	server, app := v.Server, v.Name
	state := m.serverConfig(server)
	backend := m.backend
	path := m.path
	da := m.desiredApp(server, app)
	if da == nil || da.Record == nil {
		return func() tea.Msg {
			return saveDoneMsg{form: f, err: fmt.Errorf("app %q is no longer configured on %s", app, server)}
		}
	}
	var localValue, localBuildDir string
	var keepLocal bool
	if f.purpose == formEditAppSource {
		var localErr error
		localValue, localBuildDir, keepLocal, localErr = localSourceFromForm(&v)
		if localErr != nil {
			return func() tea.Msg { return saveDoneMsg{form: f, err: localErr} }
		}
		// Directory and Git sources are both server-side image sources.
		// Changing only the local source value does not require a server write.
		if da.Record.Source.Type == "image" && (v.SourceKind == "dir" || v.SourceKind == "git") {
			return func() tea.Msg {
				if err := config.SetLocalSourceEntry(path, server, app, localValue, localBuildDir, !keepLocal); err != nil {
					return saveDoneMsg{form: f, err: err}
				}
				if _, err := config.Load(path); err != nil {
					return saveDoneMsg{form: f, err: err}
				}
				return saveDoneMsg{form: f}
			}
		}
	}
	arch := config.DefaultArch
	if state != nil && state.Arch != "" {
		arch = state.Arch
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		record, err := appFromForm(&v)
		if err != nil {
			return saveDoneMsg{form: f, err: err}
		}
		if _, err := m.recordsFor(state, app, record); err != nil {
			return saveDoneMsg{form: f, err: err}
		}
		if _, err := backend.PutDesiredApp(ctx, server, app, record, da.Compose, da.Dockerfile, arch, da.Hash); err != nil {
			return saveDoneMsg{form: f, err: err}
		}
		if f.purpose == formEditAppSource {
			if err := config.SetLocalSourceEntry(path, server, app, localValue, localBuildDir, !keepLocal); err != nil {
				return saveDoneMsg{form: f, warning: "Saved on the server, but could not update the local source: " + err.Error()}
			}
		}
		return saveDoneMsg{form: f}
	}
}

func (m *model) finishSave(msg saveDoneMsg) tea.Cmd {
	f := msg.form
	if f == nil || m.form != f {
		return nil
	}
	f.saving = false
	server := f.serverName
	if f.app != nil && f.app.Server != "" {
		server = f.app.Server
	}
	if msg.err != nil {
		if errors.Is(msg.err, ops.ErrConfigConflict) {
			m.form = nil
			m.openNotice("Changed on the server", "This configuration changed on the server since you loaded it. Nothing was saved; reloading the latest version.")
			return m.refreshServer(server)
		}
		f.err = msg.err.Error()
		f.form = m.buildForm(f)
		return nil
	}
	m.form = nil
	label := "Saved."
	if f.app != nil && f.app.Name != "" {
		label = "Saved " + f.app.Name + "."
	}
	if msg.warning != "" {
		m.showToast(toastWarning, msg.warning)
	} else {
		m.showToast(toastSuccess, label)
	}
	switch f.purpose {
	case formAddApp, formEditApp:
		if err := m.reloadConfig(); err != nil {
			m.showToast(toastError, err.Error())
		}
		m.page = pageServer
		m.serverName = server
		m.appName = f.app.Name
		for i, e := range m.appEntries() {
			if e.Name == m.appName {
				m.appCursor = i
				break
			}
		}
		return m.refreshServer(server)
	case formEditAppSource, formEditAppDomains, formEditAppCommands:
		if err := m.reloadConfig(); err != nil {
			m.showToast(toastError, err.Error())
		}
		m.page = pageAppEdit
		return m.refreshServer(server)
	case formServerSettings:
		m.page = pageServer
		if msg.warning == "" {
			m.showToast(toastSuccess, "Saved server settings.")
		}
		return m.refreshServer(server)
	}
	return m.refreshServer(server)
}

func (m *model) deleteServer(name string) tea.Cmd {
	tmp := m.doc.Clone()
	if err := tmp.DeleteServer(name); err != nil {
		m.showToast(toastError, err.Error())
		return nil
	}
	if err := tmp.Save(); err != nil {
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
	da := m.desiredApp(server, app)
	if da == nil {
		m.showToast(toastWarning, "Configuration is still loading; try again.")
		return nil
	}
	expected := da.Hash
	backend := m.backend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := backend.RemoveDesiredApp(ctx, server, app, expected)
		return deleteDoneMsg{server: server, app: app, err: err}
	}
}

func (m *model) finishDelete(msg deleteDoneMsg) tea.Cmd {
	if msg.err != nil {
		if errors.Is(msg.err, ops.ErrConfigConflict) {
			m.openNotice("Changed on the server", "This app changed on the server since you loaded it. Nothing was deleted; reloading the latest version.")
			return m.refreshServer(msg.server)
		}
		m.showToast(toastError, msg.err.Error())
		return nil
	}
	m.page = pageServer
	m.serverName = msg.server
	m.appName = ""
	m.appCursor = clampIndex(m.appCursor, len(m.appEntries()))
	m.showToast(toastSuccess, "Deleted "+msg.app+"; data directories and .env were kept.")
	return m.refreshServer(msg.server)
}

func (m *model) reloadConfig() error {
	cfg, err := config.Load(m.path)
	if err != nil {
		return err
	}
	doc, err := yamldoc.Load(m.path)
	if err != nil {
		return err
	}
	m.cfg = cfg
	m.doc = doc
	m.backend.SetConfig(cfg)
	m.serverCursor = clampIndex(m.serverCursor, len(cfg.ServerNames()))
	for name := range m.variances {
		if _, err := cfg.Server(name); err != nil {
			delete(m.variances, name)
			delete(m.varianceErr, name)
			delete(m.varianceLoading, name)
			delete(m.statuses, name)
			delete(m.statusErr, name)
			delete(m.statusLoading, name)
		}
	}
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
	if srv.IsLocal() {
		return "local"
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
	app := m.recordFor(m.serverName, name)
	if app == nil {
		return ""
	}
	return strings.Join(app.Domains, ", ")
}

func (m *model) appType(name string) string {
	app := m.recordFor(m.serverName, name)
	if app == nil {
		return ""
	}
	if app.Source.Type == "image" {
		if srv := m.currentServer(); srv != nil {
			if _, ok := config.GitSource(srv.Sources[name]); ok {
				return "git"
			}
		}
		return "image"
	}
	return "release"
}

// syncAlertLines lists everything on a server that is out of sync with its
// desired configuration, one user-facing line per item.
func (m *model) syncAlertLines(server string) []string {
	report, ok := m.variances[server]
	if !ok {
		return nil
	}
	var lines []string
	for _, v := range report.Apps {
		switch v.State {
		case ops.VariancePending:
			lines = append(lines, v.App+": config changed, pending deploy")
		case ops.VarianceNotDeployed:
			lines = append(lines, v.App+": not deployed, deploy it")
		}
	}
	if report.ProxyOutOfSync {
		lines = append(lines, "Caddy: "+proxyAlertReason(report.ProxyReason)+", restart Caddy to apply (p)")
	}
	if report.SetupPending {
		lines = append(lines, "Server: "+setupAlertReason(report.SetupReason)+", run setup (s)")
	}
	return lines
}

func proxyAlertReason(reason string) string {
	switch {
	case strings.Contains(reason, "caddy_image"):
		return "caddy_image changed"
	case strings.Contains(reason, "not running"):
		return "proxy is not running"
	default:
		return "domains changed"
	}
}

func setupAlertReason(reason string) string {
	switch {
	case strings.Contains(reason, "admin_user"):
		return "admin_user changed"
	case strings.Contains(reason, "app uids"):
		return "app uids changed"
	default:
		return "setup has not run"
	}
}

func (m *model) activityTime(a *activity) string {
	if a == nil {
		return ""
	}
	return a.start.Format("15:04:05")
}
