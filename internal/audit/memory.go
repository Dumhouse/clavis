package audit

import (
	"context"
	"fmt"
	"sync"
)

// Store persists audit events. Implementations must guarantee that Append is
// atomic per org (last event read + seal + insert) so the chain has no forks.
type Store interface {
	Append(ctx context.Context, event *Event) error
	Last(ctx context.Context, orgID string) (*Event, error)
	List(ctx context.Context, orgID string, afterSeq int64, limit int) ([]Event, error)
}

// MemoryStore is an in-memory Store for tests and local development.
type MemoryStore struct {
	mu     sync.Mutex
	events map[string][]Event
}

// NewMemoryStore returns an empty memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{events: make(map[string][]Event)}
}

// Append implements Store.
func (m *MemoryStore) Append(ctx context.Context, event *Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	chain := m.events[event.OrgID]
	var prev *Event
	if len(chain) > 0 {
		prev = &chain[len(chain)-1]
	}
	if err := Seal(prev, event); err != nil {
		return err
	}
	m.events[event.OrgID] = append(chain, *event)
	return nil
}

// Last implements Store.
func (m *MemoryStore) Last(ctx context.Context, orgID string) (*Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	chain := m.events[orgID]
	if len(chain) == 0 {
		return nil, nil
	}
	last := chain[len(chain)-1]
	return &last, nil
}

// List implements Store.
func (m *MemoryStore) List(ctx context.Context, orgID string, afterSeq int64, limit int) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Event
	for _, ev := range m.events[orgID] {
		if ev.Seq <= afterSeq {
			continue
		}
		out = append(out, ev)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

// Verify recomputes the store's chain for an org.
func (m *MemoryStore) Verify(ctx context.Context, orgID string) error {
	events, err := m.List(ctx, orgID, 0, 0)
	if err != nil {
		return err
	}
	if err := Verify(events); err != nil {
		return fmt.Errorf("audit: org %s: %w", orgID, err)
	}
	return nil
}
