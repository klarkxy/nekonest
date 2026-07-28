package adapters

import (
	"context"
	"sync"
)

type pollWatchEntry struct {
	id     uint64
	cancel context.CancelFunc
}

// pollWatchRegistry owns replaceable, cancellable polling watchers.
type pollWatchRegistry struct {
	mu      sync.Mutex
	nextID  uint64
	entries map[string]pollWatchEntry
}

func (r *pollWatchRegistry) start(key string) (context.Context, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[string]pollWatchEntry)
	}
	if previous, ok := r.entries[key]; ok {
		previous.cancel()
	}
	r.nextID++
	ctx, cancel := context.WithCancel(context.Background())
	r.entries[key] = pollWatchEntry{id: r.nextID, cancel: cancel}
	return ctx, r.nextID
}

func (r *pollWatchRegistry) finish(key string, id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.entries[key]; ok && current.id == id {
		delete(r.entries, key)
	}
}

func (r *pollWatchRegistry) stopAll() {
	r.mu.Lock()
	entries := r.entries
	r.entries = make(map[string]pollWatchEntry)
	r.mu.Unlock()
	for _, entry := range entries {
		entry.cancel()
	}
}
