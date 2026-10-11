//go:build windows

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
	_ "time/tzdata"

	"fyne.io/fyne/v2/test"
)

func TestIntervalRotation(t *testing.T) {
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	a := automationSettings{Enabled: true, Mode: intervalMode, Minutes: 30, Playlist: []string{"a", "missing", "A", "b", "c"}}
	exists := func(p string) bool { return p != "missing" }
	var s automationState
	if got := s.advance(a, now, "a", exists); got != "" {
		t.Fatal("changed before first interval", got)
	}
	if got := s.advance(a, now.Add(29*time.Minute), "a", exists); got != "" {
		t.Fatal("changed too soon", got)
	}
	if got := s.advance(a, now.Add(30*time.Minute), "a", exists); got != "b" {
		t.Fatal(got)
	}
	// A failed b must not loop; c is attempted at the next interval.
	if got := s.advance(a, now.Add(31*time.Minute), "a", exists); got != "" {
		t.Fatal(got)
	}
	if got := s.advance(a, now.Add(60*time.Minute), "a", exists); got != "c" {
		t.Fatal(got)
	}
	if got := s.advance(a, now.Add(90*time.Minute), "c", exists); got != "a" {
		t.Fatal("did not wrap", got)
	}
	// Waking after many intervals causes one change, not a burst.
	wake := now.Add(8 * time.Hour)
	if got := s.advance(a, wake, "a", exists); got != "b" {
		t.Fatal(got)
	}
	if got := s.advance(a, wake, "b", exists); got != "" {
		t.Fatal("catch-up burst", got)
	}
	s.paused = true
	if got := s.advance(a, wake.Add(time.Hour), "b", exists); got != "" {
		t.Fatal("stop restarted playback", got)
	}
	s.manual(a, wake.Add(time.Hour), "c", exists)
	if got := s.advance(a, wake.Add(time.Hour), "c", exists); got != "" {
		t.Fatal("manual selection was immediately replaced")
	}
}

func TestIntervalNeedsTwoAvailableVideos(t *testing.T) {
	a := automationSettings{Enabled: true, Mode: intervalMode, Minutes: 1, Playlist: []string{"a", "A", "missing"}}
	exists := func(p string) bool { return p != "missing" }
	if a.validate(exists) == nil {
		t.Fatal("accepted duplicate and missing videos")
	}
	var s automationState
	if s.advance(a, time.Now(), "", exists) != "" || !s.next.IsZero() {
		t.Fatal("rotation started without two videos")
	}
}

func TestDailyScheduleBoundariesAndResume(t *testing.T) {
	a := automationSettings{Enabled: true, Mode: dailyMode, Schedule: []scheduledWallpaper{{"18:00", "night"}, {"08:00", "day"}, {"12:00", "missing"}}}
	exists := func(p string) bool { return p != "missing" }
	now := time.Date(2026, 10, 11, 7, 59, 0, 0, time.UTC)
	var s automationState
	if got := s.advance(a, now, "", exists); got != "night" {
		t.Fatal("previous day's slot not restored", got)
	}
	if got := s.advance(a, now.Add(time.Minute), "night", exists); got != "day" {
		t.Fatal("boundary missed", got)
	}
	if got := s.advance(a, now.Add(2*time.Minute), "day", exists); got != "" {
		t.Fatal("repeated slot", got)
	}
	if got := s.advance(a, now.Add(12*time.Hour), "day", exists); got != "night" {
		t.Fatal("sleep catch-up failed", got)
	}
	s.manual(a, now.Add(13*time.Hour), "manual", exists)
	if got := s.advance(a, now.Add(14*time.Hour), "manual", exists); got != "" {
		t.Fatal("manual override did not last until next slot", got)
	}
	if got := s.advance(a, now.Add(24*time.Hour+time.Minute), "manual", exists); got != "day" {
		t.Fatal(got)
	}
}

func TestDailyCalendarAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	exists := func(string) bool { return true }
	a := []scheduledWallpaper{{"08:00", "day"}, {"20:00", "night"}}
	for _, now := range []time.Time{time.Date(2026, 3, 7, 21, 0, 0, 0, loc), time.Date(2026, 10, 31, 21, 0, 0, 0, loc)} {
		current, next := dailySlots(a, now, exists)
		if current.video != "night" || next.Hour() != 8 || next.Day() != now.Day()+1 && next.Day() != 1 {
			t.Fatalf("calendar drift: %v %v", current, next)
		}
	}
}

func TestAutomationValidation(t *testing.T) {
	exists := func(p string) bool { return p != "" && p != "missing" }
	for _, clock := range []string{"24:00", "12:60", "8:00", "08:-1", "+8:00", "oops"} {
		if _, err := clockMinutes(clock); err == nil {
			t.Fatal("accepted invalid time", clock)
		}
	}
	for _, a := range []automationSettings{
		{Mode: intervalMode, Minutes: 0, Playlist: []string{"a", "b"}},
		{Mode: intervalMode, Minutes: 10081, Playlist: []string{"a", "b"}},
		{Mode: dailyMode},
		{Mode: dailyMode, Schedule: []scheduledWallpaper{{"08:00", "a"}, {"08:00", "b"}}},
		{Mode: dailyMode, Schedule: []scheduledWallpaper{{"08:00", "missing"}}},
	} {
		if a.validate(exists) == nil {
			t.Fatalf("accepted invalid setup: %#v", a)
		}
	}
}

func TestAutomationSettingsAndEditorIsolation(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	pathA := filepath.Join(t.TempDir(), "one.mp4")
	pathB := filepath.Join(t.TempDir(), "two.mp4")
	for _, path := range []string{pathA, pathB} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := Settings{playbackOptions: playbackOptions{GPU: autoGPU}, Library: []string{pathA, pathB}, Automation: automationSettings{Mode: intervalMode, Minutes: 30, Playlist: []string{pathA, pathB}, Schedule: []scheduledWallpaper{{"08:00", pathA}}}}
	if err := saveSettings(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings()
	if err != nil || !reflect.DeepEqual(got.Automation, cfg.Automation) {
		t.Fatal("settings did not round trip", err)
	}
	a := test.NewApp()
	defer a.Quit()
	u := newStudio(a, a.NewWindow("Automation"), cfg)
	defer u.cancel()
	u.automationUI.draft.Schedule[0].Time = "09:00"
	if u.cfg.Automation.Schedule[0].Time != "08:00" {
		t.Fatal("unsaved edit changed live schedule")
	}
	u.applyAutomation()
	u.automationUI.draft.Schedule[0].Time = "10:00"
	if u.cfg.Automation.Schedule[0].Time != "09:00" {
		t.Fatal("saved settings share editor memory")
	}
	u.navigate(2)
	if u.body.Objects[0] != u.pages[2] {
		t.Fatal("automation page unavailable")
	}
}
