package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/app"
)

func DefaultBoot(cfg AppConfig, send func(tea.Msg)) (*app.Service, error) {
	return app.Boot(app.Config{
		DBStorage: cfg.DBStorage,
		DataDir:   cfg.DataDir,
		KeyHex:    cfg.KeyHex,
		Interval:  cfg.Interval,
		OnEvent: func(ev app.Event) {
			if ev.Poll {
				send(pollTickMsg{at: ev.At, inserted: ev.Inserted})
			} else {
				send(logMsg{at: ev.At, text: ev.Text})
			}
		},
	}, func(s string) { send(bootStepMsg{text: s}) })
}
