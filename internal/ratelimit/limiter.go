// Package ratelimit implements the in-memory per-identity fixed-window limiter
// used by the API. M3 moves counters to the database for multi-replica setups.
package ratelimit

import (
	"sync"
	"time"
)

// Result describes one rate-limit decision.
type Result struct {
	Allowed   bool
	Limit     int
	Remaining int
	Reset     time.Time
}

type entry struct {
	count int
	reset time.Time
}

// Limiter is a fixed-window limiter.
type Limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	byKey  map[string]entry
}

// New returns a limiter with limit requests per window. limit <= 0 disables it.
func New(limit int, window time.Duration) *Limiter {
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{limit: limit, window: window, now: time.Now, byKey: make(map[string]entry)}
}

// Allow records a request and returns the decision.
func (l *Limiter) Allow(key string) Result {
	if l.limit <= 0 {
		return Result{Allowed: true}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	current, ok := l.byKey[key]
	if !ok || now.After(current.reset) {
		reset := now.Add(l.window)
		l.byKey[key] = entry{count: 1, reset: reset}
		if len(l.byKey) > 20_000 {
			l.prune(now)
		}
		return Result{Allowed: true, Limit: l.limit, Remaining: l.limit - 1, Reset: reset}
	}
	if current.count >= l.limit {
		return Result{Allowed: false, Limit: l.limit, Remaining: 0, Reset: current.reset}
	}
	current.count++
	l.byKey[key] = current
	return Result{Allowed: true, Limit: l.limit, Remaining: l.limit - current.count, Reset: current.reset}
}

func (l *Limiter) prune(now time.Time) {
	for key, value := range l.byKey {
		if now.After(value.reset) {
			delete(l.byKey, key)
		}
	}
}

// SetClock replaces the clock in tests.
func (l *Limiter) SetClock(now func() time.Time) { l.now = now }
