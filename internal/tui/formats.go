package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/app"
	"volteye/internal/capture"
	"volteye/internal/store"
)

type formatsLoadedMsg struct{ formats []capture.FormatConfig }

type formatForm struct {
	editing  bool
	editID   int64
	step     int
	name     textinput.Model
	sig      textinput.Model
	openB    textinput.Model
	closeB   textinput.Model
	aliases  textinput.Model
	catKey   textinput.Model
}

type formatsPanel struct {
	svc       *app.Service
	st        *store.Store
	formats   []capture.FormatConfig
	cursor    int
	offset    int
	w, h      int
	form      *formatForm
	confirmID int64
	status    string
}

func newFormatsPanel(svc *app.Service) Panel {
	return &formatsPanel{svc: svc, st: svc.St}
}

func (p *formatsPanel) Title() string { return "格式" }

func (p *formatsPanel) Help() string {
	if p.form != nil {
		return "回车 下一步/保存 · Esc 取消"
	}
	if p.confirmID != 0 {
		return "y 确认删除 · n/Esc 取消"
	}
	return "↑↓ 移动 · 空格 启停 · n 新建 · e 编辑 · d 删除 · r 刷新"
}

func (p *formatsPanel) CapturesInput() bool { return p.form != nil || p.confirmID != 0 }
func (p *formatsPanel) SetSize(w, h int)    { p.w, p.h = w, h }

func (p *formatsPanel) Init() tea.Cmd { return p.reload }

func (p *formatsPanel) reload() tea.Msg {
	formats, err := p.st.ListFormats()
	if err != nil {
		return logMsg{text: "load formats: " + err.Error()}
	}
	return formatsLoadedMsg{formats: formats}
}

func mkFormatInput(ph string, limit int) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = ph
	ti.CharLimit = limit
	return ti
}

func newFormatForm() *formatForm {
	f := &formatForm{
		name:    mkFormatInput("格式名称，如：csg-c-催办单", 64),
		sig:     mkFormatInput("签名正则，如：(?s)【南方电网】.*催办", 256),
		openB:   mkFormatInput("开括号，如：【 或 [", 8),
		closeB:  mkFormatInput("闭括号，如：】 或 ]", 8),
		aliases: mkFormatInput("字段映射，分号分隔，如：工单号=order_no;地址=address", 512),
		catKey:  mkFormatInput("分类链标记（可留空），如：业务类型为", 64),
	}
	f.name.Focus()
	return f
}

func editFormatForm(cfg *capture.FormatConfig) *formatForm {
	f := newFormatForm()
	f.editing = true
	f.editID = cfg.ID
	f.name.SetValue(cfg.Name)
	f.sig.SetValue(cfg.Signature)
	f.openB.SetValue(cfg.OpenB)
	f.closeB.SetValue(cfg.CloseB)
	f.aliases.SetValue(strings.ReplaceAll(strings.TrimSpace(cfg.Aliases), "\n", ";"))
	f.catKey.SetValue(cfg.CategoryKey)
	return f
}

func (p *formatsPanel) current() *capture.FormatConfig {
	if p.cursor < 0 || p.cursor >= len(p.formats) {
		return nil
	}
	return &p.formats[p.cursor]
}

func (p *formatsPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case formatsLoadedMsg:
		p.formats = msg.formats
		if p.cursor >= len(p.formats) {
			p.cursor = max(0, len(p.formats)-1)
		}
		return p, nil
	case tea.KeyMsg:
		if p.form != nil {
			return p.updateForm(msg)
		}
		if p.confirmID != 0 {
			switch msg.String() {
			case "y":
				if err := p.svc.DeleteFormat(p.confirmID); err != nil {
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
			if p.cursor < len(p.formats)-1 {
				p.cursor++
			}
		case " ":
			if f := p.current(); f != nil {
				if err := p.svc.SetFormatEnabled(f.ID, !f.Enabled); err != nil {
					p.status = err.Error()
				}
				return p, p.reload
			}
		case "n":
			p.form = newFormatForm()
			return p, nil
		case "e":
			if f := p.current(); f != nil {
				p.form = editFormatForm(f)
			}
			return p, nil
		case "d":
			if f := p.current(); f != nil {
				p.confirmID = f.ID
			}
			return p, nil
		case "r":
			return p, p.reload
		}
	}
	return p, nil
}

func (p *formatsPanel) formInputs() []*textinput.Model {
	f := p.form
	return []*textinput.Model{&f.name, &f.sig, &f.openB, &f.closeB, &f.aliases, &f.catKey}
}

func (p *formatsPanel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := p.form
	if msg.String() == "esc" {
		p.form = nil
		return p, nil
	}
	inputs := p.formInputs()
	if msg.String() == "enter" {
		if f.step < len(inputs)-1 {
			inputs[f.step].Blur()
			f.step++
			return p, inputs[f.step].Focus()
		}
		cfg := capture.FormatConfig{
			ID:          f.editID,
			Name:        f.name.Value(),
			Kind:        "bracketkv",
			Signature:   f.sig.Value(),
			OpenB:       f.openB.Value(),
			CloseB:      f.closeB.Value(),
			Aliases:     strings.ReplaceAll(f.aliases.Value(), ";", "\n"),
			CategoryKey: f.catKey.Value(),
		}
		var err error
		if f.editing {
			err = p.svc.UpdateFormat(cfg)
		} else {
			err = p.svc.AddFormat(cfg)
		}
		if err != nil {
			p.status = err.Error()
			return p, nil
		}
		p.status = ""
		p.form = nil
		return p, p.reload
	}
	var cmd tea.Cmd
	*inputs[f.step], cmd = inputs[f.step].Update(msg)
	return p, cmd
}

func (p *formatsPanel) View() string {
	var sb strings.Builder
	header := fmt.Sprintf("   %s %s %s %s",
		padRunes("启用", 4), padRunes("名称", 18), padRunes("括号", 6), "签名正则")
	sb.WriteString("  " + stTableHead.Render(header) + "\n")
	sb.WriteString("  " + rule(min(p.w-4, 90)) + "\n")
	if len(p.formats) == 0 {
		sb.WriteString("  " + stMuted.Render("暂无格式，按 n 新建") + "\n")
	}
	pageSize := max(1, p.h-16)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+pageSize {
		p.offset = p.cursor - pageSize + 1
	}
	end := min(len(p.formats), p.offset+pageSize)
	for i := p.offset; i < end; i++ {
		f := p.formats[i]
		en := stFaint.Render("○")
		if f.Enabled {
			en = stOk.Render("●")
		}
		line := fmt.Sprintf(" %s   %s %s %s",
			en, padRunes(f.Name, 18), padRunes(f.OpenB+f.CloseB, 6), f.Signature)
		if p.confirmID == f.ID {
			line += stBad.Render("  [确认删除? y/n]")
		}
		if i == p.cursor {
			line = stCursorRow.Render("▸") + line
		} else {
			line = " " + line
		}
		sb.WriteString(line + "\n")
	}
	if f := p.current(); f != nil && p.form == nil {
		sb.WriteString("\n  " + stPanelTitle.Render("字段映射") + "\n")
		for _, line := range strings.Split(strings.TrimSpace(f.Aliases), "\n") {
			sb.WriteString("    " + stMuted.Render(line) + "\n")
		}
		if f.CategoryKey != "" {
			sb.WriteString("    " + stMuted.Render("分类链标记: "+f.CategoryKey) + "\n")
		}
	}
	if p.form != nil {
		title := "新建格式"
		if p.form.editing {
			title = "编辑格式"
		}
		sb.WriteString("\n  " + stPanelTitle.Render(title) + "\n")
		sb.WriteString("  名称:     " + p.form.name.View() + "\n")
		sb.WriteString("  签名:     " + p.form.sig.View() + "\n")
		sb.WriteString("  开括号:   " + p.form.openB.View() + "\n")
		sb.WriteString("  闭括号:   " + p.form.closeB.View() + "\n")
		sb.WriteString("  字段映射: " + p.form.aliases.View() + "\n")
		sb.WriteString("  分类链:   " + p.form.catKey.View() + "\n")
	}
	if p.status != "" {
		sb.WriteString("\n  " + styleBad.Render(p.status))
	}
	return sb.String()
}
