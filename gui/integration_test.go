//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueRowRender(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	row := newFileRow()
	for _, state := range []string{"Ready", "Converting", "Converted", "Failed"} {
		row.Set(queueEntry{Path: "sample.mlw", State: state})
		row.Resize(fyne.NewSize(640, 76))
		test.WidgetRenderer(row).Layout(row.Size())
	}
}

func TestBrowseDialog(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("picker test")
	w.Resize(fyne.NewSize(1180, 820))
	w.Show()
	u := newStudio(a, w, Settings{playbackOptions: playbackOptions{GPU: autoGPU, AutoPause: true}})
	defer u.cancel()
	u.browse([]string{".mlw", ".mp4"})
	if len(w.Canvas().Overlays().List()) == 0 {
		t.Fatal("picker was not displayed")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	want := Settings{playbackOptions: playbackOptions{Video: `C:\O'Brien\video.mp4`, GPU: "Example adapter", AutoPause: false}, Startup: true, Library: []string{"one.mp4", "two.mp4"}}
	if err := saveSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Video != want.Video || got.GPU != want.GPU || got.AutoPause || !got.Startup || len(got.Library) != 2 {
		t.Fatalf("preferences changed: %#v", got)
	}
	want.AutoPause = true
	if err = saveSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err = loadSettings()
	if err != nil || !got.AutoPause {
		t.Fatal("atomic settings update failed")
	}
}

func integrationVideos() []string {
	if path := os.Getenv("DRIFT_TEST_VIDEO"); path != "" {
		if fileExists(path) {
			return []string{path}
		}
		return nil
	}
	paths, _ := filepath.Glob(filepath.Join("..", ".local", "samples", "*.mp4"))
	return paths
}

func TestRealPlayerIPC(t *testing.T) {
	if os.Getenv("MLW_INTEGRATION") != "1" {
		t.Skip("set MLW_INTEGRATION=1 for real player IPC")
	}
	mpv, _ := filepath.Abs("mpv.exe")
	samples := integrationVideos()
	if !fileExists(mpv) || len(samples) == 0 {
		t.Skip("mpv/sample unavailable")
	}
	video, _ := filepath.Abs(samples[0])
	pipe := fmt.Sprintf(`\\.\pipe\mlw-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, mpv, "--no-config", "--load-scripts=no", "--audio=no", "--vo=null", "--loop-file=inf", "--input-ipc-server="+pipe, "--", video)
	var log boundedLog
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	for {
		value, err := mpvRequest(ctx, pipe, "get_property", "video-params")
		if err == nil && string(value) != "null" {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("player readiness failed: %s", log.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, pause := range []bool{true, false} {
		if _, err := mpvRequest(ctx, pipe, "set_property", "pause", pause); err != nil {
			t.Fatal(err)
		}
		value, err := mpvRequest(ctx, pipe, "get_property", "pause")
		if err != nil {
			t.Fatal(err)
		}
		var got bool
		if json.Unmarshal(value, &got) != nil || got != pause {
			t.Fatalf("pause ack/readback: %s", value)
		}
	}
	// A newly rotated wallpaper may start paused; returning to the desktop
	// must both clear pause and advance decoded video time.
	var maximized atomic.Bool
	maximized.Store(true)
	monitor := &WallpaperMonitor{interval: 10 * time.Millisecond, detect: maximized.Load, send: func(c context.Context, paused bool) error {
		_, err := mpvRequest(c, pipe, "set_property", "pause", paused)
		return err
	}}
	monitor.Start()
	defer monitor.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for !monitor.paused.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !monitor.paused.Load() {
		t.Fatal("monitor did not pause actual player")
	}
	start, err := mpvRequest(ctx, pipe, "get_property", "time-pos")
	if err != nil {
		t.Fatal(err)
	}
	var initial float64
	if err := json.Unmarshal(start, &initial); err != nil {
		t.Fatal(err)
	}
	maximized.Store(false)
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		value, err := mpvRequest(ctx, pipe, "get_property", "time-pos")
		var pos float64
		if err == nil && json.Unmarshal(value, &pos) == nil && pos > initial+0.1 && !monitor.paused.Load() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("video remained frozen after desktop became visible")

}
func TestUIQueueGuards(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")
	u := newStudio(a, w, Settings{playbackOptions: playbackOptions{GPU: autoGPU, AutoPause: true}})
	defer u.cancel()
	u.queue.Add([]string{"sample.mlw"})
	u.refreshQueue()
	if u.convert.Disabled() {
		t.Fatal("convert disabled for ready file")
	}
	u.converting = true
	u.refreshControls()
	u.addFiles([]string{"more.mlw"})
	if !u.clear.Disabled() || !u.convert.Disabled() || !u.importBtn.Disabled() {
		t.Fatal("queue actions enabled during conversion")
	}
	if len(u.queue.Snapshot()) != 1 {
		t.Fatal("queue changed during conversion")
	}
}
func TestRealFrameExtraction(t *testing.T) {
	if os.Getenv("MLW_INTEGRATION") != "1" {
		t.Skip("set MLW_INTEGRATION=1 to use bundled mpv and sample media")
	}
	mpv, err := filepath.Abs("mpv.exe")
	if err != nil || !fileExists(mpv) {
		t.Skip("mpv unavailable")
	}
	samples := integrationVideos()
	if len(samples) == 0 {
		t.Skip("no sample video")
	}
	video, _ := filepath.Abs(samples[0])
	dir := t.TempDir()
	out := filepath.Join(dir, "frame.jpg")
	if err = extractFrame(context.Background(), video, mpv, out); err != nil {
		t.Fatal(err)
	}
	short := filepath.Join(dir, "short.mkv")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, mpv, "--no-config", "--load-scripts=no", "--no-audio", "--frames=2", "--vf=scale=160:90", "--ovc=mpeg4", "--o="+short, "--", video)
	log, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("short fixture: %v %s", err, log)
	}
	if err = extractFrame(context.Background(), short, mpv, filepath.Join(dir, "short.jpg")); err != nil {
		t.Fatalf("short clip fallback: %v", err)
	}
}
