package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"github.com/Viking602/venat/message"
)

const (
	maxToolUpdatesPerCall     = uint64(65_536)
	maxToolUpdateBytesPerCall = uint64(64 << 20)
)

var (
	// ErrToolUpdateProtocol reports an invalid update kind, payload, lifecycle,
	// or terminal result that disagrees with streamed output.
	ErrToolUpdateProtocol = errors.New("tool update protocol violation")
	// ErrToolUpdateLimit reports that one call exceeded its fixed update count
	// or decoded-byte budget.
	ErrToolUpdateLimit = errors.New("tool update limit exceeded")
)

// CloneUpdate returns an update whose mutable data is independent of input.
func CloneUpdate(input Update) Update {
	input.Data = maps.Clone(input.Data)
	input.Parts = message.CloneContent(input.Parts)
	return input
}

type toolUpdateDriver struct {
	next Driver
}

func (driver toolUpdateDriver) Definition() Definition { return driver.next.Definition() }

func (driver toolUpdateDriver) Execute(ctx context.Context, call Call, sink UpdateSink) (Result, error) {
	childCtx, cancel := context.WithCancel(ctx)
	state := &toolUpdateState{
		call:   call,
		sink:   sink,
		cancel: cancel,
	}
	defer state.close()

	result, executeErr := driver.next.Execute(childCtx, call, state.emit)
	parts, updateCount, updateErr := state.finish()
	normalized, protocolErr := normalizeUpdatedResult(call, result, parts)
	if updateErr != nil {
		if protocolErr != nil {
			return normalized, errors.Join(updateErr, protocolErr)
		}
		return normalized, updateErr
	}
	if protocolErr != nil {
		return normalized, protocolErr
	}
	if executeErr != nil && updateCount > 0 && errors.Is(executeErr, ErrNotExecuted) {
		return normalized, fmt.Errorf("%w: driver emitted %d update(s) before reporting that it was not executed", ErrToolUpdateProtocol, updateCount)
	}
	return normalized, executeErr
}

type toolUpdateState struct {
	mu sync.Mutex

	call   Call
	sink   UpdateSink
	cancel context.CancelFunc

	closed    bool
	count     uint64
	bytes     uint64
	output    []message.ContentPart
	updateErr error
}

func (state *toolUpdateState) emit(input Update) error {
	update := CloneUpdate(input)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return fmt.Errorf("%w: update emitted after Driver.Execute returned", ErrToolUpdateProtocol)
	}
	if state.updateErr != nil {
		return state.updateErr
	}
	if err := validateUpdate(update); err != nil {
		state.failLocked(err)
		return err
	}
	if state.count >= maxToolUpdatesPerCall {
		err := fmt.Errorf("%w: count exceeds %d", ErrToolUpdateLimit, maxToolUpdatesPerCall)
		state.failLocked(err)
		return err
	}

	update.ToolCallID = state.call.ID
	update.OperationID = state.call.OperationID
	update.Sequence = state.count + 1
	decodedBytes := decodedUpdateBytes(update)
	if decodedBytes > maxToolUpdateBytesPerCall-state.bytes {
		err := fmt.Errorf("%w: decoded bytes exceed %d", ErrToolUpdateLimit, maxToolUpdateBytesPerCall)
		state.failLocked(err)
		return err
	}

	state.count++
	state.bytes += decodedBytes
	if update.Kind == UpdateOutput {
		state.output = append(state.output, message.CloneContent(update.Parts)...)
	}
	if state.sink == nil {
		return nil
	}
	if err := deliverUpdate(state.sink, update); err != nil {
		state.failLocked(err)
		return err
	}
	return nil
}

func (state *toolUpdateState) failLocked(err error) {
	if state.updateErr == nil {
		state.updateErr = err
		state.cancel()
	}
}

func (state *toolUpdateState) finish() ([]message.ContentPart, uint64, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.closed = true
	state.cancel()
	return message.CloneContent(state.output), state.count, state.updateErr
}

func (state *toolUpdateState) close() {
	state.mu.Lock()
	state.closed = true
	state.mu.Unlock()
	state.cancel()
}

func validateUpdate(update Update) error {
	switch update.Kind {
	case UpdateProgress:
		if len(update.Parts) != 0 {
			return fmt.Errorf("%w: progress update contains output parts", ErrToolUpdateProtocol)
		}
	case UpdateOutput:
		if len(update.Parts) == 0 {
			return fmt.Errorf("%w: output update has no parts", ErrToolUpdateProtocol)
		}
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrToolUpdateProtocol, update.Kind)
	}
	return nil
}

func normalizeUpdatedResult(call Call, result Result, output []message.ContentPart) (Result, error) {
	result = message.CloneToolResult(result)
	result.ToolCallID = call.ID
	result.Name = call.Name
	if len(output) == 0 {
		result.SyncLegacyContent()
		return result, nil
	}
	if len(result.Parts) == 0 {
		result.Parts = message.CloneContent(output)
		result.SyncLegacyContent()
		return result, nil
	}

	expected := Result{Parts: message.CloneContent(output)}
	expected.SyncLegacyContent()
	result.SyncLegacyContent()
	if result.Content != expected.Content || !equalContent(result.Parts, expected.Parts) {
		return result, fmt.Errorf("%w: terminal result differs from streamed output", ErrToolUpdateProtocol)
	}
	return result, nil
}

func equalContent(left, right []message.ContentPart) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !equalContentPart(left[index], right[index]) {
			return false
		}
	}
	return true
}

func equalContentPart(left, right message.ContentPart) bool {
	if left.ID != right.ID || left.Kind != right.Kind || left.Text != right.Text ||
		left.MediaType != right.MediaType || left.URI != right.URI ||
		left.Filename != right.Filename || left.Signature != right.Signature ||
		!bytes.Equal(left.Data, right.Data) || !bytes.Equal(left.ProviderData, right.ProviderData) {
		return false
	}
	if left.Source == nil || right.Source == nil {
		return left.Source == nil && right.Source == nil
	}
	return *left.Source == *right.Source
}

func decodedUpdateBytes(update Update) uint64 {
	total := uint64(8 + len(update.Kind) + len(update.ToolCallID) + len(update.OperationID) + len(update.Message))
	for key, value := range update.Data {
		total += uint64(len(key) + len(value))
	}
	for _, part := range update.Parts {
		total += uint64(len(part.ID) + len(part.Kind) + len(part.Text) + len(part.Data) + len(part.MediaType) + len(part.URI) + len(part.Filename) + len(part.Signature) + len(part.ProviderData))
		if part.Source != nil {
			total += uint64(len(part.Source.ID) + len(part.Source.URL) + len(part.Source.Title) + len(part.Source.MediaType) + len(part.Source.Filename))
		}
	}
	return total
}

func deliverUpdate(sink UpdateSink, update Update) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("tool update sink panicked: %v", recovered)
		}
	}()
	return sink(update)
}

func synchronizeUpdateSink(sink UpdateSink) UpdateSink {
	if sink == nil {
		return nil
	}
	var mu sync.Mutex
	return func(update Update) error {
		mu.Lock()
		defer mu.Unlock()
		return sink(update)
	}
}
