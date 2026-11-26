package tui

import "github.com/charmbracelet/lipgloss"

// Colours are ported from the lewdev TUI theme.
var (
	colBackground = lipgloss.Color("#0b0f14")
	colPanel      = lipgloss.Color("#111923")
	colPanelAlt   = lipgloss.Color("#0f1620")
	colBorder     = lipgloss.Color("#26384a")
	colAccent     = lipgloss.Color("#63c9ff")
	colAccentSoft = lipgloss.Color("#1d3a4c")
	colText       = lipgloss.Color("#d7e2ed")
	colMuted      = lipgloss.Color("#7c8b9b")
	colSuccess    = lipgloss.Color("#5bd68a")
	colWarning    = lipgloss.Color("#f0c95a")
	colError      = lipgloss.Color("#ff7b72")
	colSelection  = lipgloss.Color("#17384d")
	colWhite      = lipgloss.Color("#f4f8fc")

	tintSuccess = lipgloss.Color("#122a1c")
	tintError   = lipgloss.Color("#301417")
	tintWarning = lipgloss.Color("#2d2612")
	tintInfo    = lipgloss.Color("#102333")
	tintMuted   = lipgloss.Color("#1a222c")
)

var (
	styleBase      = lipgloss.NewStyle().Background(colBackground).Foreground(colText)
	styleAccent    = lipgloss.NewStyle().Foreground(colAccent)
	styleMuted     = lipgloss.NewStyle().Foreground(colMuted)
	styleText      = lipgloss.NewStyle().Foreground(colText)
	styleWhite     = lipgloss.NewStyle().Foreground(colWhite)
	styleSuccess   = lipgloss.NewStyle().Foreground(colSuccess)
	styleWarning   = lipgloss.NewStyle().Foreground(colWarning)
	styleError     = lipgloss.NewStyle().Foreground(colError)
	stylePanel     = lipgloss.NewStyle().Background(colPanel)
	styleSelection = lipgloss.NewStyle().Background(colSelection)
	styleTab       = lipgloss.NewStyle().Foreground(colMuted).Background(colPanelAlt)
	styleTabActive = lipgloss.NewStyle().Foreground(colAccent).Background(colSelection)
)
