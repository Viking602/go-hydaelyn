package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/scripted"
)

func TestSingleRunnerStartExecuteSettlesDurableLifecycle(t *testing.T) {
	now := time.Date(2026, time.August, 4, 14, 0, 0, 0, time.UTC)
	governance := api.GovernancePolicy{
		Budget: api.Budget{
			MaxTokens: 500, MaxToolCalls: 8,
			MaxRuntime: time.Hour, MaxModelCalls: 3,
		},
		MaxConcurrentRuns: 1,
	}
	coordinator := newSingleRunnerTestCoordinator(now, scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 4, OutputTokens: 2, TotalTokens: 6}},
	}), governance)
	coordinator.Worker.UsagePricer = func(context.Context, api.UsageRecord) (int64, string, error) {
		return 7, "test-credit", nil
	}

	started, err := coordinator.Start(context.Background(), StartSingleRunRequest{
		RunID: "single-success", Request: "finish the task", WriteTargets: []string{"answer"},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if started.Run.Status != api.RunStatusRunning || started.Task.Status != api.TaskStatusDispatched || started.Admission.State != api.AdmissionReserved {
		t.Fatalf("started state = %#v", started)
	}
	if started.Task.Budget == nil ||
		started.Task.Budget.MaxTokens != 500 ||
		started.Task.Budget.MaxToolCalls != 8 ||
		started.Task.Budget.MaxWallClock != time.Hour ||
		started.Task.Budget.MaxSteps != 3 {
		t.Fatalf("derived task budget = %#v", started.Task.Budget)
	}

	var observedLease api.TaskExecutionLease
	result, err := coordinator.Execute(context.Background(), ExecuteSingleRunRequest{
		RunID: started.Run.ID,
		OnLeaseAcquired: func(lease api.TaskExecutionLease) error {
			observedLease = lease
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Execution.State != ExecutionCompleted || result.Run.Status != api.RunStatusCompleted || result.Task.Status != api.TaskStatusCompleted {
		t.Fatalf("completed result = %#v", result)
	}
	if result.Admission.State != api.AdmissionSettled || result.Admission.ConsumedCredits != 7 || result.Admission.Failed {
		t.Fatalf("settled admission = %#v", result.Admission)
	}
	if observedLease.ID == "" || observedLease.TaskID != started.Task.ID {
		t.Fatalf("observed lease = %#v", observedLease)
	}
}

func TestSingleRunnerExecuteUsesTransientEngineOverride(t *testing.T) {
	now := time.Date(2026, time.August, 4, 14, 30, 0, 0, time.UTC)
	coordinator := newSingleRunnerTestCoordinator(now, scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "stale"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), api.GovernancePolicy{})
	started, err := coordinator.Start(context.Background(), StartSingleRunRequest{
		RunID: "single-engine-override", Request: "use rebuilt engine",
	})
	if err != nil {
		t.Fatal(err)
	}
	override := agent.Engine{
		Provider: scripted.New([]provider.Event{
			{Kind: provider.EventTextDelta, Text: "rebuilt"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}),
		Model: "rebuilt-model",
	}
	result, err := coordinator.Execute(context.Background(), ExecuteSingleRunRequest{
		RunID: started.Run.ID, Engine: &override,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Execution.Result.Text != "rebuilt" {
		t.Fatalf("result text=%q", result.Execution.Result.Text)
	}
}

func TestSingleRunnerStartRetryReturnsExistingDurableState(t *testing.T) {
	ctx := context.Background()
	coordinator := newSingleRunnerTestCoordinator(
		time.Date(2026, time.August, 4, 14, 45, 0, 0, time.UTC),
		scripted.New(nil),
		api.GovernancePolicy{},
	)
	request := StartSingleRunRequest{
		RunID: "single-start-idempotent", Request: "start exactly once",
		Goal: "preserve this task", Tags: []string{"durable"},
	}
	first, err := coordinator.Start(ctx, request)
	if err != nil {
		t.Fatalf("Start(first) error = %v", err)
	}
	eventsBefore, err := coordinator.Runner.ListEvents(ctx, request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	envelopesBefore, err := coordinator.Runner.ListEnvelopes(ctx, request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.Start(ctx, request)
	if err != nil {
		t.Fatalf("Start(retry) error = %v", err)
	}
	eventsAfter, err := coordinator.Runner.ListEvents(ctx, request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	envelopesAfter, err := coordinator.Runner.ListEnvelopes(ctx, request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Run.ID != first.Run.ID || second.Task.ID != first.Task.ID ||
		second.Envelope.ID != first.Envelope.ID {
		t.Fatalf("retry state = %#v, want existing %#v", second, first)
	}
	if len(eventsAfter) != len(eventsBefore) || len(envelopesAfter) != len(envelopesBefore) {
		t.Fatalf(
			"retry appended durable records: events %d->%d envelopes %d->%d",
			len(eventsBefore),
			len(eventsAfter),
			len(envelopesBefore),
			len(envelopesAfter),
		)
	}
	changed := request
	changed.Goal = "replace the durable task"
	if _, err := coordinator.Start(ctx, changed); !errors.Is(err, api.ErrIdempotencyConflict) {
		t.Fatalf("Start(conflicting retry) error = %v, want ErrIdempotencyConflict", err)
	}
	coordinator.AgentVersion = "v2"
	if _, err := coordinator.Start(ctx, request); !errors.Is(err, api.ErrIdempotencyConflict) {
		t.Fatalf("Start(version conflict) error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestSingleRunnerStartRepairsRunCreatedBeforeTask(t *testing.T) {
	ctx := context.Background()
	coordinator := newSingleRunnerTestCoordinator(
		time.Date(2026, time.August, 4, 14, 50, 0, 0, time.UTC),
		scripted.New(nil),
		api.GovernancePolicy{},
	)
	request := StartSingleRunRequest{
		RunID: "single-start-repair", RootTaskID: "single-start-repair-root",
		TaskID: "single-start-repair-task", Request: "repair partial start",
	}
	metadata, err := coordinator.startMetadata(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.Runner.StartRun(ctx, api.StartRunCommand{
		RunID: request.RunID, RootTaskID: request.RootTaskID,
		Request: request.Request, AgentVersion: coordinator.AgentVersion, Metadata: metadata,
	}); err != nil {
		t.Fatalf("StartRun(partial) error = %v", err)
	}
	state, err := coordinator.Start(ctx, request)
	if err != nil {
		t.Fatalf("Start(repair) error = %v", err)
	}
	if state.Run.Status != api.RunStatusRunning ||
		state.Task.Status != api.TaskStatusDispatched ||
		state.Envelope.ID == "" {
		t.Fatalf("repaired state = %#v", state)
	}
	events, err := coordinator.Runner.ListEvents(ctx, request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	runStarted := 0
	for _, event := range events {
		if event.Type == api.EventRunStarted {
			runStarted++
		}
	}
	if runStarted != 1 {
		t.Fatalf("RunStarted events = %d, want 1", runStarted)
	}
}

func TestSingleRunnerConcurrentStartLoserDoesNotFailWinnerRun(t *testing.T) {
	ctx := context.Background()
	coordinator := newSingleRunnerTestCoordinator(
		time.Date(2026, time.August, 4, 14, 55, 0, 0, time.UTC),
		scripted.New(nil),
		api.GovernancePolicy{MaxConcurrentRuns: 1},
	)
	request := StartSingleRunRequest{
		RunID: "single-concurrent-start", RootTaskID: "single-concurrent-start-root",
		TaskID: "single-concurrent-start-task", Request: "start concurrently",
	}
	metadata, err := coordinator.startMetadata(request)
	if err != nil {
		t.Fatal(err)
	}
	winnerAdmission, err := coordinator.reserveAdmission(ctx, request.RunID)
	if err != nil {
		t.Fatalf("winner reserveAdmission() error = %v", err)
	}
	if _, _, err := coordinator.Runner.StartRun(ctx, api.StartRunCommand{
		RunID: request.RunID, RootTaskID: request.RootTaskID,
		Request: request.Request, AgentVersion: coordinator.AgentVersion, Metadata: metadata,
	}); err != nil {
		t.Fatalf("winner StartRun() error = %v", err)
	}
	loserAdmission, err := coordinator.reserveAdmission(ctx, request.RunID)
	if err != nil {
		t.Fatalf("loser reserveAdmission() error = %v", err)
	}
	if loserAdmission.ID != winnerAdmission.ID {
		t.Fatalf("loser admission = %q, want shared reservation %q", loserAdmission.ID, winnerAdmission.ID)
	}

	_, _, created, err := coordinator.runAndRootForStart(ctx, request, metadata, api.Run{}, false)
	if err != nil {
		t.Fatalf("loser runAndRootForStart() error = %v", err)
	}
	if created {
		t.Fatal("losing start reported ownership of the winner's run")
	}
	cleanupReservation := startOwnedReservation(loserAdmission, created)
	cause := errors.New("losing setup was cancelled")
	if err := coordinator.abortStart(ctx, request.RunID, created, cleanupReservation, cause); !errors.Is(err, cause) {
		t.Fatalf("abortStart() error = %v, want original cause", err)
	}
	run, err := coordinator.Runner.Run(ctx, request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != api.RunStatusCreated {
		t.Fatalf("winner run status = %q, want created", run.Status)
	}
	admission, err := coordinator.loadAdmission(ctx, request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if admission.State != api.AdmissionReserved {
		t.Fatalf("winner admission state = %q, want reserved", admission.State)
	}
}

func TestSingleRunnerAdmissionDenialDoesNotCreateRun(t *testing.T) {
	now := time.Date(2026, time.August, 4, 15, 0, 0, 0, time.UTC)
	governance := api.GovernancePolicy{MaxConcurrentRuns: 1}
	coordinator := newSingleRunnerTestCoordinator(now, scripted.New(nil), governance)
	if _, err := coordinator.Start(context.Background(), StartSingleRunRequest{RunID: "single-held", Request: "hold"}); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	_, err := coordinator.Start(context.Background(), StartSingleRunRequest{RunID: "single-denied", Request: "deny"})
	var denied *AdmissionDeniedError
	if !errors.As(err, &denied) || denied.Reason != api.AdmissionDeniedConcurrency {
		t.Fatalf("second Start() error = %v, want concurrency AdmissionDeniedError", err)
	}
	if _, loadErr := coordinator.Runner.Run(context.Background(), "single-denied"); !errors.Is(loadErr, api.ErrNotFound) {
		t.Fatalf("denied run load error = %v, want ErrNotFound", loadErr)
	}
}

func TestSingleRunnerRejectsUnsupportedActionCallBudget(t *testing.T) {
	coordinator := newSingleRunnerTestCoordinator(
		time.Date(2026, time.August, 4, 15, 5, 0, 0, time.UTC),
		scripted.New(nil),
		api.GovernancePolicy{Budget: api.Budget{MaxActionCalls: 1}},
	)
	_, err := coordinator.Start(context.Background(), StartSingleRunRequest{
		RunID: "single-action-budget", Request: "do not ignore the budget",
	})
	if !errors.Is(err, ErrSingleRunInvalid) {
		t.Fatalf("Start() error = %v, want ErrSingleRunInvalid", err)
	}
}

func TestSingleRunnerRejectsUnsupportedCreditBudget(t *testing.T) {
	coordinator := newSingleRunnerTestCoordinator(
		time.Date(2026, time.August, 4, 15, 6, 0, 0, time.UTC),
		scripted.New(nil),
		api.GovernancePolicy{Budget: api.Budget{MaxCredits: 1}},
	)
	_, err := coordinator.Start(context.Background(), StartSingleRunRequest{
		RunID: "single-credit-budget", Request: "do not ignore the credit budget",
	})
	if !errors.Is(err, ErrSingleRunInvalid) {
		t.Fatalf("Start() error = %v, want ErrSingleRunInvalid", err)
	}
}

func TestSingleRunBudgetIntersectsCallerAndGovernance(t *testing.T) {
	requested := &api.TaskBudget{
		MaxTokens: 1000, MaxWallClock: 2 * time.Hour, MaxToolCalls: 2, MaxSteps: 10,
	}
	got := singleRunBudget(requested, api.Budget{
		MaxTokens: 500, MaxRuntime: time.Hour, MaxToolCalls: 8, MaxModelCalls: 3,
	})
	if got == nil || got == requested {
		t.Fatalf("singleRunBudget() = %#v, want an independent budget", got)
	}
	requested.MaxTokens = 1
	requested.MaxWallClock = time.Minute
	requested.MaxToolCalls = 1
	requested.MaxSteps = 1
	if got.MaxTokens != 500 || got.MaxWallClock != time.Hour || got.MaxToolCalls != 2 || got.MaxSteps != 3 {
		t.Fatalf("singleRunBudget() = %#v, want governance intersection", got)
	}

	for _, requested := range []*api.TaskBudget{nil, {}} {
		got = singleRunBudget(requested, api.Budget{
			MaxTokens: 500, MaxRuntime: time.Hour, MaxToolCalls: 8, MaxModelCalls: 3,
		})
		if got == nil || got.MaxTokens != 500 || got.MaxWallClock != time.Hour ||
			got.MaxToolCalls != 8 || got.MaxSteps != 3 {
			t.Fatalf("unbounded request budget = %#v, want governance ceilings", got)
		}
	}
	got = singleRunBudget(&api.TaskBudget{
		MaxTokens: 100, MaxWallClock: time.Minute, MaxToolCalls: 1, MaxSteps: 2,
	}, api.Budget{
		MaxTokens: 500, MaxRuntime: time.Hour, MaxToolCalls: 8, MaxModelCalls: 3,
	})
	if got == nil || got.MaxTokens != 100 || got.MaxWallClock != time.Minute ||
		got.MaxToolCalls != 1 || got.MaxSteps != 2 {
		t.Fatalf("narrow request budget = %#v, want caller ceilings", got)
	}
	if got := singleRunBudget(nil, api.Budget{}); got != nil {
		t.Fatalf("unbounded budget = %#v, want nil", got)
	}
}

func TestSingleRunnerExpiresOverdueAdmissionBeforeResume(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 4, 15, 10, 0, 0, time.UTC)
	driver := &recordingProvider{}
	coordinator := newSingleRunnerTestCoordinator(
		now,
		driver,
		api.GovernancePolicy{MaxConcurrentRuns: 1},
	)
	started, err := coordinator.Start(ctx, StartSingleRunRequest{
		RunID: "single-expired-admission", Request: "resume only with valid admission",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	coordinator.Now = func() time.Time { return started.Admission.ExpiresAt.Add(time.Second) }
	if _, err := coordinator.Resume(ctx, started.Run.ID); !errors.Is(err, ErrSingleRunNotResumable) {
		t.Fatalf("Resume() error = %v, want ErrSingleRunNotResumable", err)
	}
	expired, err := coordinator.Runner.LoadAdmissionReservation(ctx, started.Admission.ID)
	if err != nil {
		t.Fatal(err)
	}
	if expired.State != api.AdmissionExpired {
		t.Fatalf("admission state = %q, want expired", expired.State)
	}
	if _, err := coordinator.Execute(ctx, ExecuteSingleRunRequest{RunID: started.Run.ID}); !errors.Is(err, ErrSingleRunNotResumable) {
		t.Fatalf("Execute() error = %v, want ErrSingleRunNotResumable", err)
	}
	if len(driver.requests) != 0 {
		t.Fatalf("provider requests = %d, want no execution under expired admission", len(driver.requests))
	}
}

func TestSingleRunnerReportTerminalizesSetupFailure(t *testing.T) {
	now := time.Date(2026, time.August, 4, 15, 30, 0, 0, time.UTC)
	coordinator := newSingleRunnerTestCoordinator(now, scripted.New(nil), api.GovernancePolicy{
		MaxConcurrentRuns: 1,
	})
	started, err := coordinator.Start(context.Background(), StartSingleRunRequest{
		RunID: "single-setup-failure", Request: "fail during setup",
	})
	if err != nil {
		t.Fatal(err)
	}
	reported, err := coordinator.Report(context.Background(), started.Run.ID, api.TypedReport{
		Status: api.ReportStatusFailed, Kind: "setup_error", Summary: "provider binding failed",
	})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if reported.Run.Status != api.RunStatusFailed || reported.Task.Status != api.TaskStatusFailed {
		t.Fatalf("reported state = %#v", reported)
	}
	if reported.Admission.State != api.AdmissionReleased {
		t.Fatalf("reported admission = %#v, want released", reported.Admission)
	}
}

func TestSingleRunnerSuspendResumePreservesExecution(t *testing.T) {
	now := time.Date(2026, time.August, 4, 16, 0, 0, 0, time.UTC)
	driver := newSuspendThenCompleteProvider()
	coordinator := newSingleRunnerTestCoordinator(now, driver, api.GovernancePolicy{MaxConcurrentRuns: 1})
	started, err := coordinator.Start(context.Background(), StartSingleRunRequest{RunID: "single-suspend", Request: "pause safely"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	type executionResponse struct {
		result SingleRunResult
		err    error
	}
	completed := make(chan executionResponse, 1)
	go func() {
		result, executeErr := coordinator.Execute(context.Background(), ExecuteSingleRunRequest{RunID: started.Run.ID})
		completed <- executionResponse{result: result, err: executeErr}
	}()
	select {
	case <-driver.started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider execution did not start")
	}
	if err := coordinator.Suspend(context.Background(), started.Run.ID); err != nil {
		t.Fatalf("Suspend() error = %v", err)
	}
	response := <-completed
	if !errors.Is(response.err, ErrSingleRunSuspended) || response.result.Execution.State != ExecutionSuspended {
		t.Fatalf("suspended execution result=%#v error=%v", response.result, response.err)
	}
	paused, err := coordinator.load(context.Background(), started.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Run.Status != api.RunStatusBlocked || paused.Task.Status != api.TaskStatusPaused || paused.Admission.State != api.AdmissionSuspended {
		t.Fatalf("paused state = %#v", paused)
	}

	resumed, err := coordinator.Resume(context.Background(), started.Run.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Run.Status != api.RunStatusRunning || resumed.Task.Status != api.TaskStatusDispatched || resumed.Envelope.ID == "" {
		t.Fatalf("resumed state = %#v", resumed)
	}
	finished, err := coordinator.Execute(context.Background(), ExecuteSingleRunRequest{RunID: started.Run.ID})
	if err != nil {
		t.Fatalf("resumed Execute() error = %v", err)
	}
	if finished.Run.Status != api.RunStatusCompleted || finished.Admission.State != api.AdmissionSettled {
		t.Fatalf("finished resumed state = %#v", finished)
	}
}

func TestSingleRunnerCancelTerminalizesActiveExecution(t *testing.T) {
	now := time.Date(2026, time.August, 4, 17, 0, 0, 0, time.UTC)
	driver := newSuspendThenCompleteProvider()
	coordinator := newSingleRunnerTestCoordinator(now, driver, api.GovernancePolicy{MaxConcurrentRuns: 1})
	started, err := coordinator.Start(context.Background(), StartSingleRunRequest{RunID: "single-cancel", Request: "cancel me"})
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() {
		_, executeErr := coordinator.Execute(context.Background(), ExecuteSingleRunRequest{RunID: started.Run.ID})
		completed <- executeErr
	}()
	select {
	case <-driver.started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider execution did not start")
	}
	if err := coordinator.Cancel(context.Background(), started.Run.ID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if executeErr := <-completed; !errors.Is(executeErr, ErrSingleRunCancelled) {
		t.Fatalf("Execute() error = %v, want ErrSingleRunCancelled", executeErr)
	}
	cancelled, err := coordinator.load(context.Background(), started.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Run.Status != api.RunStatusCancelled || cancelled.Task.Status != api.TaskStatusCancelled || cancelled.Admission.State != api.AdmissionSettled {
		t.Fatalf("cancelled state = %#v", cancelled)
	}
}

func TestSingleRunnerRefusesToInterruptForeignLease(t *testing.T) {
	now := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	coordinator := newSingleRunnerTestCoordinator(now, scripted.New(nil), api.GovernancePolicy{})
	started, err := coordinator.Start(context.Background(), StartSingleRunRequest{RunID: "single-foreign", Request: "owned elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	if _, acquired, acquireErr := coordinator.Runner.AcquireTaskExecution(context.Background(), api.AcquireTaskExecutionCommand{
		RunID: started.Run.ID, TaskID: started.Task.ID, EnvelopeID: started.Envelope.ID,
		HolderType: api.HolderAgent, HolderID: "single-agent", TTL: time.Minute,
	}); acquireErr != nil || !acquired {
		t.Fatalf("foreign acquire acquired=%t error=%v", acquired, acquireErr)
	}
	if err := coordinator.Suspend(context.Background(), started.Run.ID); !errors.Is(err, ErrSingleRunNotOwned) {
		t.Fatalf("Suspend() error = %v, want ErrSingleRunNotOwned", err)
	}
}

func newSingleRunnerTestCoordinator(now time.Time, driver provider.Driver, governance api.GovernancePolicy) *SingleRunner {
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "single-agent", Role: "test"})
	worker := AgentWorker{
		Runner: runner, Engine: agent.Engine{Provider: driver, Model: "test-model"},
		AgentID: "single-agent", Model: "test-model", TTL: time.Minute,
	}
	return &SingleRunner{
		Runner: runner, Worker: worker,
		Admission:    StandardAdmissionController{Runner: runner, Now: func() time.Time { return now }},
		AgentVersion: "v1", Governance: governance, Now: func() time.Time { return now },
	}
}

type suspendThenCompleteProvider struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
}

func newSuspendThenCompleteProvider() *suspendThenCompleteProvider {
	return &suspendThenCompleteProvider{started: make(chan struct{})}
}

func (p *suspendThenCompleteProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "suspend-then-complete", Models: []string{"test-model"}}
}

func (p *suspendThenCompleteProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	p.mu.Lock()
	call := p.calls
	p.calls++
	if call == 0 {
		close(p.started)
	}
	p.mu.Unlock()
	if call == 0 {
		return &cancelAwareStream{ctx: ctx}, nil
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "resumed"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

type cancelAwareStream struct {
	ctx context.Context
}

func (s *cancelAwareStream) Recv() (provider.Event, error) {
	<-s.ctx.Done()
	return provider.Event{}, s.ctx.Err()
}

func (*cancelAwareStream) Close() error { return nil }

var (
	_ provider.Driver = (*suspendThenCompleteProvider)(nil)
	_ provider.Stream = (*cancelAwareStream)(nil)
)
