//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
	"syscall"
)

type Settings struct {
	playbackOptions
	Startup    bool               `json:"startup"`
	Library    []string           `json:"library"`
	Automation automationSettings `json:"automation"`
}

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValue = "MLWLiveWallpaper"

func appDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "MLWStudio")
	return dir, os.MkdirAll(dir, 0700)
}
func loadSettings() (Settings, error) {
	s := Settings{playbackOptions: playbackOptions{GPU: autoGPU, AutoPause: true}}
	s.Automation.defaults()
	dir, err := appDataDir()
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	s.Automation.defaults()
	if s.GPU == "" {
		s.GPU = autoGPU
	}
	return s, err
}
func saveSettings(s Settings) error {
	dir, err := appDataDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "settings-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return moveFile(f.Name(), filepath.Join(dir, "settings.json"), true)
}
func setAutoStart(enabled bool) error {
	if !enabled {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		if err != nil {
			return err
		}
		defer k.Close()
		err = k.DeleteValue(runValue)
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValue, syscall.EscapeArg(exe)+" --restore")
}
func isAutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}
