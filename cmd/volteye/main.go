package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/store"
	"volteye/internal/tray"
	"volteye/internal/tui"
)

func flagIsSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func traySettingEnabled(dataPath string) bool {
	st, err := store.Open(filepath.Join(dataPath, "volteye.db"))
	if err != nil {
		return false
	}
	defer st.Close()
	return st.GetSetting("tray_enabled", "0") == "1"
}

func runTea(cfg tui.AppConfig) *tea.Program {
	m := tui.NewRoot(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.SetSender(p.Send)
	if _, err := p.Run(); err != nil {
		fmt.Println("[-]", err)
		os.Exit(1)
	}
	return p
}

func runWithTray(cfg tui.AppConfig) {
	var (
		mu   sync.Mutex
		prog *tea.Program
	)
	tray.Run("VoltEye 群消息采集",
		func() {
			m := tui.NewRoot(cfg)
			p := tea.NewProgram(m, tea.WithAltScreen())
			mu.Lock()
			prog = p
			mu.Unlock()
			m.SetSender(p.Send)
			if _, err := p.Run(); err != nil {
				fmt.Println("[-]", err)
			}
			tray.Quit()
		},
		func() {
			mu.Lock()
			p := prog
			mu.Unlock()
			if p != nil {
				p.Quit()
			} else {
				tray.Quit()
			}
		},
	)
}

func main() {
	var (
		dbStorage = flag.String("dbstorage", "", "db_storage directory path (auto-detect if empty)")
		keyHex    = flag.String("key", "", "64-hex-char db key (skip memory scan)")
		dataDir   = flag.String("data", "data", "data directory for local store")
		interval  = flag.Duration("interval", 3*time.Second, "poll interval")
		trayFlag  = flag.Bool("tray", false, "show system tray icon (console can be hidden to tray)")
	)
	flag.Parse()

	dataPath := *dataDir
	if !flagIsSet("data") {
		if exe, err := os.Executable(); err == nil {
			dataPath = filepath.Join(filepath.Dir(exe), "data")
		}
	}
	cfg := tui.AppConfig{
		DBStorage: *dbStorage,
		DataDir:   dataPath,
		Interval:  *interval,
		KeyHex:    *keyHex,
	}
	if *trayFlag || traySettingEnabled(dataPath) {
		runWithTray(cfg)
		return
	}
	runTea(cfg)
}
