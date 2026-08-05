//go:build windows

package tray

import (
	"github.com/energye/systray"
	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleWindow    = kernel32.NewProc("GetConsoleWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

const (
	swHide    = 0
	swRestore = 9
)

func consoleHWND() uintptr {
	h, _, _ := procGetConsoleWindow.Call()
	return h
}

func ShowConsole(show bool) {
	h := consoleHWND()
	if h == 0 {
		return
	}
	if show {
		procShowWindow.Call(h, swRestore)
		procSetForegroundWindow.Call(h)
	} else {
		procShowWindow.Call(h, swHide)
	}
}

func Run(tooltip string, startApp func(), quitApp func()) {
	systray.Run(func() {
		systray.SetIcon(MakeIcon(32))
		systray.SetTooltip(tooltip)
		mShow := systray.AddMenuItem("显示窗口", "显示 VoltEye 控制台窗口")
		mHide := systray.AddMenuItem("隐藏到托盘", "隐藏控制台窗口，后台继续采集")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出 VoltEye", "停止采集并退出")
		systray.SetOnDClick(func(systray.IMenu) { ShowConsole(true) })
		mShow.Click(func() { ShowConsole(true) })
		mHide.Click(func() { ShowConsole(false) })
		mQuit.Click(func() { quitApp() })
		go startApp()
	}, func() {})
}

func Quit() {
	systray.Quit()
}
