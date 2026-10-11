//go:build windows

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const intervalMode = "interval"
const dailyMode = "daily"

type scheduledWallpaper struct {
	Time  string `json:"time"`
	Video string `json:"video"`
}

type automationSettings struct {
	Enabled  bool                 `json:"enabled"`
	Mode     string               `json:"mode"`
	Minutes  int                  `json:"interval_minutes"`
	Playlist []string             `json:"playlist"`
	Schedule []scheduledWallpaper `json:"schedule"`
}

func (a *automationSettings) defaults() {
	if a.Mode == "" {
		a.Mode = intervalMode
	}
	if a.Minutes == 0 {
		a.Minutes = 30
	}
}

func clockMinutes(s string) (int, error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("use 24-hour time, for example 08:30")
	}
	h, e1 := strconv.Atoi(s[:2])
	m, e2 := strconv.Atoi(s[3:])
	if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 || fmt.Sprintf("%02d:%02d", h, m) != s {
		return 0, fmt.Errorf("use a time between 00:00 and 23:59")
	}
	return h*60 + m, nil
}

func (a automationSettings) validate(exists func(string) bool) error {
	if a.Mode != intervalMode && a.Mode != dailyMode {
		return fmt.Errorf("choose an automation mode")
	}
	if a.Mode == intervalMode {
		if a.Minutes < 1 || a.Minutes > 10080 {
			return fmt.Errorf("choose an interval from 1 to 10,080 minutes (7 days)")
		}
		if len(availableVideos(a.Playlist, exists)) < 2 {
			return fmt.Errorf("select at least two available videos for rotation")
		}
	} else {
		if len(a.Schedule) == 0 {
			return fmt.Errorf("add at least one scheduled wallpaper")
		}
		seen := map[int]bool{}
		for _, entry := range a.Schedule {
			minute, err := clockMinutes(entry.Time)
			if err != nil {
				return err
			}
			if seen[minute] {
				return fmt.Errorf("%s is used more than once; choose a different time", entry.Time)
			}
			seen[minute] = true
			if !exists(entry.Video) {
				return fmt.Errorf("choose an available video for %s", entry.Time)
			}
		}
	}
	return nil
}

func availableVideos(paths []string, exists func(string) bool) []string {
	var result []string
	seen := map[string]bool{}
	for _, path := range paths {
		key := strings.ToLower(path)
		if path != "" && !seen[key] && exists(path) {
			seen[key] = true
			result = append(result, path)
		}
	}
	return result
}

type wallpaperSlot struct {
	at    time.Time
	video string
}

// Build local calendar occurrences rather than adding 24 hours, which drifts
// across daylight-saving changes. Missed entries are collapsed to the latest.
func dailySlots(entries []scheduledWallpaper, now time.Time, exists func(string) bool) (wallpaperSlot, time.Time) {
	var slots []wallpaperSlot
	for _, entry := range entries {
		minute, err := clockMinutes(entry.Time)
		if err != nil || !exists(entry.Video) {
			continue
		}
		for day := -1; day <= 2; day++ {
			at := time.Date(now.Year(), now.Month(), now.Day()+day, minute/60, minute%60, 0, 0, now.Location())
			slots = append(slots, wallpaperSlot{at, entry.Video})
		}
	}
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].at.Before(slots[j].at) })
	var current wallpaperSlot
	for _, slot := range slots {
		if slot.at.After(now) {
			return current, slot.at
		}
		current = slot
	}
	return current, time.Time{}
}

// Owned by the UI goroutine. An attempt consumes its slot even if playback
// fails, so bad media cannot cause a rapid retry loop or repeated pop-ups.
type automationState struct {
	paused   bool
	next     time.Time
	lastSlot time.Time
	cursor   string
}

func (s *automationState) reset() { *s = automationState{} }
func (s *automationState) manual(a automationSettings, now time.Time, video string, exists func(string) bool) {
	s.reset()
	s.cursor = video
	if a.Mode == dailyMode {
		slot, next := dailySlots(a.Schedule, now, exists)
		s.lastSlot = slot.at
		s.next = next
	} else {
		s.next = now.Add(time.Duration(a.Minutes) * time.Minute)
	}
}

func (s *automationState) advance(a automationSettings, now time.Time, current string, exists func(string) bool) string {
	if !a.Enabled || s.paused {
		return ""
	}
	if a.Mode == dailyMode {
		slot, next := dailySlots(a.Schedule, now, exists)
		s.next = next
		if slot.at.IsZero() || slot.at.Equal(s.lastSlot) {
			return ""
		}
		s.lastSlot = slot.at
		if strings.EqualFold(slot.video, current) {
			return ""
		}
		return slot.video
	}
	if a.Mode != intervalMode || a.Minutes < 1 || a.Minutes > 10080 {
		s.next = time.Time{}
		return ""
	}
	if !s.next.IsZero() && now.Before(s.next) {
		return ""
	}
	paths := availableVideos(a.Playlist, exists)
	if len(paths) < 2 {
		s.next = time.Time{}
		return ""
	}
	first := s.next.IsZero()
	s.next = now.Add(time.Duration(a.Minutes) * time.Minute)
	if first {
		for _, path := range paths {
			if strings.EqualFold(path, current) {
				s.cursor = current
				return ""
			}
		}
	}
	cursor := s.cursor
	if cursor == "" {
		cursor = current
	}
	index := -1
	for i, path := range paths {
		if strings.EqualFold(path, cursor) {
			index = i
			break
		}
	}
	for offset := 1; offset <= len(paths); offset++ {
		path := paths[(index+offset)%len(paths)]
		if !strings.EqualFold(path, current) {
			s.cursor = path
			return path
		}
	}
	return ""
}
