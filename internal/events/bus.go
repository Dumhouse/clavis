// Package events is the in-process event bus used for SSE updates.
package events

import (
	"sync"
	"time"
)

// Event describes a change worth refreshing in connected clients. It never
// carries secret values or key names.
type Event struct {
	Type        string    `json:"type"`
	Project     string    `json:"project,omitempty"`
	Environment string    `json:"environment,omitempty"`
	At          time.Time `json:"at"`
}

// Bus fans out events to subscribers per organization.
type Bus struct {
	mu   sync.RWMutex
	subs map[string]map[chan Event]struct{}
}

// NewBus returns an empty event bus.
func NewBus() *Bus {
	return &Bus{subs: make(map[string]map[chan Event]struct{})}
}

// Subscribe returns a channel of events for an org and a cancel function.
func (b *Bus) Subscribe(orgID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	if b.subs[orgID] == nil {
		b.subs[orgID] = make(map[chan Event]struct{})
	}
	b.subs[orgID][ch] = struct{}{}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		if set, ok := b.subs[orgID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(b.subs, orgID)
			}
		}
		b.mu.Unlock()
		close(ch)
	}
	return ch, cancel
}

// Publish sends an event to all subscribers. Slow consumers are skipped.
func (b *Bus) Publish(orgID string, event Event) {
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs[orgID] {
		select {
		case ch <- event:
		default:
		}
	}
}

// Subscribers reports the current subscriber count (used by tests/metrics).
func (b *Bus) Subscribers(orgID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[orgID])
}
