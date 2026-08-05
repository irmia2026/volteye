package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"volteye/internal/app"
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

func runTea(cfg tui.AppConfig) {
	m := tui.NewRoot(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.SetSender(p.Send)
	if _, err := p.Run(); err != nil {
		fmt.Println("[-]", err)
		os.Exit(1)
	}
}

func openConsoleIO() (*os.File, *os.File, bool) {
	if tray.ConsoleAttached() {
		return os.Stdin, os.Stdout, false
	}
	if !tray.AllocConsole() {
		return os.Stdin, os.Stdout, false
	}
	in, errIn := os.OpenFile("CONIN$", os.O_RDWR, 0)
	out, errOut := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if errIn != nil || errOut != nil {
		tray.FreeConsole()
		return os.Stdin, os.Stdout, false
	}
	return in, out, true
}

func runWithTray(cfg tui.AppConfig) {
	cfg.TrayMode = true
	var (
		mu       sync.Mutex
		prog     *tea.Program
		svc      *app.Service
		attachMu sync.Mutex
		quitting atomic.Bool
	)
	getSvc := func() *app.Service {
		mu.Lock()
		defer mu.Unlock()
		return svc
	}

	cfg.Boot = func(c tui.AppConfig, send func(tea.Msg)) (*app.Service, error) {
		if s := getSvc(); s != nil {
			return s, nil
		}
		s, err := tui.DefaultBoot(c, send)
		if s != nil {
			mu.Lock()
			svc = s
			mu.Unlock()
		}
		return s, err
	}

	attach := func() {
		attachMu.Lock()
		defer attachMu.Unlock()
		mu.Lock()
		if prog != nil {
			mu.Unlock()
			return
		}
		mu.Unlock()
	in, out, allocated := openConsoleIO()
	m := tui.NewRoot(cfg)
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(in), tea.WithOutput(out))
		mu.Lock()
		prog = p
		mu.Unlock()
		m.SetSender(p.Send)
		_, err := p.Run()
		mu.Lock()
		prog = nil
		mu.Unlock()
		if allocated {
			in.Close()
			out.Close()
		}
		if tray.ConsoleAttached() {
			tray.FreeConsole()
		}
		if err != nil {
			fmt.Println("[-]", err)
		}
		if quitting.Load() || getSvc() == nil {
			tray.Quit()
		}
	}

	quitView := func() {
		mu.Lock()
		p := prog
		mu.Unlock()
		if p != nil {
			p.Quit()
		}
	}

	tray.Run("VoltEye 群消息采集",
		func() {
			go attach()
		},
		func() {
			quitView()
		},
		func() {
			go attach()
		},
		func() {
			quitting.Store(true)
			if s := getSvc(); s != nil {
				s.Stop()
			}
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
		trayFlag  = flag.Bool("tray", false, "run with system tray icon (closing the window keeps the service running)")
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
