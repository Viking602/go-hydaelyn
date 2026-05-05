package adapter_test

import (
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core/adapter"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func TestCommandToCore_StartRun(t *testing.T) {
	cmd := api.StartRunCommand{RunID: "r1", RootTaskID: "t1", Request: "hello"}
	result, ok := adapter.CommandToCore(cmd)
	if !ok {
		t.Fatal("expected ok=true for StartRunCommand")
	}
	if result.CommandName() == "" {
		t.Error("CommandName should not be empty")
	}
}

func TestCommandToCore_AdvanceRun(t *testing.T) {
	cmd := api.AdvanceRunCommand{RunID: "r1"}
	result, ok := adapter.CommandToCore(cmd)
	if !ok {
		t.Fatal("expected ok=true for AdvanceRunCommand")
	}
	if result == nil {
		t.Error("expected non-nil command")
	}
}

func TestCommandToCore_DispatchTask(t *testing.T) {
	cmd := api.DispatchTaskCommand{RunID: "r1", TaskID: "t1", TargetAgentID: "agent-1"}
	result, ok := adapter.CommandToCore(cmd)
	if !ok {
		t.Fatal("expected ok=true for DispatchTaskCommand")
	}
	if result == nil {
		t.Error("expected non-nil command")
	}
}

func TestCommandToCore_AcquireTaskExecution(t *testing.T) {
	cmd := api.AcquireTaskExecutionCommand{
		RunID:      "r1",
		TaskID:     "t1",
		HolderType: api.HolderType(model.HolderAgent),
		HolderID:   "agent-1",
		TTL:        30 * time.Second,
	}
	result, ok := adapter.CommandToCore(cmd)
	if !ok {
		t.Fatal("expected ok=true for AcquireTaskExecutionCommand")
	}
	if result == nil {
		t.Error("expected non-nil command")
	}
}

func TestCommandToCore_RequestApproval(t *testing.T) {
	cmd := api.RequestApprovalCommand{
		RunID:            "r1",
		TaskID:           "t1",
		RequesterAgentID: "agent-1",
		Reason:           "needs review",
	}
	result, ok := adapter.CommandToCore(cmd)
	if !ok {
		t.Fatal("expected ok=true for RequestApprovalCommand")
	}
	if result == nil {
		t.Error("expected non-nil command")
	}
}

func TestCommandToCore_StartTraceSpan(t *testing.T) {
	cmd := api.StartTraceSpanCommand{
		RunID:   "r1",
		TaskID:  "t1",
		Name:    "my-span",
		Component: "worker",
	}
	result, ok := adapter.CommandToCore(cmd)
	if !ok {
		t.Fatal("expected ok=true for StartTraceSpanCommand")
	}
	if result == nil {
		t.Error("expected non-nil command")
	}
}

func TestStartRunCommandToCore(t *testing.T) {
	cmd := api.StartRunCommand{
		RunID:      "r1",
		RootTaskID: "root",
		Request:    "do something",
		Metadata:   map[string]string{"env": "prod"},
	}
	core := adapter.StartRunCommandToCore(cmd)
	if core.RunID != "r1" {
		t.Errorf("RunID mismatch: got %q", core.RunID)
	}
	if core.RootTaskID != "root" {
		t.Errorf("RootTaskID mismatch: got %q", core.RootTaskID)
	}
	if core.Metadata["env"] != "prod" {
		t.Errorf("Metadata not copied: got %v", core.Metadata)
	}
}

func TestCreateTaskCommandToCore(t *testing.T) {
	cmd := api.CreateTaskCommand{
		RunID:      "r1",
		TaskID:     "t1",
		Type:       api.TaskType(model.TaskTypeWorker),
		AwaitMode:  api.AwaitMode(model.AwaitModeAll),
		DependsOn:  []string{"dep1", "dep2"},
	}
	core := adapter.CreateTaskCommandToCore(cmd)
	if core.RunID != "r1" {
		t.Errorf("RunID mismatch")
	}
	if core.Type != model.TaskTypeWorker {
		t.Errorf("Type mismatch: got %q", core.Type)
	}
	if len(core.DependsOn) != 2 {
		t.Errorf("DependsOn not copied: got %v", core.DependsOn)
	}
}

func TestTransitionRunCommandToCore(t *testing.T) {
	cmd := api.TransitionRunCommand{RunID: "r1", To: api.RunStatus(model.RunStatusRunning)}
	core := adapter.TransitionRunCommandToCore(cmd)
	if core.RunID != "r1" {
		t.Errorf("RunID mismatch")
	}
	if core.To != model.RunStatusRunning {
		t.Errorf("To mismatch: got %q", core.To)
	}
}

func TestTransitionTaskCommandToCore(t *testing.T) {
	cmd := api.TransitionTaskCommand{
		RunID:  "r1",
		TaskID: "t1",
		To:     api.TaskStatus(model.TaskStatusCompleted),
	}
	core := adapter.TransitionTaskCommandToCore(cmd)
	if core.To != model.TaskStatusCompleted {
		t.Errorf("To mismatch: got %q", core.To)
	}
}
