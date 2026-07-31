package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent = lipgloss.Color("#7aa2f7")
	colorGood   = lipgloss.Color("#9ece6a")
	colorWarn   = lipgloss.Color("#e0af68")
	colorBad    = lipgloss.Color("#f7768e")
	colorMuted  = lipgloss.Color("#565f89")
	colorText   = lipgloss.Color("#c0caf5")
	colorBright = lipgloss.Color("#ffffff")

	styleTabActive = lipgloss.NewStyle().
			Foreground(colorBright).
			Background(colorAccent).
			Bold(true).
			Padding(0, 1)
	styleTabIdle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)
	styleFooter = lipgloss.NewStyle().
			Foreground(colorMuted)
	styleSeparator = lipgloss.NewStyle().
			Foreground(colorMuted)
	styleTitle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)
	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)
	styleGood   = lipgloss.NewStyle().Foreground(colorGood)
	styleWarn   = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad    = lipgloss.NewStyle().Foreground(colorBad)
	styleMuted  = lipgloss.NewStyle().Foreground(colorMuted)
	styleCursor = lipgloss.NewStyle().
			Foreground(colorBright).
			Background(lipgloss.Color("#283457"))
	styleHeader = lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true)
)
