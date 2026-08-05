package tui

import (
	"path/filepath"
	"testing"

	"volteye/internal/app"
	"volteye/internal/store"
)

func TestSettingsDefaultIntervalMatchesCollector(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := app.NewService(st, nil, nil, dir, nil)
	p := newSettingsPanel(svc).(*settingsPanel)
	if p.intervals[p.intIdx] != svc.PollInterval() {
		t.Fatalf("settings shows %s but collector runs %s", p.intervals[p.intIdx], svc.PollInterval())
	}
}
