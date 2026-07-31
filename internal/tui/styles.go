package tui

import "github.com/charmbracelet/lipgloss"

var (
	colInk     = lipgloss.Color("#d7e0ea")
	colMuted   = lipgloss.Color("#61707f")
	colFaint   = lipgloss.Color("#33404d")
	colAccent  = lipgloss.Color("#79b8d8")
	colAccent2 = lipgloss.Color("#a9d3e8")
	colPanel   = lipgloss.Color("#8fa8bb")
	colOk      = lipgloss.Color("#82c4a6")
	colWarn    = lipgloss.Color("#cfa15e")
	colBad     = lipgloss.Color("#cf7d7d")
	colCursor  = lipgloss.Color("#1c2733")

	stBrand = lipgloss.NewStyle().
		Foreground(colAccent2).
		Bold(true)

	stHeaderMeta = lipgloss.NewStyle().
			Foreground(colMuted)

	stRule = lipgloss.NewStyle().
		Foreground(colFaint)

	stNavActive = lipgloss.NewStyle().
			Foreground(colAccent2).
			Bold(true)

	stNavIdle = lipgloss.NewStyle().
			Foreground(colMuted)

	stPanelTitle = lipgloss.NewStyle().
			Foreground(colPanel).
			Bold(true)

	stStatus = lipgloss.NewStyle().
			Foreground(colMuted)

	stDot = lipgloss.NewStyle().
		Foreground(colOk)

	stLabel = lipgloss.NewStyle().
		Foreground(colMuted)

	stValue = lipgloss.NewStyle().
		Foreground(colInk).
		Bold(true)

	stAccent = lipgloss.NewStyle().
			Foreground(colAccent)

	stAccentBold = lipgloss.NewStyle().
			Foreground(colAccent2).
			Bold(true)

	stWarn  = lipgloss.NewStyle().Foreground(colWarn)
	stBad   = lipgloss.NewStyle().Foreground(colBad)
	stOk    = lipgloss.NewStyle().Foreground(colOk)
	stMuted = lipgloss.NewStyle().Foreground(colMuted)
	stFaint = lipgloss.NewStyle().Foreground(colFaint)
	stInk   = lipgloss.NewStyle().Foreground(colInk)

	stCursorRow = lipgloss.NewStyle().
			Foreground(colAccent2).
			Bold(true)

	stTableHead = lipgloss.NewStyle().
			Foreground(colMuted)
)

var (
	styleTabActive = stNavActive
	styleTabIdle   = stNavIdle
	styleFooter    = stStatus
	styleSeparator = stRule
	styleTitle     = stPanelTitle
	styleBox       = lipgloss.NewStyle().Padding(0, 1)
	styleGood      = stOk
	styleWarn      = stWarn
	styleBad       = stBad
	styleMuted     = stMuted
	styleCursor    = stCursorRow
	styleHeader    = stTableHead
	styleMatched   = lipgloss.NewStyle().Foreground(colWarn)
)
