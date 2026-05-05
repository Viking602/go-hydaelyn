package task_test

import (
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	tasksvc "github.com/Viking602/go-hydaelyn/internal/task"
)

func TestPureTaskTransitionCanBumpVersion(t *testing.T) {
	task := model.Task{
		ID:      "task-1",
		RunID:   "run-1",
		Status:  model.TaskStatusDispatched,
		Version: 3,
	}
	next, err := tasksvc.PureTaskTransition(task, model.TaskStatusRunning, true)
	if err != nil {
		t.Fatalf("PureTaskTransition() error = %v", err)
	}
	if next.Status != model.TaskStatusRunning || next.Version != 4 || next.UpdatedAt.IsZero() {
		t.Fatalf("PureTaskTransition() = %#v", next)
	}
}

func TestPureTaskTransitionRejectsTerminalTask(t *testing.T) {
	_, err := tasksvc.PureTaskTransition(model.Task{
		ID:      "task-1",
		RunID:   "run-1",
		Status:  model.TaskStatusCompleted,
		Version: 1,
	}, model.TaskStatusRunning, true)
	if !errors.Is(err, model.ErrTerminalState) {
		t.Fatalf("PureTaskTransition() error = %v, want ErrTerminalState", err)
	}
}
