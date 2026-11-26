package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/harrisonclewis/ship/internal/config"
)

func (m *model) View() string {
	if m.width <= 0 {
		m.width = 80
	}
	if m.height <= 0 {
		m.height = 24
	}

	var content string
	switch {
	case m.form != nil:
		content = m.viewForm()
	case m.confirm != nil:
		content = m.viewConfirm()
	default:
		switch m.page {
		case pageServers:
			content = m.viewServers()
		case pageActivity:
			content = m.viewActivity()
		case pageServer:
			content = m.viewServer()
		case pageServerActions:
			content = m.viewServerActions()
		case pageAppActions:
			content = m.viewAppActions()
		case pageOutput:
			content = m.viewOutput()
		case pageRestorePick:
			content = m.viewRestorePick()
		case pageCompare:
			content = m.viewCompare()
		}
	}

	var sections []string
	sections = append(sections, headerBar(m.crumbs(), m.configPathLabel(), m.dryRun, m.width))
	if m.page == pageServers || m.page == pageActivity {
		sections = append(sections, tabBar(m.tab))
	}
	if m.toast != nil {
		sections = append(sections, renderToast(m.toast, m.width))
	}
	sections = append(sections, content)
	sections = append(sections, renderKeyHints(m.hints(), m.width))

	out := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return styleBase.Width(m.width).Height(m.height).Render(out)
}

func (m *model) crumbs() []string {
	label := ""
	if a := m.outputActivity(); a != nil {
		label = a.label
	}
	switch {
	case m.form != nil:
		f := m.form
		switch f.purpose {
		case formAddServer:
			return []string{"Servers", "New server"}
		case formEditServer:
			name := ""
			if f.server != nil {
				name = f.server.OriginalName
			}
			return []string{"Servers", name, "Edit"}
		case formAddApp:
			return []string{"Servers", f.app.Server, "New app"}
		case formEditApp:
			return []string{"Servers", f.app.Server, f.app.OriginalName, "Edit"}
		case formDeployVersion:
			return []string{"Servers", f.serverName, f.appName, "Deploy"}
		case formRestorePath:
			return []string{"Servers", f.serverName, f.appName, "Restore"}
		case formLogsService:
			return []string{"Servers", f.serverName, f.appName, "Logs"}
		case formImportApps:
			return []string{"Servers", f.serverName, "Import"}
		}
	case m.page == pageServer:
		return []string{"Servers", m.serverName}
	case m.page == pageServerActions:
		return []string{"Servers", m.serverName, "Actions"}
	case m.page == pageAppActions:
		return []string{"Servers", m.serverName, m.appName}
	case m.page == pageOutput:
		if m.outputBack == pageActivity {
			return []string{"Activity", label}
		}
		return []string{"Servers", m.serverName, label}
	case m.page == pageRestorePick:
		return []string{"Servers", m.serverName, m.appName, "Restore"}
	case m.page == pageCompare:
		return []string{"Servers", m.serverName, m.appName, "Compare"}
	case m.page == pageActivity:
		return []string{"Activity"}
	}
	return []string{"Servers"}
}

func (m *model) hints() []keyHint {
	if m.form != nil {
		return []keyHint{{"enter", "next/finish"}, {"tab", "next field"}, {"esc", "cancel"}}
	}
	if m.confirm != nil {
		return []keyHint{{"enter/y", "confirm"}, {"esc/n", "cancel"}}
	}
	switch m.page {
	case pageActivity:
		return []keyHint{{"enter", "output"}, {"tab", "switch"}, {"q", "quit"}}
	case pageServer:
		return []keyHint{
			{"enter", "app"}, {"a", "add app"}, {"m", "actions"}, {"d", "deploy all"},
			{"b", "backup all"}, {"r", "refresh"}, {"esc", "back"}, {"q", "quit"},
		}
	case pageServerActions, pageAppActions:
		return []keyHint{{"enter", "run"}, {"esc", "back"}, {"q", "quit"}}
	case pageOutput:
		hints := []keyHint{{"esc", "back"}, {"↑/↓", "scroll"}}
		if a := m.outputActivity(); a != nil && a.state == runRunning {
			hints = append(hints, keyHint{"ctrl+c", "cancel"})
		} else {
			hints = append(hints, keyHint{"q", "quit"})
		}
		return hints
	case pageRestorePick:
		return []keyHint{{"enter", "restore"}, {"p", "local path"}, {"r", "refresh"}, {"esc", "back"}}
	case pageCompare:
		return []keyHint{{"u", "use server version"}, {"k/esc", "keep mine"}, {"↑/↓", "scroll"}}
	default:
		return []keyHint{{"enter", "open"}, {"a", "add server"}, {"r", "refresh"}, {"tab", "switch"}, {"q", "quit"}}
	}
}

func colourFor(kind string) lipgloss.Style {
	switch kind {
	case "success":
		return styleSuccess
	case "warning":
		return styleWarning
	case "error":
		return styleError
	case "accent":
		return styleAccent
	default:
		return styleMuted
	}
}

func (m *model) viewServers() string {
	inner := m.width - 2
	rows := max(1, m.bodyHeight()-4)
	names := m.backend.Servers()
	body := make([]string, 0, rows+1)

	showApps := inner >= 68
	showIP := inner >= 50
	nameW := 18
	ipW := 18
	appsW := 20
	statusW := inner - 2 - nameW - ipW - appsW
	if !showApps {
		statusW = inner - 2 - nameW - ipW
	}
	if !showIP {
		statusW = inner - 2 - nameW
	}
	if statusW < 12 {
		statusW = 12
	}
	if !showIP {
		nameW = inner - 2 - statusW
		if nameW < 8 {
			nameW = 8
		}
	}
	if showIP && !showApps {
		ipW = inner - 2 - nameW - statusW
		if ipW < 8 {
			ipW = 8
		}
	}

	header := padRight("  NAME", 2+nameW) + styleMuted.Render(padRight("IP", ipW))
	if showApps {
		header += styleMuted.Render(padRight("APPLICATIONS", appsW))
	}
	header += styleMuted.Render(padRight("STATUS", statusW))
	body = append(body, header)

	if len(names) == 0 {
		body = append(body, styleMuted.Render("No servers configured. Press a to add one."))
	} else {
		start := windowStart(m.serverCursor, len(names), rows)
		for i := start; i < len(names) && i < start+rows; i++ {
			name := names[i]
			line := m.serverRow(name, nameW, ipW, appsW, statusW, showIP, showApps, i == m.serverCursor)
			body = append(body, line)
		}
	}
	return panel(fmt.Sprintf("Servers (%d)", len(names)), strings.Join(body, "\n"), m.width, m.bodyHeight(), false)
}

func (m *model) serverRow(name string, nameW, ipW, appsW, statusW int, showIP, showApps, selected bool) string {
	srv, err := m.backend.Server(name)
	apps := ""
	if err == nil {
		names := srv.AppNames()
		apps = fmt.Sprintf("%d  %s", len(names), joinLimited(names, appsW-4))
	}
	status, kind := m.statusText(name)
	ip := m.serverHost(name)

	cursor := " "
	if selected {
		cursor = "›"
	}
	plain := cursor + " " + padRight(clip(name, nameW-1), nameW)
	if showIP {
		plain += padRight(clip(ip, ipW-1), ipW)
	}
	if showApps {
		plain += padRight(clip(apps, appsW-1), appsW)
	}
	total := 2 + nameW + statusW
	if showIP {
		total += ipW
	}
	if showApps {
		total += appsW
	}
	if selected {
		plain += padRight(clip(status, statusW), statusW)
		return styleSelection.Render(fit(plain, total))
	}
	line := styleAccent.Render(cursor+" ") + styleText.Render(padRight(clip(name, nameW-1), nameW))
	if showIP {
		line += styleMuted.Render(padRight(clip(ip, ipW-1), ipW))
	}
	if showApps {
		line += styleMuted.Render(padRight(clip(apps, appsW-1), appsW))
	}
	line += colourFor(kind).Render(clip(status, statusW))
	return line
}

func joinLimited(names []string, width int) string {
	if width <= 0 || len(names) == 0 {
		return ""
	}
	out := ""
	for i, n := range names {
		candidate := n
		if i > 0 {
			candidate = out + ", " + n
		}
		if ansi.StringWidth(candidate) > width {
			if out == "" {
				return clip(n, width)
			}
			return clip(out+", …", width)
		}
		out = candidate
	}
	return out
}

func windowStart(selected, total, visible int) int {
	if total <= visible || visible <= 0 {
		return 0
	}
	start := selected - visible/2
	if start < 0 {
		start = 0
	}
	if start > total-visible {
		start = total - visible
	}
	return start
}

func (m *model) viewActivity() string {
	inner := m.width - 2
	rows := max(1, m.bodyHeight()-4)
	body := []string{}
	showServer := inner >= 70
	showTarget := inner >= 58
	timeW, serverW, actionW, targetW, stateW := 10, 12, 24, 12, 12
	if !showTarget {
		targetW = 0
	}
	if !showServer {
		serverW = 0
	}
	actionW = inner - 2 - timeW - serverW - targetW - stateW
	if actionW < 12 {
		actionW = 12
	}
	header := styleMuted.Render(padRight("TIME", timeW))
	if showServer {
		header += styleMuted.Render(padRight("SERVER", serverW))
	}
	header += styleMuted.Render(padRight("ACTION", actionW))
	if showTarget {
		header += styleMuted.Render(padRight("TARGET", targetW))
	}
	header += styleMuted.Render("STATE")
	body = append(body, header)

	if len(m.activities) == 0 {
		body = append(body, styleMuted.Render("No activity yet. Operations you run appear here."))
	} else {
		start := windowStart(m.activityCur, len(m.activities), rows)
		for i := start; i < len(m.activities) && i < start+rows; i++ {
			a := m.activities[i]
			plain := padRight(a.start.Format("15:04:05"), timeW)
			if showServer {
				plain += padRight(clip(a.req.Server, serverW-1), serverW)
			}
			plain += padRight(clip(a.label, actionW-1), actionW)
			if showTarget {
				plain += padRight(clip(a.req.target(), targetW-1), targetW)
			}
			if i == m.activityCur {
				plain += a.state.String()
				body = append(body, styleSelection.Render(fit(plain, inner)))
				continue
			}
			line := styleMuted.Render(padRight(a.start.Format("15:04:05"), timeW))
			if showServer {
				line += styleText.Render(padRight(clip(a.req.Server, serverW-1), serverW))
			}
			line += styleText.Render(padRight(clip(a.label, actionW-1), actionW))
			if showTarget {
				line += styleMuted.Render(padRight(clip(a.req.target(), targetW-1), targetW))
			}
			line += stateBadge(a.state)
			body = append(body, line)
		}
	}
	return panel(fmt.Sprintf("Activity (%d)", len(m.activities)), strings.Join(body, "\n"), m.width, m.bodyHeight(), false)
}

func (m *model) viewServer() string {
	srv := m.currentServer()
	if srv == nil {
		return panel("Server", styleError.Render("This server is no longer in ship.yaml."), m.width, m.bodyHeight(), false)
	}
	hostLine := infoRow("Host", srv.Host, 10)
	archLine := infoRow("Admin", srv.AdminUser, 10) + styleMuted.Render(" · ") + styleText.Render(srv.Arch)
	status, kind := m.statusText(m.serverName)
	proxyLine := styleMuted.Render(padRight("Proxy", 10)) + colourFor(kind).Render(status)
	if m.statusLoading[m.serverName] {
		proxyLine += styleMuted.Render("  " + spinner(m.spinnerFrame) + " loading")
	}
	info := strings.Join([]string{hostLine, archLine, proxyLine}, "\n")
	if report, ok := m.statuses[m.serverName]; ok && report.RebootRequired {
		info += "\n" + styleWarning.Render("reboot required")
	} else if errText := m.statusErr[m.serverName]; errText != "" {
		info += "\n" + styleError.Render(clip(errText, m.width-10))
	}
	infoHeight := strings.Count(info, "\n") + 3
	if infoHeight < 5 {
		infoHeight = 5
	}

	appsHeight := m.bodyHeight() - infoHeight - 1
	if appsHeight < 4 {
		appsHeight = 4
	}
	top := panel("Server", info, m.width, infoHeight, false)
	return lipgloss.JoinVertical(lipgloss.Left, top, "", m.viewApps(appsHeight))
}

func (m *model) viewApps(height int) string {
	inner := m.width - 2
	rows := max(1, height-4)
	entries := m.appEntries()
	body := []string{}

	showDomains := inner >= 76
	showPrevious := inner >= 62
	showType := inner >= 50
	showBadge := inner >= 36
	nameW := 14
	badgeW := 7
	typeW := 8
	currentW := 14
	previousW := 14
	healthW := 11
	if !showBadge {
		badgeW = 0
	}
	if !showType {
		typeW = 0
	}
	if !showPrevious {
		previousW = 0
	}
	domainsW := 0
	if showDomains {
		domainsW = inner - 2 - nameW - badgeW - typeW - currentW - previousW - healthW
		if domainsW < 6 {
			domainsW = 0
			showDomains = false
		}
	}
	if !showDomains {
		currentW = inner - 2 - nameW - badgeW - typeW - previousW - healthW
		if currentW < 10 {
			currentW = 10
		}
	}

	header := padRight("  APP", 2+nameW)
	if showBadge {
		header += padRight("", badgeW)
	}
	if showType {
		header += styleMuted.Render(padRight("TYPE", typeW))
	}
	header += styleMuted.Render(padRight("CURRENT", currentW))
	if showPrevious {
		header += styleMuted.Render(padRight("PREVIOUS", previousW))
	}
	header += styleMuted.Render(padRight("HEALTH", healthW))
	if showDomains {
		header += styleMuted.Render("DOMAINS")
	}
	body = append(body, header)

	if len(entries) == 0 {
		body = append(body, styleMuted.Render("No apps on this server. Press a to add one."))
	} else {
		start := windowStart(m.appCursor, len(entries), rows)
		loading := m.statusLoading[m.serverName]
		_, haveStatus := m.statuses[m.serverName]
		for i := start; i < len(entries) && i < start+rows; i++ {
			entry := entries[i]
			name := entry.Name
			st, deployed := m.appStatus(m.serverName, name)
			current, previous, health := "", "", ""
			typ, domains := "", ""
			maintenance := false
			badge := ""

			if entry.Local {
				if deployed {
					current = displayAppValue(st.Current)
					previous = displayAppValue(st.Previous)
					health = displayAppValue(st.Health)
					maintenance = st.Maintenance
				} else if loading && !haveStatus {
					current, previous, health = "…", "…", "…"
				} else if haveStatus {
					current, health = "not deployed", "—"
				} else {
					current, previous, health = "?", "?", "?"
				}
				typ = m.appType(name)
				domains = m.appDomains(name)
			} else {
				sa := m.serverApp(name)
				switch {
				case sa != nil && sa.RecordErr != "":
					current, health = "unreadable", "newer ship?"
				case sa != nil && sa.Record == nil:
					current, health = "no record", "older ship"
				case loading && !haveStatus:
					current, previous, health = "on server", "…", "…"
				default:
					current = "on server"
					if deployed {
						previous = displayAppValue(st.Previous)
						health = displayAppValue(st.Health)
					} else {
						health = "—"
					}
				}
				if sa != nil && sa.Record != nil {
					typ = appRecordType(sa.Record)
					domains = strings.Join(sa.Record.Domains, ", ")
				}
			}

			if diff, ok := m.diffFor(m.serverName, name); ok && diff.Changed() {
				badge = "DIFF"
			} else if maintenance {
				badge = "MAINT"
			}

			displayName := name
			missingCompose := entry.Local && m.composeMissing(name)
			if missingCompose {
				displayName = name + "!"
			}
			cursor := " "
			if i == m.appCursor {
				cursor = "›"
			}
			plain := cursor + " " + padRight(clip(displayName, nameW-1), nameW)
			if showBadge {
				plain += padRight(badge, badgeW)
			}
			if showType {
				plain += padRight(typ, typeW)
			}
			plain += padRight(clip(current, currentW-1), currentW)
			if showPrevious {
				plain += padRight(clip(previous, previousW-1), previousW)
			}
			plain += padRight(clip(health, healthW-1), healthW)
			if showDomains {
				plain += clip(domains, domainsW)
			}
			if i == m.appCursor {
				body = append(body, styleSelection.Render(fit(plain, inner)))
				continue
			}
			line := styleAccent.Render(cursor+" ") + styleText.Render(padRight(clip(displayName, nameW-1), nameW))
			if missingCompose {
				line = styleAccent.Render(cursor+" ") + styleWarning.Render(padRight(clip(displayName, nameW-1), nameW))
			}
			if showBadge {
				if badge != "" {
					line += badgeStyle(tintWarning, colWarning).Render(padRight(badge, badgeW))
				} else {
					line += strings.Repeat(" ", badgeW)
				}
			}
			if showType {
				line += styleMuted.Render(padRight(typ, typeW))
			}
			line += styleText.Render(padRight(clip(current, currentW-1), currentW))
			if showPrevious {
				line += styleMuted.Render(padRight(clip(previous, previousW-1), previousW))
			}
			line += colourFor(healthKind(health)).Render(padRight(clip(health, healthW-1), healthW))
			if showDomains {
				line += styleMuted.Render(clip(domains, domainsW))
			}
			body = append(body, line)
		}
	}
	return panel(fmt.Sprintf("Applications (%d)", len(entries)), strings.Join(body, "\n"), m.width, height, false)
}

func appRecordType(record *config.App) string {
	if record == nil {
		return ""
	}
	if record.Source.Type == "image" {
		return "image"
	}
	return "release"
}

func displayAppValue(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func healthKind(health string) string {
	switch strings.ToLower(health) {
	case "healthy":
		return "success"
	case "starting", "unhealthy":
		return "warning"
	case "missing", "exited", "dead":
		return "error"
	case "not deployed":
		return "muted"
	}
	return "muted"
}

func (m *model) viewServerActions() string {
	body := make([]string, 0, len(serverActionDefs))
	inner := m.width - 2
	for i, def := range serverActionDefs {
		line := actionLine(def.key, def.label, def.desc, def.danger, i == m.serverActCur, inner)
		body = append(body, line)
	}
	return panel("Server actions", strings.Join(body, "\n"), m.width, m.bodyHeight(), false)
}

func (m *model) viewAppActions() string {
	defs := m.appActions()
	body := make([]string, 0, len(defs))
	inner := m.width - 2
	for i, def := range defs {
		line := actionLine(def.key, def.label, def.desc, def.danger, i == m.appActCur, inner)
		body = append(body, line)
	}
	if len(defs) == 0 {
		body = append(body, styleError.Render("This app is no longer in ship.yaml or on the server."))
	}
	return panel("App actions · "+m.appName, strings.Join(body, "\n"), m.width, m.bodyHeight(), false)
}

func actionLine(key, label, desc string, danger, selected bool, width int) string {
	keyW := 2
	labelW := 26
	if width < 60 {
		desc = ""
	}
	if width < 40 {
		labelW = width - keyW - 3
	}
	if desc != "" {
		available := width - 2 - keyW - labelW
		if available < 1 {
			desc = ""
		} else {
			desc = clip(desc, available)
		}
	}
	prefix := "  "
	if selected {
		prefix = "› "
	}
	plain := prefix + padRight(key, keyW) + padRight(label, labelW)
	if desc != "" {
		plain += desc
	}
	if selected {
		return styleSelection.Render(fit(plain, width))
	}
	labelStyle := styleText
	if danger {
		labelStyle = styleError
	}
	out := styleAccent.Render(prefix) + styleMuted.Render(padRight(key, keyW)) + labelStyle.Render(padRight(label, labelW))
	if desc != "" {
		out += styleMuted.Render(desc)
	}
	return out
}

func (m *model) viewOutput() string {
	entry := m.outputActivity()
	title := "Output"
	content := ""
	if entry != nil {
		title = entry.label + " · " + entry.req.Server
		if entry.state == runRunning {
			title += "  " + spinner(m.spinnerFrame) + " running"
		} else {
			title += "  · " + entry.state.String()
		}
		content = m.vp.View()
		if strings.TrimSpace(content) == "" && entry.state == runRunning {
			content = styleMuted.Render(spinner(m.spinnerFrame) + " starting…")
		}
	} else {
		content = styleMuted.Render("No run selected.")
	}
	return panel(title, content, m.width, m.bodyHeight(), true)
}

func (m *model) viewCompare() string {
	content := m.vp.View()
	if strings.TrimSpace(content) == "" {
		content = styleMuted.Render("No differences.")
	}
	return panel("Compare · "+m.appName+" · server "+m.serverName, content, m.width, m.bodyHeight(), true)
}

func (m *model) viewRestorePick() string {
	var body []string
	inner := m.width - 2
	if m.backupsLoading {
		body = append(body, styleAccent.Render(spinner(m.spinnerFrame)+" loading backups…"))
	} else if m.backupsErr != "" {
		body = append(body, styleError.Render(m.backupsErr))
		body = append(body, "")
	}
	if !m.backupsLoading {
		if len(m.backups) == 0 && m.backupsErr == "" {
			body = append(body, styleMuted.Render("No stored backups for "+m.backupsApp+"."))
		}
		for i, entry := range m.backups {
			plain := fmt.Sprintf("%-2s %-46s %8s  %s", cursorFor(i == m.backupCursor), filepathBase(entry.Path), humanSize(entry.Size), entry.ModTime.Format("2006-01-02 15:04"))
			if i == m.backupCursor {
				body = append(body, styleSelection.Render(fit(plain, inner)))
			} else {
				body = append(body, styleText.Render(fit("  "+filepathBase(entry.Path)+"  "+humanSize(entry.Size)+"  "+entry.ModTime.Format("2006-01-02 15:04"), inner)))
			}
		}
		localIndex := len(m.backups)
		plain := cursorFor(m.backupCursor == localIndex) + " Enter a local path…"
		if m.backupCursor == localIndex {
			body = append(body, styleSelection.Render(fit(plain, inner)))
		} else {
			body = append(body, styleMuted.Render("  Enter a local path…"))
		}
	}
	return panel("Restore "+m.backupsApp, strings.Join(body, "\n"), m.width, m.bodyHeight(), false)
}

func cursorFor(selected bool) string {
	if selected {
		return "›"
	}
	return " "
}

func filepathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func (m *model) viewForm() string {
	f := m.form
	content := ""
	if f.err != "" {
		content = styleError.Render(f.err) + "\n\n"
	}
	if f.form != nil {
		content += f.form.View()
	}
	body := lipgloss.Place(m.width, m.bodyHeight(), lipgloss.Center, lipgloss.Center,
		panel(f.title, content, m.formWidth()+2, m.bodyHeight(), true))
	return body
}

func (m *model) viewConfirm() string {
	c := m.confirm
	width := 60
	if width > m.width-8 {
		width = m.width - 8
	}
	if width < 30 {
		width = 30
	}
	content := wrapText(c.body, width-4)
	lines := []string{strings.Join(content, "\n"), "", styleMuted.Render("Enter/y confirm   ·   Esc/n cancel")}
	body := lipgloss.Place(m.width, m.bodyHeight(), lipgloss.Center, lipgloss.Center,
		panel(c.title, strings.Join(lines, "\n"), width, 0, true))
	return body
}
