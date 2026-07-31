package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"volteye/internal/store"
	"volteye/internal/wechatdb"
)

type messagesLoadedMsg struct{ rows []store.MessageRow }

type messagesPanel struct {
	st          *store.Store
	engine      ruleNamer
	vp          viewport.Model
	rows        []store.MessageRow
	filter      textinput.Model
	filtering   bool
	filterText  string
	onlyMatched bool
	follow      bool
	w, h        int
	status      string
}

type ruleNamer interface {
	RuleNames(ids []int64) []string
}

func newMessagesPanel(st *store.Store, engine ruleNamer) Panel {
	ti := textinput.New()
	ti.Placeholder = "群名或内容关键词，回车确认，Esc 取消"
	ti.CharLimit = 64
	return &messagesPanel{st: st, engine: engine, filter: ti, follow: true}
}

func (p *messagesPanel) Title() string { return "消息流" }

func (p *messagesPanel) Help() string {
	if p.filtering {
		return "回车:应用过滤  Esc:取消"
	}
	return "↑/↓/pgup/pgdn:滚动  f:跟随最新  m:只看匹配  /:过滤  c:清过滤"
}

func (p *messagesPanel) CapturesInput() bool { return p.filtering }
func (p *messagesPanel) Init() tea.Cmd       { return p.reload }

func (p *messagesPanel) SetSize(w, h int) {
	p.w, p.h = w, h
	p.vp.Width = w - 2
	p.vp.Height = h - 3
	p.filter.Width = w - 10
	p.renderRows()
}

func (p *messagesPanel) reload() tea.Msg {
	rows, err := p.st.QueryMessages(store.MessageFilter{
		Keyword:     p.filterText,
		OnlyMatched: p.onlyMatched,
		Limit:       500,
	})
	if err != nil {
		return logMsg{text: "query messages: " + err.Error()}
	}
	return messagesLoadedMsg{rows: rows}
}

func (p *messagesPanel) renderRows() {
	var sb strings.Builder
	if len(p.rows) == 0 {
		sb.WriteString(styleMuted.Render("  暂无消息") + "\n")
	}
	for _, r := range p.rows {
		sb.WriteString(p.renderRow(r) + "\n")
	}
	atBottom := p.vp.AtBottom()
	yoff := p.vp.YOffset
	p.vp.SetContent(sb.String())
	if p.follow || atBottom {
		p.vp.GotoBottom()
	} else {
		p.vp.SetYOffset(yoff)
	}
}

var styleMatched = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)

func (p *messagesPanel) renderRow(r store.MessageRow) string {
	ts := time.Unix(r.CreateTime, 0).Format("01-02 15:04")
	group := r.GroupName
	if group == "" {
		group = r.GroupWxid
	}
	sender := r.SenderWxid
	if sender == "" {
		sender = "-"
	}
	brief := wechatdb.TypeBrief(r.LocalType)
	content := wechatdb.Preview(r.Content, 80)
	body := content
	if brief != "" {
		body = brief + " " + content
	}
	meta := styleMuted.Render(ts) + " " + padRunes(group, 14) + " " + padRunes(sender, 16)
	if r.Matched {
		tags := ""
		if p.engine != nil && r.MatchedRules != "" {
			ids := parseIDs(r.MatchedRules)
			names := p.engine.RuleNames(ids)
			if len(names) > 0 {
				tags = " ⚑" + strings.Join(names, ",")
			}
		}
		return styleMatched.Render("●") + " " + meta + " " + styleMatched.Render(body+tags)
	}
	return "  " + meta + " " + body
}

func parseIDs(s string) []int64 {
	var out []int64
	for _, part := range strings.Split(s, ",") {
		var id int64
		if _, err := fmt.Sscanf(part, "%d", &id); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func (p *messagesPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case messagesLoadedMsg:
		p.rows = msg.rows
		p.renderRows()
		return p, nil
	case pollTickMsg:
		return p, p.reload
	case tea.KeyMsg:
		if p.filtering {
			switch msg.String() {
			case "enter":
				p.filterText = strings.TrimSpace(p.filter.Value())
				p.filtering = false
				p.filter.Blur()
				return p, p.reload
			case "esc":
				p.filtering = false
				p.filter.Blur()
				return p, nil
			}
			var cmd tea.Cmd
			p.filter, cmd = p.filter.Update(msg)
			return p, cmd
		}
		switch msg.String() {
		case "/":
			p.filtering = true
			p.filter.SetValue(p.filterText)
			return p, p.filter.Focus()
		case "c":
			p.filterText = ""
			p.onlyMatched = false
			return p, p.reload
		case "m":
			p.onlyMatched = !p.onlyMatched
			return p, p.reload
		case "f":
			p.follow = true
			p.vp.GotoBottom()
			return p, nil
		case "g":
			p.follow = false
			p.vp.GotoTop()
			return p, nil
		case "G":
			p.follow = true
			p.vp.GotoBottom()
			return p, nil
		}
		if msg.String() == "up" || msg.String() == "down" || msg.String() == "pgup" || msg.String() == "pgdown" {
			p.follow = false
		}
	}
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return p, cmd
}

func (p *messagesPanel) View() string {
	title := styleTitle.Render("消息流")
	var flags []string
	if p.onlyMatched {
		flags = append(flags, styleWarn.Render("只看匹配"))
	}
	if p.filterText != "" {
		flags = append(flags, "过滤:"+p.filterText)
	}
	if p.follow {
		flags = append(flags, "跟随")
	}
	if len(flags) > 0 {
		title += "  " + styleMuted.Render("["+strings.Join(flags, " ")+"]")
	}
	title += styleMuted.Render(fmt.Sprintf("  %d 条", len(p.rows)))
	body := p.vp.View()
	if p.filtering {
		body += "\n" + p.filter.View()
	}
	return title + "\n" + body
}
