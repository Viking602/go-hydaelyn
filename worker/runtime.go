package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Viking602/venat/api"
)

// EnvelopePoller is the source side of the worker runtime. Each Poll call
// returns at most batchSize envelopes the worker should attempt to lease
// and execute. Implementations decide where envelopes come from (an
// in-process channel, a NATS subject, a Redis queue, a SQL scan), and
// whether Poll blocks for new work or returns immediately when nothing
// is pending.
//
// Pollers MUST be safe for concurrent calls when the Runtime is configured
// with Concurrency > 1.
type EnvelopePoller interface {
	Poll(ctx context.Context, batchSize int) ([]api.TaskEnvelope, error)
}

// EnvelopeExecutor is the sink side. The Runtime delegates per-envelope
// execution here so callers can plug in custom telemetry, isolation, or
// engine-selection logic. AgentWorker satisfies this interface via its
// ExecuteEnvelope method — see Runtime.NewFromAgentWorker.
type EnvelopeExecutor interface {
	ExecuteEnvelope(ctx context.Context, req ExecuteEnvelopeRequest) (ExecutionOutcome, error)
}

// PollerFunc adapts an ordinary function into an EnvelopePoller. Useful
// for tests and one-off in-process drivers.
type PollerFunc func(ctx context.Context, batchSize int) ([]api.TaskEnvelope, error)

// Poll satisfies EnvelopePoller.
func (f PollerFunc) Poll(ctx context.Context, batchSize int) ([]api.TaskEnvelope, error) {
	return f(ctx, batchSize)
}

// ExecutorFunc adapts an ordinary function into an EnvelopeExecutor.
type ExecutorFunc func(ctx context.Context, req ExecuteEnvelopeRequest) (ExecutionOutcome, error)

// ExecuteEnvelope satisfies EnvelopeExecutor.
func (f ExecutorFunc) ExecuteEnvelope(ctx context.Context, req ExecuteEnvelopeRequest) (ExecutionOutcome, error) {
	return f(ctx, req)
}

// ErrorHandler is invoked when an envelope execution returns a non-nil
// error. It receives the envelope and the error so callers can route the
// failure to logs, metrics, alerting, or a dead-letter sink. A nil handler
// drops errors silently.
type ErrorHandler func(ctx context.Context, env api.TaskEnvelope, err error)

// RuntimeOptions configures Runtime.Run. All fields are optional; the
// zero value yields a single-goroutine loop polling every 250ms.
type RuntimeOptions struct {
	// Concurrency is the number of envelopes processed in parallel.
	// Defaults to 1 when zero.
	Concurrency int

	// BatchSize is the per-poll fetch hint passed to EnvelopePoller.
	// Defaults to Concurrency * 2 (minimum 1).
	BatchSize int

	// PollInterval is the sleep between polls when the last poll returned
	// no envelopes. Defaults to 250ms. Polls that returned envelopes do
	// not sleep — the loop re-polls immediately.
	PollInterval time.Duration

	// PerEnvelopeTTL is the default lease TTL applied when the envelope
	// itself doesn't carry one. Defaults to AgentWorker's own TTL fallback.
	PerEnvelopeTTL time.Duration

	// OnError, when non-nil, receives every executor error along with the
	// envelope that produced it. The runtime itself does not treat
	// executor errors as fatal — it logs (via this hook) and moves on.
	OnError ErrorHandler

	// ShutdownDrainTimeout is how long Stop waits for in-flight envelopes
	// to finish before returning. Defaults to 30s.
	ShutdownDrainTimeout time.Duration
}

// Runtime is the long-running poll loop that feeds envelopes from an
// EnvelopePoller into an EnvelopeExecutor. It owns concurrency, sleeps,
// and graceful drain — but stays out of the way of lease/retry/dead-letter
// logic, which lives in the executor (AgentWorker + Runner).
//
// Lifecycle:
//
//	rt := worker.NewRuntime(poller, agentWorker, opts)
//	go rt.Run(ctx)        // start the loop
//	// … later …
//	if err := rt.Stop(); err != nil { /* drain timeout */ }
//
// Run blocks until ctx is cancelled or Stop is called. Stop returns a
// drain-timeout error only if running envelopes don't finish within
// ShutdownDrainTimeout.
type Runtime struct {
	poller   EnvelopePoller
	executor EnvelopeExecutor
	opts     RuntimeOptions

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup

	// runStarted flips true on the first Run call and is used by Stop to
	// distinguish "Run never called" (return immediately) from "Run in
	// flight" (wait on runDone). runDone is closed by Run on exit and
	// runErr holds Run's drain return value — Stop reads it once runDone
	// fires, observing the value via the channel-close happens-before.
	runStarted atomic.Bool
	runDone    chan struct{}
	runErr     error
	cancel     atomic.Value // context.CancelFunc set by Run
}

// NewRuntime wires a poller + executor with options. Both arguments are
// required; nil values cause Run to return immediately.
func NewRuntime(p EnvelopePoller, e EnvelopeExecutor, opts RuntimeOptions) *Runtime {
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}
	if opts.BatchSize < 1 {
		opts.BatchSize = opts.Concurrency * 2
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 250 * time.Millisecond
	}
	if opts.ShutdownDrainTimeout <= 0 {
		opts.ShutdownDrainTimeout = 30 * time.Second
	}
	return &Runtime{
		poller:   p,
		executor: e,
		opts:     opts,
		stopCh:   make(chan struct{}),
		runDone:  make(chan struct{}),
	}
}

// ErrRuntimeMisconfigured is returned by Run when poller or executor is
// nil at startup. The runtime refuses to enter its main loop in that
// case to avoid silently dropping work.
var ErrRuntimeMisconfigured = errors.New("worker: runtime missing poller or executor")

// ErrRuntimeAlreadyStarted is returned when Run is called more than once on
// the same Runtime. Each Runtime is a one-shot loop; construct a new one to
// restart after Stop.
var ErrRuntimeAlreadyStarted = errors.New("worker: Run already started")

// Run blocks until ctx is cancelled or Stop is called. A nil poller or
// executor returns ErrRuntimeMisconfigured immediately so caller-side
// bugs surface loudly rather than as silent no-ops.
func (r *Runtime) Run(ctx context.Context) error {
	if !r.runStarted.CompareAndSwap(false, true) {
		return ErrRuntimeAlreadyStarted
	}
	defer close(r.runDone)
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel.Store(cancel)
	defer cancel()
	err := r.run(runCtx)
	r.runErr = err
	return err
}

func (r *Runtime) run(ctx context.Context) error {
	if r.poller == nil || r.executor == nil {
		return ErrRuntimeMisconfigured
	}

	sem := make(chan struct{}, r.opts.Concurrency)

	for {
		select {
		case <-ctx.Done():
			return r.drain(ctx.Err())
		case <-r.stopCh:
			return r.drain(nil)
		default:
		}

		envs, err := r.poller.Poll(ctx, r.opts.BatchSize)
		if err != nil {
			// ctx.Err()/Cancelled from the poller is a normal shutdown
			// signal, not a fault — don't burn an OnError event on it.
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				r.report(ctx, api.TaskEnvelope{}, fmt.Errorf("poll: %w", err))
			}
			if !sleepOrStop(ctx, r.stopCh, r.opts.PollInterval) {
				return r.drain(nil)
			}
			continue
		}
		if len(envs) == 0 {
			if !sleepOrStop(ctx, r.stopCh, r.opts.PollInterval) {
				return r.drain(nil)
			}
			continue
		}

		for _, env := range envs {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return r.drain(ctx.Err())
			case <-r.stopCh:
				return r.drain(nil)
			}
			r.wg.Add(1)
			go func(env api.TaskEnvelope) {
				defer r.wg.Done()
				defer func() { <-sem }()
				if _, err := r.executor.ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{
					Envelope: env,
					TTL:      r.opts.PerEnvelopeTTL,
				}); err != nil {
					r.report(ctx, env, err)
				}
			}(env)
		}
	}
}

// Stop signals Run to exit after draining in-flight envelopes. Returns
// nil on clean drain, or a timeout error if ShutdownDrainTimeout elapses
// before workers finish.
//
// Stop blocks on Run's exit (not directly on the WaitGroup) so that the
// only goroutine calling wg.Add is also the one calling wg.Wait via
// drain — eliminating the Add/Wait race that arises when an external
// caller waits on the WaitGroup concurrently with the loop still
// dispatching envelopes.
func (r *Runtime) Stop() error {
	r.stopOnce.Do(func() { close(r.stopCh) })
	if !r.runStarted.Load() {
		return nil
	}
	select {
	case <-r.runDone:
		return r.runErr
	case <-time.After(r.opts.ShutdownDrainTimeout):
		r.cancelInFlight()
		return fmt.Errorf("worker: drain timed out after %s", r.opts.ShutdownDrainTimeout)
	}
}

func (r *Runtime) cancelInFlight() {
	if cancel, ok := r.cancel.Load().(context.CancelFunc); ok && cancel != nil {
		cancel()
	}
}

func (r *Runtime) drain(cause error) error {
	if drainErr := r.waitDrain(r.opts.ShutdownDrainTimeout); drainErr != nil {
		return drainErr
	}
	return cause
}

func (r *Runtime) waitDrain(timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		r.cancelInFlight()
		return fmt.Errorf("worker: drain timed out after %s", timeout)
	}
}

func (r *Runtime) report(ctx context.Context, env api.TaskEnvelope, err error) {
	if r.opts.OnError == nil {
		return
	}
	r.opts.OnError(ctx, env, err)
}

// sleepOrStop returns false when ctx or stop fires before the timer.
func sleepOrStop(ctx context.Context, stop <-chan struct{}, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	case <-stop:
		return false
	}
}

// ChannelPoller is a trivial in-process EnvelopePoller backed by a Go
// channel. Useful for tests and single-process deployments where the
// command layer can deliver envelopes directly into the worker without
// going through an external queue.
//
// Producers should call Submit (or send to Ch directly) for each new
// envelope; the Runtime drains the channel via Poll. Close the channel
// to signal end-of-stream — Poll returns a zero-length slice and the
// runtime sleeps on its PollInterval until ctx cancels.
type ChannelPoller struct {
	Ch chan api.TaskEnvelope
}

// NewChannelPoller returns a ChannelPoller with the given buffer depth.
// Pick a buffer >= expected steady-state concurrency to avoid producer
// stalls.
func NewChannelPoller(buffer int) *ChannelPoller {
	return &ChannelPoller{Ch: make(chan api.TaskEnvelope, buffer)}
}

// Submit enqueues an envelope, blocking if the underlying channel is
// full. Returns ctx.Err() if the context cancels first.
func (c *ChannelPoller) Submit(ctx context.Context, env api.TaskEnvelope) error {
	select {
	case c.Ch <- env:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Poll drains up to batchSize envelopes from the channel without
// blocking past the first read. Returns immediately with a zero-length
// slice when no envelopes are available — the Runtime will sleep on its
// PollInterval before re-polling.
func (c *ChannelPoller) Poll(ctx context.Context, batchSize int) ([]api.TaskEnvelope, error) {
	out := make([]api.TaskEnvelope, 0, batchSize)
	// First read: block briefly so callers don't tight-loop on an empty channel.
	timer := time.NewTimer(50 * time.Millisecond)
	defer timer.Stop()
	select {
	case env, ok := <-c.Ch:
		if !ok {
			return out, nil
		}
		out = append(out, env)
	case <-ctx.Done():
		return out, ctx.Err()
	case <-timer.C:
		return out, nil
	}
	for len(out) < batchSize {
		select {
		case env, ok := <-c.Ch:
			if !ok {
				return out, nil
			}
			out = append(out, env)
		default:
			return out, nil
		}
	}
	return out, nil
}
