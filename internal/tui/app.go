package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"volteye/internal/extract"
	"volteye/internal/store"
	"volteye/internal/sync"
)

type AppConfig struct {
	DBStorage string
	DataDir   string
	Interval  time.Duration
	KeyHex    string
	Engine    *extract.Engine
	Boot      func(cfg AppConfig, send func(tea.Msg)) (*store.Store, *sync.Collector, error)
}

type rootModel struct {
	cfg     AppConfig
	send    func(tea.Msg)
	panels  []Panel
	active  int
	width   int
	height  int
	booting bool
	bootLog []string
	bootErr string
	st      *store.Store
	col     *sync.Collector
}

func NewRoot(cfg AppConfig) *rootModel {
	if cfg.Engine == nil {
		cfg.Engine = extract.NewEngine()
	}
	return &rootModel{cfg: cfg, booting: true}
}

func (m *rootModel) SetSender(f func(tea.Msg)) { m.send = f }

func (m *rootModel) Init() tea.Cmd {
	return func() tea.Msg {
		for m.send == nil {
			time.Sleep(10 * time.Millisecond)
		}
		boot := m.cfg.Boot
		if boot == nil {
			boot = DefaultBoot
		}
		st, col, err := boot(m.cfg, m.send)
		return bootDoneMsg{st: st, col: col, err: err}
	}
}

func (m *rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for _, p := range m.panels {
			p.SetSize(msg.Width, msg.Height-4)
		}
		return m, nil
	case bootStepMsg:
		m.bootLog = append(m.bootLog, msg.text)
		return m, nil
	case bootDoneMsg:
		m.booting = false
		if msg.err != nil {
			m.bootErr = msg.err.Error()
			return m, nil
		}
		m.st, m.col = msg.st, msg.col
		m.panels = []Panel{
			newOverviewPanel(m.st),
			newGroupsPanel(m.st),
			newMessagesPanel(m.st, m.cfg.Engine),
			newRulesPanel(m.st, m.cfg.Engine),
			newExportPanel(m.st, m.cfg.DataDir),
			newStubPanel("设置", "M5 里程碑：轮询间隔 / 保留策略 / 自启"),
			newLogsPanel(),
		}
		if m.width > 0 {
			for _, p := range m.panels {
				p.SetSize(m.width, m.height-4)
			}
		}
		cmds := []tea.Cmd{startCollectorCmd(m.col)}
		for _, p := range m.panels {
			cmds = append(cmds, p.Init())
		}
		return m, tea.Batch(cmds...)
	case tea.KeyMsg:
		if m.booting || m.bootErr != "" {
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return m, tea.Quit
			}
			return m, nil
		}
		if m.panels[m.active].CapturesInput() {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			nm, cmd := m.panels[m.active].Update(msg)
			m.panels[m.active] = nm.(Panel)
			return m, cmd
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
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

func startCollectorCmd(col *sync.Collector) tea.Cmd {
	if col == nil {
		return nil
	}
	return func() tea.Msg {
		_ = col.Run(nilContext())
		return nil
	}
}

func (m *rootModel) View() string {
	if m.booting {
		var b strings.Builder
		b.WriteString(styleTitle.Render("VoltEye 启动中 ...") + "\n\n")
		for _, line := range m.bootLog {
			b.WriteString("  " + line + "\n")
		}
		b.WriteString("\n" + styleFooter.Render("ctrl+c 退出"))
		return b.String()
	}
	if m.bootErr != "" {
		return styleBad.Render("启动失败") + "\n\n  " + m.bootErr + "\n\n" +
			styleWarn.Render("提示：请确认微信 4.x 已登录，并右键\"以管理员身份运行\"本程序。") + "\n\n" +
			styleFooter.Render("q 退出")
	}

	var tabs []string
	for i, p := range m.panels {
		label := fmt.Sprintf("%d %s", i+1, p.Title())
		if i == m.active {
			tabs = append(tabs, styleTabActive.Render(label))
		} else {
			tabs = append(tabs, styleTabIdle.Render(label))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)

	sep := styleSeparator.Render(strings.Repeat("─", max(m.width, 1)))
	help := m.panels[m.active].Help()
	footer := styleFooter.Render(help + "  |  tab/数字:切换面板  q:退出")

	content := m.panels[m.active].View()
	return lipgloss.JoinVertical(lipgloss.Left, tabBar, sep, content, sep, footer)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
