package cron_test

import (
	"context"
	"fmt"
	"strings"
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

func TestDriver_RejectsInvalidTriggerTimezone(t *testing.T) {
	d := cron.New(cron.Options{})
	_, err := d.Register(
		api.Trigger{ID: "x", Type: api.TriggerSchedule, Config: map[string]string{"cron": "0 9 * * * *", "timezone": "No/SuchZone"}},
		"agent-1",
		trigger.HandlerFunc(func(context.Context, trigger.TriggerContext) error { return nil }),
	)
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
	if !strings.Contains(err.Error(), "invalid timezone") {
		t.Fatalf("error = %v, want invalid timezone", err)
	}
}

func TestDriver_AppliesValidTriggerTimezone(t *testing.T) {
	d := cron.New(cron.Options{})
	var hits int32
	if _, err := d.Register(
		api.Trigger{ID: "tz", Type: api.TriggerSchedule, Config: map[string]string{"cron": "* * * * * *", "timezone": "UTC"}},
		"agent-1",
		trigger.HandlerFunc(func(context.Context, trigger.TriggerContext) error {
			atomic.AddInt32(&hits, 1)
			return nil
		}),
	); err != nil {
		t.Fatalf("Register with valid timezone: %v", err)
	}
	d.Start()
	defer func() { _ = d.Stop(context.Background()) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&hits) < 1 {
		time.Sleep(50 * time.Millisecond)
	}
	if atomic.LoadInt32(&hits) < 1 {
		t.Fatalf("expected at least 1 CRON_TZ firing within 3s, got %d", atomic.LoadInt32(&hits))
	}
}

func TestDriver_TrimsPaddedCronSpec(t *testing.T) {
	d := cron.New(cron.Options{})
	if _, err := d.Register(
		api.Trigger{ID: "padded", Type: api.TriggerSchedule, Config: map[string]string{"cron": "  * * * * * *  "}},
		"agent-1",
		trigger.HandlerFunc(func(context.Context, trigger.TriggerContext) error { return nil }),
	); err != nil {
		t.Fatalf("Register with padded cron spec: %v", err)
	}
	if got := d.List(); len(got) != 1 {
		t.Fatalf("expected 1 registration for padded spec, got %d", len(got))
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

// TestDriver_RecoversPanickingHandler is the regression for the cron
// panic-crash bug: robfig/cron v3 does not recover job panics, so a
// panicking handler used to crash the cron worker goroutine and the
// process. After the fix, the panic is caught by fire()'s recover and
// routed to the logger; the driver keeps scheduling and Stop returns
// cleanly.
func TestDriver_RecoversPanickingHandler(t *testing.T) {
	var logged atomic.Bool
	var captured strings.Builder
	d := cron.New(cron.Options{
		Logger: func(format string, args ...any) {
			// Simple capture: flag any panic-related log line.
			out := fmt.Sprintf(format, args...)
			captured.WriteString(out)
			if strings.Contains(out, "panicked") {
				logged.Store(true)
			}
		},
	})
	if _, err := d.Register(
		api.Trigger{ID: "boom", Type: api.TriggerSchedule, Config: map[string]string{"cron": "* * * * * *"}},
		"agent-1",
		trigger.HandlerFunc(func(context.Context, trigger.TriggerContext) error {
			panic("handler exploded")
		}),
	); err != nil {
		t.Fatalf("Register: %v", err)
	}
	d.Start()
	defer func() { _ = d.Stop(context.Background()) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !logged.Load() {
		time.Sleep(50 * time.Millisecond)
	}
	if !logged.Load() {
		t.Fatalf("expected panic to be recovered and logged within 3s; log captured: %q", captured.String())
	}
}
