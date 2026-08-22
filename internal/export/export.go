package export

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"volteye/internal/capture"
	"volteye/internal/store"
	"volteye/internal/wechatdb"
)

type Options struct {
	GroupWxid   string
	OnlyMatched bool
	Start       time.Time
	End         time.Time
}

var headers = []string{"时间", "群名称", "群wxid", "发送人", "类型", "匹配", "匹配规则", "内容"}

func MessagesXLSX(st *store.Store, opts Options, outPath string) (int, error) {
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return 0, err
	}
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Sheet1"
	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return 0, err
	}
	headerRow := make([]any, len(headers))
	for i, h := range headers {
		headerRow[i] = h
	}
	if err := sw.SetRow("A1", headerRow); err != nil {
		return 0, err
	}

	ruleNames := map[string]string{}
	if rules, err := st.ListRules(); err == nil {
		for _, r := range rules {
			ruleNames[fmt.Sprintf("%d", r.ID)] = r.Name
		}
	}

	n := 0
	rowIdx := 2
	err = st.StreamMessages(store.MessageFilter{
		GroupWxid:   opts.GroupWxid,
		OnlyMatched: opts.OnlyMatched,
		StartTime:   opts.Start.Unix(),
		EndTime:     opts.End.Unix(),
	}, func(m store.MessageRow) error {
		matched := ""
		if m.Matched {
			matched = "是"
		}
		rules := resolveRuleNames(m.MatchedRules, ruleNames)
		typeBrief := wechatdb.TypeBrief(m.LocalType)
		if typeBrief != "" {
			typeBrief = typeBrief[1 : len(typeBrief)-1]
		}
		cell, _ := excelize.CoordinatesToCellName(1, rowIdx)
		row := []any{
			time.Unix(m.CreateTime, 0).Format("2006-01-02 15:04:05"),
			m.GroupName,
			m.GroupWxid,
			m.SenderWxid,
			typeBrief,
			matched,
			rules,
			m.Content,
		}
		if err := sw.SetRow(cell, row); err != nil {
			return err
		}
		rowIdx++
		n++
		return nil
	})
	if err != nil {
		return n, err
	}
	if err := sw.Flush(); err != nil {
		return n, err
	}
	if err := f.SaveAs(outPath); err != nil {
		return n, err
	}
	return n, nil
}

func resolveRuleNames(ids string, names map[string]string) string {
	if ids == "" {
		return ""
	}
	out := ""
	for i := 0; i < len(ids); i++ {
		start := i
		for i < len(ids) && ids[i] != ',' {
			i++
		}
		id := ids[start:i]
		name := names[id]
		if name == "" {
			name = id
		}
		if out != "" {
			out += ","
		}
		out += name
	}
	return out
}

// ---------------------------------------------------------------------------
// work-order export: production-grade workbook
// ---------------------------------------------------------------------------

// WorkOrderHeaders matches the client's 工单汇总模板 column layout exactly.
var WorkOrderHeaders = []string{
	"紧急程度", "业务类型", "故障子类", "具体故障", "工作单号", "派工时间",
	"地址", "联系人", "联系方式", "联系电话", "诉求内容", "用户编号", "用户名称", "是否完成",
}

// woColWidths are tuned per column for readability (Excel width units).
var woColWidths = []float64{9, 10, 12, 16, 24, 16, 30, 9, 8, 14, 55, 18, 14, 10}

// centerCols are 1-based column indexes rendered centered.
var centerCols = map[int]bool{1: true, 2: true, 3: true, 4: true, 6: true, 8: true, 9: true, 14: true}

// SheetForCategory routes an order to its workbook sheet by the first
// category level. Unknown/new categories fall back to DefaultSheet, so a
// category the client has never seen still lands somewhere sensible.
// New sheets from the client get added here without touching anything else.
var SheetForCategory = map[string]string{
	"故障报修": "故障报修",
}

const DefaultSheet = "咨询意见"

// preferredSheetOrder fixes the sheet order in fresh workbooks; any sheet not
// listed here follows alphabetically.
var preferredSheetOrder = []string{"故障报修", "咨询意见"}

func sheetOf(o capture.WorkOrder) string {
	lv := o.CategoryLevels()
	if len(lv) > 0 {
		if s, ok := SheetForCategory[lv[0]]; ok {
			return s
		}
	}
	return DefaultSheet
}

const unrecognized = "未识别到"

// salutations are placeholder values the sender system puts into 联系人
// instead of a real name.
var salutations = map[string]bool{
	"先生": true, "女士": true, "男士": true, "客户": true, "微信用户": true, "用户": true,
}

// displayContact merges the account name with a bare salutation:
// 联系人="先生" + 用户名称="游卫东" renders as "游卫东(先生)". Real contact
// names and empty account names pass through unchanged.
func displayContact(o capture.WorkOrder) string {
	c := strings.TrimSpace(o.ContactName)
	u := strings.TrimSpace(o.UserName)
	if c == "" {
		return u
	}
	if salutations[c] && u != "" {
		return u + "(" + c + ")"
	}
	return c
}

// workOrderRow maps an order to the 14-column layout. Category levels
// distribute across 业务类型/故障子类/具体故障; levels deeper than 3 merge into
// 具体故障; absent pieces follow the "未识别到" convention. 是否完成 stays
// empty: it is the client's manual workflow column.
func workOrderRow(o capture.WorkOrder) []any {
	lv := o.CategoryLevels()
	level := func(i int) string {
		if i < len(lv) && lv[i] != "" {
			return lv[i]
		}
		return unrecognized
	}
	detail := unrecognized
	if len(lv) > 3 {
		detail = strings.Join(lv[2:], "/")
	} else {
		detail = level(2)
	}
	priority := o.Priority
	if priority == "" {
		priority = unrecognized
	}
	var dispatch any
	if o.DispatchTime > 0 {
		dispatch = time.Unix(o.DispatchTime, 0)
	}
	desc := strings.TrimSpace(o.Description)
	desc = strings.TrimPrefix(desc, "诉求内容：") // sender-side boilerplate
	return []any{
		priority, level(0), level(1), detail, o.OrderNo, dispatch,
		o.Address, displayContact(o), o.ContactWay, o.ContactPhone, desc,
		o.UserNo, o.UserName, "",
	}
}

// ---------------------------------------------------------------------------
// styling
// ---------------------------------------------------------------------------

const (
	woColorBrand   = "305496" // header/title blue
	woColorBand    = "F2F6FC" // zebra stripe
	woColorBorder  = "BFBFBF" // thin cell border
	woColorMeta    = "808080"
	woColorCrit    = "C00000" // 特急
	woColorHigh    = "E36C09" // 紧急
	woColorDone    = "FFF2CC" // manual-entry column hint
	woColorDoneAlt = "FDF6E3"
)

type woStyles struct {
	title, meta, header           int
	evenL, evenC, oddL, oddC      int
	dateEven, dateOdd             int
	doneEven, doneOdd             int
	critEven, critOdd             int
	highEven, highOdd             int
}

func woBorder() []excelize.Border {
	b := func(side string) excelize.Border {
		return excelize.Border{Type: side, Color: woColorBorder, Style: 1}
	}
	return []excelize.Border{b("left"), b("right"), b("top"), b("bottom")}
}

func newWOStyles(f *excelize.File) woStyles {
	mk := func(s *excelize.Style) int {
		id, err := f.NewStyle(s)
		if err != nil {
			return 0
		}
		return id
	}
	base := excelize.Style{
		Border:    woBorder(),
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Font:      &excelize.Font{Size: 10},
	}
	centered := base
	centered.Alignment = &excelize.Alignment{Vertical: "top", Horizontal: "center", WrapText: true}
	band := func(s *excelize.Style, fill string) {
		s.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{fill}}
	}
	prio := func(s *excelize.Style, color string) {
		s.Font = &excelize.Font{Size: 10, Bold: true, Color: color}
	}
	derive := func(base excelize.Style, mods ...func(*excelize.Style)) int {
		s := base
		for _, m := range mods {
			m(&s)
		}
		return mk(&s)
	}
	withBand := func(fill string) func(*excelize.Style) { return func(s *excelize.Style) { band(s, fill) } }
	withPrio := func(color string) func(*excelize.Style) { return func(s *excelize.Style) { prio(s, color) } }
	withNumFmt := func(n int) func(*excelize.Style) {
		return func(s *excelize.Style) { s.NumFmt = n }
	}
	return woStyles{
		title: mk(&excelize.Style{
			Font:      &excelize.Font{Size: 14, Bold: true, Color: "FFFFFF"},
			Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{woColorBrand}},
			Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "left", Indent: 1},
		}),
		meta: mk(&excelize.Style{
			Font:      &excelize.Font{Size: 9, Color: woColorMeta},
			Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "left", Indent: 1},
		}),
		header: mk(&excelize.Style{
			Font:      &excelize.Font{Size: 10, Bold: true, Color: "FFFFFF"},
			Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{woColorBrand}},
			Border:    woBorder(),
			Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "center", WrapText: true},
		}),
		evenL:    derive(base, withBand(woColorBand)),
		evenC:    derive(centered, withBand(woColorBand)),
		oddL:     derive(base),
		oddC:     derive(centered),
		dateEven: derive(centered, withBand(woColorBand), withNumFmt(22)),
		dateOdd:  derive(centered, withNumFmt(22)),
		doneEven: derive(centered, withBand(woColorDoneAlt)),
		doneOdd:  derive(centered, withBand(woColorDone)),
		critEven: derive(centered, withBand(woColorBand), withPrio(woColorCrit)),
		critOdd:  derive(centered, withPrio(woColorCrit)),
		highEven: derive(centered, withBand(woColorBand), withPrio(woColorHigh)),
		highOdd:  derive(centered, withPrio(woColorHigh)),
	}
}

// dataStyle picks the per-cell style: zebra banding + centered columns, with
// priority/font overrides for 紧急程度 and the manual-entry 是否完成 column.
func (s woStyles) dataStyle(col int, even bool, priority string) int {
	if col == 1 {
		switch priority {
		case "特急":
			if even {
				return s.critEven
			}
			return s.critOdd
		case "紧急":
			if even {
				return s.highEven
			}
			return s.highOdd
		}
	}
	if col == 14 {
		if even {
			return s.doneEven
		}
		return s.doneOdd
	}
	if col == 6 {
		if even {
			return s.dateEven
		}
		return s.dateOdd
	}
	if centerCols[col] {
		if even {
			return s.evenC
		}
		return s.oddC
	}
	if even {
		return s.evenL
	}
	return s.oddL
}

// woRowHeight estimates a data row height from the wrapped text volume.
func woRowHeight(o capture.WorkOrder) float64 {
	lines := 1
	estimate := func(text string, perLine int) int {
		n := 0
		for _, seg := range strings.Split(text, "\n") {
			r := utf8.RuneCountInString(seg)
			if r == 0 {
				n++
			} else {
				n += (r + perLine - 1) / perLine
			}
		}
		return n
	}
	if l := estimate(o.Description, 27); l > lines {
		lines = l
	}
	if l := estimate(o.Address, 14); l > lines {
		lines = l
	}
	h := float64(lines)*15 + 7
	if h < 22 {
		return 22
	}
	if h > 160 {
		return 160
	}
	return h
}

// ---------------------------------------------------------------------------
// writing
// ---------------------------------------------------------------------------

// WorkOrdersXLSX writes orders to outPath as a styled workbook, one sheet per
// business type.
func WorkOrdersXLSX(st *store.Store, filter store.WorkOrderFilter, outPath string) (int, error) {
	f := excelize.NewFile()
	defer f.Close()
	return writeWorkOrders(f, st, filter, func() error {
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		return f.SaveAs(outPath)
	})
}

// AppendWorkOrdersXLSX appends orders to an existing VoltEye export file,
// preserving any manual edits (是否完成, remark columns). Orders whose
// 工作单号 already exists in the target sheet are skipped.
func AppendWorkOrdersXLSX(st *store.Store, filter store.WorkOrderFilter, outPath string) (int, error) {
	f, err := excelize.OpenFile(outPath)
	if err != nil {
		return 0, fmt.Errorf("打开追加目标失败: %w", err)
	}
	defer f.Close()
	return writeWorkOrders(f, st, filter, func() error { return f.Save() })
}

func lastCol() string {
	c, _ := excelize.ColumnNumberToName(len(WorkOrderHeaders))
	return c
}

// writeWorkOrders groups orders by target sheet (deterministic order) and
// writes each with full styling. Existing sheets are appended after their
// last row with order-number dedup.
func writeWorkOrders(f *excelize.File, st *store.Store, filter store.WorkOrderFilter, save func() error) (int, error) {
	groups := map[string][]capture.WorkOrder{}
	err := st.StreamWorkOrders(filter, func(o capture.WorkOrder) error {
		s := sheetOf(o)
		groups[s] = append(groups[s], o)
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(groups) == 0 {
		return 0, nil
	}

	var names []string
	for _, s := range preferredSheetOrder {
		if _, ok := groups[s]; ok {
			names = append(names, s)
		}
	}
	var rest []string
	for s := range groups {
		preferred := false
		for _, p := range preferredSheetOrder {
			if p == s {
				preferred = true
				break
			}
		}
		if !preferred {
			rest = append(rest, s)
		}
	}
	sort.Strings(rest)
	names = append(names, rest...)

	styles := newWOStyles(f)
	total := 0
	for _, name := range names {
		n, err := writeOrderSheet(f, name, groups[name], styles)
		if err != nil {
			return total, err
		}
		total += n
	}
	if total == 0 {
		return 0, nil // all duplicates; leave the file untouched
	}
	if err := save(); err != nil {
		return total, err
	}
	return total, nil
}

// writeOrderSheet appends orders to one sheet, creating and styling it when
// the sheet is empty. It returns how many rows were actually written.
func writeOrderSheet(f *excelize.File, name string, orders []capture.WorkOrder, styles woStyles) (int, error) {
	idx, err := f.GetSheetIndex(name)
	if err != nil {
		return 0, err
	}
	if idx < 0 {
		// reuse the default Sheet1 of a fresh file for the first sheet
		if def := f.GetSheetName(0); def == "Sheet1" {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return 0, err
			}
		} else if _, nerr := f.NewSheet(name); nerr != nil {
			return 0, nerr
		}
	}

	rows, err := f.GetRows(name)
	if err != nil {
		return 0, err
	}
	fresh := len(rows) == 0

	// index existing order numbers (column E) for append dedup
	seen := map[string]bool{}
	for _, r := range rows {
		if len(r) > 4 && r[4] != "" {
			seen[r[4]] = true
		}
	}
	nextRow := len(rows) + 1

	if fresh {
		if err := layoutNewSheet(f, name, styles); err != nil {
			return 0, err
		}
		nextRow = 4
	}

	written := 0
	for _, o := range orders {
		if seen[o.OrderNo] {
			continue
		}
		row := workOrderRow(o)
		cell, _ := excelize.CoordinatesToCellName(1, nextRow)
		if err := f.SetSheetRow(name, cell, &row); err != nil {
			return written, err
		}
		even := written%2 == 1
		priority, _ := row[0].(string)
		for col := 1; col <= len(WorkOrderHeaders); col++ {
			c, _ := excelize.CoordinatesToCellName(col, nextRow)
			_ = f.SetCellStyle(name, c, c, styles.dataStyle(col, even, priority))
		}
		f.SetRowHeight(name, nextRow, woRowHeight(o))
		seen[o.OrderNo] = true
		nextRow++
		written++
	}

	if written > 0 && fresh {
		meta := fmt.Sprintf("共 %d 条 · 导出于 %s · VoltEye 自动采集",
			written, time.Now().Format("2006-01-02 15:04"))
		_ = f.SetCellValue(name, "A2", meta)
	}
	return written, nil
}

// layoutNewSheet writes the title/meta/header scaffolding and applies column
// widths, freeze panes and the autofilter.
func layoutNewSheet(f *excelize.File, name string, styles woStyles) error {
	last := lastCol()
	for i, w := range woColWidths {
		c, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(name, c, c, w); err != nil {
			return err
		}
	}
	if err := f.MergeCell(name, "A1", last+"1"); err != nil {
		return err
	}
	if err := f.MergeCell(name, "A2", last+"2"); err != nil {
		return err
	}
	if err := f.SetCellValue(name, "A1", fmt.Sprintf("工单汇总表 · %s", name)); err != nil {
		return err
	}
	_ = f.SetCellStyle(name, "A1", last+"1", styles.title)
	_ = f.SetCellStyle(name, "A2", last+"2", styles.meta)
	f.SetRowHeight(name, 1, 30)
	f.SetRowHeight(name, 2, 16)

	header := make([]any, len(WorkOrderHeaders))
	for i, h := range WorkOrderHeaders {
		header[i] = h
	}
	if err := f.SetSheetRow(name, "A3", &header); err != nil {
		return err
	}
	_ = f.SetCellStyle(name, "A3", last+"3", styles.header)
	f.SetRowHeight(name, 3, 24)

	if err := f.SetPanes(name, &excelize.Panes{
		Freeze:      true,
		YSplit:      3,
		TopLeftCell: "A4",
		ActivePane:  "bottomLeft",
	}); err != nil {
		return err
	}
	return f.AutoFilter(name, "A3:"+last+"3", nil)
}
