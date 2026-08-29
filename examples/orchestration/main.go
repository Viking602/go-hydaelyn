package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/orchestration"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

var (
	ErrInvalidCatalog  = errors.New("example: invalid agent catalog")
	ErrUnknownRoute    = errors.New("example: unknown agent route")
	ErrEventProjection = errors.New("example: invalid execution event projection")
)

type AgentID string

type ExecutionEvent struct {
	Sequence   uint64
	Path       []string
	AgentID    AgentID
	DispatchID string
	Route      string
	Frame      agent.Frame
}

type AgentBinding struct {
	ID     AgentID
	Engine agent.Engine
}

type CatalogEntry struct {
	Route   string
	Binding AgentBinding
}

// AgentCatalog is an application-owned immutable route map. Agent identity and
// code revision remain outside the Engine and orchestration contracts.
type AgentCatalog struct {
	bindings map[string]AgentBinding
}

func NewAgentCatalog(entries ...CatalogEntry) (AgentCatalog, error) {
	bindings := make(map[string]AgentBinding, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Route) == "" {
			return AgentCatalog{}, fmt.Errorf("%w: route is empty", ErrInvalidCatalog)
		}
		if strings.TrimSpace(string(entry.Binding.ID)) == "" {
			return AgentCatalog{}, fmt.Errorf("%w: route %q has an empty agent ID", ErrInvalidCatalog, entry.Route)
		}
		if nilInterface(entry.Binding.Engine.Provider) {
			return AgentCatalog{}, fmt.Errorf("%w: route %q has no provider", ErrInvalidCatalog, entry.Route)
		}
		if _, duplicate := bindings[entry.Route]; duplicate {
			return AgentCatalog{}, fmt.Errorf("%w: duplicate route %q", ErrInvalidCatalog, entry.Route)
		}
		bindings[entry.Route] = entry.Binding
	}
	return AgentCatalog{bindings: bindings}, nil
}

func (catalog AgentCatalog) Resolve(route string) (AgentBinding, error) {
	binding, ok := catalog.bindings[route]
	if !ok {
		return AgentBinding{}, ErrUnknownRoute
	}
	return binding, nil
}

type RouteError struct {
	DispatchID string
	Route      string
}

func (failure *RouteError) Error() string {
	if failure == nil {
		return ErrUnknownRoute.Error()
	}
	return fmt.Sprintf("example: dispatch %q has unknown route %q", failure.DispatchID, failure.Route)
}

func (*RouteError) Unwrap() error { return ErrUnknownRoute }

type catalogExecutor struct {
	catalog AgentCatalog
	events  *executionEventSink
}

func (executor catalogExecutor) Execute(ctx context.Context, dispatch orchestration.Dispatch, sink agent.Sink) (agent.Result, error) {
	binding, err := executor.catalog.Resolve(dispatch.Route)
	if err != nil {
		return agent.Result{}, &RouteError{DispatchID: dispatch.ID, Route: dispatch.Route}
	}
	if executor.events != nil {
		if err := executor.events.Register(dispatch, binding); err != nil {
			return agent.Result{}, err
		}
	}
	return binding.Engine.RunStream(ctx, dispatch.Request, dispatch.OutputPolicy, sink), nil
}

const parentDispatchMetadata = "example.parentDispatchId"

type eventRegistration struct {
	agentID AgentID
	route   string
	parent  string
}

type executionEventSink struct {
	mu            sync.Mutex
	nextSequence  uint64
	registrations map[string]eventRegistration
	events        []ExecutionEvent
}

func newExecutionEventSink() *executionEventSink {
	return &executionEventSink{registrations: make(map[string]eventRegistration)}
}

func (sink *executionEventSink) Register(dispatch orchestration.Dispatch, binding AgentBinding) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if _, exists := sink.registrations[dispatch.ID]; exists {
		return fmt.Errorf("%w: dispatch %q was registered twice", ErrEventProjection, dispatch.ID)
	}
	sink.registrations[dispatch.ID] = eventRegistration{
		agentID: binding.ID,
		route:   dispatch.Route,
		parent:  dispatch.Metadata[parentDispatchMetadata],
	}
	return nil
}

func (sink *executionEventSink) Emit(ctx context.Context, frame agent.Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	registration, ok := sink.registrations[frame.Source]
	if !ok {
		return fmt.Errorf("%w: frame source %q is not registered", ErrEventProjection, frame.Source)
	}
	path, err := sink.pathLocked(frame.Source)
	if err != nil {
		return err
	}
	if len(path) == 0 || path[len(path)-1] != frame.Source {
		return fmt.Errorf("%w: frame source %q disagrees with path", ErrEventProjection, frame.Source)
	}
	sink.nextSequence++
	sink.events = append(sink.events, ExecutionEvent{
		Sequence:   sink.nextSequence,
		Path:       path,
		AgentID:    registration.agentID,
		DispatchID: frame.Source,
		Route:      registration.route,
		Frame:      cloneFrame(frame),
	})
	return nil
}

func (sink *executionEventSink) Events() []ExecutionEvent {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	events := make([]ExecutionEvent, len(sink.events))
	for index, event := range sink.events {
		events[index] = event
		events[index].Path = slices.Clone(event.Path)
		events[index].Frame = cloneFrame(event.Frame)
	}
	return events
}

func (sink *executionEventSink) pathLocked(dispatchID string) ([]string, error) {
	var reversed []string
	seen := make(map[string]struct{})
	for current := dispatchID; current != ""; {
		if _, duplicate := seen[current]; duplicate {
			return nil, fmt.Errorf("%w: parent cycle at dispatch %q", ErrEventProjection, current)
		}
		seen[current] = struct{}{}
		registration, ok := sink.registrations[current]
		if !ok {
			return nil, fmt.Errorf("%w: parent dispatch %q is not registered", ErrEventProjection, current)
		}
		reversed = append(reversed, current)
		current = registration.parent
	}
	slices.Reverse(reversed)
	return reversed, nil
}

func cloneFrame(frame agent.Frame) agent.Frame {
	frame.ProviderState = append(json.RawMessage(nil), frame.ProviderState...)
	if frame.ToolCall != nil {
		call := *frame.ToolCall
		call.Arguments = append(json.RawMessage(nil), call.Arguments...)
		frame.ToolCall = &call
	}
	if frame.ToolCallDelta != nil {
		delta := *frame.ToolCallDelta
		if delta.Index != nil {
			index := *delta.Index
			delta.Index = &index
		}
		frame.ToolCallDelta = &delta
	}
	if frame.ToolResult != nil {
		result := message.CloneToolResult(*frame.ToolResult)
		frame.ToolResult = &result
	}
	if frame.ToolUpdate != nil {
		update := tool.CloneUpdate(*frame.ToolUpdate)
		frame.ToolUpdate = &update
	}
	return frame
}

type deterministicProvider struct{}

func (deterministicProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "example-deterministic"}
}

func (deterministicProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	if len(request.Messages) > 0 {
		last := request.Messages[len(request.Messages)-1]
		if last.Role == message.RoleTool && last.ToolResult != nil {
			return provider.NewSliceStream([]provider.Event{
				{Kind: provider.EventTextDelta, Text: last.ToolResult.TextContent()},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			}), nil
		}
	}
	prompt := "Summarize"
	if len(request.Messages) > 0 {
		prompt = request.Messages[len(request.Messages)-1].TextContent()
	}
	arguments, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: prompt})
	if err != nil {
		return nil, err
	}
	return provider.NewSliceStream([]provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        "uppercase-1",
				Name:      "uppercase",
				Arguments: arguments,
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}), nil
}

func newSummarizerEngine() (agent.Engine, error) {
	uppercase, err := kit.Tool("uppercase", func(_ context.Context, input struct {
		Text string `json:"text"`
	},
	) (string, error) {
		return strings.ToUpper(input.Text), nil
	})
	if err != nil {
		return agent.Engine{}, err
	}
	return agent.Engine{
		Provider:   deterministicProvider{},
		Tools:      tool.NewBus(uppercase),
		Model:      "example-model",
		ToolMode:   tool.ModeSequential,
		LoopPolicy: agent.LoopPolicy{MaxIterations: 3},
	}, nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func main() {
	engine, err := newSummarizerEngine()
	if err != nil {
		panic(err)
	}
	catalog, err := NewAgentCatalog(CatalogEntry{
		Route: "summarize",
		Binding: AgentBinding{
			ID:     AgentID("summarizer-v1"),
			Engine: engine,
		},
	})
	if err != nil {
		panic(err)
	}
	events := newExecutionEventSink()
	scheduler := orchestration.SchedulerFunc(func(_ context.Context, state orchestration.State) ([]orchestration.Dispatch, error) {
		switch state.Tick {
		case 0:
			return []orchestration.Dispatch{
				{ID: "dispatch-b", Route: "summarize", Request: agent.Request{Prompt: "second"}},
				{ID: "dispatch-a", Route: "summarize", Request: agent.Request{Prompt: "first"}},
			}, nil
		case 1:
			return []orchestration.Dispatch{{
				ID:       "dispatch-c",
				Route:    "summarize",
				Request:  agent.Request{Prompt: "child"},
				Metadata: map[string]string{parentDispatchMetadata: "dispatch-a"},
				Handoff:  &orchestration.Handoff{From: "unrelated-label", To: "summarize"},
			}}, nil
		default:
			return nil, nil
		}
	})

	state, err := orchestration.Drive(
		context.Background(),
		scheduler,
		catalogExecutor{catalog: catalog, events: events},
		orchestration.DriveOptions{MaxTicks: 2, MaxConcurrency: 2, Sink: events},
	)
	if err != nil {
		panic(err)
	}
	for _, outcome := range state.Outcomes {
		fmt.Printf("orchestration: %s=%s\n", outcome.Dispatch.ID, outcome.Result.Text)
	}
	for _, event := range events.Events() {
		if event.Frame.Kind == agent.FrameDone && event.Frame.StopReason == provider.StopReasonComplete {
			fmt.Printf("event: %d path=%s agent=%s route=%s\n", event.Sequence, strings.Join(event.Path, "/"), event.AgentID, event.Route)
		}
	}
}
