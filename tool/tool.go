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
		b.mu.RLock()
		defer b.mu.RUnlock()
		drivers := make([]Driver, 0, len(b.drivers))
		for _, driver := range b.drivers {
			drivers = append(drivers, driver)
		}
		return NewBus(drivers...)
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
	type item struct {
		index  int
		result Result
		err    error
	}
	results := make([]Result, len(calls))
	ch := make(chan item, len(calls))
	var wg sync.WaitGroup
	for idx, call := range calls {
		wg.Add(1)
		go func(index int, current Call) {
			defer wg.Done()
			result, err := b.Execute(ctx, current, sink)
			ch <- item{index: index, result: result, err: err}
		}(idx, call)
	}
	wg.Wait()
	close(ch)
	for item := range ch {
		if item.err != nil {
			return nil, item.err
		}
		results[item.index] = item.result
	}
	return results, nil
}
