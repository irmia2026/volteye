package tui

import (
	"time"

	"volteye/internal/app"
)

type bootStepMsg struct{ text string }

type bootDoneMsg struct {
	svc *app.Service
	err error
}

type logMsg struct {
	at   time.Time
	text string
}

type pollTickMsg struct {
	at       time.Time
	inserted int
}
