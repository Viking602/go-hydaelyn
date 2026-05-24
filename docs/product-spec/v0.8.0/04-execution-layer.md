# 04 — Execution Layer

## Goal

Provide a production-grade Worker Runtime that pulls envelopes off the Mailbox, executes them via `agent.Engine`, and handles all the messy operational concerns (heartbeat, retry, dead-letter, graceful shutdown). Provide a Trigger Runtime in `transport/` that translates declarative `Trigger` data into `Runner.QueueRun` calls.

## Worker Runtime

### Current state

`worker/worker.go` exposes `AgentWorker` with `ExecuteEnvelope(ctx, envelope) error`. It does one envelope at a time and assumes the caller polls.

### Target state

New type `worker.Runtime` wraps `AgentWorker` with a complete control loop.

`worker/runtime.go` (new):

```go
package worker

import (
    "context"
    "time"

    "github.com/Viking602/go-hydaelyn"
    "github.com/Viking602/go-hydaelyn/api"
)

// Runtime is the production worker process. It polls envelopes, acquires
// leases, heartbeats, executes via AgentWorker, handles retries and
// dead-lettering, and shuts down gracefully on context cancellation.
type Runtime struct {
    Runner      *hydaelyn.Runner
    WorkerID    string
    Engine      AgentEngine    // wraps agent.Engine
    Concurrency int            // max parallel in-flight envelopes; default 1
    PollInterval time.Duration // default 250ms
    HeartbeatInterval time.Duration // default 30s
    LeaseExtension    time.Duration // default 2min
    MaxAttempts       int           // default 3
    BackoffStrategy   BackoffStrategy // default ExponentialBackoff{Base:1s,Max:60s}
    DeadLetterSink    DeadLetterSink  // default DeadLetterStoreSink wrapping api.DeadLetterStore
}

// Start begins polling. Blocks until ctx is canceled or a fatal error occurs.
// Returns nil on graceful shutdown.
func (r *Runtime) Start(ctx context.Context) error

// Shutdown signals the runtime to stop accepting new envelopes and waits
// for in-flight work to complete (or until grace expires).
func (r *Runtime) Shutdown(ctx context.Context) error
```

### Operational protocol

For each envelope:

1. **Poll**: `Runner.NextEnvelope(ctx, WorkerID)` returns next envelope or `(nil, nil)` if empty.
2. **Acquire lease**: `Runner.AcquireTaskExecution(ctx, ...)` with CAS. If contended, skip envelope.
3. **Spawn heartbeat goroutine**: every `HeartbeatInterval`, extend lease by `LeaseExtension`.
4. **Execute**: call `AgentWorker.ExecuteEnvelope` under the acquired lease.
5. **On success**: `Runner.AckEnvelope(ctx, envelopeID)`, release lease, emit `EventEnvelopeAcked`.
6. **On error**: increment attempt counter, decide:
   - if attempts < MaxAttempts and error is retryable: requeue with backoff
   - else: dead-letter via `DeadLetterSink.Send(envelope, error, attempts)`, release lease, emit `EventEnvelopeDeadLettered`
7. **On context cancel**: complete current envelope or release lease (depending on `Shutdown` grace) and exit.

### Retry classification

```go
type Retryable interface {
    Retryable() bool
}
```

Errors that satisfy `Retryable() bool == true` are retried. `api.ErrPolicyDenied`, `api.ErrInvalidCommand` are not retryable by default. Network/timeout errors from provider drivers are retryable.

### Dead-letter

New interface and store:

```go
// api/store.go additions
type DeadLetterStore interface {
    SaveDeadLetter(context.Context, DeadLetterRecord) error
    ListDeadLetters(context.Context, DeadLetterSelector) ([]DeadLetterRecord, error)
    RequeueDeadLetter(context.Context, string) error
}

type DeadLetterRecord struct {
    ID         string         `json:"id"`
    EnvelopeID string         `json:"envelopeId"`
    Envelope   TaskEnvelope   `json:"envelope"`
    Reason     string         `json:"reason"`
    Attempts   int            `json:"attempts"`
    LastError  string         `json:"lastError"`
    CreatedAt  time.Time      `json:"createdAt"`
}

type DeadLetterSelector struct {
    WorkerID  string
    Since     time.Time
    Limit     int
}
```

### Backoff strategy

```go
type BackoffStrategy interface {
    Next(attempt int) time.Duration
}

type ExponentialBackoff struct {
    Base   time.Duration
    Max    time.Duration
    Jitter float64 // 0.0..1.0 fraction
}
```

### Concurrency

`Concurrency > 1` spawns a worker pool. Each goroutine independently polls and processes. Lease acquisition serializes via CAS — if two goroutines race for the same envelope, only one acquires the lease.

### Graceful shutdown

`Shutdown(ctx)`:

1. Set internal flag, polling goroutines stop accepting new envelopes
2. Wait for in-flight executions to complete or ctx expires
3. On ctx expiry, release leases of in-flight envelopes (they will be re-acquired by another worker)
4. Return

## Trigger Runtime

Declarative `Trigger` data lives on `AgentProfile.Triggers`. Execution lives in transport adapters.

### transport/scheduler/

```go
package scheduler

// Scheduler reads schedule-type Triggers from a Registry and dispatches Runs
// via the Runner.
type Scheduler struct {
    Registry api.Registry
    Runner   *hydaelyn.Runner
    Clock    Clock // injectable for tests
}

func (s *Scheduler) Start(ctx context.Context) error
func (s *Scheduler) Shutdown(ctx context.Context) error
```

For each profile with a `TriggerSchedule` trigger:
- Parse `Trigger.Source` as cron expression (use `robfig/cron/v3`)
- On fire: `Runner.RunFromProfile(ctx, profile.ID, RunInput{Context: trigger.Filter})`
- Emit `EventTriggerFired`

### transport/webhook/

```go
package webhook

// Listener exposes an HTTP server that matches incoming requests against
// webhook-type Triggers and dispatches Runs.
type Listener struct {
    Registry api.Registry
    Runner   *hydaelyn.Runner
    Addr     string
}

func (l *Listener) Start(ctx context.Context) error
```

Match rules:
- Trigger.Source = HTTP path (e.g., `/github/issues`)
- Trigger.Filter applied as request header / query / JSON-path match
- On match: `Runner.RunFromProfile` with request body as `RunInput.Context["body"]`

### transport/event/

```go
package event

// Bus is an in-process event bus that matches publications against
// event-type Triggers.
type Bus struct {
    Registry api.Registry
    Runner   *hydaelyn.Runner
}

func (b *Bus) Publish(ctx context.Context, event Event) error
func (b *Bus) Start(ctx context.Context) error
```

`Event` is a generic envelope `{Type string, Source string, Payload json.RawMessage}`. Triggers match on `Type == Trigger.Source`.

## New events

Append to `api.EventType` constants:

- `EventEnvelopeAcked`
- `EventEnvelopeDeadLettered`
- `EventEnvelopeRequeued`
- `EventTriggerFired`
- `EventWorkerStarted`
- `EventWorkerShutdown`

## Verification

- `TestRuntime_HappyPath` — start runtime, queue envelope, observe ack
- `TestRuntime_RetryThenDeadLetter` — envelope fails 3 times, lands in dead-letter store
- `TestRuntime_GracefulShutdown` — Shutdown waits for in-flight, returns clean
- `TestRuntime_ConcurrencyN_NoLeaseConflict` — N=10 goroutines never double-acquire a lease
- `TestRuntime_LeaseHeartbeat` — lease extended without restart while task long-running
- `TestScheduler_CronFires` — fake clock advances, cron-style Trigger fires, RunFromProfile called
- `TestWebhookListener_MatchAndDispatch` — POST hits configured path, profile dispatched
- `TestEventBus_MultipleSubscribers` — one event matches multiple profiles, all fire
