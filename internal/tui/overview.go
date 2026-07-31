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
		rows, _ := p.st.QueryMessages(store.MessageFilter{OnlyMatched: true, Limit: 8})
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

func statBlock(value, label string, width int) string {
	v := stAccentBold.Render(value)
	l := stMuted.Render(label)
	return lipgloss.NewStyle().Width(width).Render(v + "\n" + l)
}

func (p *overviewPanel) View() string {
	total, _ := p.st.TotalMessages()
	matched, _ := p.st.MatchedCount()
	monitored, _ := p.st.MonitoredGroups()
	counts, _ := p.st.GroupMessageCounts()
	latest, _ := p.st.LatestTimes()

	bw := max(12, (p.w-4)/4)
	stats := lipgloss.JoinHorizontal(lipgloss.Top,
		statBlock(fmt.Sprintf("%d", len(monitored)), "监控群", bw),
		statBlock(fmt.Sprintf("%d", total), "落盘消息", bw),
		statBlock(fmt.Sprintf("%d", matched), "规则匹配", bw),
		statBlock(compactAgo(p.lastPoll), "最近轮询", bw),
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

	var left strings.Builder
	left.WriteString(stTableHead.Render("  分群落盘") + "\n")
	left.WriteString("  " + rule(min(34, p.w/2-4)) + "\n")
	limit := min(len(grows), max(1, p.h-9))
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

	var right strings.Builder
	right.WriteString(stTableHead.Render("  最近匹配") + "\n")
	right.WriteString("  " + rule(min(34, p.w/2-4)) + "\n")
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

	colW := max(30, (p.w-4)/2)
	l := lipgloss.NewStyle().Width(colW).Render(left.String())
	r := lipgloss.NewStyle().Width(colW).Render(right.String())
	return stats + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, l, r)
}

func padRunes(s string, width int) string {
	w := lipgloss.Width(s)
	if w > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(s)
	}
	return s + strings.Repeat(" ", width-w)
}
