package orchestrator

import (
	"context"
	"testing"

	runtimeimpl "github.com/Viking602/go-hydaelyn/internal/runtime"
)

func TestFacadeStateMutationsUseCommandPath(t *testing.T) {
	ctx := context.Background()
	durable := &facadeRecordingUnitOfWork{store: runtimeimpl.NewMemoryRuntime()}
	rt := &Runtime{inner: runtimeimpl.NewRuntime(runtimeimpl.Config{StoreProvider: facadeRecordingStoreProvider{uow: durable}})}

	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-facade-command", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if !durable.committed {
		t.Fatalf("StartRun() did not commit through ExecuteCommand")
	}
	if _, err := durable.store.Run(ctx, run.ID); err != nil {
		t.Fatalf("durable RunStore missing facade-created run: %v", err)
	}

	task, err := rt.CreateTask(ctx, CreateTaskCommand{RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := durable.store.Task(ctx, run.ID, task.ID); err != nil {
		t.Fatalf("durable TaskStore missing facade-created task: %v", err)
	}

	env, err := rt.DispatchTask(ctx, DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("DispatchTask() error = %v", err)
	}
	if _, err := durable.store.LoadEnvelope(ctx, env.ID); err != nil {
		t.Fatalf("durable MailboxOutbox missing facade-dispatched envelope: %v", err)
	}
}

type facadeRecordingStoreProvider struct {
	uow *facadeRecordingUnitOfWork
}

func (p facadeRecordingStoreProvider) Begin(context.Context) (runtimeimpl.UnitOfWork, error) {
	p.uow.committed = false
	p.uow.rolledBack = false
	return p.uow, nil
}

type facadeRecordingUnitOfWork struct {
	store      *runtimeimpl.Runtime
	committed  bool
	rolledBack bool
}

func (u *facadeRecordingUnitOfWork) Runs() runtimeimpl.RunStore                    { return u.store }
func (u *facadeRecordingUnitOfWork) Tasks() runtimeimpl.TaskStore                  { return u.store }
func (u *facadeRecordingUnitOfWork) Events() runtimeimpl.EventStore                { return u.store }
func (u *facadeRecordingUnitOfWork) Blackboard() runtimeimpl.BlackboardStore       { return u.store }
func (u *facadeRecordingUnitOfWork) MailboxOutbox() runtimeimpl.MailboxOutboxStore { return u.store }
func (u *facadeRecordingUnitOfWork) UserMessages() runtimeimpl.UserMessageStore    { return u.store }
func (u *facadeRecordingUnitOfWork) Trace() runtimeimpl.TraceStore                 { return u.store }
func (u *facadeRecordingUnitOfWork) Commit(context.Context) error {
	u.committed = true
	return nil
}
func (u *facadeRecordingUnitOfWork) Rollback(context.Context) error {
	u.rolledBack = true
	return nil
}
