package events

import (
	"testing"
	"time"
)

func TestBusFanoutAndCancel(t *testing.T) {
	bus := NewBus()
	first, cancelFirst := bus.Subscribe("org-1")
	second, cancelSecond := bus.Subscribe("org-1")
	defer cancelSecond()

	bus.Publish("org-1", Event{Type: "secrets.changed", Project: "web", Environment: "dev"})
	for _, ch := range []<-chan Event{first, second} {
		select {
		case event := <-ch:
			if event.Type != "secrets.changed" || event.Project != "web" || event.At.IsZero() {
				t.Fatalf("event = %+v", event)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	}

	// Other orgs do not receive events.
	other, cancelOther := bus.Subscribe("org-2")
	defer cancelOther()
	bus.Publish("org-1", Event{Type: "secrets.changed"})
	select {
	case event := <-other:
		t.Fatalf("unexpected cross-org event: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}

	cancelFirst()
	if got := bus.Subscribers("org-1"); got != 1 {
		t.Fatalf("subscribers = %d", got)
	}
	if got := bus.Subscribers("missing"); got != 0 {
		t.Fatalf("missing subscribers = %d", got)
	}
}

func TestBusSlowConsumerDoesNotBlock(t *testing.T) {
	bus := NewBus()
	_, cancel := bus.Subscribe("org")
	defer cancel()
	// Fill the buffer and then publish more; Publish must not block.
	for i := 0; i < 1000; i++ {
		bus.Publish("org", Event{Type: "secrets.changed"})
	}
}
