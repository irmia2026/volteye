package tui

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"volteye/internal/extract"
	"volteye/internal/store"
	"volteye/internal/sync"
)

func fakeBoot(st *store.Store) func(AppConfig, func(tea.Msg)) (*store.Store, *sync.Collector, error) {
	return func(cfg AppConfig, send func(tea.Msg)) (*store.Store, *sync.Collector, error) {
		send(bootStepMsg{text: "fake boot step"})
		return st, nil, nil
	}
}

func TestBootAndTabSwitch(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertGroup("111@chatroom", "测试群甲"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMonitored("111@chatroom", true); err != nil {
		t.Fatal(err)
	}

	m := NewRoot(AppConfig{Boot: fakeBoot(st)})
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	m.SetSender(tm.Send)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("群管理"))
	}, teatest.WithCheckInterval(50*time.Millisecond), teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	time.Sleep(300 * time.Millisecond)
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("测试群甲"))
	}, teatest.WithCheckInterval(50*time.Millisecond), teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestMessagesAndRulesPanels(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertGroup("222@chatroom", "供电所值班群"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertMessages([]store.Message{
		{GroupWxid: "222@chatroom", LocalID: 1, CreateTime: 1700000000, SenderWxid: "zhangsan", Content: "新工单：城北台区低压抢修", LocalType: 1},
		{GroupWxid: "222@chatroom", LocalID: 2, CreateTime: 1700000060, SenderWxid: "lisi", Content: "收到，马上处理", LocalType: 1},
	}); err != nil {
		t.Fatal(err)
	}
	ruleID, err := st.AddRule("工单", "工单,报修", "")
	if err != nil {
		t.Fatal(err)
	}
	engine := extract.NewEngine()
	if err := engine.Load([]extract.Rule{{ID: ruleID, Name: "工单", Keywords: []string{"工单", "报修"}, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	hits := engine.Match("新工单：城北台区低压抢修")
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %v", hits)
	}
	if err := st.MarkMessageScanned(1, "1"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkMessageScanned(2, ""); err != nil {
		t.Fatal(err)
	}

	cfg := AppConfig{Boot: fakeBoot(st), Engine: engine}
	m := NewRoot(cfg)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))
	m.SetSender(tm.Send)

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("群管理"))
	}, teatest.WithCheckInterval(50*time.Millisecond), teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("城北台区低压抢修")) && bytes.Contains(out, []byte("供电所值班群"))
	}, teatest.WithCheckInterval(50*time.Millisecond), teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("启用")) && bytes.Contains(out, []byte("工单,报修"))
	}, teatest.WithCheckInterval(50*time.Millisecond), teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

