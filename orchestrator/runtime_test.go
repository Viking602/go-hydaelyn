package orchestrator

import (
	"context"
	"testing"

	core "github.com/Viking602/go-hydaelyn/internal/core"
)

func TestFacadeStateMutationsUseCommandPath(t *testing.T) {
	ctx := context.Background()
	rt, durable := newFacadeRuntimeWithRecordingStore()

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

func TestFacadeFanOutUsesCommandPath(t *testing.T) {
	ctx := context.Background()
	rt, durable := newFacadeRuntimeWithRecordingStore()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-facade-fanout", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	rt.RegisterAgent(AgentProfile{ID: "agent-a", Role: "fanout"})
	rt.RegisterAgent(AgentProfile{ID: "agent-b", Role: "fanout"})
	fanout, err := rt.CreateTask(ctx, CreateTaskCommand{RunID: run.ID, TaskID: "fanout", OwnerComponent: "dispatcher"})
	if err != nil {
		t.Fatalf("CreateTask(fanout) error = %v", err)
	}
	durable.committed = false
	envs, err := rt.DispatchTaskFanOut(ctx, FanOutDispatchTaskCommand{
		RunID:  run.ID,
		TaskID: fanout.ID,
		To:     Address{Kind: AddressKindRole, Role: "fanout"},
	})
	if err != nil {
		t.Fatalf("DispatchTaskFanOut() error = %v", err)
	}
	if !durable.committed {
		t.Fatalf("DispatchTaskFanOut() did not commit through ExecuteCommand")
	}
	if len(envs) != 2 {
		t.Fatalf("expected 2 fan-out envelopes, got %d", len(envs))
	}
	if _, err := durable.store.LoadEnvelope(ctx, envs[0].ID); err != nil {
		t.Fatalf("durable MailboxOutbox missing first fan-out envelope: %v", err)
	}
	if _, err := durable.store.LoadEnvelope(ctx, envs[1].ID); err != nil {
		t.Fatalf("durable MailboxOutbox missing second fan-out envelope: %v", err)
	}
}

func TestFacadeWriteItemUsesCommandPath(t *testing.T) {
	ctx := context.Background()
	rt, durable := newFacadeRuntimeWithRecordingStore()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: "run-facade-blackboard", RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := rt.CreateTask(ctx, CreateTaskCommand{RunID: run.ID, TaskID: "worker", OwnerAgentID: "agent-a"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	durable.committed = false
	if err := rt.WriteItem(ctx, BlackboardItem{
		RunID:  run.ID,
		TaskID: task.ID,
		Type:   BlackboardItemEvidence,
		Source: SourceIdentity{
			Type: SourceAgent,
			ID:   "agent-a",
		},
		Visibility: BlackboardVisibilityAgentVisible,
		Payload:    "persist me",
	}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	if !durable.committed {
		t.Fatalf("WriteItem() did not commit through ExecuteCommand")
	}
	items, err := durable.store.SelectItems(ctx, run.ID, BlackboardSelector{ItemTypes: []BlackboardItemType{BlackboardItemEvidence}})
	if err != nil {
		t.Fatalf("durable BlackboardStore select error = %v", err)
	}
	if len(items) != 1 || items[0].Payload != "persist me" {
		t.Fatalf("durable BlackboardStore missing facade-written item: %#v", items)
	}
}

func newFacadeRuntimeWithRecordingStore() (*Runner, *facadeRecordingUnitOfWork) {
	durable := &facadeRecordingUnitOfWork{store: core.NewMemoryRuntime()}
	rt := New(Config{StoreProvider: facadeRecordingStoreProvider{uow: durable}})
	return rt, durable
}

type facadeRecordingStoreProvider struct {
	uow *facadeRecordingUnitOfWork
}

func (p facadeRecordingStoreProvider) Begin(context.Context) (core.UnitOfWork, error) {
	p.uow.committed = false
	p.uow.rolledBack = false
	return p.uow, nil
}

type facadeRecordingUnitOfWork struct {
	store      *core.Runtime
	committed  bool
	rolledBack bool
}

func (u *facadeRecordingUnitOfWork) Runs() core.RunStore                    { return u.store }
func (u *facadeRecordingUnitOfWork) Tasks() core.TaskStore                  { return u.store }
func (u *facadeRecordingUnitOfWork) Events() core.EventStore                { return u.store }
func (u *facadeRecordingUnitOfWork) Blackboard() core.BlackboardStore       { return u.store }
func (u *facadeRecordingUnitOfWork) MailboxOutbox() core.MailboxOutboxStore { return u.store }
func (u *facadeRecordingUnitOfWork) UserMessages() core.UserMessageStore    { return u.store }
func (u *facadeRecordingUnitOfWork) Trace() core.TraceStore                 { return u.store }
func (u *facadeRecordingUnitOfWork) Commit(context.Context) error {
	u.committed = true
	return nil
}
func (u *facadeRecordingUnitOfWork) Rollback(context.Context) error {
	u.rolledBack = true
	return nil
}
