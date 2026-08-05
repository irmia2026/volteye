package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/app"
	"volteye/internal/store"
)

type rulesLoadedMsg struct{ rules []store.Rule }

type rescanDoneMsg struct {
	n   int64
	err error
}

type ruleForm struct {
	step    int
	name    textinput.Model
	kws     textinput.Model
	re      textinput.Model
	keyword string
	nameVal string
}

type rulesPanel struct {
	svc       *app.Service
	st        *store.Store
	rules     []store.Rule
	cursor    int
	offset    int
	w, h      int
	form      *ruleForm
	confirmID int64
	status    string
}

func newRulesPanel(svc *app.Service) Panel {
	return &rulesPanel{svc: svc, st: svc.St}
}

func (p *rulesPanel) Title() string { return "规则" }

func (p *rulesPanel) Help() string {
	if p.form != nil {
		return "回车 下一步/保存 · Esc 取消"
	}
	if p.confirmID != 0 {
		return "y 确认删除 · n/Esc 取消"
	}
	return "↑↓ 移动 · 空格 启停 · n 新建 · d 删除 · R 重扫全部 · r 刷新"
}

func (p *rulesPanel) CapturesInput() bool { return p.form != nil || p.confirmID != 0 }
func (p *rulesPanel) SetSize(w, h int)    { p.w, p.h = w, h }

func (p *rulesPanel) Init() tea.Cmd { return p.reload }

func (p *rulesPanel) reload() tea.Msg {
	rules, err := p.st.ListRules()
	if err != nil {
		return logMsg{text: "load rules: " + err.Error()}
	}
	return rulesLoadedMsg{rules: rules}
}

func newRuleForm() *ruleForm {
	mk := func(ph string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = ph
		ti.CharLimit = 128
		return ti
	}
	f := &ruleForm{
		name: mk("规则名称，如：工单"),
		kws:  mk("关键词，逗号分隔，如：工单,报修"),
		re:   mk("正则表达式（可留空）"),
	}
	f.name.Focus()
	return f
}

func (p *rulesPanel) current() *store.Rule {
	if p.cursor < 0 || p.cursor >= len(p.rules) {
		return nil
	}
	return &p.rules[p.cursor]
}

func (p *rulesPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case rulesLoadedMsg:
		p.rules = msg.rules
		if p.cursor >= len(p.rules) {
			p.cursor = max(0, len(p.rules)-1)
		}
		return p, nil
	case rescanDoneMsg:
		if msg.err != nil {
			p.status = "重扫失败: " + msg.err.Error()
		} else {
			p.status = fmt.Sprintf("已重置 %d 条消息，将在下轮轮询重新匹配", msg.n)
		}
		return p, nil
	case tea.KeyMsg:
		if p.form != nil {
			return p.updateForm(msg)
		}
		if p.confirmID != 0 {
			switch msg.String() {
			case "y":
				if err := p.svc.DeleteRule(p.confirmID); err != nil {
					p.status = err.Error()
				}
				p.confirmID = 0
				return p, p.reload
			default:
				p.confirmID = 0
				return p, nil
			}
		}
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.rules)-1 {
				p.cursor++
			}
		case " ":
			if r := p.current(); r != nil {
				if err := p.svc.SetRuleEnabled(r.ID, !r.Enabled); err != nil {
					p.status = err.Error()
				}
				return p, p.reload
			}
		case "n":
			p.form = newRuleForm()
			return p, nil
		case "d":
			if r := p.current(); r != nil {
				p.confirmID = r.ID
			}
			return p, nil
		case "R":
			return p, func() tea.Msg {
				n, err := p.svc.RescanAll()
				return rescanDoneMsg{n: n, err: err}
			}
		case "r":
			return p, p.reload
		}
	}
	return p, nil
}

func (p *rulesPanel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := p.form
	if msg.String() == "esc" {
		p.form = nil
		return p, nil
	}
	if msg.String() == "enter" {
		switch f.step {
		case 0:
			f.nameVal = f.name.Value()
			f.step = 1
			f.name.Blur()
			return p, f.kws.Focus()
		case 1:
			f.keyword = f.kws.Value()
			f.step = 2
			f.kws.Blur()
			return p, f.re.Focus()
		case 2:
			if err := p.svc.AddRule(f.nameVal, f.keyword, f.re.Value()); err != nil {
				p.status = err.Error()
				return p, nil
			}
			p.status = ""
			p.form = nil
			return p, p.reload
		}
	}
	var cmd tea.Cmd
	switch f.step {
	case 0:
		f.name, cmd = f.name.Update(msg)
	case 1:
		f.kws, cmd = f.kws.Update(msg)
	case 2:
		f.re, cmd = f.re.Update(msg)
	}
	return p, cmd
}

func (p *rulesPanel) View() string {
	var sb strings.Builder
	header := fmt.Sprintf("   %s %s %s %s",
		padRunes("启用", 4), padRunes("名称", 16), padRunes("关键词", 36), "正则")
	sb.WriteString("  " + stTableHead.Render(header) + "\n")
	sb.WriteString("  " + rule(min(p.w-4, 80)) + "\n")
	if len(p.rules) == 0 {
		sb.WriteString("  " + stMuted.Render("暂无规则，按 n 新建") + "\n")
	}
	pageSize := max(1, p.h-8)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+pageSize {
		p.offset = p.cursor - pageSize + 1
	}
	end := min(len(p.rules), p.offset+pageSize)
	for i := p.offset; i < end; i++ {
		r := p.rules[i]
		en := stFaint.Render("○")
		if r.Enabled {
			en = stOk.Render("●")
		}
		line := fmt.Sprintf(" %s   %s %s %s",
			en, padRunes(r.Name, 16), padRunes(r.Keywords, 36), r.Regex)
		if p.confirmID == r.ID {
			line += stBad.Render("  [确认删除? y/n]")
		}
		if i == p.cursor {
			line = stCursorRow.Render("▸") + line
		} else {
			line = " " + line
		}
		sb.WriteString(line + "\n")
	}
	if p.form != nil {
		sb.WriteString("\n  " + stPanelTitle.Render("新建规则") + "\n")
		sb.WriteString("  名称:   " + p.form.name.View() + "\n")
		sb.WriteString("  关键词: " + p.form.kws.View() + "\n")
		sb.WriteString("  正则:   " + p.form.re.View() + "\n")
	}
	if p.status != "" {
		sb.WriteString("\n  " + styleBad.Render(p.status))
	}
	return sb.String()
}
