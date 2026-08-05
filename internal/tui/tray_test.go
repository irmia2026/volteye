package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/store"
)

func bootedRoot(t *testing.T, cfg AppConfig) (*rootModel, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg.Boot = fakeBoot(st)
	cfg.DataDir = dir
	m := NewRoot(cfg)
	m.SetSender(func(tea.Msg) {})
	batch := m.Init()().(tea.BatchMsg)
	for _, c := range batch {
		if msg := c(); msg != nil {
			if _, ok := msg.(bootDoneMsg); ok {
				um, _ := m.Update(msg)
				m = um.(*rootModel)
			}
		}
	}
	return m, st
}

func TestQInTrayModeClosesViewButKeepsService(t *testing.T) {
	m, st := bootedRoot(t, AppConfig{TrayMode: true})
	um, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = um.(*rootModel)
	if cmd == nil {
		t.Fatal("q in tray mode should close the view (tea.Quit)")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
	if _, err := st.TotalMessages(); err != nil {
		t.Fatal("service store must stay open after view closes in tray mode")
	}
}

func TestCtrlCInTrayModeKeepsService(t *testing.T) {
	m, st := bootedRoot(t, AppConfig{TrayMode: true})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c in tray mode should close the view")
	}
	if _, err := st.TotalMessages(); err != nil {
		t.Fatal("service store must stay open after ctrl+c in tray mode")
	}
}

func TestQWithoutTrayStopsService(t *testing.T) {
	m, st := bootedRoot(t, AppConfig{})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q without tray must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
	if _, err := st.TotalMessages(); err == nil {
		t.Fatal("non-tray quit must stop the service (store closed)")
	}
}
