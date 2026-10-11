package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func encryptedFixture(t *testing.T) []byte {
	t.Helper()
	key, _ := hex.DecodeString(aesKeyHex)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := cipher.NewGCM(block)
	data := append([]byte("Root\x00sample.mp4\x00"), make([]byte, 12)...)
	nonce := make([]byte, 12)
	data = append(data, nonce...)
	data = append(data, make([]byte, 4)...)
	return append(data, g.Seal(nil, nonce, []byte("authenticated video fixture"), nil)...)
}
func TestAuthentication(t *testing.T) {
	data := encryptedFixture(t)
	got, err := decodeMLW(bytes.Clone(data))
	if err != nil || string(got) != "authenticated video fixture" {
		t.Fatalf("valid: %s %v", got, err)
	}
	for _, index := range []int{len(data) - 1, len(data) - 20} {
		bad := bytes.Clone(data)
		bad[index] ^= 1
		if _, err = decodeMLW(bad); err == nil {
			t.Fatal("accepted tampering")
		}
	}
	for _, bad := range [][]byte{nil, []byte("Root\x00unterminated"), []byte("Root\x00x\x00"), data[:30]} {
		if _, err = decodeMLW(bad); err == nil {
			t.Fatal("accepted malformed header")
		}
	}
}
func TestConversionPreservesOutputs(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sample.MLW")
	if err := os.WriteFile(src, encryptedFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "sample.mp4")
	os.WriteFile(existing, []byte("keep me"), 0600)
	out, err := decryptMLW(src)
	if err != nil {
		t.Fatal(err)
	}
	if out == existing {
		t.Fatal("reused existing destination")
	}
	got, _ := os.ReadFile(existing)
	if string(got) != "keep me" {
		t.Fatal("overwrote output")
	}
	got, _ = os.ReadFile(out)
	if string(got) != "authenticated video fixture" {
		t.Fatal("wrong output")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = decryptFile(ctx, src); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
func TestConcurrentPublication(t *testing.T) {
	base := filepath.Join(t.TempDir(), "video")
	var wg sync.WaitGroup
	paths := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := writeUnique(context.Background(), base, ".mp4", []byte("content"))
			if err != nil {
				t.Error(err)
			} else {
				paths <- path
			}
		}()
	}
	wg.Wait()
	close(paths)
	seen := map[string]bool{}
	for path := range paths {
		if seen[path] {
			t.Fatal("duplicate publication")
		}
		seen[path] = true
	}
	if len(seen) != 12 {
		t.Fatal("missing outputs")
	}
}
func TestConversionSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.mlw")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(maxMLWSize + 1); err != nil {
		f.Close()
		t.Skip(err)
	}
	f.Close()
	if _, err = decryptMLW(path); err == nil {
		t.Fatal("accepted oversized input")
	}
}
func TestQueueBatchIsolation(t *testing.T) {
	q := &fileQueue{}
	q.Add([]string{"A.MLW", "a.mlw"})
	batch, err := q.Begin()
	if err != nil || len(batch) != 1 {
		t.Fatal("deduplication/begin failed")
	}
	if q.Clear() == nil || q.Add([]string{"b.mlw"}) == nil {
		t.Fatal("mutable active queue")
	}
	if _, err = q.Begin(); err == nil {
		t.Fatal("concurrent batch allowed")
	}
	q.Update(0, "Converted", "done", "out.mp4")
	if batch[0].State != "Ready" {
		t.Fatal("batch aliased queue")
	}
	q.Finish()
	if q.Clear() != nil || len(q.Snapshot()) != 0 {
		t.Fatal("clear failed")
	}
}
func TestMonitorRetriesAndRestarts(t *testing.T) {
	var calls atomic.Int32
	var last atomic.Bool
	var failed atomic.Bool
	failed.Store(true)
	m := &WallpaperMonitor{interval: time.Millisecond, detect: func() bool { return true }, send: func(ctx context.Context, v bool) error {
		calls.Add(1)
		if v && failed.Swap(false) {
			return errors.New("pipe not ready")
		}
		last.Store(v)
		return nil
	}}
	m.Start()
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatal("did not retry failed command")
	}
	m.Stop()
	if last.Load() {
		t.Fatal("stop did not resume")
	}
	before := calls.Load()
	m.Start()
	deadline = time.Now().Add(time.Second)
	for calls.Load() == before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	m.Stop()
	if calls.Load() < before+2 {
		t.Fatal("restart reused stale pause state")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				m.Start()
				m.Stop()
			}
		}()
	}
	wg.Wait()
	m.Stop()
}
func TestForegroundPolling(t *testing.T) {
	for i := 0; i < 2200; i++ {
		foregroundAppMaximized()
	}
}
func TestPlayerArguments(t *testing.T) {
	args := playerArgs(playbackOptions{Video: "C:\\a b\\video.mp4", GPU: autoGPU}, 123, "pipe")
	joined := bytes.Join(func() [][]byte {
		var b [][]byte
		for _, a := range args {
			b = append(b, []byte(a))
		}
		return b
	}(), []byte(" "))
	if bytes.Contains(joined, []byte("--d3d11-adapter")) {
		t.Fatal("auto forces adapter")
	}
	if args[len(args)-2] != "--" {
		t.Fatal("file is not separated from options")
	}
	if !bytes.Contains(joined, []byte("--audio=no")) {
		t.Fatal("wallpaper audio enabled")
	}
}
func FuzzDecodeMLW(f *testing.F) {
	f.Add([]byte("Root\x00x\x00"))
	f.Add([]byte("anything"))
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = decodeMLW(data) })
}
