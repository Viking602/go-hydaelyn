package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/durable"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

type approvalStatus string

const (
	approvalPending  approvalStatus = "pending"
	approvalApproved approvalStatus = "approved"
	approvalDenied   approvalStatus = "denied"
)

type approvalKey struct {
	ExecutionID durable.ExecutionID
	OperationID string
}

type approvalRequest struct {
	Key       approvalKey
	ToolName  string
	Arguments json.RawMessage
}

type approvalAuditFact struct {
	Sequence uint64
	Action   string
	Status   approvalStatus
	Actor    string
}

type approvalRecord struct {
	Request approvalRequest
	Status  approvalStatus
	Audit   []approvalAuditFact
}

type approvalState struct {
	mu            sync.Mutex
	nextSequence  uint64
	records       map[approvalKey]approvalRecord
	notifications chan approvalRequest
}

// approvalStore is application-owned state. reopen simulates a new process
// handle over the same application persistence; durable knows nothing about it.
type approvalStore struct {
	state *approvalState
}

func newApprovalStore() *approvalStore {
	return &approvalStore{state: &approvalState{
		records:       make(map[approvalKey]approvalRecord),
		notifications: make(chan approvalRequest, 1024),
	}}
}

func (store *approvalStore) reopen() *approvalStore {
	return &approvalStore{state: store.state}
}

func (store *approvalStore) request(executionID durable.ExecutionID, call tool.Call) (approvalRecord, error) {
	key := approvalKey{ExecutionID: executionID, OperationID: call.OperationID}
	if executionID == "" || call.OperationID == "" || call.Name == "" {
		return approvalRecord{}, errors.New("invalid approval request identity")
	}
	store.state.mu.Lock()
	if existing, ok := store.state.records[key]; ok {
		record := cloneApprovalRecord(existing)
		store.state.mu.Unlock()
		return record, nil
	}
	request := approvalRequest{
		Key:       key,
		ToolName:  call.Name,
		Arguments: append(json.RawMessage(nil), call.Arguments...),
	}
	store.state.nextSequence++
	record := approvalRecord{
		Request: request,
		Status:  approvalPending,
		Audit: []approvalAuditFact{{
			Sequence: store.state.nextSequence,
			Action:   "requested",
			Status:   approvalPending,
			Actor:    "agent-hook",
		}},
	}
	store.state.records[key] = cloneApprovalRecord(record)
	store.state.mu.Unlock()
	store.state.notifications <- cloneApprovalRequest(request)
	return cloneApprovalRecord(record), nil
}

func (store *approvalStore) decide(key approvalKey, decision approvalStatus, actor string) (approvalRecord, error) {
	if decision != approvalApproved && decision != approvalDenied {
		return approvalRecord{}, errors.New("approval decision must be approved or denied")
	}
	if actor == "" {
		return approvalRecord{}, errors.New("approval actor is empty")
	}
	store.state.mu.Lock()
	defer store.state.mu.Unlock()
	record, ok := store.state.records[key]
	if !ok {
		return approvalRecord{}, errors.New("approval request not found")
	}
	if record.Status == decision {
		return cloneApprovalRecord(record), nil
	}
	if record.Status != approvalPending {
		return approvalRecord{}, fmt.Errorf("approval already decided as %s", record.Status)
	}
	store.state.nextSequence++
	record.Status = decision
	record.Audit = append(record.Audit, approvalAuditFact{
		Sequence: store.state.nextSequence,
		Action:   "decided",
		Status:   decision,
		Actor:    actor,
	})
	store.state.records[key] = cloneApprovalRecord(record)
	return cloneApprovalRecord(record), nil
}

func (store *approvalStore) lookup(key approvalKey) (approvalRecord, bool) {
	store.state.mu.Lock()
	defer store.state.mu.Unlock()
	record, ok := store.state.records[key]
	return cloneApprovalRecord(record), ok
}

func (store *approvalStore) nextRequest(ctx context.Context) (approvalRequest, error) {
	select {
	case <-ctx.Done():
		return approvalRequest{}, ctx.Err()
	case request := <-store.state.notifications:
		return cloneApprovalRequest(request), nil
	}
}

func (store *approvalStore) requestCount() int {
	store.state.mu.Lock()
	defer store.state.mu.Unlock()
	return len(store.state.records)
}

func cloneApprovalRequest(request approvalRequest) approvalRequest {
	request.Arguments = append(json.RawMessage(nil), request.Arguments...)
	return request
}

func cloneApprovalRecord(record approvalRecord) approvalRecord {
	record.Request = cloneApprovalRequest(record.Request)
	record.Audit = append([]approvalAuditFact(nil), record.Audit...)
	return record
}

type approvalHook struct {
	executionID durable.ExecutionID
	store       *approvalStore
}

func (hook approvalHook) TransformContext(_ context.Context, messages []message.Message) ([]message.Message, error) {
	return messages, nil
}

func (approvalHook) BeforeModelCall(context.Context, *provider.Request) error { return nil }

func (hook approvalHook) BeforeToolCall(ctx context.Context, call *tool.Call) error {
	record, err := hook.store.request(hook.executionID, *call)
	if err != nil {
		return err
	}
	if record.Status != approvalPending {
		return nil
	}
	<-ctx.Done()
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}

func (approvalHook) AfterToolCall(context.Context, *tool.Result) error { return nil }
func (approvalHook) OnEvent(context.Context, provider.Event) error     { return nil }

type approvalDriver struct {
	executionID durable.ExecutionID
	store       *approvalStore
	next        tool.Driver
}

func (driver approvalDriver) Definition() tool.Definition {
	return driver.next.Definition()
}

func (driver approvalDriver) Execute(ctx context.Context, call tool.Call, sink tool.UpdateSink) (tool.Result, error) {
	record, ok := driver.store.lookup(approvalKey{ExecutionID: driver.executionID, OperationID: call.OperationID})
	if !ok || record.Status == approvalPending {
		return tool.Result{}, fmt.Errorf("%w: approval is not decided", tool.ErrNotExecuted)
	}
	if record.Status == approvalDenied {
		return tool.Result{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    "approval denied by application",
			IsError:    true,
		}, nil
	}
	return driver.next.Execute(ctx, call, sink)
}

func approvalEngine(executionID durable.ExecutionID, store *approvalStore, driver provider.Driver, mode tool.Mode, drivers ...tool.Driver) agent.Engine {
	wrapped := make([]tool.Driver, len(drivers))
	for index := range drivers {
		wrapped[index] = approvalDriver{executionID: executionID, store: store, next: drivers[index]}
	}
	return agent.Engine{
		Provider: driver,
		Tools:    tool.NewBus(wrapped...),
		Hooks:    agent.NewHookChain(approvalHook{executionID: executionID, store: store}),
		Model:    "approval-model",
		ToolMode: mode,
	}
}

type approvalDemoProvider struct {
	calls atomic.Int32
}

func (*approvalDemoProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "approval-demo"}
}

func (driver *approvalDemoProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	if driver.calls.Add(1) == 1 {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "send-1", Name: "send", Arguments: json.RawMessage(`{"text":"ship"}`)}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "approved and sent"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

type approvalRun struct {
	result agent.Result
	err    error
}

func runApprovalDemo(ctx context.Context) error {
	const executionID durable.ExecutionID = "approval-run"
	backend := newBackend()
	approvals := newApprovalStore()
	driver := &approvalDemoProvider{}
	var toolCalls atomic.Int32
	send, err := kit.Tool("send", func(_ context.Context, input struct {
		Text string `json:"text"`
	},
	) (string, error) {
		toolCalls.Add(1)
		return "sent:" + input.Text, nil
	})
	if err != nil {
		return err
	}
	first, err := durable.New(backend, durable.Options{OwnerID: "approval-process-one"})
	if err != nil {
		return err
	}
	runDone := make(chan approvalRun, 1)
	go func() {
		result, runErr := first.Start(ctx, executionID, approvalEngine(executionID, approvals, driver, tool.ModeSequential, send), agent.Request{Prompt: "send with approval"}, agent.OutputPolicy{})
		runDone <- approvalRun{result: result, err: runErr}
	}()
	request, err := approvals.nextRequest(ctx)
	if err != nil {
		return err
	}
	if err := first.Suspend(ctx, executionID); err != nil {
		return err
	}
	if run := <-runDone; !errors.Is(run.err, durable.ErrSuspended) {
		return fmt.Errorf("approval start: expected suspension: %w", run.err)
	}
	if _, err := approvals.decide(request.Key, approvalApproved, "demo-operator"); err != nil {
		return err
	}
	execution, err := backend.LoadExecution(ctx, executionID)
	if err != nil || execution.Checkpoint == nil {
		return fmt.Errorf("load approval checkpoint: %w", err)
	}
	second, err := durable.New(backend.reopen(), durable.Options{OwnerID: "approval-process-two"})
	if err != nil {
		return err
	}
	result, err := second.ResumeWithOptions(ctx, executionID, approvalEngine(executionID, approvals.reopen(), driver, tool.ModeSequential, send), durable.ResumeOptions{Target: durable.ResumeTarget{
		CheckpointSequence: execution.Checkpoint.Sequence,
		Phase:              agent.ContinuationModelComplete,
		OperationID:        request.Key.OperationID,
	}})
	if err != nil {
		return err
	}
	if result.Failure != nil {
		return result.Failure
	}
	fmt.Printf("approval: suspend->approve->targeted-resume; tool calls=%d; text=%q\n", toolCalls.Load(), result.Text)
	return nil
}
