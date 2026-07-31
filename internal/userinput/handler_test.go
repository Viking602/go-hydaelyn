package userinput_test

import (
	"context"
	"fmt"
	"testing"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/memory"
	"github.com/Viking602/venat/internal/userinput"
)

func userInputIDGenerator() userinput.IDGenerator {
	next := 0
	return func(prefix string) string {
		next++
		return fmt.Sprintf("%s-%d", prefix, next)
	}
}

func TestSubmitUserInputWritesBlackboardAndRedispatchesWaitingTask(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	run := model.Run{ID: "run-1", RootTaskID: "task-1", Status: model.RunStatusWaitingUserInput}
	task := model.Task{ID: "task-1", RunID: run.ID, Status: model.TaskStatusWaitingUserInput, Version: 2, OwnerAgentID: "agent-1", Error: "need input", WriteTargets: []string{"answer"}}
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}

	bus := commandbus.NewBus()
	userinput.RegisterHandlers(bus, userinput.HandlerOptions{NewID: userInputIDGenerator()})
	result, err := bus.Execute(ctx, uow, userinput.SubmitUserInputCommand{RunID: run.ID, TaskID: task.ID, Input: "use option b"})
	if err != nil {
		t.Fatalf("SubmitUserInput error = %v", err)
	}
	submitted := result.(userinput.SubmitResult)
	if !submitted.RunTransition || !submitted.Redispatched || !submitted.TaskTransition {
		t.Fatalf("submit result = %#v", submitted)
	}
	if submitted.Task.Status != model.TaskStatusDispatched || submitted.Task.Version != 3 || submitted.Task.Error != "" {
		t.Fatalf("redispatched task = %#v", submitted.Task)
	}
	if submitted.Item.Key != "user_input" || submitted.Item.Payload != "use option b" {
		t.Fatalf("blackboard item = %#v", submitted.Item)
	}
	if submitted.Envelope.Status != "pending" || submitted.Envelope.WriteTargets[0] != "answer" {
		t.Fatalf("envelope = %#v", submitted.Envelope)
	}
}
