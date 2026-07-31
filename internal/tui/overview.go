package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"volteye/internal/store"
	"volteye/internal/wechatdb"
)

type overviewMatchedMsg struct{ rows []store.MessageRow }

type overviewPanel struct {
	st           *store.Store
	w, h         int
	lastPoll     time.Time
	lastInserted int
	polls        int
	matched      []store.MessageRow
}

func newOverviewPanel(st *store.Store) Panel {
	return &overviewPanel{st: st}
}

func (p *overviewPanel) Title() string       { return "总览" }
func (p *overviewPanel) Help() string        { return "自动刷新" }
func (p *overviewPanel) CapturesInput() bool { return false }
func (p *overviewPanel) SetSize(w, h int)    { p.w, p.h = w, h }

func (p *overviewPanel) Init() tea.Cmd {
	return func() tea.Msg {
		rows, _ := p.st.QueryMessages(store.MessageFilter{OnlyMatched: true, Limit: 12})
		return overviewMatchedMsg{rows: rows}
	}
}

func (p *overviewPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pollTickMsg:
		p.lastPoll = msg.at
		p.lastInserted = msg.inserted
		p.polls++
		return p, p.Init()
	case overviewMatchedMsg:
		p.matched = msg.rows
	}
	return p, nil
}

func statCard(value, label string, width int) string {
	v := stAccentBold.Render(value)
	l := stMuted.Render(label)
	inner := lipgloss.NewStyle().
		Width(width - 4).
		Align(lipgloss.Center).
		Render(v + "\n" + l)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colFaint).
		Padding(1, 1).
		Width(width - 2).
		Render(inner)
}

func (p *overviewPanel) View() string {
	total, _ := p.st.TotalMessages()
	matched, _ := p.st.MatchedCount()
	monitored, _ := p.st.MonitoredGroups()
	counts, _ := p.st.GroupMessageCounts()
	latest, _ := p.st.LatestTimes()

	cardW := max(14, (p.w-10)/4)
	cards := lipgloss.JoinHorizontal(lipgloss.Top,
		statCard(fmt.Sprintf("%d", len(monitored)), "监控群", cardW),
		"  ",
		statCard(fmt.Sprintf("%d", total), "落盘消息", cardW),
		"  ",
		statCard(fmt.Sprintf("%d", matched), "规则匹配", cardW),
		"  ",
		statCard(compactAgo(p.lastPoll), "最近轮询", cardW),
	)

	type grow struct {
		name  string
		count int64
		last  int64
	}
	var grows []grow
	for _, g := range monitored {
		grows = append(grows, grow{g.DisplayName(), counts[g.Wxid], latest[g.Wxid]})
	}
	sort.Slice(grows, func(i, j int) bool { return grows[i].last > grows[j].last })

	sectionH := p.h - lipgloss.Height(cards) - 4
	if sectionH < 4 {
		sectionH = 4
	}
	listH := sectionH - 2

	var left strings.Builder
	left.WriteString("  " + stTableHead.Render("分群落盘") + "\n")
	left.WriteString("  " + rule(min(36, p.w/2-6)) + "\n")
	limit := min(len(grows), max(1, listH))
	for i := 0; i < limit; i++ {
		g := grows[i]
		ago := ""
		if g.last > 0 {
			ago = compactAgo(time.Unix(g.last, 0))
		}
		left.WriteString("  " + stInk.Render(padRunes(g.name, 20)) +
			stAccent.Render(fmt.Sprintf("%7d", g.count)) + "  " +
			stFaint.Render(ago) + "\n")
	}
	if len(grows) == 0 {
		left.WriteString("  " + stMuted.Render("暂无监控群，到「群管理」勾选") + "\n")
	}
	for i := lipgloss.Height(left.String()); i < sectionH; i++ {
		left.WriteString("\n")
	}

	var right strings.Builder
	right.WriteString("  " + stTableHead.Render("最近匹配") + "\n")
	right.WriteString("  " + rule(min(36, p.w/2-6)) + "\n")
	if len(p.matched) == 0 {
		right.WriteString("  " + stMuted.Render("暂无匹配消息") + "\n")
	}
	for _, r := range p.matched {
		ts := time.Unix(r.CreateTime, 0).Format("01-02 15:04")
		group := r.GroupName
		if group == "" {
			group = r.GroupWxid
		}
		preview := wechatdb.Preview(r.Content, 16)
		right.WriteString("  " + stFaint.Render(ts) + "  " +
			stAccent.Render(padRunes(group, 10)) + " " +
			stInk.Render(preview) + "\n")
	}
	for i := lipgloss.Height(right.String()); i < sectionH; i++ {
		right.WriteString("\n")
	}

	colW := max(30, (p.w-6)/2)
	l := lipgloss.NewStyle().Width(colW).Render(left.String())
	r := lipgloss.NewStyle().Width(colW).Render(right.String())
	return "\n  " + cards + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, l, r)
}

func padRunes(s string, width int) string {
	w := lipgloss.Width(s)
	if w > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(s)
	}
	return s + strings.Repeat(" ", width-w)
}
