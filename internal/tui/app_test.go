package tui

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

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
