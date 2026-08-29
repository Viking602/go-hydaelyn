package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/orchestration"
	"github.com/Viking602/venat/provider"
)

func TestAgentCatalog_ValidatesAndResolvesImmutableBindings(t *testing.T) {
	engine := agent.Engine{Provider: deterministicProvider{}, Model: "model"}
	var typedNil *deterministicProvider
	for name, entries := range map[string][]CatalogEntry{
		"empty route": {{Binding: AgentBinding{ID: "agent-v1", Engine: engine}}},
		"empty ID":    {{Route: "route", Binding: AgentBinding{Engine: engine}}},
		"nil engine":  {{Route: "route", Binding: AgentBinding{ID: "agent-v1"}}},
		"typed nil engine": {{
			Route:   "route",
			Binding: AgentBinding{ID: "agent-v1", Engine: agent.Engine{Provider: typedNil}},
		}},
		"duplicate route": {
			{Route: "route", Binding: AgentBinding{ID: "agent-v1", Engine: engine}},
			{Route: "route", Binding: AgentBinding{ID: "agent-v2", Engine: engine}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewAgentCatalog(entries...); !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("NewAgentCatalog() error = %v, want ErrInvalidCatalog", err)
			}
		})
	}

	entries := []CatalogEntry{{Route: "summarize", Binding: AgentBinding{ID: "summarizer-v1", Engine: engine}}}
	catalog, err := NewAgentCatalog(entries...)
	if err != nil {
		t.Fatalf("NewAgentCatalog() error = %v", err)
	}
	entries[0].Binding.ID = "changed"
	binding, err := catalog.Resolve("summarize")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if binding.ID != "summarizer-v1" {
		t.Fatalf("resolved binding ID = %q, want immutable summarizer-v1", binding.ID)
	}
	if _, err := catalog.Resolve("missing"); !errors.Is(err, ErrUnknownRoute) {
		t.Fatalf("Resolve(missing) error = %v, want ErrUnknownRoute", err)
	}
}

func TestCatalogExecutor_RunsRealEnginesConcurrentlyAndFoldsStably(t *testing.T) {
	engine, err := newSummarizerEngine()
	if err != nil {
		t.Fatalf("newSummarizerEngine() error = %v", err)
	}
	catalog, err := NewAgentCatalog(CatalogEntry{Route: "summarize", Binding: AgentBinding{ID: "summarizer-v1", Engine: engine}})
	if err != nil {
		t.Fatalf("NewAgentCatalog() error = %v", err)
	}
	scheduler := orchestration.SchedulerFunc(func(_ context.Context, state orchestration.State) ([]orchestration.Dispatch, error) {
		if state.Tick != 0 {
			return nil, nil
		}
		return []orchestration.Dispatch{
			{ID: "dispatch-b", Route: "summarize", Request: agent.Request{Prompt: "second"}},
			{ID: "dispatch-a", Route: "summarize", Request: agent.Request{Prompt: "first"}},
		}, nil
	})
	state, err := orchestration.Drive(context.Background(), scheduler, catalogExecutor{catalog: catalog}, orchestration.DriveOptions{MaxTicks: 1, MaxConcurrency: 2})
	if err != nil {
		t.Fatalf("Drive() error = %v", err)
	}
	if got := []string{state.Outcomes[0].Dispatch.ID, state.Outcomes[1].Dispatch.ID}; !reflect.DeepEqual(got, []string{"dispatch-a", "dispatch-b"}) {
		t.Fatalf("outcome order = %v", got)
	}
	if state.Outcomes[0].Result.Text != "FIRST" || state.Outcomes[1].Result.Text != "SECOND" {
		t.Fatalf("outcomes = %#v", state.Outcomes)
	}
	for _, outcome := range state.Outcomes {
		if outcome.Result.Failure != nil || len(outcome.Result.Steps) != 2 || len(outcome.Result.Steps[0].ToolCalls) != 1 {
			t.Fatalf("dispatch %q did not run model-to-tool-to-output: %#v", outcome.Dispatch.ID, outcome.Result)
		}
	}
}

func TestExecutionEventSink_ProjectsHierarchyAndSerialDelivery(t *testing.T) {
	engine, err := newSummarizerEngine()
	if err != nil {
		t.Fatalf("newSummarizerEngine() error = %v", err)
	}
	catalog, err := NewAgentCatalog(CatalogEntry{Route: "summarize", Binding: AgentBinding{ID: "summarizer-v1", Engine: engine}})
	if err != nil {
		t.Fatalf("NewAgentCatalog() error = %v", err)
	}
	events := newExecutionEventSink()
	scheduler := orchestration.SchedulerFunc(func(_ context.Context, state orchestration.State) ([]orchestration.Dispatch, error) {
		switch state.Tick {
		case 0:
			return []orchestration.Dispatch{
				{ID: "root-b", Route: "summarize", Request: agent.Request{Prompt: "second"}},
				{ID: "root-a", Route: "summarize", Request: agent.Request{Prompt: "first"}},
			}, nil
		case 1:
			return []orchestration.Dispatch{{
				ID:       "child",
				Route:    "summarize",
				Request:  agent.Request{Prompt: "child"},
				Metadata: map[string]string{parentDispatchMetadata: "root-a"},
				Handoff:  &orchestration.Handoff{From: "root-b", To: "summarize"},
			}}, nil
		default:
			return nil, nil
		}
	})
	if _, err := orchestration.Drive(
		context.Background(),
		scheduler,
		catalogExecutor{catalog: catalog, events: events},
		orchestration.DriveOptions{MaxTicks: 2, MaxConcurrency: 2, Sink: events},
	); err != nil {
		t.Fatalf("Drive() error = %v", err)
	}

	projected := events.Events()
	if len(projected) == 0 {
		t.Fatal("no execution events projected")
	}
	seen := map[string]bool{}
	for index, event := range projected {
		if event.Sequence != uint64(index+1) {
			t.Fatalf("event %d sequence = %d, want %d", index, event.Sequence, index+1)
		}
		if event.DispatchID != event.Frame.Source || len(event.Path) == 0 || event.Path[len(event.Path)-1] != event.DispatchID {
			t.Fatalf("event invariant failed: %#v", event)
		}
		if event.AgentID != "summarizer-v1" || event.Route != "summarize" {
			t.Fatalf("event annotation = agent %q route %q", event.AgentID, event.Route)
		}
		seen[event.DispatchID] = true
		switch event.DispatchID {
		case "root-a", "root-b":
			if !reflect.DeepEqual(event.Path, []string{event.DispatchID}) {
				t.Fatalf("root path = %v", event.Path)
			}
		case "child":
			if !reflect.DeepEqual(event.Path, []string{"root-a", "child"}) {
				t.Fatalf("child path = %v; Handoff.From must not define ancestry", event.Path)
			}
		default:
			t.Fatalf("unexpected dispatch ID %q", event.DispatchID)
		}
	}
	for _, dispatchID := range []string{"root-a", "root-b", "child"} {
		if !seen[dispatchID] {
			t.Fatalf("no events for %q", dispatchID)
		}
	}
}

func TestExecutionEventSink_ClonesFramesAndRejectsMissingParents(t *testing.T) {
	engine := agent.Engine{Provider: deterministicProvider{}, Model: "model"}
	binding := AgentBinding{ID: "summarizer-v1", Engine: engine}
	events := newExecutionEventSink()
	if err := events.Register(orchestration.Dispatch{ID: "root", Route: "summarize"}, binding); err != nil {
		t.Fatalf("Register(root) error = %v", err)
	}
	arguments := json.RawMessage(`{"query":"before"}`)
	providerState := json.RawMessage(`{"cursor":"before"}`)
	frame := agent.Frame{
		Source:        "root",
		Kind:          agent.FrameToolCall,
		ToolCall:      &message.ToolCall{ID: "call-1", Name: "lookup", Arguments: arguments},
		ProviderState: providerState,
	}
	if err := events.Emit(context.Background(), frame); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
	frame.ToolCall.Arguments[0] = '['
	frame.ProviderState[0] = '['
	first := events.Events()
	if string(first[0].Frame.ToolCall.Arguments) != `{"query":"before"}` || string(first[0].Frame.ProviderState) != `{"cursor":"before"}` {
		t.Fatalf("stored frame aliased caller payload: %#v", first[0].Frame)
	}
	first[0].Path[0] = "changed"
	first[0].Frame.ToolCall.Arguments[0] = '['
	second := events.Events()
	if !reflect.DeepEqual(second[0].Path, []string{"root"}) || string(second[0].Frame.ToolCall.Arguments) != `{"query":"before"}` {
		t.Fatalf("Events() returned aliased payload: %#v", second[0])
	}

	missingParent := newExecutionEventSink()
	if err := missingParent.Register(orchestration.Dispatch{
		ID:       "child",
		Route:    "summarize",
		Metadata: map[string]string{parentDispatchMetadata: "missing"},
	}, binding); err != nil {
		t.Fatalf("Register(child) error = %v", err)
	}
	if err := missingParent.Emit(context.Background(), agent.Frame{Source: "child", Kind: agent.FrameDone}); !errors.Is(err, ErrEventProjection) {
		t.Fatalf("Emit(missing parent) error = %v, want ErrEventProjection", err)
	}
	if len(missingParent.Events()) != 0 {
		t.Fatal("missing-parent frame was recorded")
	}
}

type failingProvider struct{}

func (failingProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "failing"} }
func (failingProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return nil, errors.New("model unavailable")
}

func TestCatalogExecutor_KeepsAgentFailureAsOutcomeData(t *testing.T) {
	catalog, err := NewAgentCatalog(CatalogEntry{
		Route: "fail",
		Binding: AgentBinding{
			ID:     "failure-v1",
			Engine: agent.Engine{Provider: failingProvider{}, Model: "model"},
		},
	})
	if err != nil {
		t.Fatalf("NewAgentCatalog() error = %v", err)
	}
	scheduler := orchestration.SchedulerFunc(func(_ context.Context, state orchestration.State) ([]orchestration.Dispatch, error) {
		if state.Tick == 0 {
			return []orchestration.Dispatch{{ID: "failure", Route: "fail", Request: agent.Request{Prompt: "fail"}}}, nil
		}
		return nil, nil
	})
	state, err := orchestration.Drive(context.Background(), scheduler, catalogExecutor{catalog: catalog}, orchestration.DriveOptions{MaxTicks: 1})
	if err != nil {
		t.Fatalf("Drive() infrastructure error = %v", err)
	}
	if len(state.Outcomes) != 1 || state.Outcomes[0].Result.Failure == nil {
		t.Fatalf("state = %#v, want Agent failure as outcome data", state)
	}
}

type countingProvider struct {
	calls atomic.Int64
}

func (*countingProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "counting"} }
func (driver *countingProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	driver.calls.Add(1)
	return provider.NewSliceStream(nil), nil
}

func TestCatalogExecutor_UnknownRouteIsInfrastructureErrorBeforeEffect(t *testing.T) {
	driver := &countingProvider{}
	catalog, err := NewAgentCatalog(CatalogEntry{
		Route:   "known",
		Binding: AgentBinding{ID: "known-v1", Engine: agent.Engine{Provider: driver, Model: "model"}},
	})
	if err != nil {
		t.Fatalf("NewAgentCatalog() error = %v", err)
	}
	scheduler := orchestration.SchedulerFunc(func(context.Context, orchestration.State) ([]orchestration.Dispatch, error) {
		return []orchestration.Dispatch{{ID: "unknown-dispatch", Route: "missing", Request: agent.Request{Prompt: "never run"}}}, nil
	})
	state, err := orchestration.Drive(context.Background(), scheduler, catalogExecutor{catalog: catalog}, orchestration.DriveOptions{MaxTicks: 1})
	var dispatchFailure *orchestration.DispatchError
	var routeFailure *RouteError
	if !errors.Is(err, ErrUnknownRoute) || !errors.As(err, &dispatchFailure) || !errors.As(err, &routeFailure) {
		t.Fatalf("Drive() error = %v, want DispatchError wrapping RouteError", err)
	}
	if routeFailure.DispatchID != "unknown-dispatch" || routeFailure.Route != "missing" {
		t.Fatalf("RouteError = %#v", routeFailure)
	}
	if !reflect.DeepEqual(dispatchFailure.Result, agent.Result{}) || len(state.Outcomes) != 0 || driver.calls.Load() != 0 {
		t.Fatalf("state = %#v, partial result = %#v, provider calls = %d", state, dispatchFailure.Result, driver.calls.Load())
	}
}
