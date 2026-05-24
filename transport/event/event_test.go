package event_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/transport/event"
	"github.com/Viking602/go-hydaelyn/transport/trigger"
)

func TestDriver_PublishFiresMatchingTrigger(t *testing.T) {
	d := event.New(event.Options{})
	var fired int32
	_, err := d.Register(
		api.Trigger{ID: "t", Type: api.TriggerEvent, Config: map[string]string{"topic": "incident.created"}},
		"agent",
		trigger.HandlerFunc(func(ctx context.Context, tc trigger.TriggerContext) error {
			atomic.AddInt32(&fired, 1)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := d.Publish(context.Background(), event.Event{Topic: "incident.created"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if atomic.LoadInt32(&fired) != 1 {
		t.Fatalf("expected 1 firing, got %d", atomic.LoadInt32(&fired))
	}
}

func TestDriver_FilterRequiresAllAttributes(t *testing.T) {
	d := event.New(event.Options{})
	var fired int32
	_, _ = d.Register(
		api.Trigger{
			ID:     "high-only",
			Type:   api.TriggerEvent,
			Config: map[string]string{"topic": "incident.created"},
			Filter: map[string]string{"severity": "high"},
		},
		"agent",
		trigger.HandlerFunc(func(ctx context.Context, tc trigger.TriggerContext) error {
			atomic.AddInt32(&fired, 1)
			return nil
		}),
	)
	_ = d.Publish(context.Background(), event.Event{Topic: "incident.created", Attributes: map[string]string{"severity": "low"}})
	if atomic.LoadInt32(&fired) != 0 {
		t.Fatal("low-severity should not match")
	}
	_ = d.Publish(context.Background(), event.Event{Topic: "incident.created", Attributes: map[string]string{"severity": "high"}})
	if atomic.LoadInt32(&fired) != 1 {
		t.Fatalf("expected high to fire, got %d", atomic.LoadInt32(&fired))
	}
}

func TestDriver_Deregister(t *testing.T) {
	d := event.New(event.Options{})
	_, _ = d.Register(
		api.Trigger{ID: "x", Type: api.TriggerEvent, Config: map[string]string{"topic": "t"}},
		"a",
		trigger.HandlerFunc(func(ctx context.Context, t trigger.TriggerContext) error { return nil }),
	)
	if !d.Deregister("x") {
		t.Fatal("Deregister returned false")
	}
	if d.Deregister("x") {
		t.Fatal("second Deregister should return false")
	}
}
