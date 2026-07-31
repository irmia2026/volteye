package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

type stubPanel struct {
	title string
	note  string
	w, h  int
}

func newStubPanel(title, note string) Panel {
	return &stubPanel{title: title, note: note}
}

func (p *stubPanel) Title() string       { return p.title }
func (p *stubPanel) Help() string        { return "敬请期待" }
func (p *stubPanel) CapturesInput() bool { return false }
func (p *stubPanel) SetSize(w, h int)    { p.w, p.h = w, h }
func (p *stubPanel) Init() tea.Cmd       { return nil }

func (p *stubPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return p, nil }

func (p *stubPanel) View() string {
	return "\n  " + styleMuted.Render(p.note)
}
