# Drift

![Drift logo](gui/assets/drift-logo.png)

[![Windows checks](https://github.com/hashir-one/drift/actions/workflows/check.yml/badge.svg)](https://github.com/hashir-one/drift/actions/workflows/check.yml)

Live wallpapers, at your pace. Drift plays silent desktop video wallpapers, rotates playlists, schedules daily changes, and converts authenticated `.mlw` files. MLW is a supported file extension, not the app name.

## Run

Build Drift using the instructions below, then launch `gui/drift.exe`. Successful GitHub Actions runs also provide a `drift-windows-x64` executable artifact. Install [mpv for Windows](https://mpv.io/installation/) and keep `mpv.exe` beside Drift, or make it available on PATH. mpv and wallpaper videos are not included in this repository. Import or drop an MLW/MP4 file to begin. Converted MP4s are saved beside their source; existing outputs receive a numbered sibling instead of being overwritten.

An active wallpaper continues when the window is closed. Use the Drift tray menu to reopen the window, stop playback, or quit. One controller runs per Windows session; if launching again does not open a window, use its tray menu. Startup, adapter selection, auto-pause, and the video library are saved under `%APPDATA%\MLWStudio`.

## Automatic wallpaper changes

Open **Automation** in the sidebar. Import or convert videos first, then choose a mode:

- **Every interval:** select at least two videos and enter an interval in minutes (1 to 10,080). Videos rotate in collection order; unavailable files are skipped.
- **At specific times:** add daily entries such as `08:00` and `18:30`, selecting a video for each. Times use the computer's local 24-hour clock. Each video stays active until the next entry, including overnight.

Check **Enable automatic wallpaper changes** and click **Save changes**. Only the selected mode runs. The page shows the next change. Preferences and both mode configurations are saved; editing a form does not change the active setup until you save.

Automation continues in the tray. **Stop** pauses playback and automation for this session; **Resume automation** resumes the saved plan. Setting a wallpaper manually restarts the interval or holds it until the next daily entry. Quitting stops automation; reopening resumes an enabled plan. Enable **Restore wallpaper when I sign in** in Settings for login startup.

After sleep, daily mode selects the most recent applicable entry; interval mode makes at most one overdue change and starts a fresh interval. A failed video is reported in the status area without repeated pop-ups, and the next entry/interval is tried normally. The current player is retained until its replacement is ready. Missing scheduled videos are skipped. Windows remains responsible for sleep and login; the app does not wake the computer.

## Build and check

Requires Windows amd64, Go 1.26.9 or newer (Go's automatic toolchain download is supported), and MinGW-w64 GCC for Fyne/OpenGL. Run `gui\build.bat` from any directory; it creates `gui\drift.exe`. The build uses your existing TEMP/TMP settings. A GCC installation on PATH is preferred; the existing local `C:\gcc\mingw64\bin` location is a fallback.

From `gui`, with GCC on PATH:

```powershell
go test ./...
go test -race ./...
go vet ./...
go mod verify
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
```

For media integration checks, set `MLW_INTEGRATION=1` and run `go test -run TestRealFrameExtraction -v`. This uses `gui/mpv.exe` and a local MP4 specified by `DRIFT_TEST_VIDEO` (or placed in `.local/samples`), creates temporary outputs, and checks both normal and short clips. These fixtures stay local and are not committed.

The Python command remains available: `python mlw_extractor.py file.mlw` (requires the `cryptography` package). It uses the same authentication and no-overwrite policy. Atomic publication in the Python tool requires a filesystem supporting hard links; it fails safely otherwise.

## Behavior and limits

- Only authenticated MLW payloads are accepted; damaged or unsupported files fail explicitly.
- The input limit is 512 MiB. Cancellation is checked between read, authentication, and publication; an individual AES operation is not interruptible.
- Auto-pause checks the foreground app only. Hidden/cloaked windows, the desktop, and Drift itself are excluded. Playback resumes when you return to the desktop. The player status explicitly shows when auto-pause is active. This is not a per-monitor occlusion detector.
- Auto GPU mode leaves Windows GPU preferences untouched. An explicit adapter is passed only to this player instance.
- A full Explorer restart may require setting the wallpaper again. The desktop embedding interface is Windows-shell dependent.

## Brand assets

Editable vector sources are `gui/assets/drift-icon.svg` and `gui/assets/drift-logo.svg`. PNG previews and a Windows ICO with 16–256 px sizes are included in the same folder. The flowing D mark uses indigo and mint. The icon is embedded in the window, tray, and executable; Windows file details identify the product as Drift 2.1.

Run `go run ./tools/brand` from `gui` to regenerate the PNG/ICO assets. `build.bat` also regenerates the Windows resource with MinGW `windres`. The build produces only `drift.exe`; obsolete launcher names are no longer generated. The legacy settings directory, app ID, and startup value are retained so existing videos, schedules, and sign-in settings continue to work.

## Repository layout

- `gui/` — Go/Fyne app, Windows integration, scheduler, and tests.
- `gui/assets/` — logo, icon, and Windows resource source.
- `gui/tools/brand/` — reproducible SVG-to-PNG/ICO asset generator.
- `mlw_extractor.py` — optional command-line converter, with tests and requirements.
- `.github/workflows/check.yml` — Windows build, native tests, headless race checks, dependency scanning, and Python tests.

Local executables, mpv, generated Windows resource objects, sample wallpapers, settings, and audit history are excluded from Git. Personal media is never uploaded by Drift.
