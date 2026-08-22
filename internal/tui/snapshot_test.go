package tui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/store"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func drainCmd(m *rootModel, cmd tea.Cmd) *rootModel {
	if cmd == nil {
		return m
	}
	if msg := cmd(); msg != nil {
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = drainCmd(m, c)
			}
			return m
		}
		um, _ := m.Update(msg)
		return um.(*rootModel)
	}
	return m
}

func TestSnapshotLayout(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.UpsertGroup("111@chatroom", "供电所值班一群")
	st.UpsertGroup("222@chatroom", "")
	st.SetGroupAlias("222@chatroom", "城北抢修联络")
	st.UpsertGroup("333@chatroom", "三沙供电服务群")
	st.SetMonitored("111@chatroom", true)
	st.SetMonitored("222@chatroom", true)
	st.SetBackfill("222@chatroom", true)
	st.InsertMessages([]store.Message{
		{GroupWxid: "111@chatroom", LocalID: 1, CreateTime: time.Now().Unix() - 300, SenderWxid: "wxid_zhang", Content: "新工单：城北台区低压抢修，请速处理", LocalType: 1},
		{GroupWxid: "222@chatroom", LocalID: 2, CreateTime: time.Now().Unix() - 120, SenderWxid: "wxid_li", Content: "收到，已安排人员", LocalType: 1},
		{GroupWxid: "111@chatroom", LocalID: 3, CreateTime: time.Now().Unix() - 60, SenderWxid: "wxid_wang", Content: "", LocalType: 3},
	})
	st.AddRule("工单", "工单,报修", "")
	st.AddRule("提需", "提需,需求", "")
	st.MarkMessageScanned(1, "1")
	st.MarkMessageScanned(2, "")
	st.MarkMessageScanned(3, "")

	m := NewRoot(AppConfig{Boot: fakeBoot(st)})
	m.SetSender(func(tea.Msg) {})
	batch := m.Init()().(tea.BatchMsg)
	var tm tea.Msg
	for _, c := range batch {
		if msg := c(); msg != nil {
			if _, ok := msg.(bootDoneMsg); ok {
				tm = msg
			}
		}
	}
	um, _ := m.Update(tm)
	m = um.(*rootModel)
	um, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 32})
	m = um.(*rootModel)

	for _, p := range m.panels {
		m = drainCmd(m, p.Init())
	}
	um, _ = m.Update(pollTickMsg{at: time.Now(), inserted: 2})
	m = um.(*rootModel)
	for _, p := range m.panels {
		m = drainCmd(m, p.Init())
	}

	titles := []string{"总览", "群管理", "工单", "消息流", "格式", "规则", "导出", "设置", "日志"}
	for i := range m.panels {
		m.active = i
		fmt.Printf("===== %s =====\n", titles[i])
		fmt.Println(stripANSI(m.View()))
	}
}
