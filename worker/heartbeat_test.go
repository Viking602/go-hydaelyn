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
	if got := runner.ActiveLeaseCountContext(ctx, run.ID, task.ID); got != 1 {
		t.Fatalf("active leases after Recover() = %d, want 1", got)
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
}

func TestPulseLeaseHeartbeatIgnoresErrorAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := pulseLeaseHeartbeat(ctx, func(context.Context) error {
		return api.ErrLeaseNotActive
	})
	if err != nil {
		t.Fatalf("pulseLeaseHeartbeat() error = %v, want nil after cancel", err)
	}
}

func TestLeaseRenewalPulseSkipsUntilExpiryWouldAdvance(t *testing.T) {
	var pulses atomic.Int32
	expires := time.Now().UTC().Add(time.Hour)
	pulse := leaseRenewalPulse(expires, time.Minute, func(context.Context) error {
		pulses.Add(1)
		return nil
	})
	if err := pulse(context.Background()); err != nil {
		t.Fatalf("pulse() error = %v", err)
	}
	if got := pulses.Load(); got != 0 {
		t.Fatalf("pulses = %d, want 0 while remaining expiry exceeds TTL", got)
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
