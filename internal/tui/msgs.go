package tui

import (
	"time"

	"volteye/internal/store"
	"volteye/internal/sync"
)

type bootStepMsg struct{ text string }

type bootDoneMsg struct {
	st  *store.Store
	col *sync.Collector
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
