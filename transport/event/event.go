// Package event is the in-process pub/sub transport driver for
// api.TriggerEvent. It is intentionally minimal: a topic-keyed map of
// registrations dispatched whenever Publish is called. Production
// deployments that need durable or cross-process delivery should adapt
// this driver to NATS, Kafka, Redis Streams, etc. — the public surface
// (Register, Deregister, Publish) is small enough to mirror against any
// of those.
//
// Trigger configuration:
//
//	Trigger.Type = api.TriggerEvent
//	Trigger.Config["topic"] = "incident.created"
//	Trigger.Filter["severity"] = "high"            // optional; AND across keys
//
// On each matching event, the registered Handler receives a
// TriggerContext whose Source is the topic name and Payload is the raw
// event body.
package event

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/transport/trigger"
)

// Event is the unit Publish accepts. Attributes feed Trigger.Filter
// matching: a trigger fires only when every key in its Filter map is
// present on the Event with the same value.
type Event struct {
	Topic      string
	Payload    []byte
	Attributes map[string]string
	OccurredAt time.Time
}

// Driver routes Events to registered triggers. The zero value is
// unusable; construct via New.
type Driver struct {
	mu     sync.RWMutex
	byTopic map[string][]trigger.Registration
	logger func(format string, args ...any)
}

// Options configures Driver construction.
type Options struct {
	Logger func(format string, args ...any)
}

// New constructs a Driver.
func New(opts Options) *Driver {
	logger := opts.Logger
	if logger == nil {
		logger = func(format string, args ...any) {}
	}
	return &Driver{byTopic: map[string][]trigger.Registration{}, logger: logger}
}

// Register adds an event trigger. The trigger's Config["topic"] is
// required; Filter (if any) is matched against Event.Attributes on every
// Publish.
func (d *Driver) Register(t api.Trigger, agentID string, h trigger.Handler) (trigger.Registration, error) {
	if t.Type != api.TriggerEvent {
		return trigger.Registration{}, fmt.Errorf("event: unsupported trigger type %q", t.Type)
	}
	topic := t.Config["topic"]
	if topic == "" {
		return trigger.Registration{}, fmt.Errorf("event: trigger %q missing config[\"topic\"]", t.ID)
	}
	reg := trigger.Registration{Trigger: t, AgentID: agentID, Handler: h}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.byTopic[topic] = append(d.byTopic[topic], reg)
	d.logger("event: registered %s on topic %q", t.ID, topic)
	return reg, nil
}

// Deregister removes a previously-registered trigger by ID. Returns
// false when no trigger with that ID is registered.
func (d *Driver) Deregister(triggerID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for topic, regs := range d.byTopic {
		for i, r := range regs {
			if r.Trigger.ID == triggerID {
				d.byTopic[topic] = append(regs[:i], regs[i+1:]...)
				if len(d.byTopic[topic]) == 0 {
					delete(d.byTopic, topic)
				}
				d.logger("event: removed %s", triggerID)
				return true
			}
		}
	}
	return false
}

// List returns a snapshot of currently-registered triggers.
func (d *Driver) List() []trigger.Registration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []trigger.Registration
	for _, regs := range d.byTopic {
		out = append(out, regs...)
	}
	return out
}

// Publish dispatches an event to every trigger registered on its topic
// whose Filter matches the event's Attributes. Handlers run in series
// in the calling goroutine; callers that need fan-out parallelism
// should wrap with their own worker pool. Returns the first handler
// error encountered, if any.
func (d *Driver) Publish(ctx context.Context, e Event) error {
	d.mu.RLock()
	regs := append([]trigger.Registration(nil), d.byTopic[e.Topic]...)
	d.mu.RUnlock()
	for _, r := range regs {
		if !matchFilter(r.Trigger.Filter, e.Attributes) {
			continue
		}
		tc := trigger.TriggerContext{
			Trigger:    r.Trigger,
			AgentID:    r.AgentID,
			FiredAt:    nonZeroTime(e.OccurredAt),
			Source:     e.Topic,
			Payload:    e.Payload,
			Attributes: e.Attributes,
		}
		if err := r.Handler.Handle(ctx, tc); err != nil {
			d.logger("event: trigger %s handler failed: %v", r.Trigger.ID, err)
			return err
		}
	}
	return nil
}

func matchFilter(filter, attrs map[string]string) bool {
	if len(filter) == 0 {
		return true
	}
	for k, v := range filter {
		if attrs[k] != v {
			return false
		}
	}
	return true
}

func nonZeroTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}
