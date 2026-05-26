// Package webhook is the HTTP transport driver for api.TriggerWebhook.
// It produces an http.Handler that callers mount into their own
// router/mux — the driver does not start a server itself so it can sit
// behind authenticated middleware and share a port with the rest of the
// application.
//
// Trigger configuration:
//
//	Trigger.Type = api.TriggerWebhook
//	Trigger.Config["path"]   = "/hooks/incidents"
//	Trigger.Config["method"] = "POST"           // optional; defaults to POST
//	Trigger.Config["secret"] = "env:HOOK_TOKEN" // optional; see VerifyToken below
//
// On each matching request the registered Handler receives a
// TriggerContext whose Source is the URL path, Payload is the raw body
// (capped at MaxBodyBytes), and Attributes are the request headers
// flattened to lowercase key form.
package webhook

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/transport/trigger"
)

const defaultMaxBodyBytes = 1 << 20 // 1 MiB

// Driver routes inbound HTTP requests to registered webhook triggers.
// The zero value is unusable; construct via New.
type Driver struct {
	mu     sync.RWMutex
	routes map[routeKey]trigger.Registration
	logger func(format string, args ...any)
	max    int64
	// verifyToken, when non-nil, runs against every incoming request
	// before the trigger handler. Returning false short-circuits with a
	// 401. Use it to validate webhook signatures or shared secrets.
	verifyToken func(r *http.Request, expected string) bool
}

// Options configures Driver construction.
type Options struct {
	Logger       func(format string, args ...any)
	MaxBodyBytes int64
	// VerifyToken receives the inbound request and the value of
	// Trigger.Config["secret"] for the matched route. Returning false
	// causes the driver to respond with 401 and skip the handler. A nil
	// VerifyToken disables verification entirely — fine for in-cluster
	// triggers behind a service mesh, dangerous on the open internet.
	VerifyToken func(r *http.Request, expected string) bool
}

// New constructs a Driver. Mount Driver.Handler into your http.ServeMux
// or chi/router to start accepting webhook firings.
func New(opts Options) *Driver {
	logger := opts.Logger
	if logger == nil {
		logger = func(format string, args ...any) {}
	}
	maxBytes := opts.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBodyBytes
	}
	return &Driver{
		routes:      map[routeKey]trigger.Registration{},
		logger:      logger,
		max:         maxBytes,
		verifyToken: opts.VerifyToken,
	}
}

// Register adds a webhook trigger. Returns an error if path is missing,
// the method is malformed, or a (method, path) pair is already
// registered.
func (d *Driver) Register(t api.Trigger, agentID string, h trigger.Handler) (trigger.Registration, error) {
	if t.Type != api.TriggerWebhook {
		return trigger.Registration{}, fmt.Errorf("webhook: unsupported trigger type %q", t.Type)
	}
	path := t.Config["path"]
	if path == "" {
		return trigger.Registration{}, fmt.Errorf("webhook: trigger %q missing config[\"path\"]", t.ID)
	}
	if t.Config["secret"] != "" && d.verifyToken == nil {
		return trigger.Registration{}, fmt.Errorf("webhook: trigger %q configures secret without verifier", t.ID)
	}
	method := strings.ToUpper(strings.TrimSpace(t.Config["method"]))
	if method == "" {
		method = http.MethodPost
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	key := routeKey{Method: method, Path: path}
	if _, dup := d.routes[key]; dup {
		return trigger.Registration{}, fmt.Errorf("webhook: %s %s already registered", method, path)
	}
	reg := trigger.Registration{Trigger: t, AgentID: agentID, Handler: h}
	d.routes[key] = reg
	d.logger("webhook: registered %s %s -> %s", method, path, t.ID)
	return reg, nil
}

// Deregister removes a webhook trigger. Returns false when no matching
// route is registered.
func (d *Driver) Deregister(triggerID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, r := range d.routes {
		if r.Trigger.ID == triggerID {
			delete(d.routes, k)
			d.logger("webhook: removed %s", triggerID)
			return true
		}
	}
	return false
}

// List returns a snapshot of registered webhooks.
func (d *Driver) List() []trigger.Registration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]trigger.Registration, 0, len(d.routes))
	for _, r := range d.routes {
		out = append(out, r)
	}
	return out
}

// Handler returns the http.Handler callers mount into their server.
// The handler runs through the registered routes table and dispatches
// to the matching trigger's Handler with the request body and headers.
func (d *Driver) Handler() http.Handler {
	return http.HandlerFunc(d.serve)
}

func (d *Driver) serve(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	reg, ok := d.routes[routeKey{Method: r.Method, Path: r.URL.Path}]
	d.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if secret := reg.Trigger.Config["secret"]; secret != "" {
		if d.verifyToken == nil {
			http.Error(w, "webhook verifier not configured", http.StatusInternalServerError)
			return
		}
		if !d.verifyToken(r, secret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, d.max+1))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	if int64(len(body)) > d.max {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	attrs := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		if len(v) > 0 {
			attrs[strings.ToLower(k)] = v[0]
		}
	}
	tc := trigger.TriggerContext{
		Trigger:    reg.Trigger,
		AgentID:    reg.AgentID,
		FiredAt:    time.Now().UTC(),
		Source:     r.URL.Path,
		Payload:    body,
		Attributes: attrs,
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := reg.Handler.Handle(ctx, tc); err != nil {
		d.logger("webhook: trigger %s handler failed: %v", reg.Trigger.ID, err)
		http.Error(w, "handler failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type routeKey struct {
	Method string
	Path   string
}
