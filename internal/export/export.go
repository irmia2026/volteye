package export

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xuri/excelize/v2"

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
