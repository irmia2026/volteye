package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/tui"
)

func main() {
	var (
		dbStorage = flag.String("dbstorage", "", "db_storage directory path (auto-detect if empty)")
		keyHex    = flag.String("key", "", "64-hex-char db key (skip memory scan)")
		dataDir   = flag.String("data", "data", "data directory for local store")
		interval  = flag.Duration("interval", 3*time.Second, "poll interval")
	)
	flag.Parse()

	cfg := tui.AppConfig{
		DBStorage: *dbStorage,
		DataDir:   *dataDir,
		Interval:  *interval,
		KeyHex:    *keyHex,
	}
	m := tui.NewRoot(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.SetSender(p.Send)
	if _, err := p.Run(); err != nil {
		fmt.Println("[-]", err)
		os.Exit(1)
	}
}
