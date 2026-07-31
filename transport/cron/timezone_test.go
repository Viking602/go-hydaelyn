package cron

import (
	"context"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/transport/trigger"
)

func TestScheduleSpecAppliesTimezone(t *testing.T) {
	got, err := scheduleSpec("0 0 9 * * *", map[string]string{"timezone": "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("scheduleSpec() error = %v", err)
	}
	if got != "CRON_TZ=Asia/Shanghai 0 0 9 * * *" {
		t.Fatalf("scheduleSpec() = %q", got)
	}
}

func TestScheduleSpecLeavesDefaultTimezoneSpecUntouched(t *testing.T) {
	got, err := scheduleSpec("0 0 9 * * *", nil)
	if err != nil {
		t.Fatalf("scheduleSpec() error = %v", err)
	}
	if got != "0 0 9 * * *" {
		t.Fatalf("scheduleSpec() = %q", got)
	}
}

func TestDriverRegisterUsesTriggerTimezone(t *testing.T) {
	d := New(Options{DefaultLocation: time.UTC})
	_, err := d.Register(
		api.Trigger{ID: "morning", Type: api.TriggerSchedule, Config: map[string]string{"cron": "0 0 9 * * *", "timezone": "Asia/Shanghai"}},
		"agent-1",
		trigger.HandlerFunc(func(context.Context, trigger.TriggerContext) error { return nil }),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	entryID := d.jobs["morning"]
	entry := d.cron.Entry(entryID)
	got := entry.Schedule.Next(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	want := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next firing = %s, want %s", got, want)
	}
}
