// Package trigger declares the small set of types shared across every
// transport driver under transport/* (scheduler, webhook, event).
//
// A Driver in this codebase is anything that watches an external signal
// and turns it into a Runner command. The shared shape is:
//
//	Driver --reads--> Trigger config
//	Driver --calls--> Handler with TriggerContext
//	Handler --emits--> api.Command via Runner.ExecuteCommand
//
// Drivers live in sibling packages so the heavy dependency (robfig/cron,
// net/http multiplexers, message-bus clients) stays out of the framework
// core. transport/trigger itself depends only on api.
package trigger

import (
	"context"
	"maps"
	"time"

	"github.com/Viking602/venat/api"
)

// TriggerContext is the value handed to a Handler each time a Trigger
// fires. Drivers fill the fields they know about; consumers should
// tolerate missing fields the same way an HTTP handler tolerates a
// missing optional header.
type TriggerContext struct {
	// Trigger is the declarative spec that produced this firing.
	Trigger api.Trigger

	// AgentID is the agent the trigger is bound to. Drivers learn it from
	// the AgentDefinition that registered the trigger.
	AgentID string

	// FiredAt is the wallclock time the driver saw the firing condition.
	// Use this for replay determinism rather than time.Now() inside the
	// handler.
	FiredAt time.Time

	// Source is a driver-specific string describing where the firing
	// came from — a cron expression, an HTTP path, a topic name, etc.
	Source string

	// Payload is the optional body that accompanied the firing: an HTTP
	// request body, an event-bus message, scheduler-supplied metadata.
	// Drivers SHOULD pass the raw bytes through; handlers decide how to
	// decode.
	Payload []byte

	// Attributes are driver-supplied key/value pairs (e.g. HTTP headers,
	// event-bus metadata, scheduler tags) the handler may inspect.
	Attributes map[string]string
}

// Handler turns a TriggerContext into one or more Runner commands. The
// most common implementation issues a StartRunCommand with the trigger's
// agent and payload mapped into the request body.
type Handler interface {
	Handle(ctx context.Context, t TriggerContext) error
}

// HandlerFunc adapts an ordinary function into a Handler.
type HandlerFunc func(ctx context.Context, t TriggerContext) error

// Handle satisfies Handler.
func (f HandlerFunc) Handle(ctx context.Context, t TriggerContext) error { return f(ctx, t) }

// Registration is the row a Driver keeps in its in-memory table while
// running. Each Registration ties one Trigger to the Handler that should
// fire when it does. Drivers expose Register / Deregister methods that
// return Registration values; tests and admin APIs use them to introspect
// the live wiring.
type Registration struct {
	Trigger api.Trigger
	AgentID string
	Handler Handler
}

// Clone returns a registration whose mutable trigger maps do not alias the
// live registration or its caller.
func (r Registration) Clone() Registration {
	r.Trigger.Config = maps.Clone(r.Trigger.Config)
	r.Trigger.Filter = maps.Clone(r.Trigger.Filter)
	return r
}
