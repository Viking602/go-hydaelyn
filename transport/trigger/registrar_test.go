package trigger

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Viking602/venat/api"
)

type recordingRegistrar struct {
	registrations []Registration
	deregistered  []string
	failID        string
}

func (r *recordingRegistrar) Register(configured api.Trigger, agentID string, handler Handler) (Registration, error) {
	if configured.ID == r.failID {
		return Registration{}, errors.New("registration failed")
	}
	registration := Registration{Trigger: configured, AgentID: agentID, Handler: handler}
	r.registrations = append(r.registrations, registration)
	return registration, nil
}

func (r *recordingRegistrar) Deregister(triggerID string) bool {
	r.deregistered = append(r.deregistered, triggerID)
	return true
}

func TestRegistrarsRegisterAndCloseLifecycle(t *testing.T) {
	schedules := &recordingRegistrar{}
	events := &recordingRegistrar{}
	registrars := Registrars{
		api.TriggerSchedule: schedules,
		api.TriggerEvent:    events,
	}
	lifecycle, err := registrars.Register(api.AgentDefinition{
		ID: "agent-1",
		Triggers: []api.Trigger{
			{ID: "manual", Type: api.TriggerManual, Enabled: true},
			{ID: "disabled", Type: api.TriggerEvent},
			{ID: "schedule", Type: api.TriggerSchedule, Enabled: true},
			{ID: "event", Type: api.TriggerEvent, Enabled: true},
		},
	}, HandlerFunc(func(_ context.Context, _ TriggerContext) error { return nil }))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if got := lifecycle.Registrations(); len(got) != 2 || got[0].Trigger.ID != "schedule" || got[1].Trigger.ID != "event" {
		t.Fatalf("registrations = %#v", got)
	}
	if err := lifecycle.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := lifecycle.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if !reflect.DeepEqual(schedules.deregistered, []string{"schedule"}) || !reflect.DeepEqual(events.deregistered, []string{"event"}) {
		t.Fatalf("deregistered schedules=%v events=%v", schedules.deregistered, events.deregistered)
	}
}

func TestLifecycleRegistrationsOwnMutableTriggerMaps(t *testing.T) {
	registrar := &recordingRegistrar{}
	definition := api.AgentDefinition{
		ID: "agent-1",
		Triggers: []api.Trigger{{
			ID: "event", Type: api.TriggerEvent, Enabled: true,
			Config: map[string]string{"topic": "original"},
			Filter: map[string]string{"kind": "original"},
		}},
	}
	lifecycle, err := (Registrars{api.TriggerEvent: registrar}).Register(
		definition,
		HandlerFunc(func(_ context.Context, _ TriggerContext) error { return nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	definition.Triggers[0].Config["topic"] = "caller-changed"
	definition.Triggers[0].Filter["kind"] = "caller-changed"
	first := lifecycle.Registrations()
	first[0].Trigger.Config["topic"] = "snapshot-changed"
	first[0].Trigger.Filter["kind"] = "snapshot-changed"
	second := lifecycle.Registrations()
	if second[0].Trigger.Config["topic"] != "original" || second[0].Trigger.Filter["kind"] != "original" {
		t.Fatalf("registration snapshot mutated live maps: %#v", second[0].Trigger)
	}
	if registrar.registrations[0].Trigger.Config["topic"] != "original" ||
		registrar.registrations[0].Trigger.Filter["kind"] != "original" {
		t.Fatalf("registrar retained caller-owned maps: %#v", registrar.registrations[0].Trigger)
	}
}

func TestRegistrarsRegisterRollsBackPartialFailure(t *testing.T) {
	registrar := &recordingRegistrar{failID: "second"}
	_, err := (Registrars{api.TriggerEvent: registrar}).Register(api.AgentDefinition{
		ID: "agent-1",
		Triggers: []api.Trigger{
			{ID: "first", Type: api.TriggerEvent, Enabled: true},
			{ID: "second", Type: api.TriggerEvent, Enabled: true},
		},
	}, HandlerFunc(func(_ context.Context, _ TriggerContext) error { return nil }))
	if err == nil {
		t.Fatal("Register() succeeded, want failure")
	}
	if !reflect.DeepEqual(registrar.deregistered, []string{"first"}) {
		t.Fatalf("rollback deregistered = %v, want first", registrar.deregistered)
	}
}

func TestRegistrarsRegisterRejectsMissingTransport(t *testing.T) {
	_, err := (Registrars{}).Register(api.AgentDefinition{
		ID:       "agent-1",
		Triggers: []api.Trigger{{ID: "webhook", Type: api.TriggerWebhook, Enabled: true}},
	}, HandlerFunc(func(_ context.Context, _ TriggerContext) error { return nil }))
	if !errors.Is(err, ErrRegistrarMissing) {
		t.Fatalf("Register() error = %v, want ErrRegistrarMissing", err)
	}
}
