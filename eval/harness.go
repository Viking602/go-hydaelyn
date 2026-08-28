// Package eval is the v0.8.0 evaluation framework. It makes an agent run
// testable by *executing* it through the public Runner façade against a
// deterministic provider/scripted model, then grading the resulting
// api.Run with a typed assertion vocabulary.
//
// The framework owns four pieces:
//
//   - EvalCase: a single named scenario carrying an api.StartRunCommand input
//     and a list of Assertions, plus a Setup hook that returns a Harness.
//   - Harness: the execution environment for a case. It owns the *venat.Runner,
//     registers agents, and is torn down via Cleanup. The default implementation
//     wires a scripted provider into the agent loop through the worker bridge.
//   - Assertion: a typed predicate over the executed api.Run plus the Harness it
//     ran in. Concrete assertions ship in eval/assertions.
//   - EvalResult: the typed verdict (pass/fail) plus per-assertion failures,
//     duration, and a UsageSummary computed from []api.UsageRecord.
//
// Run(t, c) executes one case under go test; RunSuite(t, cases) runs many.
// The internal runCase(ctx, c) core is testing-agnostic so future pack
// self-checks can reuse it without welding eval/ to the testing package.
package eval

import (
	"github.com/Viking602/venat"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/provider"
)

// Harness is the execution environment a single EvalCase runs in. Setup
// constructs one; the framework drives the run through Runner() and tears
// the harness down via Cleanup after the case finishes.
type Harness interface {
	// Runner returns the live runner the case executes against.
	Runner() *venat.Runner
	// RegisterAgent registers an agent profile with the runner so the run
	// can dispatch tasks to it.
	RegisterAgent(profile api.AgentProfile) error
	// Cleanup releases any resources held by the harness. Safe to call once
	// per harness; the framework always calls it after the case completes.
	Cleanup()
	// EmbeddingProvider returns the embedding provider used by the
	// EmbeddingSimilarity matcher, or nil when none is configured. Only the
	// EmbeddingSimilarity matcher requires one; every other assertion ignores it.
	EmbeddingProvider() EmbeddingProvider
}

// EmbeddingProvider turns text into a vector for the EmbeddingSimilarity
// matcher. The framework ships only the comparator; applications inject a
// concrete provider via the Harness. The matcher itself lands in M2.
type EmbeddingProvider interface {
	// Embed returns the embedding vector for text.
	Embed(text string) ([]float64, error)
}

// DefaultHarness is the framework's reference Harness. It wires a deterministic
// provider/scripted model into the agent loop through the worker bridge, exactly
// as the M0 spike proved. Construct it with NewHarness; the zero value is not
// usable.
type DefaultHarness struct {
	runner    *venat.Runner
	agentID   string
	model     string
	script    []provider.Event
	embedding EmbeddingProvider
}

// HarnessOption customizes a DefaultHarness built by NewHarness.
type HarnessOption func(*DefaultHarness)

// WithScript sets the deterministic scripted-provider event stream the agent
// loop runs against. When unset, NewHarness uses a single text-delta + done
// stream that completes immediately.
func WithScript(events []provider.Event) HarnessOption {
	return func(h *DefaultHarness) { h.script = events }
}

// WithAgentID sets the id of the agent the case dispatches its task to.
// Defaults to "agent".
func WithAgentID(id string) HarnessOption {
	return func(h *DefaultHarness) { h.agentID = id }
}

// WithModel sets the model name recorded by the worker bridge. Defaults to
// "scripted".
func WithModel(model string) HarnessOption {
	return func(h *DefaultHarness) { h.model = model }
}

// WithEmbeddingProvider injects the embedding provider returned by
// EmbeddingProvider(). Defaults to nil.
func WithEmbeddingProvider(p EmbeddingProvider) HarnessOption {
	return func(h *DefaultHarness) { h.embedding = p }
}

// NewHarness constructs a DefaultHarness backed by a fresh in-memory runner and
// a deterministic scripted provider. The single registered agent (id from
// WithAgentID, default "agent") owns the case's task; the run is driven to a
// terminal api.RunStatus by runCase. It panics when an option produces an
// invalid initial agent because EvalCase.Setup cannot return an error.
func NewHarness(opts ...HarnessOption) *DefaultHarness {
	h := &DefaultHarness{
		runner:  venat.NewDevelopment(),
		agentID: "agent",
		model:   "scripted",
		script: []provider.Event{
			{Kind: provider.EventTextDelta, Text: "ok"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		},
	}
	for _, opt := range opts {
		opt(h)
	}
	if err := h.runner.RegisterAgent(api.AgentProfile{ID: h.agentID}); err != nil {
		panic(err)
	}
	return h
}

// Runner returns the live runner.
func (h *DefaultHarness) Runner() *venat.Runner { return h.runner }

// RegisterAgent registers an additional agent profile with the runner.
func (h *DefaultHarness) RegisterAgent(profile api.AgentProfile) error {
	return h.runner.RegisterAgent(profile)
}

// Cleanup is a no-op for the in-memory default harness; the runner and its
// stores are garbage-collected once the harness goes out of scope.
func (h *DefaultHarness) Cleanup() {}

// EmbeddingProvider returns the injected embedding provider, or nil.
func (h *DefaultHarness) EmbeddingProvider() EmbeddingProvider { return h.embedding }

// AgentID returns the id of the agent the case's task is dispatched to.
func (h *DefaultHarness) AgentID() string { return h.agentID }

// Model returns the model name recorded by the worker bridge.
func (h *DefaultHarness) Model() string { return h.model }

// Script returns the deterministic scripted-provider event stream the agent
// loop runs against.
func (h *DefaultHarness) Script() []provider.Event { return h.script }
