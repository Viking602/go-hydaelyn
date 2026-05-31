package cron_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/transport/cron"
	"github.com/Viking602/go-hydaelyn/transport/trigger"
)

func TestDriver_RegisterDeregister_List(t *testing.T) {
	d := cron.New(cron.Options{})
	reg, err := d.Register(
		api.Trigger{ID: "every-second", Type: api.TriggerSchedule, Config: map[string]string{"cron": "* * * * * *"}},
		"agent-1",
		trigger.HandlerFunc(func(context.Context, trigger.TriggerContext) error { return nil }),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.Trigger.ID != "every-second" {
		t.Fatalf("unexpected registration: %+v", reg)
	}
	if got := d.List(); len(got) != 1 {
		t.Fatalf("expected 1 registration, got %d", len(got))
	}
	if !d.Deregister("every-second") {
		t.Fatal("Deregister returned false")
	}
	if got := d.List(); len(got) != 0 {
		t.Fatalf("expected 0 registrations after deregister, got %d", len(got))
	}
}

func TestDriver_RejectsWrongTriggerType(t *testing.T) {
	d := cron.New(cron.Options{})
	_, err := d.Register(
		api.Trigger{ID: "x", Type: api.TriggerWebhook, Config: map[string]string{"path": "/x"}},
		"agent-1",
		trigger.HandlerFunc(func(context.Context, trigger.TriggerContext) error { return nil }),
	)
	if err == nil {
		t.Fatal("expected error for non-schedule trigger type")
	}
}

func TestDriver_FiresAtCronCadence(t *testing.T) {
	d := cron.New(cron.Options{})
	var hits int32
	if _, err := d.Register(
		api.Trigger{ID: "tick", Type: api.TriggerSchedule, Config: map[string]string{"cron": "* * * * * *"}},
		"agent-1",
		trigger.HandlerFunc(func(context.Context, trigger.TriggerContext) error {
			atomic.AddInt32(&hits, 1)
			return nil
		}),
	); err != nil {
		t.Fatalf("Register: %v", err)
	}
	d.Start()
	defer func() { _ = d.Stop(context.Background()) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&hits) < 1 {
		time.Sleep(50 * time.Millisecond)
	}
	if atomic.LoadInt32(&hits) < 1 {
		t.Fatalf("expected at least 1 firing within 3s, got %d", atomic.LoadInt32(&hits))
	}
}
