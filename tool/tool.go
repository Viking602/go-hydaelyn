package tool

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/Viking602/venat/message"
)

type Definition = message.ToolDefinition
type Schema = message.JSONSchema
type Call = message.ToolCall
type Result = message.ToolResult
type EffectType = message.ToolEffectType
type RetryPolicy = message.ToolRetryPolicy

const (
	EffectReadOnly           = message.ToolEffectReadOnly
	EffectWrite              = message.ToolEffectWrite
	EffectExternalSideEffect = message.ToolEffectExternalSideEffect
)

type Mode string

const (
	ModeSequential Mode = "sequential"
	ModeParallel   Mode = "parallel"
)

type BatchOptions struct {
	MaxConcurrency int
}

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

// ErrToolPanic wraps a panic recovered from a tool driver running on a
// goroutine the bus spawns for parallel execution. Such a panic cannot be
// recovered by the caller's stack, so executeParallel recovers it in the
// worker goroutine and records it as that call's error — surfaced by
// errors.Join exactly like any other tool failure — rather than letting it
// unwind the runtime and crash the process. In sequential mode a driver runs
// inline on the caller's stack, so its panic propagates to the caller's own
// recover instead. errors.Is(err, ErrToolPanic) reports whether a batch error
// originated from a panicking driver.
var ErrToolPanic = errors.New("tool driver panicked")

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
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

func (b *Bus) IsTerminal(name string) bool {
	driver, ok := b.Driver(name)
	if !ok {
		return false
	}
	return driver.Definition().Terminal
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
	definition := driver.Definition()
	if definition.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, definition.Timeout)
		defer cancel()
	}
	return driver.Execute(ctx, call, sink)
}

func (b *Bus) ExecuteBatch(ctx context.Context, calls []Call, mode Mode, sink UpdateSink) ([]Result, error) {
	return b.ExecuteBatchWithOptions(ctx, calls, mode, sink, BatchOptions{})
}

func (b *Bus) ExecuteBatchWithOptions(ctx context.Context, calls []Call, mode Mode, sink UpdateSink, options BatchOptions) ([]Result, error) {
	if mode == ModeParallel {
		return b.executeParallel(ctx, calls, sink, options)
	}
	results := make([]Result, 0, len(calls))
	for _, call := range calls {
		result, err := b.Execute(ctx, call, sink)
		if err != nil {
			// Return the results that already ran rather than nil: in sequential mode
			// every earlier call completed and side-effected before this one failed,
			// so the caller can record those results and a resuming caller is spared
			// from replaying them. The failed call and any after it are not included.
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (b *Bus) executeParallel(ctx context.Context, calls []Call, sink UpdateSink, options BatchOptions) ([]Result, error) {
	results := make([]Result, len(calls))
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	var sem chan struct{}
	if options.MaxConcurrency > 0 {
		sem = make(chan struct{}, options.MaxConcurrency)
	}
	for idx, call := range calls {
		wg.Add(1)
		go func(index int, current Call) {
			defer wg.Done()
			// A driver panic on this spawned goroutine cannot reach the
			// caller's recover, so contain it here and record it as the call's
			// error; errors.Join then surfaces it like any other tool failure.
			defer func() {
				if r := recover(); r != nil {
					errs[index] = fmt.Errorf("%w: %s: %v", ErrToolPanic, current.Name, r)
				}
			}()
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					errs[index] = ctx.Err()
					return
				}
			}
			results[index], errs[index] = b.Execute(ctx, current, sink)
		}(idx, call)
	}
	wg.Wait()
	// errors.Join preserves call order and surfaces every failure rather
	// than racing on whichever goroutine happened to enqueue first.
	if err := errors.Join(errs...); err != nil {
		// Return the results from the slots that ran to completion rather than
		// nil: those tools side-effected, so the caller can record them and a
		// resuming caller is spared from replaying them. Slots that errored,
		// panicked, or never started leave errs[index] non-nil and carry no
		// result, so excluding them keeps the survivors in call order.
		succeeded := make([]Result, 0, len(results))
		for index := range results {
			if errs[index] == nil {
				succeeded = append(succeeded, results[index])
			}
		}
		return succeeded, err
	}
	return results, nil
}
