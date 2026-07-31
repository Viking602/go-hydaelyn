package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/multiagent"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/scripted"
	"github.com/Viking602/venat/worker"
)

// RecoveryProviderFactory creates an isolated backing store for one recovery
// contract case. Every call to open must return a provider handle exposing the
// same committed state. cleanup releases the backing store after the case.
type RecoveryProviderFactory func(t *testing.T) (
	open func() api.StoreProvider,
	cleanup func(),
)

// RunRecoveryContractTests proves runtime-loss recovery through separate
// production Runner instances sharing one committed StoreProvider state.
func RunRecoveryContractTests(t *testing.T, factory RecoveryProviderFactory) {
	t.Helper()
	if factory == nil {
		t.Fatal("contract: nil RecoveryProviderFactory")
	}
	t.Run("Recovery", func(t *testing.T) {
		cases := []struct {
			name string
			run  func(*testing.T, func() api.StoreProvider)
		}{
			{"TestResume_ReconstructsRunState", testResumeReconstructsRunState},
			{"TestResume_ReconstructsStepTrace", testResumeReconstructsStepTrace},
			{"TestResume_ReconstructsSchedulerDecisions", testResumeReconstructsSchedulerDecisions},
			{"TestResume_AllThreeSurfacesAfterKill", testResumeAllThreeSurfacesAfterKill},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				open, cleanup := factory(t)
				if open == nil {
					t.Fatal("contract: recovery factory returned nil open function")
				}
				if cleanup == nil {
					t.Fatal("contract: recovery factory returned nil cleanup function")
				}
				t.Cleanup(cleanup)
				tc.run(t, open)
			})
		}
	})
}

type recoveryAllowPolicy struct{}

func (recoveryAllowPolicy) Authorize(context.Context, api.PolicyRequest) (api.PolicyDecision, error) {
	return api.PolicyDecision{Effect: api.PolicyEffectAllow}, nil
}

func openRecoveryRunner(t *testing.T, open func() api.StoreProvider) *venat.Runner {
	t.Helper()
	p := open()
	if p == nil {
		t.Fatal("contract: recovery open returned nil provider")
	}
	runner, err := venat.NewProduction(api.Config{StoreProvider: p, PolicyEngine: recoveryAllowPolicy{}})
	if err != nil {
		t.Fatalf("contract: NewProduction() error = %v", err)
	}
	reporter, ok := p.(api.CapabilityReporter)
	if !ok {
		t.Fatal("contract: recovery provider does not advertise transaction support")
	}
	caps, err := reporter.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("contract: Capabilities() error = %v", err)
	}
	if !caps.SupportsTransactions {
		t.Fatal("contract: recovery provider must advertise transaction support")
	}
	return runner
}

func testResumeReconstructsRunState(t *testing.T, open func() api.StoreProvider) {
	runtimeA, runtimeB := testExpiredExecutionRecovery(t, open)
	testUnresolvedActionRecovery(t, runtimeA, runtimeB)
}

func testExpiredExecutionRecovery(t *testing.T, open func() api.StoreProvider) (*venat.Runner, *venat.Runner) {
	t.Helper()
	ctx := context.Background()
	runtimeA := openRecoveryRunner(t, open)
	run, _, err := runtimeA.StartRun(ctx, api.StartRunCommand{RunID: "recovery-run-state", RootTaskID: "root", Request: "recover work"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runtimeA.CreateTask(ctx, api.CreateTaskCommand{RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runtimeA.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	lease, acquired, err := runtimeA.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{RunID: run.ID, TaskID: task.ID, EnvelopeID: envelope.ID, HolderType: api.HolderAgent, HolderID: "agent-a", TTL: time.Hour})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v err=%v", lease, acquired, err)
	}
	expireRecoveryLease(t, runtimeA, lease.ID)

	runtimeB := openRecoveryRunner(t, open)
	projection, err := runtimeB.Recover(ctx, run.ID)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if got := projection.Tasks[task.ID].Status; got != api.TaskStatusDispatched {
		t.Fatalf("recovered projection task status = %q, want %q", got, api.TaskStatusDispatched)
	}
	recovered, err := runtimeB.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if recovered.Status != api.TaskStatusDispatched {
		t.Fatalf("recovered task status = %q, want %q", recovered.Status, api.TaskStatusDispatched)
	}
	if active := runtimeB.ActiveLeaseCountContext(ctx, run.ID, task.ID); active != 0 {
		t.Fatalf("Recover() left %d active leases, want 0", active)
	}
	events := recoveryEvents(t, runtimeB, run.ID)
	version, eventCount := recovered.Version, len(events)
	if _, err := runtimeB.Recover(ctx, run.ID); err != nil {
		t.Fatalf("Recover(second) error = %v", err)
	}
	recoveredAgain, err := runtimeB.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task(after second Recover) error = %v", err)
	}
	if recoveredAgain.Version != version {
		t.Fatalf("second Recover changed task version from %d to %d", version, recoveredAgain.Version)
	}
	if got := len(recoveryEvents(t, runtimeB, run.ID)); got != eventCount {
		t.Fatalf("second Recover changed event count from %d to %d", eventCount, got)
	}
	return runtimeA, runtimeB
}

func testUnresolvedActionRecovery(t *testing.T, actionA, actionB *venat.Runner) {
	t.Helper()
	ctx := context.Background()
	actionRun, _, err := actionA.StartRun(ctx, api.StartRunCommand{RunID: "recovery-action-state", RootTaskID: "root-action", Request: "perform action"})
	if err != nil {
		t.Fatalf("StartRun(action) error = %v", err)
	}
	saveRecoveryTeamState(t, actionA, multiagent.TeamState{RunID: actionRun.ID})
	actionTask, err := actionA.CreateTask(ctx, api.CreateTaskCommand{RunID: actionRun.ID, TaskID: "action", OwnerAgentID: "agent-action", AllowsAction: true})
	if err != nil {
		t.Fatalf("CreateTask(action) error = %v", err)
	}
	actionEnvelope, err := actionA.DispatchTask(ctx, api.DispatchTaskCommand{RunID: actionRun.ID, TaskID: actionTask.ID, TargetAgentID: "agent-action"})
	if err != nil {
		t.Fatalf("DispatchTask(action) error = %v", err)
	}
	actionLease, acquired, err := actionA.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{RunID: actionRun.ID, TaskID: actionTask.ID, EnvelopeID: actionEnvelope.ID, HolderType: api.HolderAgent, HolderID: "agent-action", TTL: time.Hour})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution(action) lease=%#v acquired=%v err=%v", actionLease, acquired, err)
	}
	if _, err := actionA.StartActionAttempt(ctx, api.StartActionAttemptCommand{RunID: actionRun.ID, TaskID: actionTask.ID, LeaseID: actionLease.ID, HolderType: api.HolderAgent, HolderID: "agent-action", TaskVersion: actionLease.TaskVersion, ToolName: "deploy"}); err != nil {
		t.Fatalf("StartActionAttempt() error = %v", err)
	}
	actionEventsBefore := countRecoveryEvents(recoveryEvents(t, actionA, actionRun.ID), api.EventActionAttemptStarted)
	expireRecoveryLease(t, actionA, actionLease.ID)
	actionProjection, err := actionB.Recover(ctx, actionRun.ID)
	if err != nil {
		t.Fatalf("Recover(action) error = %v", err)
	}
	if got := actionProjection.Tasks[actionTask.ID].Status; got != api.TaskStatusReconcileRequired {
		t.Fatalf("action projection task status = %q, want %q", got, api.TaskStatusReconcileRequired)
	}
	recoveredRun, err := actionB.Run(ctx, actionRun.ID)
	if err != nil {
		t.Fatalf("Run(action) error = %v", err)
	}
	if recoveredRun.Status != api.RunStatusReconcileRequired {
		t.Fatalf("action run status = %q, want %q", recoveredRun.Status, api.RunStatusReconcileRequired)
	}
	if got := countRecoveryEvents(recoveryEvents(t, actionB, actionRun.ID), api.EventActionAttemptStarted); got != actionEventsBefore {
		t.Fatalf("action attempt replayed: before=%d after=%d", actionEventsBefore, got)
	}
	for _, candidate := range recoveryEnvelopes(t, actionB, actionRun.ID) {
		if candidate.TaskID == actionTask.ID && candidate.Status == "pending" {
			t.Fatalf("unresolved action was redispatched: %#v", candidate)
		}
	}
	schedulerCalls := 0
	_, err = (worker.TeamRunner{Runner: actionB, Team: multiagent.Team{Scheduler: multiagent.SchedulerFunc(func(context.Context, multiagent.TeamState) ([]multiagent.Dispatch, error) {
		schedulerCalls++
		return nil, nil
	})}}).Resume(ctx, actionRun.ID)
	if !errors.Is(err, api.ErrActionReconcileRequired) {
		t.Fatalf("Resume(action) error = %v, want ErrActionReconcileRequired", err)
	}
	if schedulerCalls != 0 {
		t.Fatalf("scheduler ran %d times for quarantined action", schedulerCalls)
	}
}

func testResumeReconstructsStepTrace(t *testing.T, open func() api.StoreProvider) {
	ctx := context.Background()
	runtimeA := openRecoveryRunner(t, open)
	runtimeA.RegisterAgent(api.AgentProfile{ID: "trace-agent"})
	run, _, err := runtimeA.StartRun(ctx, api.StartRunCommand{RunID: "recovery-step-trace", RootTaskID: "root", Request: "record a step"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runtimeA.CreateTask(ctx, api.CreateTaskCommand{RunID: run.ID, TaskID: "trace-task", Goal: "finish", OwnerAgentID: "trace-agent"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runtimeA.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "trace-agent"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	driver := scripted.New(recoveryScript())
	if err := (worker.AgentWorker{Runner: runtimeA, Engine: agent.Engine{Provider: driver}, AgentID: "trace-agent", Model: "scripted", TTL: time.Hour}).ExecuteEnvelope(ctx, worker.ExecuteEnvelopeRequest{Envelope: envelope, TTL: time.Hour}); err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}
	eventsA := recoveryEvents(t, runtimeA, run.ID)
	expected, err := agent.ReconstructStepTrace(eventsA, agent.StepSelector{RunID: run.ID, TaskID: task.ID, AgentID: "trace-agent"})
	if err != nil {
		t.Fatalf("ReconstructStepTrace(runtime A) error = %v", err)
	}
	if len(expected) != 1 || expected[0].ExecutionID == "" {
		t.Fatalf("runtime A step records = %#v, want one record with execution ID", expected)
	}
	leaseID := acquiredLeaseID(eventsA, task.ID)
	if expected[0].ExecutionID != leaseID {
		t.Fatalf("step execution ID = %q, original lease ID = %q", expected[0].ExecutionID, leaseID)
	}

	runtimeB := openRecoveryRunner(t, open)
	actual, err := agent.ReconstructStepTrace(recoveryEvents(t, runtimeB, run.ID), agent.StepSelector{RunID: run.ID, TaskID: task.ID, AgentID: "trace-agent"})
	if err != nil {
		t.Fatalf("ReconstructStepTrace(runtime B) error = %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("runtime B step trace = %#v, want %#v", actual, expected)
	}
	grouped, err := agent.ReconstructStepTrace(recoveryEvents(t, runtimeB, run.ID), agent.StepSelector{ExecutionID: leaseID})
	if err != nil {
		t.Fatalf("ReconstructStepTrace(original lease) error = %v", err)
	}
	if !reflect.DeepEqual(grouped, expected) {
		t.Fatalf("original-lease trace = %#v, want %#v", grouped, expected)
	}
}

func testResumeReconstructsSchedulerDecisions(t *testing.T, open func() api.StoreProvider) {
	ctx := context.Background()
	runtimeA := openRecoveryRunner(t, open)
	driver := newCountingScriptedDriver()
	frozen := startFrozenRecoveryTeam(t, runtimeA, driver, "recovery-scheduler")
	if got := driver.Count(); got != 1 {
		t.Fatalf("scripted stream count at checkpoint = %d, want 1", got)
	}
	checkpoint, instances := loadRecoveryTeamState(t, runtimeA, frozen.runID)
	assertTickOneCheckpoint(t, checkpoint, instances, frozen.first.Name)
	if got := countRecoveryEvents(recoveryEvents(t, runtimeA, frozen.runID), multiagent.EventSchedulerTick); got != 1 {
		t.Fatalf("scheduler tick events at checkpoint = %d, want 1", got)
	}
	frozen.cancel()
	runtimeB := openRecoveryRunner(t, open)
	expireActiveRecoveryLease(t, runtimeB, frozen.runID, frozen.rootTaskID)
	result, err := frozen.teamRunner(runtimeB).Resume(ctx, frozen.runID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if driver.Count() != 2 {
		t.Fatalf("scripted stream count after resume = %d, want exactly 2", driver.Count())
	}
	if result.Ticks != 2 || len(result.State.Instances) != 2 || result.State.Instances[0].ClassName != frozen.first.Name || result.State.Instances[1].ClassName != frozen.second.Name {
		t.Fatalf("resumed scheduler state = %#v, want loaded class 1 and executed class 2", result.State)
	}
	checkpoint, instances = loadRecoveryTeamState(t, runtimeB, frozen.runID)
	if checkpoint.Tick != 2 || len(checkpoint.Instances) != 2 || len(instances) != 2 {
		t.Fatalf("final checkpoint tick=%d instances=%d persisted=%d, want 2/2/2", checkpoint.Tick, len(checkpoint.Instances), len(instances))
	}
	if got := countRecoveryEvents(recoveryEvents(t, runtimeB, frozen.runID), multiagent.EventSchedulerTick); got != 2 {
		t.Fatalf("scheduler tick events after resume = %d, want exactly 2 (not 3)", got)
	}
	completed, err := runtimeB.Run(ctx, frozen.runID)
	if err != nil || completed.Status != api.RunStatusCompleted {
		t.Fatalf("resumed run = %#v err=%v, want completed", completed, err)
	}
	frozen.release()
	if stale := waitFrozenResult(t, frozen.results); stale.err == nil {
		t.Fatal("abandoned runtime A unexpectedly finalized without a stale-lease error")
	}
}

func testResumeAllThreeSurfacesAfterKill(t *testing.T, open func() api.StoreProvider) {
	runtimeA := openRecoveryRunner(t, open)
	driver := newCountingScriptedDriver()
	frozen := startFrozenRecoveryTeam(t, runtimeA, driver, "recovery-all-surfaces")
	frozen.cancel()

	runtimeB := openRecoveryRunner(t, open)
	checkpoint := assertRecoverySurfacesAfterKill(t, runtimeB, frozen, driver)
	guardCompletedRecoveryTrace(t, runtimeB, frozen, driver, checkpoint)
	resumeAllRecoverySurfaces(t, runtimeB, frozen, driver, checkpoint)
	assertStaleRecoveryRuntimeCannotFinalize(t, runtimeB, frozen)
}

type recoveryKillCheckpoint struct {
	firstExecutionID string
	trace            []agent.StepRecord
	streamCount      int
}

func assertRecoverySurfacesAfterKill(
	t *testing.T,
	runtimeB *venat.Runner,
	frozen frozenRecoveryTeam,
	driver *countingScriptedDriver,
) recoveryKillCheckpoint {
	t.Helper()
	ctx := context.Background()
	projection, err := runtimeB.ReplayRunStateContext(ctx, frozen.runID)
	if err != nil {
		t.Fatalf("ReplayRunStateContext() error = %v", err)
	}
	firstTaskID := frozen.runID + "-" + frozen.first.Name
	if got := projection.Tasks[firstTaskID].Status; got != api.TaskStatusCompleted {
		t.Fatalf("replayed class-1 task status = %q, want %q", got, api.TaskStatusCompleted)
	}
	preKillEvents := recoveryEvents(t, runtimeB, frozen.runID)
	preKillTrace, err := agent.ReconstructStepTrace(preKillEvents, agent.StepSelector{RunID: frozen.runID, TaskID: firstTaskID})
	if err != nil {
		t.Fatalf("ReconstructStepTrace(pre-kill) error = %v", err)
	}
	if len(preKillTrace) != 1 || preKillTrace[0].ExecutionID == "" {
		t.Fatalf("pre-kill step trace = %#v, want one completed step", preKillTrace)
	}
	checkpoint, instances := loadRecoveryTeamState(t, runtimeB, frozen.runID)
	assertTickOneCheckpoint(t, checkpoint, instances, frozen.first.Name)
	if got := countRecoveryEvents(preKillEvents, multiagent.EventSchedulerTick); got != 1 {
		t.Fatalf("pre-kill scheduler tick events = %d, want 1", got)
	}
	preKillCalls := driver.Count()
	if preKillCalls != 1 {
		t.Fatalf("pre-kill scripted stream count = %d, want 1", preKillCalls)
	}
	return recoveryKillCheckpoint{
		firstExecutionID: preKillTrace[0].ExecutionID,
		trace:            preKillTrace,
		streamCount:      preKillCalls,
	}
}

func guardCompletedRecoveryTrace(
	t *testing.T,
	runtimeB *venat.Runner,
	frozen frozenRecoveryTeam,
	driver *countingScriptedDriver,
	checkpoint recoveryKillCheckpoint,
) {
	t.Helper()
	driver.SetBeforeStream(func(count int) error {
		if count != checkpoint.streamCount+1 {
			return fmt.Errorf("class 2 started at stream count %d, want %d", count, checkpoint.streamCount+1)
		}
		records, err := agent.ReconstructStepTrace(
			recoveryEvents(t, runtimeB, frozen.runID),
			agent.StepSelector{ExecutionID: checkpoint.firstExecutionID},
		)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(records, checkpoint.trace) {
			return fmt.Errorf("class-1 trace changed before class 2: got %#v want %#v", records, checkpoint.trace)
		}
		return nil
	})
}

func resumeAllRecoverySurfaces(
	t *testing.T,
	runtimeB *venat.Runner,
	frozen frozenRecoveryTeam,
	driver *countingScriptedDriver,
	checkpoint recoveryKillCheckpoint,
) {
	t.Helper()
	ctx := context.Background()
	expireActiveRecoveryLease(t, runtimeB, frozen.runID, frozen.rootTaskID)
	result, err := frozen.teamRunner(runtimeB).Resume(ctx, frozen.runID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if result.Ticks != 2 || len(result.State.Instances) != 2 || driver.Count() != 2 {
		t.Fatalf("resumed all-surface state=%#v streams=%d, want two ticks/instances/streams", result.State, driver.Count())
	}
	postTrace, err := agent.ReconstructStepTrace(
		recoveryEvents(t, runtimeB, frozen.runID),
		agent.StepSelector{ExecutionID: checkpoint.firstExecutionID},
	)
	if err != nil {
		t.Fatalf("ReconstructStepTrace(post-resume) error = %v", err)
	}
	if !reflect.DeepEqual(postTrace, checkpoint.trace) {
		t.Fatalf("class-1 step trace changed after resume: got %#v want %#v", postTrace, checkpoint.trace)
	}
}

func assertStaleRecoveryRuntimeCannotFinalize(
	t *testing.T,
	runtimeB *venat.Runner,
	frozen frozenRecoveryTeam,
) {
	t.Helper()
	frozen.release()
	stale := waitFrozenResult(t, frozen.results)
	if stale.err == nil {
		t.Fatal("stale runtime A unexpectedly finalized the run")
	}
	finalRun, err := runtimeB.Run(context.Background(), frozen.runID)
	if err != nil || finalRun.Status != api.RunStatusCompleted {
		t.Fatalf("run after stale A unwind = %#v err=%v, want completed", finalRun, err)
	}
	finalEvents := recoveryEvents(t, runtimeB, frozen.runID)
	if got := countTaskEvents(finalEvents, api.EventTypedReportSubmitted, frozen.rootTaskID); got != 1 {
		t.Fatalf("root task finalizations = %d, want 1; stale A must not finalize", got)
	}
	if got := countRecoveryEvents(finalEvents, multiagent.EventSchedulerTick); got != 2 {
		t.Fatalf("final scheduler tick events = %d, want 2", got)
	}
}

type countingScriptedDriver struct {
	mu           sync.Mutex
	inner        *scripted.ScriptedProvider
	streams      int
	beforeStream func(int) error
}

func newCountingScriptedDriver() *countingScriptedDriver {
	return &countingScriptedDriver{inner: scripted.New(recoveryScript())}
}

func (d *countingScriptedDriver) Metadata() provider.Metadata { return d.inner.Metadata() }

func (d *countingScriptedDriver) Stream(ctx context.Context, request provider.Request) (provider.Stream, error) {
	d.mu.Lock()
	d.streams++
	count := d.streams
	hook := d.beforeStream
	d.mu.Unlock()
	if hook != nil {
		if err := hook(count); err != nil {
			return nil, err
		}
	}
	return d.inner.Stream(ctx, request)
}

func (d *countingScriptedDriver) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.streams
}

func (d *countingScriptedDriver) SetBeforeStream(hook func(int) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.beforeStream = hook
}

type frozenRecoveryResult struct {
	result multiagent.DriveResult
	err    error
}

type frozenRecoveryTeam struct {
	runID      string
	rootTaskID string
	first      multiagent.AgentClass
	second     multiagent.AgentClass
	team       multiagent.Team
	driver     *countingScriptedDriver
	cancel     context.CancelFunc
	release    func()
	results    <-chan frozenRecoveryResult
}

func (f frozenRecoveryTeam) teamRunner(runner *venat.Runner) worker.TeamRunner {
	return worker.TeamRunner{
		Runner:    runner,
		Team:      f.team,
		BuildDeps: agent.BuildDeps{Providers: provider.Single(f.driver)},
		Options:   multiagent.DriveOptions{MaxConcurrency: 1},
		TTL:       time.Hour,
	}
}

func startFrozenRecoveryTeam(t *testing.T, runtimeA *venat.Runner, driver *countingScriptedDriver, runID string) frozenRecoveryTeam {
	t.Helper()
	ctx := context.Background()
	run, _, err := runtimeA.StartRun(ctx, api.StartRunCommand{RunID: runID, RootTaskID: "root", Request: "run two classes"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	first := multiagent.AgentClass{Name: "class-one", Instructions: "finish class one", Model: "scripted"}
	second := multiagent.AgentClass{Name: "class-two", Instructions: "finish class two", Model: "scripted"}
	team := multiagent.NewTeam("recovery-team").AddRole(first).AddRole(second).WithScheduler(multiagent.SequentialScheduler{Classes: []multiagent.AgentClass{first, second}})
	tickSaved := make(chan struct{})
	releaseA := make(chan struct{})
	var tickOnce sync.Once
	var releaseOnce sync.Once
	runCtx, cancel := context.WithCancel(ctx)
	results := make(chan frozenRecoveryResult, 1)
	frozen := frozenRecoveryTeam{
		runID: run.ID, rootTaskID: run.RootTaskID, first: first, second: second, team: *team, driver: driver, cancel: cancel,
		release: func() { releaseOnce.Do(func() { close(releaseA) }) }, results: results,
	}
	teamRunner := frozen.teamRunner(runtimeA)
	teamRunner.Options.AfterTick = func(_ context.Context, state multiagent.TeamState) error {
		if state.Tick == 1 {
			tickOnce.Do(func() { close(tickSaved) })
			<-releaseA
		}
		return nil
	}
	go func() {
		result, err := teamRunner.Start(runCtx, run.ID)
		results <- frozenRecoveryResult{result: result, err: err}
	}()
	select {
	case <-tickSaved:
	case early := <-results:
		t.Fatalf("runtime A stopped before tick-1 checkpoint: result=%#v err=%v", early.result, early.err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for tick-1 checkpoint")
	}
	return frozen
}

func waitFrozenResult(t *testing.T, results <-chan frozenRecoveryResult) frozenRecoveryResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for abandoned runtime A to unwind")
		return frozenRecoveryResult{}
	}
}

func recoveryScript() []provider.Event {
	return []provider.Event{{Kind: provider.EventTextDelta, Text: "done"}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete}}
}

func expireRecoveryLease(t *testing.T, runner *venat.Runner, leaseID string) {
	t.Helper()
	ctx := context.Background()
	uow, err := runner.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(expire lease) error = %v", err)
	}
	lease, err := uow.Leases().LoadLease(ctx, leaseID)
	if err != nil {
		_ = uow.Rollback(ctx)
		t.Fatalf("LoadLease(%q) error = %v", leaseID, err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	lease.ExpiresAt, lease.Expiry = past, past
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		_ = uow.Rollback(ctx)
		t.Fatalf("SaveLease(expired) error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit(expired lease) error = %v", err)
	}
}

func expireActiveRecoveryLease(t *testing.T, runner *venat.Runner, runID, taskID string) {
	t.Helper()
	ctx := context.Background()
	uow, err := runner.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(active lease) error = %v", err)
	}
	lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, runID, taskID)
	if err != nil {
		_ = uow.Rollback(ctx)
		t.Fatalf("ActiveLeaseForTask() error = %v", err)
	}
	if !ok {
		_ = uow.Rollback(ctx)
		t.Fatal("ActiveLeaseForTask() found no scheduler lease")
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback(active lease lookup) error = %v", err)
	}
	expireRecoveryLease(t, runner, lease.ID)
}

func saveRecoveryTeamState(t *testing.T, runner *venat.Runner, state multiagent.TeamState) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal(team state) error = %v", err)
	}
	ctx := context.Background()
	uow, err := runner.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(save team state) error = %v", err)
	}
	if err := uow.TeamStates().SaveTeamState(ctx, api.TeamStateRecord{RunID: state.RunID, Tick: state.Tick, State: raw, UpdatedAt: time.Now().UTC()}); err != nil {
		_ = uow.Rollback(ctx)
		t.Fatalf("SaveTeamState() error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit(team state) error = %v", err)
	}
}

func loadRecoveryTeamState(t *testing.T, runner *venat.Runner, runID string) (multiagent.TeamState, []api.AgentInstanceRecord) {
	t.Helper()
	ctx := context.Background()
	uow, err := runner.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(load checkpoint) error = %v", err)
	}
	record, err := uow.TeamStates().LoadTeamState(ctx, runID)
	if err != nil {
		_ = uow.Rollback(ctx)
		t.Fatalf("LoadTeamState() error = %v", err)
	}
	instances, err := uow.AgentInstances().ListAgentInstances(ctx, api.AgentInstanceSelector{RunID: runID})
	if err != nil {
		_ = uow.Rollback(ctx)
		t.Fatalf("ListAgentInstances() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback(load checkpoint) error = %v", err)
	}
	var state multiagent.TeamState
	if err := json.Unmarshal(record.State, &state); err != nil {
		t.Fatalf("Unmarshal(team state) error = %v", err)
	}
	return state, instances
}

func assertTickOneCheckpoint(t *testing.T, state multiagent.TeamState, instances []api.AgentInstanceRecord, firstClass string) {
	t.Helper()
	if state.Tick != 1 || len(state.Instances) != 1 || state.Instances[0].ClassName != firstClass || state.Instances[0].State != multiagent.InstanceStateFinished {
		t.Fatalf("tick-1 checkpoint = %#v, want one finished %q instance", state, firstClass)
	}
	if len(instances) != 1 || instances[0].ClassName != firstClass || instances[0].State != string(multiagent.InstanceStateFinished) {
		t.Fatalf("persisted tick-1 instances = %#v, want one finished %q instance", instances, firstClass)
	}
}

func recoveryEvents(t *testing.T, runner *venat.Runner, runID string) []api.Event {
	t.Helper()
	events, err := runner.ListEvents(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListEvents(%q) error = %v", runID, err)
	}
	return events
}

func recoveryEnvelopes(t *testing.T, runner *venat.Runner, runID string) []api.TaskEnvelope {
	t.Helper()
	envelopes, err := runner.ListEnvelopes(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListEnvelopes(%q) error = %v", runID, err)
	}
	return envelopes
}

func countRecoveryEvents(events []api.Event, eventType api.EventType) int {
	count := 0
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

func countTaskEvents(events []api.Event, eventType api.EventType, taskID string) int {
	count := 0
	for _, event := range events {
		if event.Type == eventType && event.TaskID == taskID {
			count++
		}
	}
	return count
}

func acquiredLeaseID(events []api.Event, taskID string) string {
	for _, event := range events {
		if event.Type == api.EventTaskExecutionAcquired && event.TaskID == taskID {
			leaseID, _ := event.Payload["leaseId"].(string)
			return leaseID
		}
	}
	return ""
}
