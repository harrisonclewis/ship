package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

func padRight(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return clip(s, width)
	}
	return s + strings.Repeat(" ", width-w)
}

func fit(s string, width int) string {
	return padRight(clip(s, width), width)
}

// panel renders a rounded bordered panel with a title set into the top border.
func panel(title, content string, width, height int, active bool) string {
	if width < 6 {
		width = 6
	}
	borderColour := colBorder
	if active {
		borderColour = colAccent
	}
	border := lipgloss.NewStyle().Foreground(borderColour)
	inner := width - 2

	lines := strings.Split(content, "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	if height > 0 {
		maxLines := height - 2
		if maxLines < 1 {
			maxLines = 1
		}
		if len(lines) > maxLines {
			lines = lines[:maxLines]
		}
		for len(lines) < maxLines {
			lines = append(lines, "")
		}
	}

	var b strings.Builder
	if title == "" {
		b.WriteString(border.Render("╭" + strings.Repeat("─", inner) + "╮"))
	} else {
		title = clip(title, width-5)
		tw := ansi.StringWidth(title)
		fill := width - 5 - tw
		if fill < 0 {
			fill = 0
		}
		b.WriteString(border.Render("╭─"))
		b.WriteString(" ")
		b.WriteString(styleAccent.Render(title))
		b.WriteString(" ")
		b.WriteString(border.Render(strings.Repeat("─", fill) + "╮"))
	}
	for _, line := range lines {
		b.WriteString("\n")
		b.WriteString(border.Render("│"))
		b.WriteString(fit(line, inner))
		b.WriteString(border.Render("│"))
	}
	b.WriteString("\n")
	b.WriteString(border.Render("╰" + strings.Repeat("─", inner) + "╯"))
	return b.String()
}

func badgeStyle(tint, fg lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Background(tint).Foreground(fg)
}

func statusBadge(label string, fg, tint lipgloss.Color) string {
	if label == "" {
		label = "-"
	}
	return badgeStyle(tint, fg).Render("● " + label)
}

func healthColour(health string) lipgloss.Color {
	switch strings.ToLower(health) {
	case "healthy":
		return colSuccess
	case "starting", "unhealthy":
		return colWarning
	case "missing", "exited", "dead":
		return colError
	default:
		return colMuted
	}
}

func healthTint(health string) lipgloss.Color {
	switch strings.ToLower(health) {
	case "healthy":
		return tintSuccess
	case "starting", "unhealthy":
		return tintWarning
	case "missing", "exited", "dead":
		return tintError
	default:
		return tintMuted
	}
}

func toastStyle(kind toastKind) (icon string, fg, bg lipgloss.Color) {
	switch kind {
	case toastSuccess:
		return "✓", colSuccess, tintSuccess
	case toastError:
		return "✕", colError, tintError
	case toastWarning:
		return "!", colWarning, tintWarning
	default:
		return "i", colAccent, tintInfo
	}
}

func wrapText(s string, width int) []string {
	if width < 1 {
		return []string{""}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := ""
	for _, word := range words {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if ansi.StringWidth(candidate) <= width {
			line = candidate
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
		for ansi.StringWidth(word) > width {
			lines = append(lines, ansi.Truncate(word, width, ""))
			word = strings.TrimPrefix(word, ansi.Truncate(word, width, ""))
		}
		line = word
	}
	if line != "" {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func renderToast(t *toast, width int) string {
	if t == nil {
		return ""
	}
	icon, fg, bg := toastStyle(t.kind)
	style := lipgloss.NewStyle().Foreground(fg).Background(bg)
	lines := wrapText(t.msg, max(8, width-4))
	if len(lines) > 2 {
		lines = []string{lines[0], ansi.Truncate(lines[1], max(1, width-6), "") + "…"}
	}
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		prefix := "  "
		if i == 0 {
			prefix = icon + " "
		}
		out = append(out, style.Render(fit(prefix+line, width)))
	}
	return strings.Join(out, "\n")
}

type keyHint struct {
	key   string
	label string
}

func renderKeyHints(hints []keyHint, width int) string {
	if width <= 0 || len(hints) == 0 {
		return ""
	}
	var lines [][]keyHint
	var current []keyHint
	currentWidth := 0
	for _, h := range hints {
		w := ansi.StringWidth(h.key) + 1 + ansi.StringWidth(h.label)
		extra := 0
		if len(current) > 0 {
			extra = 2
		}
		if currentWidth+w+extra > width && len(current) > 0 {
			lines = append(lines, current)
			current = nil
			currentWidth = 0
			extra = 0
		}
		current = append(current, h)
		currentWidth += w + extra
	}
	if len(current) > 0 {
		lines = append(lines, current)
	}
	if len(lines) > 2 {
		lines = lines[:2]
	}
	var out []string
	for _, line := range lines {
		parts := make([]string, 0, len(line))
		for _, h := range line {
			parts = append(parts, styleAccent.Render(h.key)+" "+styleMuted.Render(h.label))
		}
		out = append(out, strings.Join(parts, "  "))
	}
	return strings.Join(out, "\n")
}

func tabBar(active int) string {
	tabs := []string{"Servers", "Activity"}
	parts := make([]string, 0, len(tabs))
	for i, name := range tabs {
		style := styleTab
		if i == active {
			style = styleTabActive
		}
		parts = append(parts, style.Padding(0, 2).Render(name))
	}
	return strings.Join(parts, " ")
}

func headerBar(breadcrumbs []string, right string, dryRun bool, width int) string {
	var b strings.Builder
	b.WriteString(styleAccent.Render("ship"))
	if len(breadcrumbs) == 0 {
		b.WriteString(styleMuted.Render(" deploy & operate"))
	} else {
		for i, crumb := range breadcrumbs {
			b.WriteString(styleMuted.Render(" › "))
			style := styleText
			if i == len(breadcrumbs)-1 {
				style = styleAccent
			}
			b.WriteString(style.Render(crumb))
		}
	}
	left := b.String()
	if ansi.StringWidth(left) > width {
		left = clip(left, width)
	}
	var rightParts []string
	if dryRun {
		rightParts = append(rightParts, badgeStyle(tintWarning, colWarning).Render(" DRY RUN "))
	}
	if right != "" {
		rightParts = append(rightParts, styleMuted.Render(right))
	}
	rightText := strings.Join(rightParts, " ")
	if rightText == "" {
		return clip(left, width)
	}
	leftW := ansi.StringWidth(left)
	rightW := ansi.StringWidth(rightText)
	if leftW+rightW+1 > width {
		return clip(left, max(1, width-rightW-1)) + " " + rightText
	}
	return left + strings.Repeat(" ", width-leftW-rightW) + rightText
}

func infoRow(label, value string, labelWidth int) string {
	if value == "" {
		value = "-"
	}
	return styleMuted.Render(padRight(label, labelWidth)) + styleText.Render(value)
}

func spinner(frame int) string {
	if len(spinnerFrames) == 0 {
		return "|"
	}
	return spinnerFrames[frame%len(spinnerFrames)]
}

func stateBadge(state runState) string {
	switch state {
	case runRunning:
		return statusBadge("running", colAccent, tintInfo)
	case runSucceeded:
		return statusBadge("succeeded", colSuccess, tintSuccess)
	case runFailed:
		return statusBadge("failed", colError, tintError)
	default:
		return statusBadge("unknown", colMuted, tintMuted)
	}
}

func formatCount(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
