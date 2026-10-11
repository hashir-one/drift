package main

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
)

type queueEntry struct{ Path, Output, State, Detail string }
type fileQueue struct {
	mu      sync.Mutex
	entries []queueEntry
	busy    bool
}

func (q *fileQueue) Add(paths []string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.busy {
		return errors.New("wait for conversion to finish before adding files")
	}
	for _, path := range paths {
		if !strings.EqualFold(filepath.Ext(path), ".mlw") {
			continue
		}
		duplicate := false
		for _, e := range q.entries {
			if strings.EqualFold(e.Path, path) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			q.entries = append(q.entries, queueEntry{Path: path, State: "Ready", Detail: "Ready to convert"})
		}
	}
	return nil
}
func (q *fileQueue) Snapshot() []queueEntry {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]queueEntry(nil), q.entries...)
}
func (q *fileQueue) Begin() ([]queueEntry, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.busy {
		return nil, errors.New("conversion already in progress")
	}
	q.busy = true
	return append([]queueEntry(nil), q.entries...), nil
}
func (q *fileQueue) Finish() { q.mu.Lock(); q.busy = false; q.mu.Unlock() }
func (q *fileQueue) Update(index int, state, detail, out string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if index >= 0 && index < len(q.entries) {
		q.entries[index].State = state
		q.entries[index].Detail = detail
		if out != "" {
			q.entries[index].Output = out
		}
	}
}
func (q *fileQueue) Clear() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.busy {
		return errors.New("cannot clear files during conversion")
	}
	q.entries = nil
	return nil
}
