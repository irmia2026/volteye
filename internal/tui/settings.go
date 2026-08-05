package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/app"
)

type clearDoneMsg struct {
	n    int64
	path string
	err  error
}

type settingsPanel struct {
	svc       *app.Service
	sel       int
	intervals []time.Duration
	intIdx    int
	daysOpts  []int
	daysIdx   int
	mbOpts    []int
	mbIdx     int
	autoStart bool
	confirm   string
	status    string
	w, h      int
}

func newSettingsPanel(svc *app.Service) Panel {
	p := &settingsPanel{
		svc:       svc,
		intervals: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second},
		daysOpts:  []int{0, 30, 90, 180, 365},
		mbOpts:    []int{0, 100, 500, 1000, 5000},
		autoStart: app.IsAutoStartEnabled(),
	}
	p.loadFromSettings()
	return p
}

func (p *settingsPanel) loadFromSettings() {
	cur := p.svc.PollInterval()
	p.intIdx = 2
	for i, d := range p.intervals {
		if d == cur {
			p.intIdx = i
			break
		}
	}
	if v, err := strconv.Atoi(p.svc.St.GetSetting("retention_days", "0")); err == nil {
		for i, d := range p.daysOpts {
			if d == v {
				p.daysIdx = i
				break
			}
		}
	}
	if v, err := strconv.Atoi(p.svc.St.GetSetting("max_db_mb", "0")); err == nil {
		for i, d := range p.mbOpts {
			if d == v {
				p.mbIdx = i
				break
			}
		}
	}
}

func (p *settingsPanel) Title() string { return "设置" }

func (p *settingsPanel) Help() string {
	if p.confirm != "" {
		return "y 确认 · n/Esc 取消"
	}
	return "↑↓ 选择 · ←→ 调整 · x 立即清理 · c 清空(先归档)"
}

func (p *settingsPanel) CapturesInput() bool { return p.confirm != "" }
func (p *settingsPanel) SetSize(w, h int)    { p.w, p.h = w, h }
func (p *settingsPanel) Init() tea.Cmd       { return nil }

func (p *settingsPanel) daysLabel() string {
	if p.daysOpts[p.daysIdx] == 0 {
		return "不自动清理"
	}
	return fmt.Sprintf("保留 %d 天", p.daysOpts[p.daysIdx])
}

func (p *settingsPanel) mbLabel() string {
	if p.mbOpts[p.mbIdx] == 0 {
		return "不限容量"
	}
	return fmt.Sprintf("上限 %d MB", p.mbOpts[p.mbIdx])
}

func (p *settingsPanel) applyRow() {
	switch p.sel {
	case 0:
		d := p.intervals[p.intIdx]
		if err := p.svc.SetPollInterval(d); err != nil {
			p.status = "轮询间隔保存失败: " + err.Error()
		} else {
			p.status = "轮询间隔已生效: " + d.String()
		}
	case 1:
		if err := p.svc.SetRetentionDays(p.daysOpts[p.daysIdx]); err != nil {
			p.status = "保留策略保存失败: " + err.Error()
		} else {
			p.status = "保留策略已保存，下次清理周期生效"
		}
	case 2:
		if err := p.svc.SetMaxDBMB(p.mbOpts[p.mbIdx]); err != nil {
			p.status = "容量上限保存失败: " + err.Error()
		} else {
			p.status = "容量上限已保存，下次清理周期生效"
		}
	case 3:
		if err := p.svc.SetAutoStart(p.autoStart); err != nil {
			p.autoStart = !p.autoStart
			p.status = "自启设置失败: " + err.Error()
		} else {
			if p.autoStart {
				p.status = "已开启开机自启"
			} else {
				p.status = "已关闭开机自启"
			}
		}
	}
}

func (p *settingsPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case clearDoneMsg:
		if msg.err != nil {
			p.status = "清空失败: " + msg.err.Error()
		} else if msg.path != "" {
			p.status = fmt.Sprintf("已清空 %d 条消息，归档: %s", msg.n, msg.path)
		} else {
			p.status = fmt.Sprintf("已清空 %d 条消息", msg.n)
		}
		return p, nil
	case tea.KeyMsg:
		if p.confirm != "" {
			switch msg.String() {
			case "y":
				if p.confirm == "clear" {
					p.status = "正在归档并清空 ..."
					p.confirm = ""
					return p, func() tea.Msg {
						n, path, err := p.svc.ClearAllMessages()
						return clearDoneMsg{n: n, path: path, err: err}
					}
				}
				p.confirm = ""
			default:
				p.confirm = ""
			}
			return p, nil
		}
		switch msg.String() {
		case "up", "k":
			if p.sel > 0 {
				p.sel--
			}
		case "down", "j":
			if p.sel < 3 {
				p.sel++
			}
		case "left", "h":
			p.adjust(-1)
		case "right", "l":
			p.adjust(1)
		case "x":
			p.status = "正在执行清理 ..."
			return p, func() tea.Msg {
				logs := p.svc.RunCleanup()
				if len(logs) == 0 {
					return logMsg{text: "清理完成：无需清理"}
				}
				return logMsg{text: "清理完成: " + strings.Join(logs, "; ")}
			}
		case "c":
			p.confirm = "clear"
		}
	}
	if _, ok := msg.(logMsg); ok && p.status == "正在执行清理 ..." {
		p.status = ""
	}
	return p, nil
}

func (p *settingsPanel) adjust(delta int) {
	switch p.sel {
	case 0:
		p.intIdx = wrap(p.intIdx+delta, len(p.intervals))
	case 1:
		p.daysIdx = wrap(p.daysIdx+delta, len(p.daysOpts))
	case 2:
		p.mbIdx = wrap(p.mbIdx+delta, len(p.mbOpts))
	case 3:
		p.autoStart = !p.autoStart
	}
	p.applyRow()
}

func (p *settingsPanel) row(idx int, label, value string) string {
	line := fmt.Sprintf(" %s %s", padRunes(label, 12), stAccent.Render("◀ ")+stInk.Render(value)+stAccent.Render(" ▶"))
	if idx == p.sel {
		return stCursorRow.Render("▸") + line
	}
	return " " + line
}

func (p *settingsPanel) View() string {
	var sb strings.Builder
	sb.WriteString(styleTitle.Render("运行设置") + "\n")
	sb.WriteString(p.row(0, "轮询间隔", p.intervals[p.intIdx].String()) + "\n")
	sb.WriteString(p.row(1, "消息保留", p.daysLabel()) + "\n")
	sb.WriteString(p.row(2, "存储容量", p.mbLabel()) + "\n")
	autoLabel := "关闭"
	if p.autoStart {
		autoLabel = "开启"
	}
	sb.WriteString(p.row(3, "开机自启", autoLabel) + "\n")
	sb.WriteString("\n" + styleMuted.Render("  数据目录: "+p.svc.DataDir) + "\n")
	sb.WriteString(styleMuted.Render("  x: 立即执行清理策略    c: 清空全部消息（先自动归档）") + "\n")
	if p.confirm == "clear" {
		sb.WriteString("\n  " + styleBad.Render("确认清空全部消息？归档后将删除，y 确认 / n 取消") + "\n")
	}
	if p.status != "" {
		sb.WriteString("\n  " + styleGood.Render(p.status) + "\n")
	}
	return sb.String()
}
