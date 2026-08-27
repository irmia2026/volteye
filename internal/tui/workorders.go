package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/app"
	"volteye/internal/capture"
	"volteye/internal/store"
)

type workOrdersLoadedMsg struct {
	orders []capture.WorkOrder
	errs   []store.ParseError
}

type workOrderExportMsg struct {
	count  int
	path   string
	append bool
	err    error
}

type workOrdersPanel struct {
	svc          *app.Service
	st           *store.Store
	orders       []capture.WorkOrder
	errs         []store.ParseError
	showErrs     bool
	cursor       int
	offset       int
	filter       textinput.Model
	filtering    bool
	filterText   string
	pathInput    textinput.Model
	enteringPath bool
	exporting    bool
	status       string
	w, h         int
}

func newWorkOrdersPanel(svc *app.Service) Panel {
	ti := textinput.New()
	ti.Placeholder = "单号/地址/电话关键词，回车确认，Esc 取消"
	ti.CharLimit = 64
	pi := textinput.New()
	pi.Placeholder = `要追加到的 VoltEye 导出文件路径，回车确认，Esc 取消`
	pi.CharLimit = 256
	return &workOrdersPanel{svc: svc, st: svc.St, filter: ti, pathInput: pi}
}

func (p *workOrdersPanel) Title() string { return "工单" }

func (p *workOrdersPanel) Help() string {
	if p.filtering || p.enteringPath {
		return "回车 应用 · Esc 取消"
	}
	if p.showErrs {
		return "↑↓ 滚动 · e 返回工单 · C 清空异常 · r 刷新"
	}
	return "↑↓ 滚动 · x 导出新表 · a 追加到旧表 · e 异常 · / 过滤 · c 清过滤"
}

func (p *workOrdersPanel) CapturesInput() bool { return p.filtering || p.enteringPath }

func (p *workOrdersPanel) SetSize(w, h int) {
	p.w, p.h = w, h
	p.filter.Width = w - 10
	p.pathInput.Width = w - 10
}

func (p *workOrdersPanel) Init() tea.Cmd { return p.reload }

func (p *workOrdersPanel) reload() tea.Msg {
	orders, err := p.st.QueryWorkOrders(store.WorkOrderFilter{Keyword: p.filterText, Limit: 500})
	if err != nil {
		return logMsg{text: "query work orders: " + err.Error()}
	}
	errs, err := p.st.ListParseErrors(200)
	if err != nil {
		return logMsg{text: "query parse errors: " + err.Error()}
	}
	return workOrdersLoadedMsg{orders: orders, errs: errs}
}

func (p *workOrdersPanel) itemCount() int {
	if p.showErrs {
		return len(p.errs)
	}
	return len(p.orders)
}

func (p *workOrdersPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workOrdersLoadedMsg:
		p.orders = msg.orders
		p.errs = msg.errs
		if p.cursor >= p.itemCount() {
			p.cursor = max(0, p.itemCount()-1)
		}
		return p, nil
	case workOrderExportMsg:
		p.exporting = false
		switch {
		case msg.err != nil:
			p.status = "失败: " + msg.err.Error()
		case msg.count == 0 && msg.append:
			p.status = "目标表已是最新，没有需要追加的新工单"
		case msg.count == 0:
			p.status = "没有可导出的工单（当前过滤条件下为 0 条）"
		case msg.append:
			p.status = fmt.Sprintf("已追加 %d 条新工单 → %s", msg.count, msg.path)
		default:
			p.status = fmt.Sprintf("已导出 %d 条工单 → %s", msg.count, msg.path)
		}
		return p, nil
	case pollTickMsg:
		return p, p.reload
	case tea.KeyMsg:
		if p.enteringPath {
			switch msg.String() {
			case "enter":
				// tolerate paths pasted with surrounding quotes
				path := strings.Trim(strings.TrimSpace(p.pathInput.Value()), `"'`)
				p.enteringPath = false
				p.pathInput.Blur()
				if path == "" {
					return p, nil
				}
				_ = p.st.SetSetting("last_append_path", path)
				p.exporting = true
				p.status = ""
				filter := store.WorkOrderFilter{Keyword: p.filterText}
				return p, func() tea.Msg {
					n, err := p.svc.AppendWorkOrders(filter, path)
					return workOrderExportMsg{count: n, path: path, append: true, err: err}
				}
			case "esc":
				p.enteringPath = false
				p.pathInput.Blur()
				return p, nil
			}
			var cmd tea.Cmd
			p.pathInput, cmd = p.pathInput.Update(msg)
			return p, cmd
		}
		if p.filtering {
			switch msg.String() {
			case "enter":
				p.filterText = strings.TrimSpace(p.filter.Value())
				p.filtering = false
				p.cursor = 0
				p.offset = 0
				return p, p.reload
			case "esc":
				p.filtering = false
				return p, nil
			}
			var cmd tea.Cmd
			p.filter, cmd = p.filter.Update(msg)
			return p, cmd
		}
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < p.itemCount()-1 {
				p.cursor++
			}
		case "e":
			p.showErrs = !p.showErrs
			p.cursor = 0
			p.offset = 0
		case "C":
			if p.showErrs {
				if err := p.st.ClearParseErrors(); err != nil {
					p.status = err.Error()
				}
				return p, p.reload
			}
		case "/":
			p.filtering = true
			p.filter.SetValue(p.filterText)
			return p, p.filter.Focus()
		case "c":
			p.filterText = ""
			p.cursor = 0
			p.offset = 0
			return p, p.reload
		case "x":
			if !p.exporting {
				p.exporting = true
				p.status = ""
				outPath := filepath.Join(p.svc.DataDir, "exports",
					fmt.Sprintf("VoltEye_工单_%s.xlsx", time.Now().Format("20060102_150405")))
				filter := store.WorkOrderFilter{Keyword: p.filterText}
				return p, func() tea.Msg {
					n, err := p.svc.ExportWorkOrders(filter, outPath)
					return workOrderExportMsg{count: n, path: outPath, err: err}
				}
			}
		case "a":
			if !p.exporting {
				p.enteringPath = true
				p.pathInput.SetValue(p.defaultAppendPath())
				return p, p.pathInput.Focus()
			}
		case "r":
			return p, p.reload
		}
	}
	return p, nil
}

func (p *workOrdersPanel) View() string {
	if p.showErrs {
		return p.viewErrors()
	}
	return p.viewOrders()
}

func (p *workOrdersPanel) viewOrders() string {
	var sb strings.Builder
	if p.filterText != "" {
		sb.WriteString("  " + stMuted.Render("过滤: "+p.filterText) + "\n")
	}
	header := fmt.Sprintf("  %s %s %s %s %s",
		padRunes("派工时间", 18), padRunes("工单号", 24), padRunes("联系人", 8),
		padRunes("联系电话", 14), "地址")
	sb.WriteString(stTableHead.Render(header) + "\n")
	sb.WriteString("  " + rule(min(p.w-4, 100)) + "\n")
	if len(p.orders) == 0 {
		sb.WriteString("  " + stMuted.Render("暂无工单") + "\n")
	}
	pageSize := max(1, p.h-16)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+pageSize {
		p.offset = p.cursor - pageSize + 1
	}
	end := min(len(p.orders), p.offset+pageSize)
	for i := p.offset; i < end; i++ {
		o := p.orders[i]
		ts := "未知时间"
		if o.DispatchTime > 0 {
			ts = time.Unix(o.DispatchTime, 0).Format("2006-01-02 15:04")
		}
		line := fmt.Sprintf("  %s %s %s %s %s",
			padRunes(ts, 18), padRunes(o.OrderNo, 24), padRunes(o.ContactName, 8),
			padRunes(o.ContactPhone, 14), o.Address)
		if i == p.cursor {
			line = stCursorRow.Render("▸") + line
		} else {
			line = " " + line
		}
		sb.WriteString(line + "\n")
	}
	if o := p.currentOrder(); o != nil {
		sb.WriteString("\n  " + stPanelTitle.Render("工单详情") + "\n")
		sb.WriteString("  " + rule(min(p.w-4, 100)) + "\n")
		detail := []struct{ k, v string }{
			{"单号", o.OrderNo},
			{"格式", o.Format},
			{"优先级", o.Priority},
			{"分类", o.Category},
			{"联系方式", o.ContactWay},
			{"描述", o.Description},
			{"用户", o.UserName + " " + o.UserNo},
			{"来源", o.GroupWxid + " / " + o.SenderWxid},
		}
		for _, d := range detail {
			if d.v == "" || d.v == " " || d.v == " / " {
				continue
			}
			sb.WriteString("  " + stMuted.Render(padRunes(d.k, 6)) + d.v + "\n")
		}
	}
	if p.exporting {
		sb.WriteString("\n  " + styleWarn.Render("导出中 ..."))
	} else if p.status != "" {
		sb.WriteString("\n  " + styleGood.Render(p.status))
	}
	if len(p.errs) > 0 {
		sb.WriteString("\n  " + stWarn.Render(fmt.Sprintf("有 %d 条解析异常，按 e 查看", len(p.errs))))
	}
	if p.filtering {
		sb.WriteString("\n  " + p.filter.View())
	}
	if p.enteringPath {
		sb.WriteString("\n  " + stLabel.Render("追加到: ") + p.pathInput.View())
	}
	return sb.String()
}

func (p *workOrdersPanel) viewErrors() string {
	var sb strings.Builder
	sb.WriteString("  " + stPanelTitle.Render("解析异常（签名命中但未能解析出工单）") + "\n")
	sb.WriteString("  " + rule(min(p.w-4, 100)) + "\n")
	if len(p.errs) == 0 {
		sb.WriteString("  " + stMuted.Render("暂无异常") + "\n")
	}
	pageSize := max(1, p.h-10)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+pageSize {
		p.offset = p.cursor - pageSize + 1
	}
	end := min(len(p.errs), p.offset+pageSize)
	for i := p.offset; i < end; i++ {
		e := p.errs[i]
		line := fmt.Sprintf("  %s %s", e.Reason, stMuted.Render(truncate(e.Content, 60)))
		if i == p.cursor {
			line = stCursorRow.Render("▸") + line
		} else {
			line = " " + line
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// defaultAppendPath prefers the newest export in dataDir\exports, then the
// path used last time, so the user rarely has to type a path at all.
func (p *workOrdersPanel) defaultAppendPath() string {
	exportsDir := filepath.Join(p.svc.DataDir, "exports")
	var newest string
	var newestTime time.Time
	if entries, err := os.ReadDir(exportsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".xlsx") {
				continue
			}
			if fi, err := e.Info(); err == nil && fi.ModTime().After(newestTime) {
				newestTime = fi.ModTime()
				newest = filepath.Join(exportsDir, e.Name())
			}
		}
	}
	if newest != "" {
		return newest
	}
	return p.st.GetSetting("last_append_path", "")
}

func (p *workOrdersPanel) currentOrder() *capture.WorkOrder {
	if p.cursor < 0 || p.cursor >= len(p.orders) {
		return nil
	}
	return &p.orders[p.cursor]
}

func truncate(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "..."
}
