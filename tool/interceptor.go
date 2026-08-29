package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// ErrInterceptorProtocol reports an invalid tool interceptor implementation.
var ErrInterceptorProtocol = errors.New("tool interceptor protocol violation")

// Interceptor surrounds one validated tool execution. It may call the supplied
// Driver zero or one time and must pass the Call through unchanged.
type Interceptor interface {
	Execute(context.Context, Driver, Call, UpdateSink) (Result, error)
}

// InterceptorFunc adapts a function to Interceptor.
type InterceptorFunc func(context.Context, Driver, Call, UpdateSink) (Result, error)

// Execute delegates to f.
func (f InterceptorFunc) Execute(ctx context.Context, next Driver, call Call, sink UpdateSink) (Result, error) {
	return f(ctx, next, call, sink)
}

// InterceptorProtocolError identifies the interceptor stage that broke the
// zero-or-one-call or immutable-call contract.
type InterceptorProtocolError struct {
	Index int
	Err   error
}

func (failure *InterceptorProtocolError) Error() string {
	if failure == nil {
		return ErrInterceptorProtocol.Error()
	}
	return fmt.Sprintf("tool interceptor %d: %v", failure.Index, failure.Err)
}

func (failure *InterceptorProtocolError) Unwrap() []error {
	if failure == nil || failure.Err == nil {
		return []error{ErrInterceptorProtocol}
	}
	return []error{ErrInterceptorProtocol, failure.Err}
}

// ChainInterceptors composes interceptors outermost-first. Nil entries are
// ignored; an empty chain returns nil.
func ChainInterceptors(interceptors ...Interceptor) Interceptor {
	filtered := make([]Interceptor, 0, len(interceptors))
	for _, interceptor := range interceptors {
		if interceptor != nil {
			filtered = append(filtered, interceptor)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return InterceptorFunc(func(ctx context.Context, driver Driver, call Call, sink UpdateSink) (Result, error) {
		next := driver
		for index := len(filtered) - 1; index >= 0; index-- {
			next = interceptorDriver{
				definition: driver.Definition,
				execute:    wrapInterceptor(index, filtered[index], next),
			}
		}
		return next.Execute(ctx, cloneCall(call), sink)
	})
}

type interceptorDriver struct {
	definition func() Definition
	execute    func(context.Context, Call, UpdateSink) (Result, error)
}

func (driver interceptorDriver) Definition() Definition { return driver.definition() }

func (driver interceptorDriver) Execute(ctx context.Context, call Call, sink UpdateSink) (Result, error) {
	return driver.execute(ctx, call, sink)
}

func wrapInterceptor(index int, interceptor Interceptor, next Driver) func(context.Context, Call, UpdateSink) (result Result, err error) {
	return func(ctx context.Context, call Call, sink UpdateSink) (result Result, err error) {
		expected, fingerprintErr := callFingerprint(call)
		if fingerprintErr != nil {
			return Result{}, &InterceptorProtocolError{Index: index, Err: fingerprintErr}
		}

		var mu sync.Mutex
		calls := 0
		closed := false
		var protocolErr error
		guarded := interceptorDriver{
			definition: next.Definition,
			execute: func(nextCtx context.Context, candidate Call, nextSink UpdateSink) (Result, error) {
				mu.Lock()
				if closed {
					mu.Unlock()
					return Result{}, errors.Join(ErrNotExecuted, &InterceptorProtocolError{Index: index, Err: errors.New("next called after interceptor returned")})
				}
				calls++
				callNumber := calls
				mu.Unlock()
				if callNumber > 1 {
					failure := &InterceptorProtocolError{Index: index, Err: errors.New("next called more than once")}
					mu.Lock()
					protocolErr = failure
					mu.Unlock()
					return Result{}, failure
				}
				candidateFingerprint, candidateErr := callFingerprint(candidate)
				if candidateErr != nil || candidate.OperationID != call.OperationID || !bytes.Equal(candidateFingerprint, expected) {
					cause := candidateErr
					if cause == nil {
						cause = errors.New("call or operation ID was modified")
					}
					failure := &InterceptorProtocolError{Index: index, Err: cause}
					mu.Lock()
					protocolErr = errors.Join(ErrNotExecuted, failure)
					mu.Unlock()
					return Result{}, errors.Join(ErrNotExecuted, failure)
				}
				return next.Execute(nextCtx, cloneCall(candidate), nextSink)
			},
		}

		defer func() {
			mu.Lock()
			closed = true
			called := calls
			violation := protocolErr
			mu.Unlock()
			if recovered := recover(); recovered != nil {
				panicErr := &InterceptorProtocolError{Index: index, Err: fmt.Errorf("panic: %v", recovered)}
				if called == 0 {
					panicErr.Err = errors.Join(ErrNotExecuted, panicErr.Err)
				}
				result = Result{}
				err = panicErr
				return
			}
			if violation != nil {
				result = Result{}
				err = errors.Join(err, violation)
				return
			}
			if called == 0 && err != nil && !errors.Is(err, ErrNotExecuted) {
				err = errors.Join(ErrNotExecuted, err)
			}
		}()
		return interceptor.Execute(ctx, guarded, call, sink)
	}
}

func callFingerprint(call Call) ([]byte, error) {
	encoded, err := json.Marshal(call)
	if err != nil {
		return nil, fmt.Errorf("encode call for interceptor validation: %w", err)
	}
	return encoded, nil
}

func cloneCall(call Call) Call {
	call.Arguments = append(json.RawMessage(nil), call.Arguments...)
	return call
}
