//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/sys/windows"
)

func fileName(path string) string { return filepath.Base(path) }
func localPath(path string) string {
	if len(path) > 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.Clean(filepath.FromSlash(path))
}

type studioUI struct {
	automation                                        automationState
	automationUI                                      *automationEditor
	automationError, playingPath                      string
	playbackRevision                                  uint64
	checkingPlayback                                  bool
	automationStartup                                 bool
	app                                               fyne.App
	window                                            fyne.Window
	ctx                                               context.Context
	cancel                                            context.CancelFunc
	wg                                                sync.WaitGroup
	cfg                                               Settings
	queue                                             fileQueue
	player                                            Player
	converting, playerBusy, closing, playing          bool
	convertCancel, previewCancel                      context.CancelFunc
	list                                              *widget.List
	empty                                             *fyne.Container
	detail, status, playerStatus                      *widget.Label
	convert, clear, importBtn, cancelBtn, start, stop *widget.Button
	progress                                          *widget.ProgressBar
	totalText, readyText, doneText                    *canvas.Text
	videoSelect, gpuSelect                            *widget.Select
	autoPause, startup                                *widget.Check
	videoPaths                                        []string
	preview                                           *fyne.Container
	selectedName, selectedPath                        *widget.Label
	body                                              *fyne.Container
	pages                                             []fyne.CanvasObject
	nav                                               []*widget.Button
}

func (u *studioUI) post(fn func()) {
	if u.ctx.Err() != nil {
		return
	}
	fyne.Do(func() {
		if u.ctx.Err() == nil {
			fn()
		}
	})
}
func (u *studioUI) work(fn func()) {
	// All callers run on the UI goroutine. Once shutdown starts no new worker
	// can race with the final Wait, including a late system-tray action.
	if u.ctx.Err() != nil {
		return
	}
	u.wg.Add(1)
	go func() { defer u.wg.Done(); fn() }()
}
func (u *studioUI) save() {
	if err := saveSettings(u.cfg); err != nil {
		dialog.ShowError(fmt.Errorf("save preferences: %w", err), u.window)
	}
}
func (u *studioUI) showError(err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		dialog.ShowError(err, u.window)
	}
}

func newStudio(a fyne.App, w fyne.Window, cfg Settings) *studioUI {
	cfg.Automation.defaults()
	ctx, cancel := context.WithCancel(context.Background())
	u := &studioUI{app: a, window: w, ctx: ctx, cancel: cancel, cfg: cfg}
	u.status = ellipsis("Ready when you are")
	u.playerStatus = ellipsis("No wallpaper running")
	u.selectedName = ellipsis("No video selected")
	u.selectedName.TextStyle.Bold = true
	u.selectedPath = ellipsis("Import an MP4 or convert an MLW file to get started.")
	u.preview = container.NewStack(previewPlaceholder())
	u.videoSelect = widget.NewSelect(nil, func(label string) {
		for i, s := range u.videoSelect.Options {
			if s == label && i < len(u.videoPaths) {
				u.selectVideo(u.videoPaths[i])
				u.save()
				return
			}
		}
	})
	u.videoSelect.PlaceHolder = "Choose from your collection"
	u.gpuSelect = widget.NewSelect([]string{autoGPU}, func(s string) { u.cfg.GPU = s; u.save() })
	u.gpuSelect.Selected = cfg.GPU
	u.autoPause = widget.NewCheck("Pause behind a maximized foreground app", func(v bool) {
		u.cfg.AutoPause = v
		u.save()
		u.autoPause.Disable()
		u.work(func() { u.player.SetAutoPause(v); u.post(u.autoPause.Enable) })
	})
	u.autoPause.Checked = cfg.AutoPause
	u.startup = widget.NewCheck("Restore wallpaper when I sign in", nil)
	u.startup.Checked = cfg.Startup
	u.startup.OnChanged = func(v bool) {
		if v && u.cfg.Video == "" {
			u.startup.Checked = false
			u.startup.Refresh()
			dialog.ShowInformation("Choose a video first", "Select a wallpaper before enabling startup.", w)
			return
		}
		previous := u.cfg.Startup
		u.cfg.Startup = v
		if err := saveSettings(u.cfg); err != nil {
			u.cfg.Startup = previous
			u.startup.Checked = previous
			u.startup.Refresh()
			u.showError(err)
			return
		}
		if err := setAutoStart(v); err != nil {
			u.cfg.Startup = previous
			u.startup.Checked = previous
			u.startup.Refresh()
			u.save()
			u.showError(err)
		}
	}
	u.start = widget.NewButtonWithIcon("Set wallpaper", theme.MediaPlayIcon(), u.startPlayback)
	u.start.Importance = widget.HighImportance
	u.stop = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), func() {
		if u.playerBusy {
			return
		}
		u.automation.paused = true
		u.playbackRevision++
		u.playerBusy = true
		u.refreshControls()
		u.work(func() {
			u.player.Stop()
			u.post(func() {
				u.playerBusy = false
				u.playing = false
				u.playingPath = ""
				u.playerStatus.SetText("Wallpaper stopped")
				u.refreshControls()
			})
		})
	})
	u.pages = []fyne.CanvasObject{u.libraryPage(), u.wallpaperPage(), u.automationPage(), u.settingsPage()}
	u.body = container.NewStack(u.pages[0])
	names := []string{"Collection", "Live wallpaper", "Automation", "Settings"}
	icons := []fyne.Resource{theme.FolderIcon(), theme.ComputerIcon(), theme.HistoryIcon(), theme.SettingsIcon()}
	for i, name := range names {
		index := i
		b := widget.NewButtonWithIcon(name, icons[i], func() { u.navigate(index) })
		b.Alignment = widget.ButtonAlignLeading
		u.nav = append(u.nav, b)
	}
	u.nav[0].Importance = widget.HighImportance
	mark := canvas.NewImageFromResource(driftIcon)
	mark.FillMode = canvas.ImageFillContain
	brand := container.NewHBox(container.NewGridWrap(fyne.NewSize(44, 44), mark), container.NewVBox(txt(appName, 24, true, ink), txt("LIVE WALLPAPERS", 9, true, muted)))
	sidebarTop := container.NewVBox(brand, gap(32), txt("WORKSPACE", 10, true, muted), gap(6), u.nav[0], u.nav[1], u.nav[2], gap(16), widget.NewSeparator(), u.nav[3])
	footer := container.NewVBox(txt("Made for your desktop", 11, false, muted), txt("Local files · Private by design", 10, false, muted), gap(4))
	sidebarBg := canvas.NewRectangle(color.White)
	sidebarBg.SetMinSize(fyne.NewSize(224, 0))
	sidebar := container.NewStack(sidebarBg, inset(20, container.NewBorder(sidebarTop, footer, nil, nil, layout.NewSpacer())))
	statusbar := container.NewBorder(nil, nil, nil, txt("DRIFT  /  "+appVersion, 10, true, muted), u.status)
	shell := container.NewBorder(nil, inset(12, statusbar), sidebar, nil, inset(26, u.body))
	w.SetContent(shell)
	w.SetOnDropped(func(_ fyne.Position, uris []fyne.URI) {
		paths := make([]string, 0, len(uris))
		for _, uri := range uris {
			if uri.Scheme() == "file" {
				paths = append(paths, localPath(uri.Path()))
			}
		}
		u.addFiles(paths)
	})
	for _, path := range cfg.Library {
		if fileExists(path) {
			u.addVideo(path)
		}
	}
	if fileExists(cfg.Video) {
		u.addVideo(cfg.Video)
		u.selectVideo(cfg.Video)
	}
	u.refreshQueue()
	u.refreshControls()
	return u
}

func (u *studioUI) navigate(index int) {
	for i, b := range u.nav {
		b.Importance = widget.LowImportance
		if i == index {
			b.Importance = widget.HighImportance
		}
		b.Refresh()
	}
	u.body.Objects = []fyne.CanvasObject{u.pages[index]}
	u.body.Refresh()
}
func pageHeader(title, description string, action fyne.CanvasObject) fyne.CanvasObject {
	if action != nil {
		action = container.NewCenter(action)
	}
	return container.NewBorder(nil, nil, nil, action, container.NewVBox(txt(title, 28, true, ink), sub(description), gap(12)))
}
func metric(value *canvas.Text, title string) fyne.CanvasObject {
	return panel(container.NewHBox(value, layout.NewSpacer(), txt(title, 12, false, muted)))
}

func (u *studioUI) libraryPage() fyne.CanvasObject {
	u.importBtn = widget.NewButtonWithIcon("Import files", theme.ContentAddIcon(), func() { u.browse([]string{".mlw", ".mp4"}) })
	u.importBtn.Importance = widget.HighImportance
	header := pageHeader("Your collection", "Turn live wallpaper files into a desktop that feels like you.", u.importBtn)
	u.totalText = txt("0", 28, true, ink)
	u.readyText = txt("0", 28, true, accent)
	u.doneText = txt("0", 28, true, ink)
	metrics := container.NewGridWithColumns(3, metric(u.totalText, "IN QUEUE"), metric(u.readyText, "READY"), metric(u.doneText, "CONVERTED"))
	u.list = widget.NewList(func() int { return len(u.queue.Snapshot()) }, func() fyne.CanvasObject { return newFileRow() }, func(id widget.ListItemID, obj fyne.CanvasObject) {
		entries := u.queue.Snapshot()
		if id >= 0 && id < len(entries) {
			obj.(*fileRow).Set(entries[id])
		}
	})
	u.detail = ellipsis("Select a file to see its details.")
	u.list.OnSelected = func(id widget.ListItemID) {
		entries := u.queue.Snapshot()
		if id >= 0 && id < len(entries) {
			e := entries[id]
			u.detail.SetText(e.Detail)
			if e.Output != "" {
				u.selectVideo(e.Output)
			}
		}
	}
	u.empty = container.NewCenter(container.NewVBox(container.NewCenter(widget.NewIcon(theme.FolderOpenIcon())), gap(10), container.NewCenter(txt("Room for something beautiful", 20, true, ink)), container.NewCenter(txt("Drop .mlw or .mp4 files anywhere in this window", 13, false, muted)), gap(4), container.NewCenter(txt("or use Import files to browse your computer.", 12, false, muted))))
	listArea := container.NewStack(u.list, u.empty)
	u.convert = widget.NewButtonWithIcon("Convert files", theme.DownloadIcon(), u.convertFiles)
	u.convert.Importance = widget.HighImportance
	u.clear = widget.NewButton("Clear queue", func() {
		if err := u.queue.Clear(); err != nil {
			u.showError(err)
			return
		}
		u.progress.SetValue(0)
		u.progress.Hide()
		u.refreshQueue()
		u.detail.SetText("Queue cleared. Your files are unchanged.")
	})
	u.cancelBtn = widget.NewButton("Cancel", func() {
		if u.convertCancel != nil {
			u.convertCancel()
			u.cancelBtn.Disable()
			u.status.SetText("Finishing the current step…")
		}
	})
	u.cancelBtn.Hide()
	u.progress = widget.NewProgressBar()
	u.progress.Hide()
	controls := container.NewBorder(nil, nil, u.clear, container.NewHBox(u.cancelBtn, u.convert), nil)
	bottom := container.NewVBox(widget.NewSeparator(), u.progress, u.detail, gap(4), controls)
	queueTitle := container.NewVBox(container.NewBorder(nil, nil, txt("Conversion queue", 17, true, ink), txt("MLW / MP4", 11, true, accent), nil), sub("Originals stay untouched. Existing videos are kept, too."), gap(6), widget.NewSeparator())
	queueCard := panel(container.NewBorder(queueTitle, bottom, nil, nil, listArea))
	return container.NewBorder(container.NewVBox(header, metrics, gap(12)), nil, nil, nil, queueCard)
}

func previewPlaceholder() fyne.CanvasObject {
	art := canvas.NewRasterWithPixels(func(x, y, w, h int) color.Color {
		fx, fy := float64(x)/float64(max(w, 1)), float64(y)/float64(max(h, 1))
		glow := math.Exp(-((fx-.7)*(fx-.7)*8 + (fy-.35)*(fy-.35)*7))
		wave := .65 + .12*math.Sin(fx*6)
		if fy > wave {
			return color.NRGBA{R: 24, G: 29, B: 58, A: 255}
		}
		if fy > wave-.12 {
			return color.NRGBA{R: 40, G: 40, B: 83, A: 255}
		}
		return color.NRGBA{R: uint8(27 + glow*51), G: uint8(32 + glow*43), B: uint8(64 + glow*86), A: 255}
	})
	art.SetMinSize(fyne.NewSize(450, 270))
	words := container.NewVBox(txt("A new perspective.", 26, true, color.White), txt("Choose a video to preview your next wallpaper.", 13, false, color.NRGBA{R: 205, G: 208, B: 230, A: 255}))
	return container.NewStack(art, inset(28, container.NewBorder(nil, words, nil, nil, layout.NewSpacer())))
}
func (u *studioUI) wallpaperPage() fyne.CanvasObject {
	browse := widget.NewButtonWithIcon("Browse MP4", theme.FolderOpenIcon(), func() { u.browse([]string{".mp4"}) })
	picker := container.NewBorder(nil, nil, nil, browse, u.videoSelect)
	metadata := container.NewVBox(u.selectedName, u.selectedPath)
	actions := container.NewBorder(nil, nil, nil, container.NewHBox(u.stop, u.start), u.playerStatus)
	header := pageHeader("Live wallpaper", "A little motion. A different atmosphere.", nil)
	content := panel(container.NewBorder(container.NewVBox(picker, gap(6)), metadata, nil, nil, u.preview))
	return container.NewBorder(header, container.NewVBox(gap(8), actions), nil, nil, content)
}
func (u *studioUI) settingsPage() fyne.CanvasObject {
	return container.NewVScroll(container.NewVBox(pageHeader("Make yourself at home", "Small preferences for a smoother desktop.", nil), panel(container.NewVBox(section("Playback", "Changes to the GPU take effect the next time you set a wallpaper."), gap(8), txt("Graphics adapter", 13, true, ink), u.gpuSelect, gap(8), u.autoPause, sub("Resumes on the desktop. Hidden windows and Drift itself do not pause playback."), u.startup)), gap(12), panel(container.NewVBox(section("Files & privacy", "Everything is processed on your computer."), gap(8), sub("Converted videos are saved beside the original MLW file. If a filename is already taken, a numbered copy is created. Files larger than 512 MiB are rejected to keep memory use predictable."), sub("Closing the window keeps an active wallpaper in the system tray. Choose Quit from the tray to stop playback and exit."))), gap(12), panel(container.NewVBox(txt(appName, 18, true, ink), sub("Version "+appVersion+"  ·  Windows desktop"), sub("MPV powers video playback. Keep mpv.exe beside the application.")))))
}

func (u *studioUI) browse(extensions []string) {
	fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil {
			u.showError(err)
			return
		}
		if r == nil {
			return
		}
		path := localPath(r.URI().Path())
		_ = r.Close()
		u.addFiles([]string{path})
	}, u.window)
	fd.SetFilter(storage.NewExtensionFileFilter(extensions))
	// Fyne 2.8.1 Resize calls MinSize before the dialog is initialized.
	// Show first; use its default size, which also works with native overrides.
	fd.Show()
}
func (u *studioUI) addFiles(paths []string) {
	if u.converting {
		u.status.SetText("Finish or cancel the current conversion before importing more files.")
		return
	}
	var mlw []string
	var lastVideo string
	for _, path := range paths {
		path = localPath(path)
		abs, err := filepath.Abs(path)
		if err != nil || !fileExists(abs) {
			continue
		}
		switch strings.ToLower(filepath.Ext(abs)) {
		case ".mlw":
			mlw = append(mlw, abs)
		case ".mp4":
			u.addVideo(abs)
			lastVideo = abs
		}
	}
	if err := u.queue.Add(mlw); err != nil {
		u.showError(err)
	}
	if lastVideo != "" {
		u.selectVideo(lastVideo)
		u.navigate(1)
	} else if len(mlw) > 0 {
		u.navigate(0)
	}
	u.refreshQueue()
	u.save()
}
func (u *studioUI) addVideo(path string) {
	for _, p := range u.videoPaths {
		if strings.EqualFold(p, path) {
			return
		}
	}
	u.videoPaths = append(u.videoPaths, path)
	label := fileName(path)
	r := []rune(label)
	if len(r) > 55 {
		label = string(r[:52]) + "…"
	}
	label = fmt.Sprintf("%02d  ·  %s", len(u.videoPaths), label)
	u.videoSelect.Options = append(u.videoSelect.Options, label)
	u.videoSelect.Refresh()
	u.cfg.Library = append([]string(nil), u.videoPaths...)
	u.refreshAutomationLibrary()
	u.refreshScheduleRows()
}
func (u *studioUI) selectVideo(path string) {
	u.addVideo(path)
	u.cfg.Video = path
	for i, p := range u.videoPaths {
		if p == path {
			u.videoSelect.Selected = u.videoSelect.Options[i]
			u.videoSelect.Refresh()
			break
		}
	}
	u.selectedName.SetText(fileName(path))
	u.selectedPath.SetText(path)
	u.refreshControls()
	if u.previewCancel != nil {
		u.previewCancel()
	}
	ctx, cancel := context.WithCancel(u.ctx)
	u.previewCancel = cancel
	u.preview.Objects = []fyne.CanvasObject{previewPlaceholder()}
	u.preview.Refresh()
	u.work(func() {
		mpv, err := findMPV()
		if err != nil {
			return
		}
		dir, err := os.MkdirTemp("", "mlw-preview-*")
		if err != nil {
			return
		}
		defer os.RemoveAll(dir)
		pathOut := filepath.Join(dir, "preview.jpg")
		if extractFrame(ctx, path, mpv, pathOut) != nil {
			return
		}
		f, err := os.Open(pathOut)
		if err != nil {
			return
		}
		img, err := jpeg.Decode(f)
		_ = f.Close()
		if err != nil || ctx.Err() != nil {
			return
		}
		u.post(func() {
			if ctx.Err() != nil {
				return
			}
			view := canvas.NewImageFromImage(img)
			view.FillMode = canvas.ImageFillContain
			view.SetMinSize(fyne.NewSize(450, 270))
			bg := canvas.NewRectangle(navy)
			u.preview.Objects = []fyne.CanvasObject{container.NewStack(bg, view)}
			u.preview.Refresh()
		})
	})
}
func (u *studioUI) refreshQueue() {
	entries := u.queue.Snapshot()
	ready, done := 0, 0
	for _, e := range entries {
		if e.State == "Converted" {
			done++
		} else {
			ready++
		}
	}
	u.totalText.Text = fmt.Sprint(len(entries))
	u.readyText.Text = fmt.Sprint(ready)
	u.doneText.Text = fmt.Sprint(done)
	u.totalText.Refresh()
	u.readyText.Refresh()
	u.doneText.Refresh()
	if len(entries) == 0 {
		u.empty.Show()
		u.list.Hide()
	} else {
		u.empty.Hide()
		u.list.Show()
	}
	u.list.Refresh()
	u.refreshControls()
}
func setEnabled(b *widget.Button, enabled bool) {
	if b == nil {
		return
	}
	if enabled {
		b.Enable()
	} else {
		b.Disable()
	}
}
func (u *studioUI) refreshControls() {
	ready := false
	entries := u.queue.Snapshot()
	for _, e := range entries {
		if e.State != "Converted" {
			ready = true
		}
	}
	setEnabled(u.convert, !u.converting && ready)
	setEnabled(u.clear, !u.converting && len(entries) > 0)
	setEnabled(u.importBtn, !u.converting)
	setEnabled(u.start, u.cfg.Video != "" && !u.playerBusy)
	setEnabled(u.stop, (u.playing || (u.cfg.Automation.Enabled && !u.automation.paused)) && !u.playerBusy)
	u.refreshAutomationStatus()
}
func (u *studioUI) convertFiles() {
	if u.converting {
		return
	}
	batch, err := u.queue.Begin()
	if err != nil {
		u.showError(err)
		return
	}
	u.converting = true
	ctx, cancel := context.WithCancel(u.ctx)
	u.convertCancel = cancel
	u.cancelBtn.Show()
	u.cancelBtn.Enable()
	u.progress.SetValue(0)
	u.progress.Show()
	u.status.SetText("Converting your files…")
	u.refreshControls()
	u.work(func() {
		for i, e := range batch {
			if ctx.Err() != nil {
				break
			}
			if e.State == "Converted" {
				continue
			}
			u.queue.Update(i, "Converting", "Verifying and decrypting…", "")
			u.post(u.refreshQueue)
			out, err := decryptFile(ctx, e.Path)
			state, detail := "Converted", out
			if err != nil {
				state = "Failed"
				detail = err.Error()
				if errors.Is(err, context.Canceled) {
					state = "Ready"
					detail = "Canceled — ready to retry"
				}
			}
			u.queue.Update(i, state, detail, out)
			progress := float64(i+1) / float64(len(batch))
			u.post(func() {
				if out != "" {
					u.addVideo(out)
					if u.cfg.Video == "" {
						u.selectVideo(out)
					}
				}
				u.progress.SetValue(progress)
				u.refreshQueue()
			})
		}
		u.post(func() {
			u.queue.Finish()
			u.converting = false
			u.convertCancel = nil
			u.cancelBtn.Hide()
			u.refreshQueue()
			if ctx.Err() != nil {
				u.status.SetText("Conversion canceled. Completed files are saved.")
			} else {
				u.progress.SetValue(1)
				u.status.SetText("Conversion finished. Select a file for details.")
			}
			cancel()
			u.save()
		})
	})
}
func (u *studioUI) startPlayback() {
	if u.playerBusy || u.cfg.Video == "" {
		return
	}
	u.automation.manual(u.cfg.Automation, time.Now(), u.cfg.Video, fileExists)
	u.automationError = ""
	u.playVideo(u.cfg.Video, false)
}

func (u *studioUI) playVideo(path string, automatic bool) {
	if u.playerBusy || path == "" || u.ctx.Err() != nil {
		return
	}
	u.playbackRevision++
	u.playerBusy = true
	u.playerStatus.SetText("Starting wallpaper…")
	u.refreshControls()
	options := u.cfg.playbackOptions
	options.Video = path
	attempts := 1
	if automatic && u.automationStartup {
		attempts = 8
	}
	u.automationStartup = false
	u.work(func() {
		var err error
		for attempt := 0; attempt < attempts; attempt++ {
			err = u.player.Start(u.ctx, options)
			if err == nil || u.ctx.Err() != nil || attempt == attempts-1 {
				break
			}
			select {
			case <-u.ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		u.post(func() {
			u.playerBusy = false
			if err != nil {
				if automatic {
					u.automationError = "Skipped " + fileName(path) + " · could not play this video."
					u.status.SetText(u.automationError)
				} else {
					u.showError(err)
				}
				if u.playingPath != "" {
					u.playerStatus.SetText("Playing · " + fileName(u.playingPath))
				} else {
					u.playerStatus.SetText("Could not start wallpaper")
				}
			} else {
				u.playing = true
				u.playingPath = path
				u.automationError = ""
				u.selectVideo(path)
				u.save()
				u.playerStatus.SetText("Playing · " + fileName(path))
				u.status.SetText("Wallpaper active. Close this window to keep it in the tray.")
			}
			u.refreshControls()
		})
	})
}
func (u *studioUI) shutdown() {
	if u.closing {
		return
	}
	u.closing = true
	u.cancel()
	u.window.Hide()
	go func() { u.player.Stop(); u.wg.Wait(); fyne.Do(u.app.Quit) }()
}
func (u *studioUI) watchPlayer() {
	u.work(func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-u.ctx.Done():
				return
			case <-ticker.C:
				u.post(u.pollPlayback)
			}
		}
	})
}

func playbackStatusText(path string, paused bool) string {
	if paused {
		return "Paused for foreground app · " + fileName(path)
	}
	return "Playing · " + fileName(path)
}

func (u *studioUI) pollPlayback() {
	if u.playerBusy || u.checkingPlayback {
		return
	}
	u.checkingPlayback = true
	revision := u.playbackRevision
	u.work(func() {
		path, paused, err := u.player.Status()
		u.post(func() {
			u.checkingPlayback = false
			if revision != u.playbackRevision || u.playerBusy {
				return
			}
			u.playing = path != ""
			u.playingPath = path
			if path != "" {
				u.playerStatus.SetText(playbackStatusText(path, paused))
			} else if err != nil {
				u.playerStatus.SetText("Playback ended — set wallpaper to retry")
			}
			u.automationTick(time.Now())
			u.refreshControls()
		})
	})
}

func main() {
	handle, err := singleInstance()
	if handle != 0 {
		defer windows.CloseHandle(handle)
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	a := app.NewWithID("io.mlwdec.studio")
	a.Settings().SetTheme(studioTheme{})
	a.SetIcon(driftIcon)
	w := a.NewWindow(appName)
	w.Resize(fyne.NewSize(1180, 820))
	w.CenterOnScreen()
	cfg, loadErr := loadSettings()
	cfg.Startup = isAutoStartEnabled()
	restore := len(os.Args) > 1 && (os.Args[1] == "--restore" || os.Args[1] == "--wallpaper")
	if len(os.Args) > 2 && os.Args[1] == "--wallpaper" {
		cfg.Video = os.Args[2]
	}
	u := newStudio(a, w, cfg)
	tray, hasTray := a.(desktop.App)
	if hasTray {
		tray.SetSystemTrayMenu(fyne.NewMenu(appName, fyne.NewMenuItem("Open Drift", func() { w.Show(); w.RequestFocus() }), fyne.NewMenuItem("Stop wallpaper", func() {
			if !u.playerBusy {
				u.stop.OnTapped()
			}
		}), fyne.NewMenuItemSeparator(), fyne.NewMenuItem("Quit", u.shutdown)))
		tray.SetSystemTrayIcon(driftIcon)
	}
	w.SetCloseIntercept(func() {
		if hasTray && (u.playing || (u.cfg.Automation.Enabled && !u.automation.paused)) && !u.converting {
			w.Hide()
		} else {
			u.shutdown()
		}
	})
	u.watchPlayer()
	u.work(func() {
		options := getGPUs(u.ctx)
		u.post(func() {
			saved := u.cfg.GPU
			found := false
			for _, s := range options {
				if s == saved {
					found = true
				}
			}
			if !found && saved != "" {
				options = append(options, saved)
			}
			u.gpuSelect.Options = options
			u.gpuSelect.Refresh()
		})
	})
	if !restore {
		w.Show()
	}
	if loadErr != nil {
		w.Show()
		dialog.ShowError(fmt.Errorf("preferences could not be loaded: %w", loadErr), w)
	}
	if cfg.Automation.Enabled {
		u.automationStartup = restore
		u.automationTick(time.Now())
	} else if restore && cfg.Video != "" {
		u.playerBusy = true
		u.refreshControls()
		u.work(func() {
			var err error
			for i := 0; i < 8; i++ {
				err = u.player.Start(u.ctx, cfg.playbackOptions)
				if err == nil || u.ctx.Err() != nil {
					break
				}
				select {
				case <-u.ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
			u.post(func() {
				u.playerBusy = false
				if err != nil {
					w.Show()
					u.showError(err)
				} else {
					u.playing = true
					u.playingPath = cfg.Video
				}
				u.refreshControls()
			})
		})
	} else if restore {
		w.Show()
	}
	a.Run()
}
