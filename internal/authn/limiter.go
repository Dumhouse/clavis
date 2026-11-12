package authn

import (
	"sync"
	"time"
)

// Limiter is a fixed-window counter used for login and setup throttling. It is
// intentionally in-memory: with multiple replicas each gets its own budget,
// and M2 moves to a database-backed identity rate limiter.
type Limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	now     func() time.Time
	entries map[string]entry
}

type entry struct {
	count int
	reset time.Time
}

// NewLimiter returns a limiter allowing limit attempts per window.
func NewLimiter(limit int, window time.Duration) *Limiter {
	if limit <= 0 {
		limit = 5
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	return &Limiter{limit: limit, window: window, now: time.Now, entries: make(map[string]entry)}
}

// Allow records an attempt and reports whether it is within the budget.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e, ok := l.entries[key]
	if !ok || now.After(e.reset) {
		l.entries[key] = entry{count: 1, reset: now.Add(l.window)}
		if len(l.entries) > 10_000 {
			l.prune(now)
		}
		return true
	}
	if e.count >= l.limit {
		return false
	}
	e.count++
	l.entries[key] = e
	return true
}

// Reset clears a key after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// RetryAfter reports the remaining window for a throttled key.
func (l *Limiter) RetryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return 0
	}
	d := e.reset.Sub(l.now())
	if d < 0 {
		return 0
	}
	return d
}

func (l *Limiter) prune(now time.Time) {
	for key, e := range l.entries {
		if now.After(e.reset) {
			delete(l.entries, key)
		}
	}
}
