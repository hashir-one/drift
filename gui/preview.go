//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func validJPEG(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		return err
	}
	if img.Bounds().Empty() {
		return errors.New("empty image")
	}
	return nil
}
func extractFrame(ctx context.Context, video, mpv, out string) error {
	dir, err := os.MkdirTemp(filepath.Dir(out), ".mlw-frame-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var last error
	for _, start := range []string{"5", "0"} {
		generated := filepath.Join(dir, "00000001.jpg")
		if err = os.Remove(generated); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		attempt, cancel := context.WithTimeout(ctx, 25*time.Second)
		cmd := exec.CommandContext(attempt, mpv, "--no-config", "--load-scripts=no", "--no-audio", "--start="+start, "--frames=1", "--vo=image", "--vo-image-format=jpg", "--vo-image-jpeg-quality=90", "--vo-image-outdir="+dir, "--", video)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		var log boundedLog
		cmd.Stdout = &log
		cmd.Stderr = &log
		err = cmd.Run()
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			err = validJPEG(generated)
		}
		if err == nil {
			return moveFile(generated, out, true)
		}
		last = fmt.Errorf("could not extract a video frame: %w %s", err, log.String())
	}
	return last
}
