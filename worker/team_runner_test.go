package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/multiagent"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/scripted"
)

func TestTeamRunnerPersistsAndResumesSchedulerState(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-team",
		RootTaskID: "root",
		Request:    "run a team",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	researcher := multiagent.AgentClass{Name: "researcher", Instructions: "research", Model: "scripted"}
	writer := multiagent.AgentClass{Name: "writer", Instructions: "write", Model: "scripted"}
	team := multiagent.NewTeam("team").
		AddRole(researcher).
		AddRole(writer).
		WithScheduler(multiagent.SequentialScheduler{Classes: []multiagent.AgentClass{researcher, writer}})
	driver := scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})
	teamRunner := TeamRunner{
		Runner:    runner,
		Team:      *team,
		BuildDeps: agent.BuildDeps{Providers: provider.Single(driver)},
	}

	result, err := teamRunner.Start(ctx, run.ID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if result.Ticks != 2 || len(result.State.Instances) != 2 {
		t.Fatalf("Start() result = %#v, want two durable ticks", result)
	}
	uow, err := runner.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	record, err := uow.TeamStates().LoadTeamState(ctx, run.ID)
	if err != nil {
		t.Fatalf("LoadTeamState() error = %v", err)
	}
	instances, err := uow.AgentInstances().ListAgentInstances(ctx, api.AgentInstanceSelector{RunID: run.ID})
	if err != nil {
		t.Fatalf("ListAgentInstances() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if record.Tick != 2 || len(instances) != 2 {
		t.Fatalf("durable team state tick=%d instances=%d, want 2 and 2", record.Tick, len(instances))
	}
	var checkpoint multiagent.TeamState
	if err := json.Unmarshal(record.State, &checkpoint); err != nil {
		t.Fatalf("Unmarshal(checkpoint) error = %v", err)
	}
	if checkpoint.Tick != 2 || len(checkpoint.Tasks) != 2 {
		t.Fatalf("checkpoint = %#v, want two completed tasks", checkpoint)
	}
	if len(checkpoint.Tasks[1].Input) == 0 {
		t.Fatalf("checkpoint dropped the previous report input: %#v", checkpoint.Tasks[1])
	}
	completedRun, err := runner.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if completedRun.Status != api.RunStatusCompleted {
		t.Fatalf("durable run status = %q, want completed", completedRun.Status)
	}

	resumed, err := teamRunner.Resume(ctx, run.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Ticks != 2 || len(resumed.State.Instances) != 2 {
		t.Fatalf("Resume() result = %#v, want unchanged terminal snapshot", resumed)
	}
}

func TestRunnerExecutorPersistsTypedHandoff(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-handoff",
		RootTaskID: "root",
		Request:    "handoff",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	class := multiagent.AgentClass{
		Name:         "receiver",
		Instructions: "receive",
		Model:        "scripted",
		InputSchema:  json.RawMessage(`{"type":"object","required":["value"],"properties":{"value":{"type":"string"}}}`),
	}
	driver := scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "accepted"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})
	dispatch := multiagent.Dispatch{
		To:        "instance-1",
		ClassName: class.Name,
		Task: api.Task{
			ID:          "task-1",
			RunID:       run.ID,
			Type:        api.TaskTypeWorker,
			Goal:        class.Instructions,
			InputSchema: class.InputSchema,
		},
		Input: json.RawMessage(`{"value":"evidence"}`),
		Handoff: &multiagent.Handoff{
			From:    "instance-0",
			Payload: json.RawMessage(`{"value":"evidence"}`),
		},
	}
	decorated, created, prepared := false, false, false
	executor := RunnerExecutor{
		Runner:    runner,
		Classes:   map[string]multiagent.AgentClass{class.Name: class},
		BuildDeps: agent.BuildDeps{Providers: provider.Single(driver)},
		DecorateEngine: func(engine agent.Engine, got multiagent.Dispatch, gotClass multiagent.AgentClass) agent.Engine {
			decorated = got.To == dispatch.To && gotClass.Name == class.Name
			return engine
		},
		BeforeTask: func(_ context.Context, got multiagent.Dispatch, gotClass multiagent.AgentClass) error {
			created = got.To == dispatch.To && gotClass.Name == class.Name
			return nil
		},
		PrepareEngine: func(_ context.Context, engine agent.Engine, got multiagent.Dispatch, gotClass multiagent.AgentClass) (agent.Engine, error) {
			prepared = got.To == dispatch.To && gotClass.Name == class.Name
			return engine, nil
		},
	}
	report, err := executor.Execute(ctx, dispatch)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if report.Status != api.ReportStatusSuccess {
		t.Fatalf("Execute() report = %#v, want success", report)
	}
	if !decorated || !created || !prepared {
		t.Fatalf("Execute() lifecycle callbacks = decorated:%t created:%t prepared:%t", decorated, created, prepared)
	}
	uow, err := runner.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	handoffs, err := uow.Handoffs().ListHandoffs(ctx, api.HandoffSelector{RunID: run.ID})
	if err != nil {
		t.Fatalf("ListHandoffs() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if len(handoffs) != 1 || handoffs[0].From != "instance-0" || handoffs[0].To != dispatch.To {
		t.Fatalf("persisted handoffs = %#v, want one typed handoff", handoffs)
	}
}

func TestTeamRunnerRejectsConcurrentResume(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-team-owner",
		RootTaskID: "root",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	teamRunner := TeamRunner{
		Runner: runner,
		Team: multiagent.Team{Scheduler: multiagent.SchedulerFunc(func(context.Context, multiagent.TeamState) ([]multiagent.Dispatch, error) {
			close(entered)
			<-release
			return nil, nil
		})},
	}
	done := make(chan error, 1)
	go func() {
		_, err := teamRunner.Start(ctx, run.ID)
		done <- err
	}()
	<-entered
	if _, err := teamRunner.Resume(ctx, run.ID); !errors.Is(err, api.ErrLeaseNotActive) {
		t.Fatalf("concurrent Resume() error = %v, want ErrLeaseNotActive", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestTeamRunnerResumePreservesFailedCheckpoint(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-team-failed", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := runner.AdvanceRun(ctx, api.AdvanceRunCommand{RunID: run.ID}); err != nil {
		t.Fatalf("AdvanceRun() error = %v", err)
	}
	saveTeamCheckpoint(t, runner, multiagent.TeamState{
		RunID: run.ID,
		Tick:  1,
		Tasks: []api.Task{{
			ID:     "child",
			RunID:  run.ID,
			Status: api.TaskStatusFailed,
			Error:  "provider failed",
		}},
		Instances: []multiagent.AgentInstance{{
			ID:        "instance-1",
			ClassName: "worker",
			TaskID:    "child",
			State:     multiagent.InstanceStateFailed,
		}},
	})
	teamRunner := TeamRunner{
		Runner: runner,
		Team: multiagent.Team{Scheduler: multiagent.SchedulerFunc(func(context.Context, multiagent.TeamState) ([]multiagent.Dispatch, error) {
			return nil, nil
		})},
	}
	result, err := teamRunner.Resume(ctx, run.ID)
	if !errors.Is(err, ErrFailedCheckpoint) || !strings.Contains(err.Error(), "provider failed") {
		t.Fatalf("Resume() error = %v, want checkpoint failure", err)
	}
	if result.State.Instances[0].State != multiagent.InstanceStateFailed {
		t.Fatalf("Resume() lost failed checkpoint: %#v", result.State)
	}
	gotRun, err := runner.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gotRun.Status != api.RunStatusFailed {
		t.Fatalf("run status = %q, want failed", gotRun.Status)
	}
	root, err := runner.Task(ctx, run.ID, run.RootTaskID)
	if err != nil {
		t.Fatalf("Task(root) error = %v", err)
	}
	if root.Status != api.TaskStatusFailed || root.Result == nil || root.Result.Status != api.ReportStatusFailed {
		t.Fatalf("root task = %#v, want failed report", root)
	}
}

func TestTeamRunnerStartReleasesLeaseAfterStateReadError(t *testing.T) {
	ctx := context.Background()
	base := venat.NewDevelopment()
	provider := &failSecondTeamStateLoadProvider{StoreProvider: base.StoreProvider()}
	runner := venat.NewDevelopment(api.Config{StoreProvider: provider})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-team-load-error", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	teamRunner := TeamRunner{
		Runner: runner,
		Team: multiagent.Team{Scheduler: multiagent.SchedulerFunc(func(context.Context, multiagent.TeamState) ([]multiagent.Dispatch, error) {
			return nil, nil
		})},
	}
	if _, err := teamRunner.Start(ctx, run.ID); !errors.Is(err, errTeamStateLoad) {
		t.Fatalf("Start() error = %v, want transient team-state read error", err)
	}
	uow, err := runner.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	lease, ok, err := uow.Leases().ActiveLeaseForTask(ctx, run.ID, run.RootTaskID)
	if err != nil {
		t.Fatalf("ActiveLeaseForTask() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if !ok || lease.Status != api.LeaseStatusReleased {
		t.Fatalf("scheduler lease = %#v, want released", lease)
	}
	envelopes := listTeamEnvelopes(t, runner, run.ID)
	if len(envelopes) != 1 || envelopes[0].Status != "delivered" {
		t.Fatalf("lease cleanup redispatched scheduler envelope: %#v", envelopes)
	}
	if _, err := teamRunner.Start(ctx, run.ID); err != nil {
		t.Fatalf("Start(retry) error = %v", err)
	}
}

func TestTeamRunnerResumeRejectsNonRunnableRuns(t *testing.T) {
	tests := []struct {
		name   string
		status api.RunStatus
		want   error
	}{
		{name: "reconciliation", status: api.RunStatusReconcileRequired, want: api.ErrActionReconcileRequired},
		{name: "approval", status: api.RunStatusWaitingApproval, want: api.ErrInvalidTransition},
		{name: "user input", status: api.RunStatusWaitingUserInput, want: api.ErrInvalidTransition},
		{name: "blocked", status: api.RunStatusBlocked, want: api.ErrInvalidTransition},
		{name: "failed", status: api.RunStatusFailed, want: api.ErrInvalidTransition},
		{name: "cancelled", status: api.RunStatusCancelled, want: api.ErrInvalidTransition},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			runner := venat.NewDevelopment()
			run, _, err := runner.StartRun(ctx, api.StartRunCommand{
				RunID:      "run-team-" + strings.ReplaceAll(test.name, " ", "-"),
				RootTaskID: "root",
			})
			if err != nil {
				t.Fatalf("StartRun() error = %v", err)
			}
			if _, err := runner.AdvanceRun(ctx, api.AdvanceRunCommand{RunID: run.ID}); err != nil {
				t.Fatalf("AdvanceRun() error = %v", err)
			}
			saveTeamCheckpoint(t, runner, multiagent.TeamState{RunID: run.ID})
			if err := runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: run.ID, To: test.status}); err != nil {
				t.Fatalf("TransitionRun() error = %v", err)
			}
			rootBefore, err := runner.Task(ctx, run.ID, run.RootTaskID)
			if err != nil {
				t.Fatalf("Task(before) error = %v", err)
			}
			teamRunner := TeamRunner{
				Runner: runner,
				Team: multiagent.Team{Scheduler: multiagent.SchedulerFunc(func(context.Context, multiagent.TeamState) ([]multiagent.Dispatch, error) {
					t.Fatal("scheduler ran for a non-runnable run")
					return nil, nil
				})},
			}
			if _, err := teamRunner.Resume(ctx, run.ID); !errors.Is(err, test.want) {
				t.Fatalf("Resume() error = %v, want %v", err, test.want)
			}
			rootAfter, err := runner.Task(ctx, run.ID, run.RootTaskID)
			if err != nil {
				t.Fatalf("Task(after) error = %v", err)
			}
			if rootAfter.Attempts != rootBefore.Attempts || rootAfter.Status != rootBefore.Status {
				t.Fatalf("Resume() mutated root task: before=%#v after=%#v", rootBefore, rootAfter)
			}
		})
	}
}

func TestTeamRunnerResumeCompletesComposingRun(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-team-composing", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := runner.AdvanceRun(ctx, api.AdvanceRunCommand{RunID: run.ID}); err != nil {
		t.Fatalf("AdvanceRun() error = %v", err)
	}
	saveTeamCheckpoint(t, runner, multiagent.TeamState{RunID: run.ID, Tick: 2})
	envelope := listTeamEnvelopes(t, runner, run.ID)[0]
	lease, acquired, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     run.RootTaskID,
		EnvelopeID: envelope.ID,
		HolderType: api.HolderComponent,
		HolderID:   envelope.TargetComponent,
		TTL:        time.Minute,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v err=%v", lease, acquired, err)
	}
	if err := runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
		RunID:       run.ID,
		TaskID:      run.RootTaskID,
		LeaseID:     lease.ID,
		HolderType:  lease.HolderType,
		HolderID:    lease.HolderID,
		TaskVersion: lease.TaskVersion,
		Report:      api.TypedReport{Status: api.ReportStatusSuccess},
	}); err != nil {
		t.Fatalf("SubmitTypedReport() error = %v", err)
	}
	if err := runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: run.ID, To: api.RunStatusComposingResponse}); err != nil {
		t.Fatalf("TransitionRun(composing) error = %v", err)
	}
	result, err := (TeamRunner{
		Runner: runner,
		Team: multiagent.Team{Scheduler: multiagent.SchedulerFunc(func(context.Context, multiagent.TeamState) ([]multiagent.Dispatch, error) {
			t.Fatal("scheduler ran while completing composed response")
			return nil, nil
		})},
	}).Resume(ctx, run.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if result.Ticks != 2 {
		t.Fatalf("Resume() ticks = %d, want 2", result.Ticks)
	}
	got, err := runner.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got.Status != api.RunStatusCompleted {
		t.Fatalf("run status = %q, want completed", got.Status)
	}
}

func TestTeamRunnerResumeAdvancesCreatedRun(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-team-created", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	saveTeamCheckpoint(t, runner, multiagent.TeamState{RunID: run.ID})
	if _, err := (TeamRunner{
		Runner: runner,
		Team: multiagent.Team{Scheduler: multiagent.SchedulerFunc(func(context.Context, multiagent.TeamState) ([]multiagent.Dispatch, error) {
			return nil, nil
		})},
	}).Resume(ctx, run.ID); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	got, err := runner.Run(ctx, run.ID)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got.Status != api.RunStatusCompleted {
		t.Fatalf("run status = %q, want completed", got.Status)
	}
}

func TestTeamRunnerResumeStopsAfterRecoveryQuarantine(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{
		RunID:      "run-team-reconcile",
		RootTaskID: "root",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := runner.AdvanceRun(ctx, api.AdvanceRunCommand{RunID: run.ID}); err != nil {
		t.Fatalf("AdvanceRun() error = %v", err)
	}
	saveTeamCheckpoint(t, runner, multiagent.TeamState{RunID: run.ID})
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "action",
		OwnerAgentID: "agent-a",
		AllowsAction: true,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	lease, acquired, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID:      run.ID,
		TaskID:     task.ID,
		EnvelopeID: envelope.ID,
		HolderType: api.HolderAgent,
		HolderID:   "agent-a",
		TTL:        10 * time.Millisecond,
	})
	if err != nil || !acquired {
		t.Fatalf("AcquireTaskExecution() lease=%#v acquired=%v err=%v", lease, acquired, err)
	}
	if _, err := runner.StartActionAttempt(ctx, api.StartActionAttemptCommand{
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  api.HolderAgent,
		HolderID:    "agent-a",
		TaskVersion: lease.TaskVersion,
		ToolName:    "deploy",
	}); err != nil {
		t.Fatalf("StartActionAttempt() error = %v", err)
	}
	if wait := time.Until(lease.ExpiresAt) + time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	envelopesBefore := listTeamEnvelopes(t, runner, run.ID)
	teamRunner := TeamRunner{
		Runner: runner,
		Team: multiagent.Team{Scheduler: multiagent.SchedulerFunc(func(context.Context, multiagent.TeamState) ([]multiagent.Dispatch, error) {
			t.Fatal("scheduler ran after recovery quarantine")
			return nil, nil
		})},
	}
	if _, err := teamRunner.Resume(ctx, run.ID); !errors.Is(err, api.ErrActionReconcileRequired) {
		t.Fatalf("Resume() error = %v, want ErrActionReconcileRequired", err)
	}
	recovered, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if recovered.Status != api.TaskStatusReconcileRequired || recovered.Attempts != 1 {
		t.Fatalf("recovered task = %#v, want quarantined with one attempt", recovered)
	}
	if envelopesAfter := listTeamEnvelopes(t, runner, run.ID); len(envelopesAfter) != len(envelopesBefore) {
		t.Fatalf("Resume() created replacement envelope: before=%d after=%d", len(envelopesBefore), len(envelopesAfter))
	}
}

func TestTeamRunnerExecutesGraphNodesSharingAgentClass(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-team-graph", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	writer := multiagent.AgentClass{Name: "writer", Instructions: "draft", Model: "scripted"}
	graph, err := multiagent.NewGraph().
		AddNode("draft-left", writer).
		AddNode("draft-right", writer).
		Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	driver := scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "drafted"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})
	result, err := (TeamRunner{
		Runner:    runner,
		Team:      multiagent.Team{Agents: []multiagent.AgentClass{writer}, Scheduler: graph},
		BuildDeps: agent.BuildDeps{Providers: provider.Single(driver)},
		Options:   multiagent.DriveOptions{MaxConcurrency: 2},
	}).Start(ctx, run.ID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(result.State.Instances) != 2 || len(result.State.Tasks) != 2 {
		t.Fatalf("graph result = %#v, want two nodes", result.State)
	}
	nodes := map[string]string{}
	for _, instance := range result.State.Instances {
		nodes[instance.ClassName] = instance.TaskID
	}
	if nodes["draft-left"] != run.ID+"-draft-left" || nodes["draft-right"] != run.ID+"-draft-right" {
		t.Fatalf("durable graph node identities = %#v", nodes)
	}
	uow, err := runner.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	records, err := uow.AgentInstances().ListAgentInstances(ctx, api.AgentInstanceSelector{RunID: run.ID})
	if err != nil {
		_ = uow.Rollback(ctx)
		t.Fatalf("ListAgentInstances() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	recordNodes := map[string]string{}
	for _, record := range records {
		recordNodes[record.ClassName] = record.TaskID
	}
	if recordNodes["draft-left"] != run.ID+"-draft-left" || recordNodes["draft-right"] != run.ID+"-draft-right" {
		t.Fatalf("persisted graph node identities = %#v", recordNodes)
	}
}

func saveTeamCheckpoint(t *testing.T, runner *venat.Runner, state multiagent.TeamState) {
	t.Helper()
	if err := (TeamRunner{Runner: runner}).saveState(context.Background(), state, false); err != nil {
		t.Fatalf("saveState() error = %v", err)
	}
}

func listTeamEnvelopes(t *testing.T, runner *venat.Runner, runID string) []api.TaskEnvelope {
	t.Helper()
	envelopes, err := runner.ListEnvelopes(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListEnvelopes() error = %v", err)
	}
	return envelopes
}

var errTeamStateLoad = errors.New("transient team-state read")

type failSecondTeamStateLoadProvider struct {
	api.StoreProvider
	loads atomic.Int32
}

func (p *failSecondTeamStateLoadProvider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	uow, err := p.StoreProvider.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failSecondTeamStateLoadUOW{UnitOfWork: uow, provider: p}, nil
}

func (p *failSecondTeamStateLoadProvider) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
	return p.StoreProvider.(api.CapabilityReporter).Capabilities(ctx)
}

type failSecondTeamStateLoadUOW struct {
	api.UnitOfWork
	provider *failSecondTeamStateLoadProvider
}

func (u failSecondTeamStateLoadUOW) TeamStates() api.TeamStateStore {
	return failSecondTeamStateLoadStore{TeamStateStore: u.UnitOfWork.TeamStates(), provider: u.provider}
}

type failSecondTeamStateLoadStore struct {
	api.TeamStateStore
	provider *failSecondTeamStateLoadProvider
}

func (s failSecondTeamStateLoadStore) LoadTeamState(ctx context.Context, runID string) (api.TeamStateRecord, error) {
	if s.provider.loads.Add(1) == 2 {
		return api.TeamStateRecord{}, errTeamStateLoad
	}
	return s.TeamStateStore.LoadTeamState(ctx, runID)
}
