package durable

import (
	"context"
	"sync"
	"time"
)

type activeExecution struct {
	runtime   *Runtime
	id        ExecutionID
	callerCtx context.Context
	ctx       context.Context
	cancel    context.CancelCauseFunc
	done      chan struct{}

	mu               sync.Mutex
	execution        Execution
	stopping         bool
	stopCause        error
	controlContext   context.Context
	suspendRequested bool
	heartbeatErr     error
	controlErr       error

	effects sync.WaitGroup
}

func newActiveExecution(ctx context.Context, runtime *Runtime, executionID ExecutionID) *activeExecution {
	runCtx, cancel := context.WithCancelCause(ctx)
	return &activeExecution{
		runtime:   runtime,
		id:        executionID,
		callerCtx: ctx,
		ctx:       runCtx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
}

func (active *activeExecution) setExecution(execution Execution) {
	active.mu.Lock()
	active.execution = cloneExecution(execution)
	active.mu.Unlock()
}

func (active *activeExecution) snapshot() Execution {
	active.mu.Lock()
	defer active.mu.Unlock()
	return cloneExecution(active.execution)
}

func (active *activeExecution) updateLease(lease Lease) {
	active.mu.Lock()
	if active.execution.Lease != nil && active.execution.Lease.OwnerID == lease.OwnerID && active.execution.Lease.Token == lease.Token {
		cloned := lease
		active.execution.Lease = &cloned
	}
	active.mu.Unlock()
}

func (active *activeExecution) beginEffect() error {
	active.mu.Lock()
	defer active.mu.Unlock()
	if active.stopping {
		if active.stopCause != nil {
			return active.stopCause
		}
		return context.Canceled
	}
	if err := active.ctx.Err(); err != nil {
		if cause := context.Cause(active.ctx); cause != nil {
			return cause
		}
		return err
	}
	active.effects.Add(1)
	return nil
}

func (active *activeExecution) endEffect() {
	active.effects.Done()
}

func (active *activeExecution) requestStop(controlContext context.Context, cause error) bool {
	active.mu.Lock()
	if active.stopCause != nil {
		active.mu.Unlock()
		return false
	}
	active.stopping = true
	active.stopCause = cause
	active.controlContext = controlContext
	active.mu.Unlock()
	active.cancel(cause)
	return true
}

func (active *activeExecution) requestSuspend(ctx context.Context) error {
	active.mu.Lock()
	if active.suspendRequested || active.stopCause != nil {
		active.mu.Unlock()
		return executionRuntimeError(active.id, ErrBusy)
	}
	active.suspendRequested = true
	active.stopping = true
	active.stopCause = ErrSuspended
	active.controlContext = ctx
	active.mu.Unlock()
	active.cancel(ErrSuspended)
	return nil
}

func (active *activeExecution) stopState() (error, context.Context, error) {
	active.mu.Lock()
	defer active.mu.Unlock()
	return active.stopCause, active.controlContext, active.heartbeatErr
}

func (active *activeExecution) recordHeartbeatError(err error) {
	if err == nil {
		return
	}
	active.mu.Lock()
	if active.heartbeatErr == nil {
		active.heartbeatErr = err
	}
	if active.stopCause == nil {
		active.stopping = true
		active.stopCause = err
	}
	active.mu.Unlock()
	active.cancel(err)
}

func (active *activeExecution) stopNewEffects() {
	active.mu.Lock()
	if !active.stopping {
		active.stopping = true
	}
	active.mu.Unlock()
}

func (active *activeExecution) waitEffects(ctx context.Context) error {
	settled := make(chan struct{})
	go func() {
		active.effects.Wait()
		close(settled)
	}()
	select {
	case <-settled:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (active *activeExecution) finish(controlErr error) {
	active.mu.Lock()
	active.controlErr = controlErr
	active.mu.Unlock()
	active.cancel(nil)
	close(active.done)
}

func (active *activeExecution) finalControlError() error {
	active.mu.Lock()
	defer active.mu.Unlock()
	return active.controlErr
}

func (active *activeExecution) settlementContext() (context.Context, context.CancelFunc) {
	base := context.WithoutCancel(active.callerCtx)
	return context.WithTimeout(base, active.runtime.settlementTimeout)
}

func (active *activeExecution) heartbeat() func() {
	interval := active.runtime.leaseTTL / 3
	if interval <= 0 {
		interval = time.Nanosecond
	}
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-active.ctx.Done():
				return
			case <-ticker.C:
				execution := active.snapshot()
				if execution.Lease == nil {
					return
				}
				lease, err := active.runtime.backend.RenewExecution(active.ctx, RenewExecutionRequest{
					ExecutionID: active.id,
					Lease:       leaseReference(*execution.Lease),
					LeaseTTL:    active.runtime.leaseTTL,
				})
				if err != nil {
					active.recordHeartbeatError(backendOperationError("renew execution", err))
					return
				}
				active.updateLease(lease)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(stop) })
		<-stopped
	}
}

func (active *activeExecution) reconcileError(attempts []Attempt) error {
	return &ReconcileRequiredError{Execution: active.snapshot(), Attempts: cloneAttempts(attempts)}
}

func (active *activeExecution) stopCauseValue() error {
	active.mu.Lock()
	defer active.mu.Unlock()
	return active.stopCause
}

func leaseReference(lease Lease) LeaseRef {
	return LeaseRef{OwnerID: lease.OwnerID, Token: lease.Token}
}

func executionRuntimeError(executionID ExecutionID, err error) error {
	return &ExecutionError{ExecutionID: executionID, Err: err}
}
