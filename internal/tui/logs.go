package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type logsPanel struct {
	lines []string
	vp    viewport.Model
	w, h  int
}

func newLogsPanel() Panel {
	return &logsPanel{}
}

func (p *logsPanel) Title() string       { return "日志" }
func (p *logsPanel) Help() string        { return "↑/↓/pgup/pgdn:滚动" }
func (p *logsPanel) CapturesInput() bool { return false }
func (p *logsPanel) Init() tea.Cmd       { return nil }

func (p *logsPanel) SetSize(w, h int) {
	p.w, p.h = w, h
	p.vp.Width = w - 2
	p.vp.Height = h - 2
	p.refresh()
}

func (p *logsPanel) refresh() {
	p.vp.SetContent(strings.Join(p.lines, "\n"))
}

func (p *logsPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case logMsg:
		line := msg.at.Format("15:04:05") + " " + msg.text
		p.lines = append(p.lines, line)
		if len(p.lines) > 1000 {
			p.lines = p.lines[len(p.lines)-1000:]
		}
		p.refresh()
		p.vp.GotoBottom()
		return p, nil
	case pollTickMsg:
		return p, nil
	}
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return p, cmd
}

func (p *logsPanel) View() string {
	return styleTitle.Render("运行日志") + "\n" + p.vp.View()
}
