package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/store"
)

type groupsLoadedMsg struct {
	groups []store.Group
	counts map[string]int64
}

type groupsPanel struct {
	st         *store.Store
	groups     []store.Group
	view       []store.Group
	counts     map[string]int64
	cursor     int
	offset     int
	w, h       int
	status     string
	editing    bool
	input      textinput.Model
	searching  bool
	search     textinput.Model
	filterText string
}

func newGroupsPanel(st *store.Store) Panel {
	ti := textinput.New()
	ti.Placeholder = "输入备注名，回车保存，Esc 取消，留空清除"
	ti.CharLimit = 32
	si := textinput.New()
	si.Placeholder = "搜索群名 / 备注 / wxid，回车确认，Esc 取消"
	si.CharLimit = 64
	return &groupsPanel{st: st, input: ti, search: si}
}

func (p *groupsPanel) Title() string { return "群管理" }

func (p *groupsPanel) Help() string {
	if p.editing {
		return "回车 保存备注 · Esc 取消"
	}
	if p.searching {
		return "回车 应用 · Esc 取消"
	}
	return "↑↓ 移动 · 空格 监控 · b 回填 · e 备注 · / 搜索 · c 清搜索 · r 刷新"
}

func (p *groupsPanel) CapturesInput() bool { return p.editing || p.searching }
func (p *groupsPanel) SetSize(w, h int) {
	p.w, p.h = w, h
	p.input.Width = w - 16
	p.search.Width = w - 16
}

func (p *groupsPanel) Init() tea.Cmd { return p.reload }

func (p *groupsPanel) reload() tea.Msg {
	groups, err := p.st.ListGroups()
	if err != nil {
		return logMsg{text: "load groups: " + err.Error()}
	}
	counts, _ := p.st.GroupMessageCounts()
	return groupsLoadedMsg{groups: groups, counts: counts}
}

// applyFilter rebuilds the visible view from the current filter text,
// matching name, alias and wxid case-insensitively.
func (p *groupsPanel) applyFilter() {
	kw := strings.ToLower(strings.TrimSpace(p.filterText))
	p.view = p.view[:0]
	for _, g := range p.groups {
		if kw == "" ||
			strings.Contains(strings.ToLower(g.Name), kw) ||
			strings.Contains(strings.ToLower(g.Alias), kw) ||
			strings.Contains(strings.ToLower(g.Wxid), kw) {
			p.view = append(p.view, g)
		}
	}
	if p.cursor >= len(p.view) {
		p.cursor = max(0, len(p.view)-1)
	}
	p.clamp()
}

func (p *groupsPanel) current() *store.Group {
	if p.cursor < 0 || p.cursor >= len(p.view) {
		return nil
	}
	return &p.view[p.cursor]
}

func (p *groupsPanel) pageSize() int {
	if n := p.h - 5; n > 1 {
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

func (p *groupsPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case groupsLoadedMsg:
		p.groups = msg.groups
		p.counts = msg.counts
		p.applyFilter()
		return p, nil
	case pollTickMsg:
		return p, p.reload
	case tea.KeyMsg:
		if p.searching {
			switch msg.String() {
			case "enter":
				p.filterText = strings.TrimSpace(p.search.Value())
				p.searching = false
				p.search.Blur()
				p.cursor = 0
				p.offset = 0
				p.applyFilter()
				return p, nil
			case "esc":
				p.searching = false
				p.search.Blur()
				return p, nil
			}
			var cmd tea.Cmd
			p.search, cmd = p.search.Update(msg)
			return p, cmd
		}
		if p.editing {
			switch msg.String() {
			case "enter":
				g := p.current()
				if g != nil {
					alias := strings.TrimSpace(p.input.Value())
					if err := p.st.SetGroupAlias(g.Wxid, alias); err != nil {
						p.status = err.Error()
					} else {
						p.status = ""
					}
				}
				p.editing = false
				p.input.Blur()
				return p, p.reload
			case "esc":
				p.editing = false
				p.input.Blur()
				return p, nil
			}
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			return p, cmd
		}
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.view)-1 {
				p.cursor++
			}
		case "pgup":
			p.cursor = max(0, p.cursor-p.pageSize())
		case "pgdown":
			p.cursor = min(len(p.view)-1, p.cursor+p.pageSize())
		case "home":
			p.cursor = 0
		case "end":
			p.cursor = max(0, len(p.view)-1)
		case "/":
			p.searching = true
			p.search.SetValue(p.filterText)
			return p, p.search.Focus()
		case "c":
			if p.filterText != "" {
				p.filterText = ""
				p.cursor = 0
				p.offset = 0
				p.applyFilter()
			}
			return p, nil
		case " ":
			if g := p.current(); g != nil {
				if err := p.st.SetMonitored(g.Wxid, !g.Monitored); err != nil {
					p.status = err.Error()
				}
				return p, p.reload
			}
		case "b":
			if g := p.current(); g != nil {
				if err := p.st.SetBackfill(g.Wxid, !g.Backfill); err != nil {
					p.status = err.Error()
				}
				return p, p.reload
			}
		case "e":
			if g := p.current(); g != nil {
				p.editing = true
				p.input.SetValue(g.Alias)
				return p, p.input.Focus()
			}
		case "r":
			return p, p.reload
		}
	}
	p.clamp()
	return p, nil
}

func (p *groupsPanel) View() string {
	var sb strings.Builder
	header := fmt.Sprintf("   %s %s %s %s %s",
		padRunes("监控", 4), padRunes("回填", 4), padRunes("群名 / 备注", 28),
		padRunes("群 wxid", 30), "已存")
	sb.WriteString("  " + stTableHead.Render(header) + "\n")
	sb.WriteString("  " + rule(min(p.w-4, 90)) + "\n")

	if len(p.view) == 0 {
		if p.filterText != "" {
			sb.WriteString("  " + stMuted.Render("没有匹配「"+p.filterText+"」的群，按 c 清除搜索") + "\n")
		} else {
			sb.WriteString("  " + stMuted.Render("没有群聊记录") + "\n")
		}
		return sb.String()
	}
	end := min(len(p.view), p.offset+p.pageSize())
	for i := p.offset; i < end; i++ {
		g := p.view[i]
		mon := stFaint.Render("○")
		if g.Monitored {
			mon = stOk.Render("●")
		}
		bf := stFaint.Render("○")
		if g.Backfill {
			bf = stWarn.Render("●")
		}
		var nameCell string
		if g.Alias != "" {
			nameCell = stAccent.Render(padRunes(g.Alias, 28))
		} else if g.Name != "" {
			nameCell = stInk.Render(padRunes(g.Name, 28))
		} else {
			nameCell = stFaint.Render(padRunes(g.Wxid, 28))
		}
		count := fmt.Sprintf("%d", p.counts[g.Wxid])
		line := fmt.Sprintf(" %s   %s   %s %s %s",
			mon, bf, nameCell, stMuted.Render(padRunes(g.Wxid, 30)), stAccent.Render(count))
		if i == p.cursor {
			line = stCursorRow.Render("▸") + line
		} else {
			line = " " + line
		}
		sb.WriteString(line + "\n")
	}
	if p.filterText != "" {
		sb.WriteString("\n  " + stFaint.Render(fmt.Sprintf("搜索「%s」 %d/%d 群 · ● 监控 %d 群",
			p.filterText, p.cursor+1, len(p.view), countMonitored(p.view))))
	} else {
		sb.WriteString("\n  " + stFaint.Render(fmt.Sprintf("%d/%d 群 · ● 监控 %d 群",
			p.cursor+1, len(p.view), countMonitored(p.view))))
	}
	if p.editing {
		sb.WriteString("\n  " + stLabel.Render("备注: ") + p.input.View())
	}
	if p.searching {
		sb.WriteString("\n  " + stLabel.Render("搜索: ") + p.search.View())
	}
	if p.status != "" {
		sb.WriteString("\n  " + stBad.Render(p.status))
	}
	return sb.String()
}

func countMonitored(groups []store.Group) int {
	n := 0
	for _, g := range groups {
		if g.Monitored {
			n++
		}
	}
	return n
}
