package tui

import (
	"fmt"
	"time"
)

func compactAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds前", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d天前", int(d.Hours()/24))
	}
}
