package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestRenderStudio(t *testing.T) {
	out := os.Getenv("MLW_RENDER_DIR")
	if out == "" {
		t.Skip("set MLW_RENDER_DIR for review images")
	}
	t.Setenv("APPDATA", t.TempDir())
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(studioTheme{})
	w := a.NewWindow("Drift")
	w.Resize(fyne.NewSize(1180, 800))
	fixtureDir := t.TempDir()
	paths := []string{filepath.Join(fixtureDir, "Morning mountains.mp4"), filepath.Join(fixtureDir, "Evening sky.mp4")}
	for _, path := range paths {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	u := newStudio(a, w, Settings{playbackOptions: playbackOptions{GPU: autoGPU, AutoPause: true}, Library: paths})
	defer u.cancel()
	w.Show()
	for i, name := range []string{"collection", "wallpaper", "automation", "settings"} {
		u.navigate(i)
		w.Resize(fyne.NewSize(1180, 800))
		img := w.Canvas().Capture()
		f, err := os.Create(filepath.Join(out, "ui-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, img)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if name == "automation" {
			u.automationUI.draft.Schedule = []scheduledWallpaper{{Time: "08:00", Video: paths[0]}, {Time: "18:30", Video: paths[1]}}
			u.refreshScheduleRows()
			u.automationUI.mode.SetSelected("At specific times")
			w.Resize(fyne.NewSize(1180, 800))
			f, err := os.Create(filepath.Join(out, "ui-automation-daily.png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, w.Canvas().Capture())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
