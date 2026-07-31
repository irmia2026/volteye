package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"volteye/internal/store"
)

type overviewPanel struct {
	st           *store.Store
	w, h         int
	lastPoll     time.Time
	lastInserted int
	polls        int
}

func newOverviewPanel(st *store.Store) Panel {
	return &overviewPanel{st: st}
}

func (p *overviewPanel) Title() string       { return "总览" }
func (p *overviewPanel) Help() string        { return "自动刷新" }
func (p *overviewPanel) CapturesInput() bool { return false }
func (p *overviewPanel) SetSize(w, h int)    { p.w, p.h = w, h }
func (p *overviewPanel) Init() tea.Cmd       { return nil }

func (p *overviewPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tick, ok := msg.(pollTickMsg); ok {
		p.lastPoll = tick.at
		p.lastInserted = tick.inserted
		p.polls++
	}
	return p, nil
}

func (p *overviewPanel) View() string {
	total, _ := p.st.TotalMessages()
	counts, _ := p.st.GroupMessageCounts()
	latest, _ := p.st.LatestTimes()
	monitored, _ := p.st.MonitoredGroups()

	status := styleGood.Render("● 采集运行中")
	pollInfo := "尚未轮询"
	if !p.lastPoll.IsZero() {
		pollInfo = fmt.Sprintf("%s 前 (本轮新增 %d, 累计轮询 %d 次)",
			time.Since(p.lastPoll).Round(time.Second), p.lastInserted, p.polls)
	}
	left := styleTitle.Render("采集状态") + "\n" +
		status + "\n" +
		fmt.Sprintf("最近轮询: %s\n", pollInfo) +
		fmt.Sprintf("监控群数: %s", styleGood.Render(fmt.Sprintf("%d", len(monitored)))) + "\n" +
		fmt.Sprintf("落盘总量: %s", styleGood.Render(fmt.Sprintf("%d", total)))

	type row struct {
		name  string
		count int64
		last  int64
	}
	var rows []row
	for _, g := range monitored {
		name := g.Name
		if name == "" {
			name = g.Wxid
		}
		rows = append(rows, row{name, counts[g.Wxid], latest[g.Wxid]})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].last > rows[j].last })

	var sb strings.Builder
	sb.WriteString(styleTitle.Render("分群落盘") + "\n")
	limit := len(rows)
	maxRows := p.h - 8
	if maxRows > 0 && limit > maxRows {
		limit = maxRows
	}
	for i := 0; i < limit; i++ {
		r := rows[i]
		ago := ""
		if r.last > 0 {
			ago = compactAgo(time.Unix(r.last, 0))
		}
		sb.WriteString(fmt.Sprintf("  %s  %s条  %s\n",
			padRunes(r.name, 24), styleGood.Render(fmt.Sprintf("%6d", r.count)), styleMuted.Render(ago)))
	}
	if len(rows) == 0 {
		sb.WriteString(styleMuted.Render("  暂无监控群，请到 [2 群管理] 勾选") + "\n")
	}

	boxW := (p.w - 6) / 2
	if boxW < 30 {
		boxW = 30
	}
	leftBox := styleBox.Width(boxW).Render(left)
	rightBox := styleBox.Width(boxW).Render(strings.TrimRight(sb.String(), "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, "  ", rightBox)
}

func padRunes(s string, width int) string {
	w := lipgloss.Width(s)
	if w > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(s)
	}
	return s + strings.Repeat(" ", width-w)
}
