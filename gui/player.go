//go:build windows

package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Microsoft/go-winio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
func findMPV() (string, error) {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "mpv.exe")
		if fileExists(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("mpv.exe"); err == nil {
		return p, nil
	}
	return "", errors.New("mpv.exe was not found. Place it beside Drift or add it to PATH")
}

type boundedLog struct {
	mu   sync.Mutex
	data []byte
}

func (b *boundedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) > 8192 {
		p = p[len(p)-8192:]
	}
	b.data = append(b.data, p...)
	if len(b.data) > 8192 {
		b.data = b.data[len(b.data)-8192:]
	}
	return n, nil
}
func (b *boundedLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}

func mpvRequest(ctx context.Context, pipe string, command ...any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := winio.DialPipeContext(ctx, pipe)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if err = json.NewEncoder(conn).Encode(map[string]any{"command": command, "request_id": 1}); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(io.LimitReader(conn, 64<<10))
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var reply struct {
			ID    int             `json:"request_id"`
			Error string          `json:"error"`
			Data  json.RawMessage `json:"data"`
		}
		if err = json.Unmarshal(line, &reply); err != nil {
			return nil, err
		}
		if reply.ID != 1 {
			continue
		}
		if reply.Error != "success" {
			return nil, fmt.Errorf("player: %s", reply.Error)
		}
		return reply.Data, nil
	}
}

type playbackOptions struct {
	Video     string `json:"video"`
	GPU       string `json:"gpu"`
	AutoPause bool   `json:"auto_pause"`
}
type playerRun struct {
	cmd     *exec.Cmd
	done    chan struct{}
	err     error
	log     boundedLog
	pipe    string
	video   string
	monitor *WallpaperMonitor
}
type Player struct {
	mu        sync.Mutex
	run       *playerRun
	lastError error
}

func playerArgs(options playbackOptions, worker uintptr, pipe string) []string {
	args := []string{"--no-config", "--load-scripts=no", "--audio=no", "--input-default-bindings=no", "--input-vo-keyboard=no", "--no-osc", "--no-border", "--loop-file=inf", "--panscan=1.0", "--wid=" + strconv.FormatUint(uint64(worker), 10), "--input-ipc-server=" + pipe}
	if options.GPU != "" && options.GPU != autoGPU {
		args = append(args, "--gpu-api=d3d11", "--d3d11-adapter="+options.GPU, "--hwdec=d3d11va")
	} else {
		args = append(args, "--hwdec=auto")
	}
	return append(args, "--", options.Video)
}
func (p *Player) Start(ctx context.Context, options playbackOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !fileExists(options.Video) {
		return errors.New("selected video no longer exists")
	}
	mpv, err := findMPV()
	if err != nil {
		return err
	}
	worker, err := getDesktopWorkerW()
	if err != nil {
		return err
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return err
	}
	pipe := `\\.\pipe\mlw-studio-` + hex.EncodeToString(token)
	run := &playerRun{done: make(chan struct{}), pipe: pipe, video: options.Video}
	run.cmd = exec.Command(mpv, playerArgs(options, worker, pipe)...)
	run.cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	run.cmd.Stdout = &run.log
	run.cmd.Stderr = &run.log
	if err = run.cmd.Start(); err != nil {
		return err
	}
	go func() { run.err = run.cmd.Wait(); close(run.done) }()
	readyCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-run.done:
			err = fmt.Errorf("player exited before video was ready: %v %s", run.err, run.log.String())
			stopRun(run)
			return err
		case <-readyCtx.Done():
			stopRun(run)
			return fmt.Errorf("player did not become ready: %w", readyCtx.Err())
		case <-ticker.C:
			value, e := mpvRequest(readyCtx, pipe, "get_property", "video-params")
			if e == nil && len(value) > 0 && string(value) != "null" {
				p.stopLocked()
				p.run = run
				p.lastError = nil
				run.monitor = &WallpaperMonitor{send: func(c context.Context, paused bool) error {
					_, e := mpvRequest(c, pipe, "set_property", "pause", paused)
					return e
				}}
				if options.AutoPause {
					run.monitor.Start()
				}
				return nil
			}
		}
	}
}
func (p *Player) stopLocked() {
	if p.run == nil {
		return
	}
	stopRun(p.run)
	p.run = nil
}
func stopRun(run *playerRun) {
	if run.monitor != nil {
		run.monitor.Stop()
	}
	select {
	case <-run.done:
	default:
		_ = run.cmd.Process.Kill()
		<-run.done
	}
}
func (p *Player) Stop() { p.mu.Lock(); defer p.mu.Unlock(); p.stopLocked(); p.lastError = nil }
func (p *Player) SetAutoPause(enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run != nil && p.run.monitor != nil {
		if enabled {
			p.run.monitor.Start()
		} else {
			p.run.monitor.Stop()
		}
	}
}
func (p *Player) Status() (string, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run != nil {
		select {
		case <-p.run.done:
			p.lastError = fmt.Errorf("playback ended: %v %s", p.run.err, p.run.log.String())
			p.stopLocked()
		default:
			return p.run.video, p.run.monitor != nil && p.run.monitor.paused.Load(), nil
		}
	}
	return "", false, p.lastError
}
