//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type automationEditor struct {
	draft         automationSettings
	list, rows    *fyne.Container
	status        *widget.Label
	apply, resume *widget.Button
	mode          *widget.Select
	enabled       *widget.Check
}

func (u *studioUI) automationPage() fyne.CanvasObject {
	draft := u.cfg.Automation
	draft.Playlist = append([]string(nil), draft.Playlist...)
	draft.Schedule = append([]scheduledWallpaper(nil), draft.Schedule...)
	u.automationUI = &automationEditor{draft: draft, list: container.NewVBox(), rows: container.NewVBox(), status: sub("Automatic changes are off.")}
	e := u.automationUI
	enabled := widget.NewCheck("Enable automatic wallpaper changes", func(v bool) { e.draft.Enabled = v })
	enabled.Checked = draft.Enabled
	e.enabled = enabled
	minutes := widget.NewEntry()
	minutes.SetText(strconv.Itoa(draft.Minutes))
	minutes.OnChanged = func(s string) { e.draft.Minutes, _ = strconv.Atoi(strings.TrimSpace(s)) }
	interval := panel(container.NewVBox(section("Playlist rotation", "Play selected videos in collection order. Missing files are skipped."),
		container.NewBorder(nil, nil, widget.NewLabel("Change every"), widget.NewLabel("minutes · 1 to 10,080"), minutes),
		sub("Choose at least two videos. Import MP4 files or convert MLW files in Collection to add more."), e.list))
	add := widget.NewButtonWithIcon("Add time", theme.ContentAddIcon(), func() {
		e.draft.Schedule = append(e.draft.Schedule, scheduledWallpaper{Time: "09:00"})
		u.refreshScheduleRows()
	})
	daily := panel(container.NewVBox(section("Daily schedule", "Use your computer's local time, in 24-hour format. The schedule repeats every day."),
		sub("Each video stays active until the next time. After sleep or restart, the most recent scheduled video is used."), e.rows, container.NewHBox(add, layout.NewSpacer())))
	modeContent := container.NewStack(interval)
	mode := widget.NewSelect([]string{"Every interval", "At specific times"}, func(value string) {
		if value == "At specific times" {
			e.draft.Mode = dailyMode
			modeContent.Objects = []fyne.CanvasObject{daily}
		} else {
			e.draft.Mode = intervalMode
			modeContent.Objects = []fyne.CanvasObject{interval}
		}
		modeContent.Refresh()
	})
	e.mode = mode
	if draft.Mode == dailyMode {
		mode.SetSelected("At specific times")
	} else {
		mode.SetSelected("Every interval")
	}
	e.apply = widget.NewButtonWithIcon("Save changes", theme.DocumentSaveIcon(), u.applyAutomation)
	e.apply.Importance = widget.HighImportance
	e.resume = widget.NewButton("Resume automation", func() { u.automation.reset(); u.automationError = ""; u.automationTick(time.Now()) })
	controls := panel(container.NewVBox(container.NewGridWithColumns(2, enabled, mode), sub("Changes take effect when you save. Only the selected mode runs.")))
	footer := container.NewVBox(e.status, container.NewHBox(e.resume, layout.NewSpacer(), e.apply))
	u.refreshAutomationLibrary()
	u.refreshScheduleRows()
	return container.NewBorder(pageHeader("A desktop that changes with you", "Choose a rhythm, or plan your day.", nil), footer, nil, nil,
		container.NewVScroll(container.NewVBox(controls, gap(8), modeContent, gap(8),
			sub("Runs while Drift is open, including in the tray. Stop pauses automation; Resume applies it again. Enable sign-in restore in Settings to run after login."))))
}

func containsVideo(paths []string, path string) bool {
	for _, p := range paths {
		if strings.EqualFold(p, path) {
			return true
		}
	}
	return false
}

func (u *studioUI) refreshAutomationLibrary() {
	e := u.automationUI
	if e == nil {
		return
	}
	e.list.Objects = nil
	paths := append([]string(nil), u.videoPaths...)
	for _, path := range e.draft.Playlist {
		if !containsVideo(paths, path) {
			paths = append(paths, path)
		}
	}
	for _, path := range paths {
		label := fileName(path)
		if !fileExists(path) {
			label += " · unavailable"
		}
		check := widget.NewCheck("", func(selected bool) {
			if selected {
				if !containsVideo(e.draft.Playlist, path) {
					e.draft.Playlist = append(e.draft.Playlist, path)
				}
			} else {
				var kept []string
				for _, p := range e.draft.Playlist {
					if !strings.EqualFold(p, path) {
						kept = append(kept, p)
					}
				}
				e.draft.Playlist = kept
			}
		})
		check.Checked = containsVideo(e.draft.Playlist, path)
		e.list.Add(container.NewBorder(nil, nil, check, nil, container.NewVBox(ellipsis(label), ellipsis(path))))
	}
	if len(paths) == 0 {
		e.list.Add(sub("Your collection has no videos yet."))
	}
	e.list.Refresh()
}

func (u *studioUI) refreshScheduleRows() {
	e := u.automationUI
	if e == nil {
		return
	}
	e.rows.Objects = nil
	for i, entry := range e.draft.Schedule {
		clock := widget.NewEntry()
		clock.SetText(entry.Time)
		clock.SetPlaceHolder("HH:MM")
		clock.OnChanged = func(s string) { e.draft.Schedule[i].Time = strings.TrimSpace(s) }
		paths := append([]string(nil), u.videoPaths...)
		if entry.Video != "" && !containsVideo(paths, entry.Video) {
			paths = append(paths, entry.Video)
		}
		labels := make([]string, len(paths))
		for j, path := range paths {
			labels[j] = fmt.Sprintf("%02d · %s", j+1, fileName(path))
		}
		picker := widget.NewSelect(labels, func(s string) {
			for j, label := range labels {
				if s == label {
					e.draft.Schedule[i].Video = paths[j]
					return
				}
			}
		})
		picker.PlaceHolder = "Choose a video"
		for j, path := range paths {
			if strings.EqualFold(path, entry.Video) {
				picker.Selected = labels[j]
			}
		}
		remove := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			e.draft.Schedule = append(e.draft.Schedule[:i], e.draft.Schedule[i+1:]...)
			u.refreshScheduleRows()
		})
		e.rows.Add(container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(100, 48), clock), remove, picker))
	}
	if len(e.draft.Schedule) == 0 {
		e.rows.Add(sub("Add a time and choose the video to play."))
	}
	e.rows.Refresh()
}

func (u *studioUI) applyAutomation() {
	if u.playerBusy {
		return
	}
	e := u.automationUI
	next := e.draft
	// Persist a stable collection order, regardless of checkbox click order.
	next.Playlist = nil
	for _, path := range u.videoPaths {
		if containsVideo(e.draft.Playlist, path) {
			next.Playlist = append(next.Playlist, path)
		}
	}
	next.Schedule = append([]scheduledWallpaper(nil), next.Schedule...)
	if next.Enabled {
		if err := next.validate(fileExists); err != nil {
			u.showError(err)
			return
		}
	}
	cfg := u.cfg
	cfg.Automation = next
	if err := saveSettings(cfg); err != nil {
		u.showError(err)
		return
	}
	u.cfg = cfg
	u.automation.reset()
	u.automationError = ""
	u.status.SetText("Automatic wallpaper preferences saved")
	u.automationTick(time.Now())
}

func (u *studioUI) automationTick(now time.Time) {
	if !u.playerBusy && u.ctx.Err() == nil {
		if path := u.automation.advance(u.cfg.Automation, now, u.playingPath, fileExists); path != "" {
			u.playVideo(path, true)
		}
	}
	u.refreshAutomationStatus()
}

func (u *studioUI) refreshAutomationStatus() {
	e := u.automationUI
	if e == nil {
		return
	}
	a := u.cfg.Automation
	status := "Automatic changes are off."
	if a.Enabled {
		switch {
		case u.automation.paused:
			status = "Paused · Resume automation or set a wallpaper to continue."
		case u.automation.next.IsZero():
			status = "Waiting for available videos. Check your playlist or schedule."
		default:
			status = "Next change · " + u.automation.next.Format("Mon 15:04")
		}
		if u.automationError != "" {
			status += "\n" + u.automationError
		}
	}
	if e.status.Text != status {
		e.status.SetText(status)
	}
	setEnabled(e.apply, !u.playerBusy)
	setEnabled(e.resume, a.Enabled && u.automation.paused && !u.playerBusy)
}
