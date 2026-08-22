package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"volteye/internal/app"
	"volteye/internal/extract"
)

type AppConfig struct {
	DBStorage string
	DataDir   string
	Interval  time.Duration
	KeyHex    string
	Engine    *extract.Engine
	TrayMode  bool
	Boot      func(cfg AppConfig, send func(tea.Msg)) (*app.Service, error)
}

type clockTickMsg time.Time

type rootModel struct {
	cfg      AppConfig
	send     func(tea.Msg)
	panels   []Panel
	active   int
	width    int
	height   int
	booting  bool
	bootLog  []string
	bootErr  string
	svc      *app.Service
	lastPoll time.Time
	clock    time.Time
}

func NewRoot(cfg AppConfig) *rootModel {
	if cfg.Engine == nil {
		cfg.Engine = extract.NewEngine()
	}
	return &rootModel{cfg: cfg, booting: true, clock: time.Now()}
}

func (m *rootModel) SetSender(f func(tea.Msg)) { m.send = f }

func (m *rootModel) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			for m.send == nil {
				time.Sleep(10 * time.Millisecond)
			}
			boot := m.cfg.Boot
			if boot == nil {
				boot = DefaultBoot
			}
			svc, err := boot(m.cfg, m.send)
			return bootDoneMsg{svc: svc, err: err}
		},
		clockCmd(),
	)
}

func clockCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return clockTickMsg(t)
	})
}

func (m *rootModel) quit() (tea.Model, tea.Cmd) {
	if !m.cfg.TrayMode && m.svc != nil {
		m.svc.Stop()
	}
	return m, tea.Quit
}

func (m *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for _, p := range m.panels {
			p.SetSize(m.contentWidth(), m.contentHeight())
		}
		return m, nil
	case clockTickMsg:
		m.clock = time.Time(msg)
		return m, clockCmd()
	case bootStepMsg:
		m.bootLog = append(m.bootLog, msg.text)
		return m, nil
	case bootDoneMsg:
		m.booting = false
		if msg.err != nil {
			m.bootErr = msg.err.Error()
			return m, nil
		}
		m.svc = msg.svc
		m.panels = []Panel{
			newOverviewPanel(m.svc.St),
			newGroupsPanel(m.svc.St),
			newWorkOrdersPanel(m.svc),
			newMessagesPanel(m.svc.St, m.svc.Engine),
			newFormatsPanel(m.svc),
			newRulesPanel(m.svc),
			newExportPanel(m.svc),
			newSettingsPanel(m.svc),
			newLogsPanel(),
		}
		if m.width > 0 {
			for _, p := range m.panels {
				p.SetSize(m.contentWidth(), m.contentHeight())
			}
		}
		m.svc.Start()
		var cmds []tea.Cmd
		for _, p := range m.panels {
			cmds = append(cmds, p.Init())
		}
		return m, tea.Batch(cmds...)
	case pollTickMsg:
		m.lastPoll = msg.at
	case tea.KeyMsg:
		if m.booting || m.bootErr != "" {
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return m.quit()
			}
			return m, nil
		}
		if m.panels[m.active].CapturesInput() {
			if msg.String() == "ctrl+c" {
				return m.quit()
			}
			nm, cmd := m.panels[m.active].Update(msg)
			m.panels[m.active] = nm.(Panel)
			return m, cmd
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m.quit()
		case "tab":
			m.active = (m.active + 1) % len(m.panels)
			return m, nil
		case "shift+tab":
			m.active = (m.active - 1 + len(m.panels)) % len(m.panels)
			return m, nil
		}
		if d, err := strconv.Atoi(msg.String()); err == nil && d >= 1 && d <= len(m.panels) {
			m.active = d - 1
			return m, nil
		}
	}
	if m.booting || len(m.panels) == 0 {
		return m, nil
	}
	var cmds []tea.Cmd
	for i, p := range m.panels {
		if _, isKey := msg.(tea.KeyMsg); isKey && i != m.active {
			continue
		}
		nm, cmd := p.Update(msg)
		m.panels[i] = nm.(Panel)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

const sidebarWidth = 14

func (m *rootModel) contentWidth() int {
	if w := m.width - sidebarWidth - 2; w > 20 {
		return w
	}
	return 20
}

func (m *rootModel) contentHeight() int {
	if h := m.height - 5; h > 5 {
		return h
	}
	return 5
}

func rule(width int) string {
	if width < 1 {
		width = 1
	}
	return stRule.Render(strings.Repeat("─", width))
}

func (m *rootModel) View() string {
	if m.booting {
		var b strings.Builder
		b.WriteString("\n  " + stBrand.Render("VOLTEYE") + "\n\n")
		b.WriteString("  " + stPanelTitle.Render("初始化") + "\n")
		b.WriteString("  " + rule(min(60, m.width-4)) + "\n")
		for _, line := range m.bootLog {
			b.WriteString("  " + stMuted.Render(line) + "\n")
		}
		b.WriteString("\n  " + stFaint.Render("ctrl+c 退出"))
		return b.String()
	}
	if m.bootErr != "" {
		return "\n  " + stBad.Render("启动失败") + "\n\n  " + m.bootErr + "\n\n" +
			"  " + stWarn.Render("提示：请确认微信 4.x 已登录，并右键“以管理员身份运行”本程序。") + "\n\n" +
			"  " + stFaint.Render("q 退出")
	}

	header := "  " + stBrand.Render("VOLTEYE")
	meta := "微信已连接 · " + m.clock.Format("15:04:05")
	gap := m.width - lipgloss.Width(header) - lipgloss.Width(meta) - 2
	if gap < 1 {
		gap = 1
	}
	header += strings.Repeat(" ", gap) + stHeaderMeta.Render(meta)

	var nav []string
	nav = append(nav, "")
	for i, p := range m.panels {
		num := stFaint.Render(fmt.Sprintf("%d ", i+1))
		if i == m.active {
			nav = append(nav, "  "+stAccentBold.Render("▍")+" "+num+stNavActive.Render(p.Title()))
		} else {
			nav = append(nav, "    "+num+stNavIdle.Render(p.Title()))
		}
	}
	sidebar := lipgloss.NewStyle().Width(sidebarWidth).Render(strings.Join(nav, "\n"))

	title := "  " + stPanelTitle.Render(m.panels[m.active].Title())
	body := title + "\n" + "  " + rule(m.contentWidth()-2) + "\n" + m.panels[m.active].View()

	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, body)

	mainH := m.height - 4
	if mainH < 3 {
		mainH = 3
	}
	main = lipgloss.NewStyle().Height(mainH).MaxHeight(mainH).Render(main)

	status := "  " + stDot.Render("●") + stStatus.Render(" 采集中")
	if !m.lastPoll.IsZero() {
		status += stStatus.Render(" · 轮询 " + compactAgo(m.lastPoll))
	}
	quitHint := "q 退出"
	if m.cfg.TrayMode {
		quitHint = "q 隐藏到托盘"
	}
	hints := stFaint.Render("tab 切换 · " + quitHint + " · " + m.panels[m.active].Help())
	status += "  " + hints

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		"  "+rule(m.width-2),
		main,
		"  "+rule(m.width-2),
		status,
	)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
