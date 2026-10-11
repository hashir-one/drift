//go:build windows

package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const autoGPU = "Auto (system default)"

// Do not overwrite Windows GPU preferences or guess dedicated GPUs by name.
func getGPUs(ctx context.Context) []string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop'; @(Get-CimInstance Win32_VideoController | Select-Object -ExpandProperty Name) | ConvertTo-Json -Compress`)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	options := []string{autoGPU}
	if err != nil {
		return options
	}
	var names []string
	if json.Unmarshal(out, &names) != nil {
		var name string
		if json.Unmarshal(out, &name) == nil {
			names = []string{name}
		}
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			options = append(options, name)
		}
	}
	return options
}
