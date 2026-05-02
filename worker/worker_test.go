package worker

import (
	"context"
	"encoding/json"
	"errors"
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

type recordingTool struct {
	definition tool.Definition
	called     bool
}

func (d *recordingTool) Definition() tool.Definition { return d.definition }

func (d *recordingTool) Execute(context.Context, tool.Call, tool.UpdateSink) (tool.Result, error) {
	d.called = true
	return tool.Result{Name: d.definition.Name, Content: "ok"}, nil
}
