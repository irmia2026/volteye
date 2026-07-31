package tui

import tea "github.com/charmbracelet/bubbletea"

type Panel interface {
	tea.Model
	Title() string
	SetSize(width, height int)
	Help() string
	CapturesInput() bool
}
