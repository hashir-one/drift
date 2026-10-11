//go:build windows

package main

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type WallpaperMonitor struct {
	paused   atomic.Bool
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	send     func(context.Context, bool) error
	detect   func() bool
	interval time.Duration
}

func (m *WallpaperMonitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	interval := m.interval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	detect := m.detect
	if detect == nil {
		detect = foregroundAppMaximized
	}
	send := func(ctx context.Context, value bool) error {
		if m.send == nil {
			return nil
		}
		err := m.send(ctx, value)
		if err == nil {
			m.paused.Store(value)
		}
		return err
	}
	go monitorLoop(ctx, m.done, interval, detect, send)
}
func monitorLoop(ctx context.Context, done chan struct{}, interval time.Duration, detect func() bool, send func(context.Context, bool) error) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var known, paused bool
	for {
		if ctx.Err() != nil {
			return
		}
		desired := detect()
		if !known || desired != paused {
			if send != nil && send(ctx, desired) == nil {
				paused = desired
				known = true
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (m *WallpaperMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel == nil {
		return
	}
	m.cancel()
	<-m.done
	m.cancel = nil
	if m.send != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if m.send(ctx, false) == nil {
			m.paused.Store(false)
		}
	}
}
