package icd

import (
	"context"
	"sync"
)

// Recorder persists the validated coding decision for a note. Agents call
// Record only after domain validation has accepted the intent.
type Recorder interface {
	Record(ctx context.Context, note Note, intent Intent) error
}

// InMemoryRecorder stores validated coding decisions in process memory.
// Production stores live in sibling subpackages (e.g. icd/postgres).
type InMemoryRecorder struct {
	mu      sync.Mutex
	entries map[string]Intent
}

func NewInMemoryRecorder() *InMemoryRecorder {
	return &InMemoryRecorder{entries: make(map[string]Intent)}
}

func (r *InMemoryRecorder) Record(_ context.Context, note Note, intent Intent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[note.ID] = intent
	return nil
}

func (r *InMemoryRecorder) Get(noteID string) (Intent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	intent, ok := r.entries[noteID]
	return intent, ok
}
