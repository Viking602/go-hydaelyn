package worker

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/scripted"
)

func TestRunLeaseHeartbeatPulsesBeforeFirstTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan struct{})
	var pulses atomic.Int32
	errCh := make(chan error, 1)
	go func() {
		errCh <- runLeaseHeartbeat(ctx, time.Hour, func(context.Context) error {
			if pulses.Add(1) == 1 {
				close(first)
			}
			return nil
		})
	}()
	select {
	case <-first:
	case err := <-errCh:
		t.Fatalf("heartbeat exited before the first pulse: %v", err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("heartbeat did not pulse before the first ttl/3 tick")
	}
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("runLeaseHeartbeat() error = %v", err)
	}
	if got := pulses.Load(); got < 1 {
		t.Fatalf("pulses = %d, want at least 1", got)
	}
}

func TestExecuteEnvelopeHeartbeatsBeforeAckAndSurvivesShortTTL(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	runner.RegisterAgent(api.AgentProfile{ID: "agent-b"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID: "run-lease-heartbeat", RootTaskID: "root", Request: "keep lease",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "task-lease-heartbeat", Goal: "keep lease",
		OwnerAgentID: "agent-a", WriteTargets: []string{"summary"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}

	ttl := 60 * time.Millisecond
	hookStarted := make(chan struct{})
	done := make(chan error, 1)
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "still held"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	go func() {
		_, execErr := (AgentWorker{
			Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted", TTL: ttl,
		}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{
			Envelope: envelope,
			TTL:      ttl,
			OnLeaseAcquired: func(api.TaskExecutionLease) error {
				close(hookStarted)
				time.Sleep(3 * ttl)
				return nil
			},
		})
		done <- execErr
	}()
	select {
	case <-hookStarted:
	case err := <-done:
		t.Fatalf("ExecuteEnvelope() finished before OnLeaseAcquired: %v", err)
	case <-time.After(time.Second):
		t.Fatal("OnLeaseAcquired did not start")
	}

	time.Sleep(2 * ttl)
	if _, err := runner.Recover(ctx, run.ID); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if got, err := runner.ActiveLeaseCountContext(ctx, run.ID, task.ID); err != nil || got != 1 {
		t.Fatalf("active leases after Recover() = %d, err=%v, want 1", got, err)
	}
	stolen, acquired, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: envelope.ID,
		HolderType: api.HolderAgent, HolderID: "agent-b", TTL: ttl,
	})
	if acquired {
		t.Fatalf("other holder acquired lease during setup: %#v", stolen)
	}
	if err != nil && !errors.Is(err, api.ErrLeaseHolderMismatch) {
		t.Fatalf("AcquireTaskExecution(other holder) error = %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}
	completed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if completed.Status != api.TaskStatusCompleted {
		t.Fatalf("task status = %s, want completed", completed.Status)
	}
}

func TestCombineExecutionErrorsPrefersHeartbeatOverCancel(t *testing.T) {
	heartbeatErr := fmt.Errorf("worker: lease heartbeat failed: %w", api.ErrLeaseNotActive)
	got := combineExecutionErrors(context.Canceled, heartbeatErr)
	if !errors.Is(got, api.ErrLeaseNotActive) {
		t.Fatalf("combineExecutionErrors() = %v, want heartbeat cause", got)
	}
	report := failureReport(got)
	if report.Kind == "cancelled" {
		t.Fatalf("failureReport kind = %q, want a non-cancelled lost-lease failure", report.Kind)
	}

	other := errors.New("tool failed")
	joined := combineExecutionErrors(other, heartbeatErr)
	if !errors.Is(joined, api.ErrLeaseNotActive) || !errors.Is(joined, other) {
		t.Fatalf("combineExecutionErrors(tool, heartbeat) = %v", joined)
	}

	kept := combineExecutionErrors(errors.Join(other, context.Canceled), heartbeatErr)
	if !errors.Is(kept, other) || !errors.Is(kept, api.ErrLeaseNotActive) {
		t.Fatalf("combineExecutionErrors(join(tool, cancel), heartbeat) = %v, want both causes", kept)
	}
	if report := failureReport(kept); report.Kind == "cancelled" {
		t.Fatalf("failureReport(compound) kind = cancelled, want a non-cancel classification")
	}
}

func TestOnlyContextCanceledWalksWrappedJoins(t *testing.T) {
	cancelOnly := (&agent.AgentFailure{
		Kind:   agent.FailureKindEngineError,
		Reason: "host canceled",
	}).WithCause(context.Canceled)
	if !onlyContextCanceled(cancelOnly) {
		t.Fatal("AgentFailure whose only cause is Canceled must still be cancel-only")
	}
	if got := failureReport(cancelOnly).Kind; got != "cancelled" {
		t.Fatalf("failureReport(cancel-only AgentFailure).Kind = %q, want cancelled", got)
	}

	recorderErr := errors.New("checkpoint recorder failed")
	wrapped := (&agent.AgentFailure{
		Kind:   agent.FailureKindEngineError,
		Reason: "loop canceled",
	}).WithCause(errors.Join(context.Canceled, recorderErr))
	if onlyContextCanceled(wrapped) {
		t.Fatal("AgentFailure wrapping Join(Canceled, recorder) must not be treated as pure cancel")
	}
	heartbeatErr := fmt.Errorf("worker: lease heartbeat failed: %w", api.ErrLeaseNotActive)
	got := combineExecutionErrors(wrapped, heartbeatErr)
	if !errors.Is(got, recorderErr) || !errors.Is(got, api.ErrLeaseNotActive) {
		t.Fatalf("combineExecutionErrors() = %v, want recorder and heartbeat causes", got)
	}
	if report := failureReport(got); report.Kind == "cancelled" {
		t.Fatalf("failureReport kind = %q, want engine/heartbeat classification", report.Kind)
	}
}

func TestFailureReportOnlyMarksPureCancel(t *testing.T) {
	if got := failureReport(context.Canceled).Kind; got != "cancelled" {
		t.Fatalf("failureReport(Canceled).Kind = %q, want cancelled", got)
	}
	if got := failureReport(fmt.Errorf("worker: %w", context.Canceled)).Kind; got != "cancelled" {
		t.Fatalf("failureReport(wrapped Canceled).Kind = %q, want cancelled", got)
	}
	compound := errors.Join(api.ErrLeaseNotActive, context.Canceled)
	if got := failureReport(compound).Kind; got == "cancelled" {
		t.Fatalf("failureReport(join(lease, cancel)).Kind = cancelled")
	}
}

func TestPulseLeaseHeartbeatAfterCancel(t *testing.T) {
	tests := []struct {
		name  string
		pulse error
		want  error
	}{
		{name: "transport failure is explained by cancel", pulse: errors.New("dial: connection refused")},
		{name: "lost lease surfaces", pulse: api.ErrLeaseNotActive, want: api.ErrLeaseNotActive},
		{name: "stolen lease surfaces", pulse: api.ErrLeaseHolderMismatch, want: api.ErrLeaseHolderMismatch},
		{name: "missing lease surfaces", pulse: api.ErrNotFound, want: api.ErrNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := pulseLeaseHeartbeat(ctx, func(context.Context) error {
				return test.pulse
			})
			if test.want == nil {
				if err != nil {
					t.Fatalf("pulseLeaseHeartbeat() error = %v, want nil after cancel", err)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("pulseLeaseHeartbeat() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestLeaseRenewalPulseSkipsUntilExpiryWouldAdvance(t *testing.T) {
	var heartbeats, validations atomic.Int32
	expires := time.Now().UTC().Add(time.Hour)
	pulse := leaseRenewalPulse(expires, time.Minute, func(context.Context) error {
		heartbeats.Add(1)
		return nil
	}, func(context.Context) error {
		validations.Add(1)
		return nil
	})
	if err := pulse(context.Background()); err != nil {
		t.Fatalf("pulse() error = %v", err)
	}
	if heartbeats.Load() != 0 || validations.Load() != 1 {
		t.Fatalf("heartbeats=%d validations=%d, want skip-extend plus validate", heartbeats.Load(), validations.Load())
	}
}

func TestLeaseRenewalPulseDetectsLostLeaseWhileSkippingExtend(t *testing.T) {
	pulse := leaseRenewalPulse(time.Now().UTC().Add(time.Hour), time.Minute, func(context.Context) error {
		t.Fatal("Heartbeat must not run when it would shorten expiry")
		return nil
	}, func(context.Context) error {
		return api.ErrLeaseNotActive
	})
	if err := pulse(context.Background()); !errors.Is(err, api.ErrLeaseNotActive) {
		t.Fatalf("pulse() error = %v, want ErrLeaseNotActive", err)
	}
}

func TestExecuteEnvelopeKeepsSuppliedLongerLease(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID: "run-supplied-lease", RootTaskID: "root", Request: "use existing lease",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "task-supplied-lease", Goal: "use existing lease",
		OwnerAgentID: "agent-a", WriteTargets: []string{"summary"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	lease, acquired, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: envelope.ID,
		HolderType: api.HolderAgent, HolderID: "agent-a", TTL: 30 * time.Minute,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v err=%v", lease, acquired, err)
	}
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "kept"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	outcome, err := (AgentWorker{
		Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted",
		TTL: 20 * time.Millisecond,
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{
		Envelope: envelope, Lease: lease, TTL: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}
	if outcome.State != ExecutionCompleted {
		t.Fatalf("outcome.State = %q, want completed", outcome.State)
	}
}

func TestExecuteEnvelopeCompletesWhenHeartbeatWouldOverlapReport(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID: "run-heartbeat-finalize", RootTaskID: "root", Request: "finish",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: "task-heartbeat-finalize", Goal: "finish",
		OwnerAgentID: "agent-a", WriteTargets: []string{"summary"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	outcome, err := (AgentWorker{
		Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted",
		TTL: 8 * time.Millisecond,
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: envelope, TTL: 8 * time.Millisecond})
	if err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}
	if outcome.State != ExecutionCompleted {
		t.Fatalf("outcome.State = %q, want completed", outcome.State)
	}
}
