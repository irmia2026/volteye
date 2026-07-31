//go:build windows

package tui

import (
	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
const runValueName = "VoltEye"

func setAutoStart(enable bool, exePath string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if enable {
		return k.SetStringValue(runValueName, `"`+exePath+`"`)
	}
	if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

func isAutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValueName)
	return err == nil
}

func isElevated() bool {
	return windows_GetCurrentProcessTokenIsElevated()
}
