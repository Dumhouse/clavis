package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := New(2, time.Minute)
	l.SetClock(func() time.Time { return now })
	first := l.Allow("user")
	second := l.Allow("user")
	third := l.Allow("user")
	if !first.Allowed || !second.Allowed || third.Allowed {
		t.Fatalf("decisions = %+v / %+v / %+v", first, second, third)
	}
	if first.Remaining != 1 || second.Remaining != 0 || third.Remaining != 0 {
		t.Fatalf("remaining = %d/%d/%d", first.Remaining, second.Remaining, third.Remaining)
	}
	now = now.Add(time.Minute + time.Second)
	if !l.Allow("user").Allowed {
		t.Fatal("window did not reset")
	}
	other := l.Allow("other")
	if !other.Allowed || other.Remaining != 1 {
		t.Fatalf("other = %+v", other)
	}
}

func TestLimiterDisabled(t *testing.T) {
	l := New(0, time.Minute)
	for i := 0; i < 100; i++ {
		if !l.Allow("x").Allowed {
			t.Fatal("disabled limiter denied")
		}
	}
}
