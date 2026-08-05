package trigger

import (
	"errors"
	"fmt"
	"sync"

	"github.com/Viking602/venat/api"
)

// ErrRegistrarMissing reports an enabled trigger whose transport has no live
// registrar in the deployment.
var ErrRegistrarMissing = errors.New("trigger: registrar missing")

// Registrar is the common registration surface implemented by schedule,
// webhook, and event transport drivers.
type Registrar interface {
	Register(api.Trigger, string, Handler) (Registration, error)
	Deregister(triggerID string) bool
}

// Registrars maps each trigger type to the transport instance that owns it.
// TriggerManual needs no registrar and is intentionally skipped.
type Registrars map[api.TriggerType]Registrar

// Register installs every enabled non-manual trigger in definition. A partial
// failure removes earlier registrations before returning.
func (r Registrars) Register(definition api.AgentDefinition, handler Handler) (*Lifecycle, error) {
	lifecycle := &Lifecycle{}
	for _, configured := range definition.Triggers {
		configured = (Registration{Trigger: configured}).Clone().Trigger
		if !configured.Enabled || configured.Type == api.TriggerManual {
			continue
		}
		if handler == nil {
			lifecycle.close()
			return nil, fmt.Errorf("trigger: handler required")
		}
		registrar := r[configured.Type]
		if registrar == nil {
			lifecycle.close()
			return nil, fmt.Errorf("%w for type %q", ErrRegistrarMissing, configured.Type)
		}
		registration, err := registrar.Register(configured, definition.ID, handler)
		if err != nil {
			lifecycle.close()
			return nil, fmt.Errorf("trigger: register %q: %w", configured.ID, err)
		}
		lifecycle.bound = append(lifecycle.bound, boundRegistration{
			registrar:    registrar,
			registration: registration,
		})
	}
	return lifecycle, nil
}

type boundRegistration struct {
	registrar    Registrar
	registration Registration
}

// Lifecycle owns one definition's live trigger registrations. Close is
// idempotent and removes registrations in reverse installation order.
type Lifecycle struct {
	mu     sync.Mutex
	bound  []boundRegistration
	closed bool
}

// Registrations returns the live registration snapshot in installation order.
func (l *Lifecycle) Registrations() []Registration {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Registration, 0, len(l.bound))
	for _, current := range l.bound {
		out = append(out, current.registration.Clone())
	}
	return out
}

// Close deregisters every trigger owned by this lifecycle. Repeated calls are
// no-ops.
func (l *Lifecycle) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.close()
	return nil
}

func (l *Lifecycle) close() {
	if l.closed {
		return
	}
	for index := len(l.bound) - 1; index >= 0; index-- {
		current := l.bound[index]
		current.registrar.Deregister(current.registration.Trigger.ID)
	}
	l.bound = nil
	l.closed = true
}
