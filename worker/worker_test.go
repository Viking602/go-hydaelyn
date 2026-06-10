package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/provider/scripted"
	"github.com/Viking602/go-hydaelyn/tool"
)

func TestAgentWorkerExecutesEnvelope(t *testing.T) {
	ctx := context.Background()
	runner := hydaelyn.New()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-worker", RootTaskID: "root", Request: "do work"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:         run.ID,
		TaskID:        "task-worker",
		Goal:          "summarize",
		OwnerAgentID:  "agent-a",
		WriteTargets:  []string{"summary"},
		ReadSelectors: []api.BlackboardSelector{{Keys: []string{"input"}}},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if err := runner.WriteItem(ctx, api.BlackboardItem{
		RunID:      run.ID,
		Type:       api.BlackboardItemContext,
		Source:     api.SourceIdentity{Type: api.SourceSystem, ID: "test"},
		Visibility: api.BlackboardVisibilityAgentVisible,
		Key:        "input",
		Payload:    "source material",
	}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "summary done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	if err := (AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env}); err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}
	completed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if completed.Status != api.TaskStatusCompleted {
		t.Fatalf("expected completed task, got %#v", completed.Status)
	}
	items, err := runner.SelectItems(ctx, run.ID, api.BlackboardSelector{Keys: []string{"summary"}})
	if err != nil {
		t.Fatalf("SelectItems() error = %v", err)
	}
	if len(items) != 1 || items[0].Payload != "summary done" {
		t.Fatalf("missing worker output item: %#v", items)
	}
}

func TestAgentWorkerValidatesAgainstTaskOutputSchema(t *testing.T) {
	ctx := context.Background()
	runner := hydaelyn.New()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-schema", RootTaskID: "root", Request: "do work"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-schema",
		Goal:         "summarize",
		OwnerAgentID: "agent-a",
		WriteTargets: []string{"summary"},
		OutputSchema: json.RawMessage(`{"type":"object","required":["summary"],"properties":{"summary":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	// The task's OutputSchema must survive the public CreateTask path so the
	// worker can validate against it; an empty schema here would make the test
	// vacuous (validation would be skipped).
	if len(task.OutputSchema) == 0 {
		t.Fatalf("CreateTask dropped OutputSchema: %#v", task)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	// The model returns prose, not JSON — invalid against the task's OutputSchema.
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "just some prose, not json"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	err = (AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env})
	if err == nil {
		t.Fatal("expected schema-validation failure on worker path, got nil error")
	}
	failed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if failed.Status != api.TaskStatusFailed {
		t.Fatalf("schema-invalid terminal output must fail the task, got status %#v", failed.Status)
	}
}

func TestAgentWorkerPersistsValidatedStructuredOutput(t *testing.T) {
	ctx := context.Background()
	runner := hydaelyn.New()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-structured", RootTaskID: "root", Request: "do work"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-structured",
		Goal:         "summarize",
		OwnerAgentID: "agent-a",
		WriteTargets: []string{"summary"},
		OutputSchema: json.RawMessage(`{"type":"object","required":["summary"],"properties":{"summary":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	// The model returns valid JSON that satisfies the task's OutputSchema, so
	// the engine populates result.Structured.
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: `{"summary":"done"}`},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	})}
	if err := (AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env}); err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}
	completed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if completed.Status != api.TaskStatusCompleted {
		t.Fatalf("expected completed task, got %#v", completed.Status)
	}
	// The validated structured payload must survive onto the persisted report;
	// a success report carrying only Summary would drop it, leaving durable
	// downstream readers (routers, graph edges) unable to route on it.
	if completed.Result == nil {
		t.Fatalf("completed task carries no typed report")
	}
	if completed.Result.Structured == nil || completed.Result.Structured["summary"] != "done" {
		t.Fatalf("validated structured output dropped from report: %#v", completed.Result)
	}
}

func TestGovernedToolBusRejectsSideEffectWithoutActionTask(t *testing.T) {
	ctx := context.Background()
	runner := hydaelyn.New()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-tool", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{RunID: run.ID, TaskID: "tool-task", OwnerAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID, HolderType: api.HolderAgent, HolderID: "agent-a", TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("AcquireTaskExecution() error = %v", err)
	}
	driver := &recordingTool{definition: tool.Definition{Name: "write", EffectType: tool.EffectWrite, RequiresActionTask: true}}
	bus := GovernedToolBus{
		Runner: runner, Bus: tool.NewBus(driver), RunID: run.ID, TaskID: task.ID,
		LeaseID: lease.ID, HolderType: api.HolderAgent, HolderID: "agent-a", TaskVersion: task.Version,
	}
	_, err = bus.Execute(ctx, tool.Call{ID: "call-1", Name: "write", Arguments: json.RawMessage(`{"value":1}`)}, nil)
	if !errors.Is(err, hydaelyn.ErrActionTaskRequired) {
		t.Fatalf("expected ErrActionTaskRequired, got %v", err)
	}
	if driver.called {
		t.Fatalf("side-effect driver was called despite governance rejection")
	}
}

func TestAgentWorkerSubmitsFailedReportAndReleasesLeaseOnEngineError(t *testing.T) {
	ctx := context.Background()
	runner := hydaelyn.New()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-worker-failure", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-worker-failure",
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}

	errBoom := errors.New("provider boom")
	err = (AgentWorker{
		Runner:  runner,
		Engine:  agent.Engine{Provider: failingProvider{err: errBoom}},
		AgentID: "agent-a",
		Model:   "scripted",
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env})
	if !errors.Is(err, errBoom) {
		t.Fatalf("ExecuteEnvelope() error = %v, want %v", err, errBoom)
	}

	failed, err := runner.Task(ctx, run.ID, task.ID)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	if failed.Status != api.TaskStatusFailed || failed.Error != errBoom.Error() {
		t.Fatalf("engine failure should submit failed report, got %#v", failed)
	}
	// The engine wraps the provider error in an AgentFailure; the worker must
	// carry that typed classification onto the persisted report so a scheduler
	// can branch on the failure mode end-to-end.
	if failed.Result == nil || failed.Result.Kind != string(agent.FailureKindEngineError) {
		t.Fatalf("failed report should carry the agent failure kind, got %#v", failed.Result)
	}
	if active := runner.ActiveLeaseCount(run.ID, task.ID); active != 0 {
		t.Fatalf("engine failure should release active lease, got %d", active)
	}
}

func TestFailureReportCarriesAgentFailureDisposition(t *testing.T) {
	failure := (&agent.AgentFailure{
		Kind:        agent.FailureKindBudgetExhausted,
		Reason:      "out of budget",
		Retryable:   true,
		Escalatable: false,
	}).WithCause(errors.New("underlying"))

	// Wrapped so the test also exercises errors.As walking the chain, the way
	// a real caller might re-wrap the failure before it reaches submitFailure.
	report := failureReport(fmt.Errorf("worker: %w", failure))

	if report.Status != api.ReportStatusFailed {
		t.Fatalf("Status = %q, want failed", report.Status)
	}
	if report.Kind != string(agent.FailureKindBudgetExhausted) {
		t.Fatalf("Kind = %q, want budget_exhausted", report.Kind)
	}
	// Distinct true/false guards against the booleans being swapped.
	if !report.Retryable || report.Escalatable {
		t.Fatalf("disposition mismatch: retryable=%v escalatable=%v", report.Retryable, report.Escalatable)
	}
}

func TestFailureReportPlainErrorHasNoKind(t *testing.T) {
	report := failureReport(errors.New("boom"))

	if report.Status != api.ReportStatusFailed || report.Summary != "boom" {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Kind != "" || report.Retryable || report.Escalatable {
		t.Fatalf("plain error should leave failure fields empty, got %#v", report)
	}
}

type recordingTool struct {
	definition tool.Definition
	called     bool
}

func (d *recordingTool) Definition() tool.Definition { return d.definition }

func (d *recordingTool) Execute(context.Context, tool.Call, tool.UpdateSink) (tool.Result, error) {
	d.called = true
	return tool.Result{Name: d.definition.Name, Content: "ok"}, nil
}

type failingProvider struct {
	err error
}

func (p failingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "failing"}
}

func (p failingProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return nil, p.err
}

func TestAgentWorkerPersistsUsageRecord(t *testing.T) {
	ctx := context.Background()
	runner := hydaelyn.New()
	runner.RegisterAgent(api.AgentProfile{ID: "agent-a"})
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "run-usage", RootTaskID: "root", Request: "meter me"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "task-usage",
		Goal:         "summarize",
		OwnerAgentID: "agent-a",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	engine := agent.Engine{Provider: scripted.New([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "done"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: 11, OutputTokens: 7}},
	})}
	if err := (AgentWorker{Runner: runner, Engine: engine, AgentID: "agent-a", Model: "scripted"}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: env}); err != nil {
		t.Fatalf("ExecuteEnvelope() error = %v", err)
	}

	records, err := runner.QueryUsage(ctx, api.UsageSelector{RunID: run.ID})
	if err != nil {
		t.Fatalf("QueryUsage() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("usage records = %d, want exactly 1 (worker must meter the run)", len(records))
	}
	record := records[0]
	if record.TaskID != task.ID || record.AgentID != "agent-a" || record.Model != "scripted" {
		t.Fatalf("usage record identity = %+v", record)
	}
	if record.InputTokens != 11 || record.OutputTokens != 7 {
		t.Fatalf("usage tokens = %d/%d, want 11/7", record.InputTokens, record.OutputTokens)
	}
}
