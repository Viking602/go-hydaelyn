package projection

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/eventpayload"
)

func TestProjectPreservesSerializedTaskFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	report := model.TypedReport{Status: model.ReportStatusSuccess, Summary: "done"}
	task := model.Task{
		ID:                 "task-1",
		RunID:              "run-1",
		Type:               model.TaskTypeWorker,
		Goal:               "deploy",
		OwnerAgentID:       "agent-1",
		Status:             model.TaskStatusCompleted,
		Version:            3,
		AllowsAction:       true,
		Tags:               []string{"critical"},
		WriteTargets:       []string{"result"},
		RetryPolicy:        model.RetryPolicy{MaxAttempts: 2, Backoff: time.Second},
		Result:             &report,
		Budget:             &model.TaskBudget{MaxTokens: 100},
		InputSchema:        json.RawMessage(`{"type":"object"}`),
		OutputSchema:       json.RawMessage(`{"type":"string"}`),
		CompletionCriteria: []string{"approved"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	events := []model.Event{
		{RunID: "run-1", Type: model.EventRunStarted, Payload: map[string]any{"run": eventpayload.Run(model.Run{ID: "run-1", Status: model.RunStatusRunning, CreatedAt: now, UpdatedAt: now})}},
		{RunID: "run-1", TaskID: task.ID, Type: model.EventTaskCreated, Payload: eventpayload.Task(task)},
	}
	raw, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var serialized []model.Event
	if err := json.Unmarshal(raw, &serialized); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	projection, err := Project(serialized)
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	got := projection.Tasks[task.ID]
	if !got.AllowsAction || got.RetryPolicy != task.RetryPolicy || got.Budget == nil || got.Budget.MaxTokens != 100 {
		t.Fatalf("projected task dropped governance fields: %#v", got)
	}
	if got.Result == nil || got.Result.Summary != "done" || string(got.InputSchema) != string(task.InputSchema) || string(got.OutputSchema) != string(task.OutputSchema) {
		t.Fatalf("projected task dropped result/schema fields: %#v", got)
	}
}
