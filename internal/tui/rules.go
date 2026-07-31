package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/extract"
	"volteye/internal/store"
)

type rulesLoadedMsg struct{ rules []store.Rule }

type ruleForm struct {
	step    int
	name    textinput.Model
	kws     textinput.Model
	re      textinput.Model
	keyword string
	nameVal string
}

type rulesPanel struct {
	st        *store.Store
	engine    *extract.Engine
	rules     []store.Rule
	cursor    int
	offset    int
	w, h      int
	form      *ruleForm
	confirmID int64
	status    string
}

func newRulesPanel(st *store.Store, engine *extract.Engine) Panel {
	return &rulesPanel{st: st, engine: engine}
}

func (p *rulesPanel) Title() string { return "规则" }

func (p *rulesPanel) Help() string {
	if p.form != nil {
		return "回车:下一步/保存  Esc:取消"
	}
	if p.confirmID != 0 {
		return "y:确认删除  n/Esc:取消"
	}
	return "↑/↓:移动  空格:启用/停用  n:新建  d:删除  r:刷新"
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

func (p *rulesPanel) syncEngine() tea.Cmd {
	return func() tea.Msg {
		rules, err := p.st.ListRules()
		if err != nil {
			return logMsg{text: "load rules: " + err.Error()}
		}
		var erules []extract.Rule
		for _, r := range rules {
			var kws []string
			for _, kw := range strings.Split(r.Keywords, ",") {
				if kw = strings.TrimSpace(kw); kw != "" {
					kws = append(kws, kw)
				}
			}
			erules = append(erules, extract.Rule{
				ID: r.ID, Name: r.Name, Keywords: kws, Regex: r.Regex, Enabled: r.Enabled,
			})
		}
		if err := p.engine.Load(erules); err != nil {
			return logMsg{text: "compile rules: " + err.Error()}
		}
		return rulesLoadedMsg{rules: rules}
	}
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
	case tea.KeyMsg:
		if p.form != nil {
			return p.updateForm(msg)
		}
		if p.confirmID != 0 {
			switch msg.String() {
			case "y":
				if err := p.st.DeleteRule(p.confirmID); err != nil {
					p.status = err.Error()
				}
				p.confirmID = 0
				return p, p.syncEngine()
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
				if err := p.st.SetRuleEnabled(r.ID, !r.Enabled); err != nil {
					p.status = err.Error()
				}
				return p, p.syncEngine()
			}
		case "n":
			p.form = newRuleForm()
			return p, nil
		case "d":
			if r := p.current(); r != nil {
				p.confirmID = r.ID
			}
			return p, nil
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
			f.nameVal = strings.TrimSpace(f.name.Value())
			if f.nameVal == "" {
				p.status = "名称不能为空"
				return p, nil
			}
			f.step = 1
			f.name.Blur()
			return p, f.kws.Focus()
		case 1:
			f.keyword = strings.TrimSpace(f.kws.Value())
			f.step = 2
			f.kws.Blur()
			return p, f.re.Focus()
		case 2:
			reText := strings.TrimSpace(f.re.Value())
			if reText != "" {
				probe := extract.NewEngine()
				if err := probe.Load([]extract.Rule{{Name: "probe", Regex: reText, Enabled: true}}); err != nil {
					p.status = "正则无效: " + err.Error()
					return p, nil
				}
			}
			if _, err := p.st.AddRule(f.nameVal, f.keyword, reText); err != nil {
				p.status = err.Error()
			} else {
				p.status = ""
			}
			p.form = nil
			return p, p.syncEngine()
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
	sb.WriteString(styleTitle.Render("识别规则") + "\n")
	header := fmt.Sprintf("  %s %s %s %s",
		padRunes("启用", 4), padRunes("名称", 16), padRunes("关键词", 36), "正则")
	sb.WriteString(styleHeader.Render(header) + "\n")
	if len(p.rules) == 0 {
		sb.WriteString(styleMuted.Render("  暂无规则，按 n 新建") + "\n")
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
		en := " "
		if r.Enabled {
			en = styleGood.Render("✓")
		}
		line := fmt.Sprintf("  %s   %s %s %s",
			en, padRunes(r.Name, 16), padRunes(r.Keywords, 36), r.Regex)
		if p.confirmID == r.ID {
			line += styleBad.Render("  [确认删除? y/n]")
		}
		if i == p.cursor {
			line = styleCursor.Render(line)
		}
		sb.WriteString(line + "\n")
	}
	if p.form != nil {
		sb.WriteString("\n" + styleTitle.Render("新建规则") + "\n")
		sb.WriteString("  名称:   " + p.form.name.View() + "\n")
		sb.WriteString("  关键词: " + p.form.kws.View() + "\n")
		sb.WriteString("  正则:   " + p.form.re.View() + "\n")
	}
	if p.status != "" {
		sb.WriteString("\n  " + styleBad.Render(p.status))
	}
	return sb.String()
}
