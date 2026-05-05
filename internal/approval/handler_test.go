package approval

import (
	"context"
	"testing"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/memory"
)

func TestDecideApprovalResumesPausedTaskAndWaitingRun(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run := model.Run{ID: "run-1", RootTaskID: "task-1", Status: model.RunStatusWaitingApproval}
	task := model.Task{ID: "task-1", RunID: run.ID, Status: model.TaskStatusPaused, Version: 2}
	approval := model.ApprovalRequest{ApprovalID: "approval-1", RunID: run.ID, TaskID: task.ID, Status: "pending"}
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		t.Fatalf("SaveApproval() error = %v", err)
	}

	bus := commandbus.NewBus()
	RegisterHandlers(bus, HandlerOptions{})
	result, err := bus.Execute(ctx, uow, DecideApprovalCommand{
		RunID:      run.ID,
		ApprovalID: approval.ApprovalID,
		DecidedBy:  "human",
		Decision:   "approved",
		Reason:     "ok",
	})
	if err != nil {
		t.Fatalf("DecideApproval error = %v", err)
	}
	decided := result.(decideApprovalResult)
	if !decided.TaskResumed || !decided.RunTransition {
		t.Fatalf("result = %#v", decided)
	}
	if decided.Task.Status != model.TaskStatusDispatched || decided.Task.Version != 3 {
		t.Fatalf("resumed task = %#v", decided.Task)
	}
	if decided.Run.Status != model.RunStatusRunning {
		t.Fatalf("resumed run = %#v", decided.Run)
	}
	events, err := uow.Events().ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 || events[0].Type != model.EventApprovalDecided || events[1].Type != model.EventRunStatusChanged {
		t.Fatalf("events = %#v", events)
	}
}

func TestRecoverResumeTokenRejectsExpiredToken(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	if err := uow.ResumeTokens().SaveResumeToken(ctx, model.ResumeToken{TokenID: "token-1", ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatalf("SaveResumeToken() error = %v", err)
	}
	bus := commandbus.NewBus()
	RegisterHandlers(bus, HandlerOptions{})
	if _, err := bus.Execute(ctx, uow, RecoverResumeTokenCommand{TokenID: "token-1"}); err != model.ErrInvalidCommand {
		t.Fatalf("RecoverResumeToken error = %v, want ErrInvalidCommand", err)
	}
}
