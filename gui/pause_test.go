//go:build windows

package main

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestForegroundPausePolicy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state pauseWindow
		want  bool
	}{
		{"maximized foreground", pauseWindow{visible: true, maximized: true}, true},
		{"hidden virtual desktop app", pauseWindow{visible: true, maximized: true, cloaked: true}, false},
		{"invisible app", pauseWindow{maximized: true}, false},
		{"minimized app", pauseWindow{visible: true, maximized: true, minimized: true}, false},
		{"normal foreground", pauseWindow{visible: true}, false},
		{"controller", pauseWindow{visible: true, maximized: true, own: true}, false},
		{"desktop", pauseWindow{visible: true, maximized: true, class: "Progman"}, false},
		{"wallpaper surface", pauseWindow{visible: true, maximized: true, class: "WorkerW"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPauseWallpaper(tc.state); got != tc.want {
				t.Fatalf("pause=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestMonitorReportsConfirmedPauseAndResume(t *testing.T) {
	var foregroundMaximized atomic.Bool
	foregroundMaximized.Store(true)
	m := &WallpaperMonitor{interval: time.Millisecond, detect: foregroundMaximized.Load, send: func(context.Context, bool) error { return nil }}
	m.Start()
	defer m.Stop()
	await := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for m.paused.Load() != want && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if m.paused.Load() != want {
			t.Fatal("confirmed pause state not updated")
		}
	}
	await(true)
	foregroundMaximized.Store(false)
	await(false)
	foregroundMaximized.Store(true)
	await(true)
	m.Stop()
	await(false)
	if !strings.HasPrefix(playbackStatusText("video.mp4", true), "Paused") {
		t.Fatal("paused player reported as playing")
	}
}
