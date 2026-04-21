package tool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Viking602/go-hydaelyn/message"
)

type Definition = message.ToolDefinition
type Schema = message.JSONSchema
type Call = message.ToolCall
type Result = message.ToolResult

type Mode string

const (
	ModeSequential Mode = "sequential"
	ModeParallel   Mode = "parallel"
)

type Update struct {
	Kind    string            `json:"kind"`
	Message string            `json:"message,omitempty"`
	Data    map[string]string `json:"data,omitempty"`
}

type UpdateSink func(Update) error

type Driver interface {
	Definition() Definition
	Execute(ctx context.Context, call Call, sink UpdateSink) (Result, error)
}

var ErrToolNotFound = errors.New("tool not found")

// CallerInfo identifies the agent invoking a tool. It is plumbed via context
// by the runtime so tools (e.g. send_message) can discover their caller
// without forcing the LLM to pass teamId/agentId as explicit arguments.
type CallerInfo struct {
	TeamRunID string
	AgentID   string
	TaskID    string
	SessionID string
}

type callerKey struct{}

// WithCaller returns a context carrying the given CallerInfo.
func WithCaller(ctx context.Context, info CallerInfo) context.Context {
	return context.WithValue(ctx, callerKey{}, info)
}

// CallerFromContext retrieves any CallerInfo previously stored via WithCaller.
func CallerFromContext(ctx context.Context) (CallerInfo, bool) {
	info, ok := ctx.Value(callerKey{}).(CallerInfo)
	return info, ok
}

type Bus struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

func NewBus(drivers ...Driver) *Bus {
	b := &Bus{
		drivers: make(map[string]Driver, len(drivers)),
	}
	for _, driver := range drivers {
		b.Register(driver)
	}
	return b
}

func (b *Bus) Register(driver Driver) {
	if driver == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.drivers[driver.Definition().Name] = driver
}

func (b *Bus) Definitions() []Definition {
	b.mu.RLock()
	defer b.mu.RUnlock()
	defs := make([]Definition, 0, len(b.drivers))
	for _, driver := range b.drivers {
		defs = append(defs, driver.Definition())
	}
	return defs
}

func (b *Bus) Subset(names []string) *Bus {
	if len(names) == 0 {
		return NewBus()
	}
	selected := make([]Driver, 0, len(names))
	for _, name := range names {
		driver, ok := b.Driver(name)
		if ok {
			selected = append(selected, driver)
		}
	}
	return NewBus(selected...)
}

func (b *Bus) Driver(name string) (Driver, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	driver, ok := b.drivers[name]
	return driver, ok
}

func (b *Bus) Execute(ctx context.Context, call Call, sink UpdateSink) (Result, error) {
	driver, ok := b.Driver(call.Name)
	if !ok {
		return Result{}, fmt.Errorf("%w: %s", ErrToolNotFound, call.Name)
	}
	return driver.Execute(ctx, call, sink)
}

func (b *Bus) ExecuteBatch(ctx context.Context, calls []Call, mode Mode, sink UpdateSink) ([]Result, error) {
	if mode == ModeParallel {
		return b.executeParallel(ctx, calls, sink)
	}
	results := make([]Result, 0, len(calls))
	for _, call := range calls {
		result, err := b.Execute(ctx, call, sink)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (b *Bus) executeParallel(ctx context.Context, calls []Call, sink UpdateSink) ([]Result, error) {
	results := make([]Result, len(calls))
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	for idx, call := range calls {
		wg.Add(1)
		go func(index int, current Call) {
			defer wg.Done()
			results[index], errs[index] = b.Execute(ctx, current, sink)
		}(idx, call)
	}
	wg.Wait()
	// errors.Join preserves call order and surfaces every failure rather
	// than racing on whichever goroutine happened to enqueue first.
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return results, nil
}
