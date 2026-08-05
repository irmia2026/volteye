package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/app"
	"volteye/internal/export"
	"volteye/internal/store"
)

type exportGroupsMsg struct{ groups []store.Group }

type exportDoneMsg struct {
	count int
	path  string
	err   error
}

type exportFile struct {
	name    string
	size    int64
	modTime time.Time
}

type exportPanel struct {
	svc      *app.Service
	st       *store.Store
	dataDir  string
	groups   []store.Group
	sel      int
	groupIdx int
	scope    int
	rangeIdx int
	running  bool
	status   string
	files    []exportFile
	w, h     int
}

var scopeLabels = []string{"全部消息", "仅匹配消息"}
var rangeLabels = []string{"全部时间", "最近1天", "最近7天", "最近30天"}
var rangeDays = []int{0, 1, 7, 30}

func newExportPanel(svc *app.Service) Panel {
	return &exportPanel{svc: svc, st: svc.St, dataDir: svc.DataDir}
}

func (p *exportPanel) Title() string { return "导出" }
func (p *exportPanel) Help() string {
	if p.running {
		return "导出中 ..."
	}
	return "↑↓ 选择 · ←→ 调整 · x 执行导出 · r 刷新"
}
func (p *exportPanel) CapturesInput() bool { return false }
func (p *exportPanel) SetSize(w, h int)    { p.w, p.h = w, h }

func (p *exportPanel) Init() tea.Cmd {
	return func() tea.Msg {
		groups, _ := p.st.MonitoredGroups()
		return exportGroupsMsg{groups: groups}
	}
}

func (p *exportPanel) loadFiles() {
	var files []exportFile
	for _, dir := range []string{"exports", "archive"} {
		entries, err := os.ReadDir(filepath.Join(p.dataDir, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".xlsx") {
				continue
			}
			if fi, err := e.Info(); err == nil {
				files = append(files, exportFile{
					name: filepath.Join(dir, e.Name()), size: fi.Size(), modTime: fi.ModTime(),
				})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	p.files = files
}

func (p *exportPanel) runExport() tea.Cmd {
	opts := export.Options{OnlyMatched: p.scope == 1}
	if p.groupIdx > 0 && p.groupIdx-1 < len(p.groups) {
		opts.GroupWxid = p.groups[p.groupIdx-1].Wxid
	}
	if d := rangeDays[p.rangeIdx]; d > 0 {
		opts.Start = time.Now().AddDate(0, 0, -d)
	}
	outPath := filepath.Join(p.dataDir, "exports",
		fmt.Sprintf("messages_%s.xlsx", time.Now().Format("20060102_150405")))
	svc := p.svc
	return func() tea.Msg {
		n, err := svc.ExportMessages(opts, outPath)
		return exportDoneMsg{count: n, path: outPath, err: err}
	}
}

func (p *exportPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case exportGroupsMsg:
		p.groups = msg.groups
		p.loadFiles()
		return p, nil
	case exportDoneMsg:
		p.running = false
		if msg.err != nil {
			p.status = "导出失败: " + msg.err.Error()
		} else {
			p.status = fmt.Sprintf("已导出 %d 条 → %s", msg.count, msg.path)
		}
		p.loadFiles()
		return p, nil
	case tea.KeyMsg:
		if p.running {
			return p, nil
		}
		maxSel := 2
		switch msg.String() {
		case "up", "k":
			if p.sel > 0 {
				p.sel--
			}
		case "down", "j":
			if p.sel < maxSel {
				p.sel++
			}
		case "left", "h":
			p.adjust(-1)
		case "right", "l":
			p.adjust(1)
		case "x":
			p.running = true
			p.status = ""
			return p, p.runExport()
		case "r":
			p.loadFiles()
		}
	}
	return p, nil
}

func (p *exportPanel) adjust(delta int) {
	switch p.sel {
	case 0:
		p.groupIdx = wrap(p.groupIdx+delta, len(p.groups)+1)
	case 1:
		p.scope = wrap(p.scope+delta, len(scopeLabels))
	case 2:
		p.rangeIdx = wrap(p.rangeIdx+delta, len(rangeLabels))
	}
}

func wrap(v, n int) int {
	if n <= 0 {
		return 0
	}
	return ((v % n) + n) % n
}

func (p *exportPanel) optionLine(idx int, label, value string) string {
	line := fmt.Sprintf(" %s %s", padRunes(label, 8), stAccent.Render("◀ ")+stInk.Render(value)+stAccent.Render(" ▶"))
	if idx == p.sel {
		return stCursorRow.Render("▸") + line
	}
	return " " + line
}

func (p *exportPanel) View() string {
	var sb strings.Builder
	sb.WriteString(styleTitle.Render("导出设置") + "\n")
	groupLabel := "全部监控群"
	if p.groupIdx > 0 && p.groupIdx-1 < len(p.groups) {
		groupLabel = p.groups[p.groupIdx-1].DisplayName()
	}
	sb.WriteString(p.optionLine(0, "群", groupLabel) + "\n")
	sb.WriteString(p.optionLine(1, "内容", scopeLabels[p.scope]) + "\n")
	sb.WriteString(p.optionLine(2, "时间", rangeLabels[p.rangeIdx]) + "\n")
	if p.running {
		sb.WriteString("\n  " + styleWarn.Render("导出中，请稍候 ...") + "\n")
	} else if p.status != "" {
		sb.WriteString("\n  " + styleGood.Render(p.status) + "\n")
	}

	sb.WriteString("\n" + styleTitle.Render("导出历史") + "\n")
	if len(p.files) == 0 {
		sb.WriteString(styleMuted.Render("  暂无导出文件") + "\n")
	}
	limit := min(len(p.files), max(1, p.h-12))
	for i := 0; i < limit; i++ {
		f := p.files[i]
		sb.WriteString(fmt.Sprintf("  %s  %8s  %s\n",
			f.modTime.Format("01-02 15:04"), humanSize(f.size), f.name))
	}
	return sb.String()
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
