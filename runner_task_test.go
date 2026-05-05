package hydaelyn

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
)

func TestCreateTask_UnderExistingRun(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, root, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	task, err := r.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		ParentTaskID: root.ID,
		Goal:         "sub-task",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID == "" {
		t.Error("expected non-empty task ID")
	}
	if task.RunID != run.ID {
		t.Errorf("task.RunID %q != run.ID %q", task.RunID, run.ID)
	}
}

func TestTask_LoadsExistingTask(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, root, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	got, err := r.Task(ctx, run.ID, root.ID)
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if got.ID != root.ID {
		t.Errorf("task ID mismatch: got %q want %q", got.ID, root.ID)
	}
}

func TestReadyTasks_ReturnsCreatedRootTask(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, _, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	tasks := r.ReadyTasks(run.ID)
	if len(tasks) == 0 {
		t.Error("expected at least one ready task after StartRun")
	}
}

func TestListTasks_IncludesRootTask(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, root, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	tasks, err := r.ListTasks(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	found := false
	for _, task := range tasks {
		if task.ID == root.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("root task not found in ListTasks result")
	}
}

func TestSaveAndLoadTask(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	run, root, _ := r.StartRun(ctx, api.StartRunCommand{Request: "test"})
	root.Goal = "updated goal"
	if err := r.SaveTask(ctx, root); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	loaded, err := r.LoadTask(ctx, run.ID, root.ID)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if loaded.Goal != "updated goal" {
		t.Errorf("goal not persisted: got %q", loaded.Goal)
	}
}
