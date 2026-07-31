//go:build windows

package tui

import "golang.org/x/sys/windows"

func windows_GetCurrentProcessTokenIsElevated() bool {
	token := windows.GetCurrentProcessToken()
	return token.IsElevated()
}
