package audit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func newEvent(org string) *Event {
	return &Event{
		ID:        "evt-" + org,
		OrgID:     org,
		Actor:     Actor{ID: "user-1", Kind: "user"},
		Action:    "secret.reveal",
		Resource:  Resource{Type: "secret", ID: "item-1"},
		CreatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Details:   Details{KeyBlindIndex: "abc", Environment: "prod"},
	}
}

func TestMemoryStoreChain(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	for i := 0; i < 5; i++ {
		if err := store.Append(ctx, newEvent("org-1")); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Verify(ctx, "org-1"); err != nil {
		t.Fatal(err)
	}
	last, err := store.Last(ctx, "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if last.Seq != 5 || last.PrevHash == "" || last.Hash == "" {
		t.Fatalf("unexpected last event: %+v", last)
	}
	events, _ := store.List(ctx, "org-1", 2, 2)
	if len(events) != 2 || events[0].Seq != 3 {
		t.Fatalf("unexpected page: %+v", events)
	}
	if _, err := store.Last(ctx, "missing"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Last(ctx, "missing"); got != nil {
		t.Fatal("missing org has an event")
	}
	if got, _ := store.List(ctx, "missing", 0, 0); len(got) != 0 {
		t.Fatal("missing org has events")
	}
}

func TestChainsArePerOrg(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if err := store.Append(ctx, newEvent("org-1")); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, newEvent("org-2")); err != nil {
		t.Fatal(err)
	}
	for _, org := range []string{"org-1", "org-2"} {
		last, _ := store.Last(ctx, org)
		if last.Seq != 1 || last.PrevHash != genesisHash {
			t.Fatalf("org %s chain: seq=%d prev=%s", org, last.Seq, last.PrevHash)
		}
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	for i := 0; i < 3; i++ {
		_ = store.Append(ctx, newEvent("org-1"))
	}
	events, _ := store.List(ctx, "org-1", 0, 0)

	tests := []struct {
		name   string
		mutate func([]Event) []Event
	}{
		{"change action", func(e []Event) []Event { e[1].Action = "secret.read"; return e }},
		{"change actor", func(e []Event) []Event { e[1].Actor.ID = "attacker"; return e }},
		{"change seq", func(e []Event) []Event { e[1].Seq = 99; return e }},
		{"break link", func(e []Event) []Event { e[2].PrevHash = strings.Repeat("a", 64); return e }},
		{"drop event", func(e []Event) []Event { return append(e[:1], e[2:]...) }},
		{"reorder", func(e []Event) []Event { e[0], e[1] = e[1], e[0]; return e }},
		{"bad hash", func(e []Event) []Event { e[0].Hash = "nothex"; return e }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			copyEvents := make([]Event, len(events))
			copy(copyEvents, events)
			if err := Verify(tc.mutate(copyEvents)); err == nil {
				t.Fatal("tampering not detected")
			}
		})
	}
}

func TestSealRejectsCrossOrg(t *testing.T) {
	prev := newEvent("org-1")
	if err := Seal(nil, prev); err != nil {
		t.Fatal(err)
	}
	if err := Seal(prev, newEvent("org-2")); err == nil {
		t.Fatal("cross-org chaining accepted")
	}
	if err := Seal(nil, nil); err == nil {
		t.Fatal("nil event accepted")
	}
}

func TestValidate(t *testing.T) {
	valid := newEvent("org-1")
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Event)
	}{
		{"no id", func(e *Event) { e.ID = "" }},
		{"no org", func(e *Event) { e.OrgID = "" }},
		{"no actor", func(e *Event) { e.Actor = Actor{} }},
		{"no action", func(e *Event) { e.Action = "" }},
		{"no resource", func(e *Event) { e.Resource = Resource{} }},
		{"no time", func(e *Event) { e.CreatedAt = time.Time{} }},
		{"long reason", func(e *Event) { e.Details.Reason = strings.Repeat("x", MaxFieldLength+1) }},
		{"long command", func(e *Event) { e.Details.Command = strings.Repeat("x", MaxFieldLength+1) }},
		{"too many keys", func(e *Event) { e.Details.ChangedKeys = make([]string, 1001) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := newEvent("org-1")
			tc.mutate(ev)
			if err := ev.Validate(); err == nil {
				t.Fatal("invalid event accepted")
			}
		})
	}
}

func TestCanonicalIsDeterministic(t *testing.T) {
	a := newEvent("org-1")
	b := newEvent("org-1")
	// Timestamps with a monotonic clock component must hash identically.
	b.CreatedAt = a.CreatedAt
	ca, err := a.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	cb, _ := b.Canonical()
	if string(ca) != string(cb) {
		t.Fatal("canonical encoding is not deterministic")
	}
	// The canonical form must not contain a value field.
	var raw map[string]any
	_ = json.Unmarshal(ca, &raw)
	if _, ok := raw["value"]; ok {
		t.Fatal("canonical event carries a value field")
	}
}

func TestHashEventRejectsBadPrev(t *testing.T) {
	ev := newEvent("org-1")
	if _, err := HashEvent("zz", ev); err == nil {
		t.Fatal("bad prev hash accepted")
	}
	if _, err := HashEvent(strings.Repeat("a", 62), ev); err == nil {
		t.Fatal("short prev hash accepted")
	}
}

func TestStoreHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := NewMemoryStore()
	if err := store.Append(ctx, newEvent("org-1")); err == nil {
		t.Fatal("append ignored cancellation")
	}
	if _, err := store.Last(ctx, "org-1"); err == nil {
		t.Fatal("last ignored cancellation")
	}
	if _, err := store.List(ctx, "org-1", 0, 0); err == nil {
		t.Fatal("list ignored cancellation")
	}
}
