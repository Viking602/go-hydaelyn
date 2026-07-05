// Package cron is the cron transport driver for api.TriggerSchedule.
// It wraps robfig/cron/v3 and exposes a small Register/Start/Stop API so
// callers can install schedule-based triggers without owning the cron
// library directly.
//
// Trigger configuration:
//
//	Trigger.Type = api.TriggerSchedule
//	Trigger.Config["cron"] = "0 */15 * * * *"   // 6-field robfig syntax
//	Trigger.Config["timezone"] = "America/Los_Angeles" // optional
//
// On each firing, the registered Handler receives a TriggerContext whose
// Source field is the cron expression. Drivers do not pass a payload —
// cron-driven runs typically read their input from the agent's default
// context sources or a metadata template stored on Trigger.Config.
package cron

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/transport/trigger"

	"github.com/robfig/cron/v3"
)

// Driver installs api.TriggerSchedule firings on top of a robfig/cron
// scheduler. It is safe for concurrent Register/Deregister calls.
//
// Handler invocations run on a driver-level context cancelled by Stop,
// wrapped in a recover so a panicking handler is logged rather than
// crashing the cron worker goroutine (robfig/cron v3 does not recover
// job panics). An optional per-firing HandlerTimeout bounds each call.
type Driver struct {
	mu             sync.Mutex
	cron           *cron.Cron
	jobs           map[string]cron.EntryID
	regs           map[string]trigger.Registration
	logger         func(format string, args ...any)
	handlerTimeout time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
}

// Options configures Driver construction.
type Options struct {
	// Logger, if set, receives a formatted line each time the driver
	// adds, removes, or fails to fire a job. Defaults to discard so the
	// driver is silent by default.
	Logger func(format string, args ...any)

	// DefaultLocation supplies the timezone used when a Trigger does not
	// declare one. Nil falls back to time.Local.
	DefaultLocation *time.Location

	// HandlerTimeout bounds each firing's handler call. Zero means no
	// per-firing timeout — the handler runs under the driver-level
	// context, which is cancelled only by Stop. A positive value wraps
	// each call in context.WithTimeout so a stuck handler cannot hold
	// the cron worker indefinitely.
	HandlerTimeout time.Duration
}

// New constructs a Driver with the given options. Call Start before
// registering triggers if you want them to fire immediately; otherwise
// Register/Deregister are valid pre-Start (jobs queue and fire when
// Start runs).
func New(opts Options) *Driver {
	loc := opts.DefaultLocation
	if loc == nil {
		loc = time.Local
	}
	logger := opts.Logger
	if logger == nil {
		logger = func(format string, args ...any) {}
	}
	dctx, cancel := context.WithCancel(context.Background())
	return &Driver{
		cron:           cron.New(cron.WithSeconds(), cron.WithLocation(loc)),
		jobs:           map[string]cron.EntryID{},
		regs:           map[string]trigger.Registration{},
		logger:         logger,
		handlerTimeout: opts.HandlerTimeout,
		ctx:            dctx,
		cancel:         cancel,
	}
}

// Start begins firing scheduled triggers. Safe to call multiple times;
// subsequent calls are no-ops.
func (d *Driver) Start() { d.cron.Start() }

// Stop drains in-flight firings and stops the scheduler. Returns when
// Stop cancels the driver-level handler context, drains in-flight
// firings, and stops the scheduler. Handlers observe ctx cancellation
// (the driver-level context passed to each firing is cancelled here),
// so a well-behaved handler returns promptly. Returns when all
// currently-running handlers complete or ctx cancels, whichever comes
// first.
func (d *Driver) Stop(ctx context.Context) error {
	d.cancel()
	stopCtx := d.cron.Stop()
	select {
	case <-stopCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Register installs a Trigger of type api.TriggerSchedule. Returns the
// Registration the driver stored so callers can route admin/list APIs
// against it. The Trigger.ID must be unique among registered triggers;
// a duplicate ID returns an error.
func (d *Driver) Register(t api.Trigger, agentID string, h trigger.Handler) (trigger.Registration, error) {
	if t.Type != api.TriggerSchedule {
		return trigger.Registration{}, fmt.Errorf("scheduler: unsupported trigger type %q", t.Type)
	}
	if t.ID == "" {
		return trigger.Registration{}, fmt.Errorf("scheduler: trigger ID required")
	}
	spec := strings.TrimSpace(t.Config["cron"])
	if spec == "" {
		return trigger.Registration{}, fmt.Errorf("scheduler: trigger %q missing config[\"cron\"]", t.ID)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if _, dup := d.jobs[t.ID]; dup {
		return trigger.Registration{}, fmt.Errorf("scheduler: trigger %q already registered", t.ID)
	}
	scheduledSpec, err := scheduleSpec(spec, t.Config)
	if err != nil {
		return trigger.Registration{}, fmt.Errorf("scheduler: trigger %q %w", t.ID, err)
	}
	reg := trigger.Registration{Trigger: t, AgentID: agentID, Handler: h}
	id, err := d.cron.AddFunc(scheduledSpec, func() {
		d.fire(t, agentID, spec, h)
	})
	if err != nil {
		return trigger.Registration{}, fmt.Errorf("scheduler: cron parse %q: %w", scheduledSpec, err)
	}
	d.jobs[t.ID] = id
	d.regs[t.ID] = reg
	d.logger("scheduler: registered %s with spec %q", t.ID, scheduledSpec)
	return reg, nil
}

func scheduleSpec(spec string, config map[string]string) (string, error) {
	zone := strings.TrimSpace(config["timezone"])
	if zone == "" {
		return spec, nil
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return "", fmt.Errorf("invalid timezone %q: %w", zone, err)
	}
	return "CRON_TZ=" + zone + " " + spec, nil
}

// fire runs one trigger firing under the driver-level context with
// panic recovery and an optional per-firing timeout. robfig/cron v3
// does not recover job panics, so without this wrapper a panicking
// handler crashes the cron worker goroutine (and the process). The
// recover logs the panic and lets the cron loop keep scheduling.
func (d *Driver) fire(t api.Trigger, agentID, spec string, h trigger.Handler) {
	defer func() {
		if r := recover(); r != nil {
			d.logger("scheduler: trigger %s panicked: %v", t.ID, r)
		}
	}()
	ctx := d.ctx
	if d.handlerTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.handlerTimeout)
		defer cancel()
	}
	tc := trigger.TriggerContext{
		Trigger:    t,
		AgentID:    agentID,
		FiredAt:    time.Now().UTC(),
		Source:     spec,
		Attributes: t.Config,
	}
	if err := h.Handle(ctx, tc); err != nil {
		d.logger("scheduler: trigger %s firing failed: %v", t.ID, err)
	}
}

// Deregister removes a previously-registered trigger by ID. Returns
// false when no trigger with that ID is registered.
func (d *Driver) Deregister(triggerID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	id, ok := d.jobs[triggerID]
	if !ok {
		return false
	}
	d.cron.Remove(id)
	delete(d.jobs, triggerID)
	delete(d.regs, triggerID)
	d.logger("scheduler: removed %s", triggerID)
	return true
}

// List returns a snapshot of currently-registered triggers. Useful for
// admin endpoints and CLI introspection.
func (d *Driver) List() []trigger.Registration {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]trigger.Registration, 0, len(d.regs))
	for _, r := range d.regs {
		out = append(out, r)
	}
	return out
}
