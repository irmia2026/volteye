package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/store"
)

type groupsLoadedMsg struct {
	groups []store.Group
	counts map[string]int64
}

type groupsPanel struct {
	st     *store.Store
	groups []store.Group
	counts map[string]int64
	cursor int
	offset int
	w, h   int
	status string
}

func newGroupsPanel(st *store.Store) Panel {
	return &groupsPanel{st: st}
}

func (p *groupsPanel) Title() string       { return "群管理" }
func (p *groupsPanel) Help() string        { return "↑/↓:移动  空格:监控  b:回填  r:刷新" }
func (p *groupsPanel) CapturesInput() bool { return false }
func (p *groupsPanel) SetSize(w, h int)    { p.w, p.h = w, h }

func (p *groupsPanel) Init() tea.Cmd { return p.reload }

func (p *groupsPanel) reload() tea.Msg {
	groups, err := p.st.ListGroups()
	if err != nil {
		return logMsg{text: "load groups: " + err.Error()}
	}
	counts, _ := p.st.GroupMessageCounts()
	return groupsLoadedMsg{groups: groups, counts: counts}
}

func (p *groupsPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case groupsLoadedMsg:
		p.groups = msg.groups
		p.counts = msg.counts
		if p.cursor >= len(p.groups) {
			p.cursor = max(0, len(p.groups)-1)
		}
		return p, nil
	case pollTickMsg:
		counts, _ := p.st.GroupMessageCounts()
		p.counts = counts
		return p, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.groups)-1 {
				p.cursor++
			}
		case "pgup":
			p.cursor = max(0, p.cursor-p.pageSize())
		case "pgdown":
			p.cursor = min(len(p.groups)-1, p.cursor+p.pageSize())
		case "home":
			p.cursor = 0
		case "end":
			p.cursor = max(0, len(p.groups)-1)
		case " ":
			if g := p.current(); g != nil {
				if err := p.st.SetMonitored(g.Wxid, !g.Monitored); err != nil {
					p.status = err.Error()
				} else {
					p.status = ""
				}
				return p, p.reload
			}
		case "b":
			if g := p.current(); g != nil {
				if err := p.st.SetBackfill(g.Wxid, !g.Backfill); err != nil {
					p.status = err.Error()
				} else {
					p.status = ""
				}
				return p, p.reload
			}
		case "r":
			return p, p.reload
		}
	}
	p.clamp()
	return p, nil
}

func (p *groupsPanel) current() *store.Group {
	if p.cursor < 0 || p.cursor >= len(p.groups) {
		return nil
	}
	return &p.groups[p.cursor]
}

func (p *groupsPanel) pageSize() int {
	if n := p.h - 4; n > 1 {
		return n
	}
	return 10
}

func (p *groupsPanel) clamp() {
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+p.pageSize() {
		p.offset = p.cursor - p.pageSize() + 1
	}
}

func (p *groupsPanel) View() string {
	var sb strings.Builder
	header := fmt.Sprintf("  %s %s %s %s %s",
		padRunes("监控", 4), padRunes("回填", 4), padRunes("群名称", 30),
		padRunes("群 wxid", 32), "已存消息")
	sb.WriteString(styleHeader.Render(header) + "\n")

	if len(p.groups) == 0 {
		sb.WriteString(styleMuted.Render("  没有群聊记录") + "\n")
		return sb.String()
	}
	end := min(len(p.groups), p.offset+p.pageSize())
	for i := p.offset; i < end; i++ {
		g := p.groups[i]
		mon, bf := " ", " "
		if g.Monitored {
			mon = styleGood.Render("✓")
		}
		if g.Backfill {
			bf = styleWarn.Render("✓")
		}
		name := g.Name
		if name == "" {
			name = styleMuted.Render("(无名群)")
		}
		line := fmt.Sprintf("  %s   %s   %s %s %d",
			mon, bf, padRunes(name, 30), padRunes(g.Wxid, 32), p.counts[g.Wxid])
		if i == p.cursor {
			line = styleCursor.Render(line)
		}
		sb.WriteString(line + "\n")
	}
	sb.WriteString(styleMuted.Render(fmt.Sprintf("\n  %d/%d 群", p.cursor+1, len(p.groups))))
	if p.status != "" {
		sb.WriteString("  " + styleBad.Render(p.status))
	}
	return sb.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
