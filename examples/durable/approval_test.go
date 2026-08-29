package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/durable"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

type approvalScriptProvider struct {
	mu        sync.Mutex
	responses [][]provider.Event
	calls     int
}

func (*approvalScriptProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "approval-test"}
}

func (driver *approvalScriptProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	if driver.calls >= len(driver.responses) {
		return nil, errors.New("approval test provider exhausted")
	}
	events := append([]provider.Event(nil), driver.responses[driver.calls]...)
	driver.calls++
	return provider.NewSliceStream(events), nil
}

func (driver *approvalScriptProvider) callCount() int {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return driver.calls
}

func approvalTool(t *testing.T, calls *atomic.Int32) tool.Driver {
	t.Helper()
	driver, err := kit.Tool("act", func(_ context.Context, input struct {
		Value string `json:"value"`
	},
	) (string, error) {
		calls.Add(1)
		return "acted:" + input.Value, nil
	})
	if err != nil {
		t.Fatalf("kit.Tool() error = %v", err)
	}
	return driver
}

func approvalToolEvents(values ...string) []provider.Event {
	events := make([]provider.Event, 0, len(values)+1)
	for index, value := range values {
		events = append(events, provider.Event{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        fmt.Sprintf("call-%d", index),
				Name:      "act",
				Arguments: json.RawMessage(fmt.Sprintf(`{"value":%q}`, value)),
			},
		})
	}
	return append(events, provider.Event{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse})
}

func approvalFinalEvents(text string) []provider.Event {
	return []provider.Event{
		{Kind: provider.EventTextDelta, Text: text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}
}

func startAndSuspendForApproval(
	ctx context.Context,
	t *testing.T,
	runtime *durable.Runtime,
	executionID durable.ExecutionID,
	engine agent.Engine,
	approvals *approvalStore,
) approvalRequest {
	t.Helper()
	done := make(chan approvalRun, 1)
	go func() {
		result, err := runtime.Start(ctx, executionID, engine, agent.Request{Prompt: "run approved tools"}, agent.OutputPolicy{})
		done <- approvalRun{result: result, err: err}
	}()
	request, err := approvals.nextRequest(ctx)
	if err != nil {
		t.Fatalf("nextRequest() error = %v", err)
	}
	if err := runtime.Suspend(ctx, executionID); err != nil {
		t.Fatalf("Suspend() error = %v", err)
	}
	if outcome := <-done; !errors.Is(outcome.err, durable.ErrSuspended) {
		t.Fatalf("Start() error = %v, result = %#v, want ErrSuspended", outcome.err, outcome.result)
	}
	return request
}

func targetForRequest(ctx context.Context, t *testing.T, backend durable.Backend, executionID durable.ExecutionID, request approvalRequest) durable.ResumeTarget {
	t.Helper()
	execution, err := backend.LoadExecution(ctx, executionID)
	if err != nil || execution.Checkpoint == nil {
		t.Fatalf("LoadExecution() = %#v, %v", execution, err)
	}
	if execution.Status != durable.ExecutionStatusSuspended || execution.Lease != nil || execution.Checkpoint.Continuation.Phase != agent.ContinuationModelComplete {
		t.Fatalf("suspended execution = %#v", execution)
	}
	return durable.ResumeTarget{
		CheckpointSequence: execution.Checkpoint.Sequence,
		Phase:              agent.ContinuationModelComplete,
		OperationID:        request.Key.OperationID,
	}
}

func assertNoApprovalNotification(t *testing.T, store *approvalStore) {
	t.Helper()
	select {
	case request := <-store.state.notifications:
		t.Fatalf("unexpected duplicate approval notification = %#v", request)
	default:
	}
}

func TestApprovalFlow_ApproveIsIdempotentAcrossProcessReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const executionID durable.ExecutionID = "approval-approve"
	backend := newBackend()
	approvals := newApprovalStore()
	driver := &approvalScriptProvider{responses: [][]provider.Event{
		approvalToolEvents("ship"),
		approvalFinalEvents("approved complete"),
	}}
	var toolCalls atomic.Int32
	action := approvalTool(t, &toolCalls)
	first, err := durable.New(backend, durable.Options{OwnerID: "approve-one"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := startAndSuspendForApproval(ctx, t, first, executionID, approvalEngine(executionID, approvals, driver, tool.ModeSequential, action), approvals)
	target := targetForRequest(ctx, t, backend, executionID, request)
	pending, ok := approvals.lookup(request.Key)
	if !ok || pending.Status != approvalPending || len(pending.Audit) != 1 || approvals.requestCount() != 1 {
		t.Fatalf("pending approval = %#v, count = %d", pending, approvals.requestCount())
	}
	decided, err := approvals.decide(request.Key, approvalApproved, "operator")
	if err != nil {
		t.Fatalf("decide() error = %v", err)
	}
	repeated, err := approvals.decide(request.Key, approvalApproved, "operator")
	if err != nil || len(repeated.Audit) != 2 || len(decided.Audit) != 2 {
		t.Fatalf("idempotent decision = %#v, %v", repeated, err)
	}

	second, err := durable.New(backend.reopen(), durable.Options{OwnerID: "approve-two"})
	if err != nil {
		t.Fatalf("New(reopen) error = %v", err)
	}
	result, err := second.ResumeWithOptions(
		ctx,
		executionID,
		approvalEngine(executionID, approvals.reopen(), driver, tool.ModeSequential, action),
		durable.ResumeOptions{Target: target},
	)
	if err != nil {
		t.Fatalf("ResumeWithOptions() error = %v", err)
	}
	if result.Text != "approved complete" || toolCalls.Load() != 1 || driver.callCount() != 2 || approvals.requestCount() != 1 {
		t.Fatalf("result = %#v, tool=%d provider=%d requests=%d", result, toolCalls.Load(), driver.callCount(), approvals.requestCount())
	}
	final, _ := approvals.lookup(request.Key)
	if final.Status != approvalApproved || len(final.Audit) != 2 {
		t.Fatalf("approval record = %#v", final)
	}
	assertNoApprovalNotification(t, approvals)
}

func TestApprovalFlow_DenyReturnsLogicalToolErrorWithoutEffect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const executionID durable.ExecutionID = "approval-deny"
	backend := newBackend()
	approvals := newApprovalStore()
	driver := &approvalScriptProvider{responses: [][]provider.Event{
		approvalToolEvents("delete"),
		approvalFinalEvents("denial handled"),
	}}
	var toolCalls atomic.Int32
	action := approvalTool(t, &toolCalls)
	first, err := durable.New(backend, durable.Options{OwnerID: "deny-one"})
	if err != nil {
		t.Fatal(err)
	}
	request := startAndSuspendForApproval(ctx, t, first, executionID, approvalEngine(executionID, approvals, driver, tool.ModeSequential, action), approvals)
	target := targetForRequest(ctx, t, backend, executionID, request)
	if _, err := approvals.decide(request.Key, approvalDenied, "operator"); err != nil {
		t.Fatalf("decide() error = %v", err)
	}
	second, err := durable.New(backend.reopen(), durable.Options{OwnerID: "deny-two"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := second.ResumeWithOptions(ctx, executionID, approvalEngine(executionID, approvals.reopen(), driver, tool.ModeSequential, action), durable.ResumeOptions{Target: target})
	if err != nil {
		t.Fatalf("ResumeWithOptions() error = %v", err)
	}
	logicalError := false
	for _, current := range result.Messages {
		if current.ToolResult != nil && current.ToolResult.ToolCallID == "call-0" && current.ToolResult.IsError && current.ToolResult.Content == "approval denied by application" {
			logicalError = true
		}
	}
	if !logicalError || result.Text != "denial handled" || toolCalls.Load() != 0 || driver.callCount() != 2 {
		t.Fatalf("result = %#v, logical error=%v tool=%d provider=%d", result, logicalError, toolCalls.Load(), driver.callCount())
	}
}

func TestApprovalFlow_StaleTargetNeverRunsApprovedEffect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const executionID durable.ExecutionID = "approval-stale-target"
	backend := newBackend()
	approvals := newApprovalStore()
	driver := &approvalScriptProvider{responses: [][]provider.Event{
		approvalToolEvents("transfer"),
		approvalFinalEvents("transfer complete"),
	}}
	var toolCalls atomic.Int32
	action := approvalTool(t, &toolCalls)
	first, err := durable.New(backend, durable.Options{OwnerID: "stale-one"})
	if err != nil {
		t.Fatal(err)
	}
	request := startAndSuspendForApproval(ctx, t, first, executionID, approvalEngine(executionID, approvals, driver, tool.ModeSequential, action), approvals)
	target := targetForRequest(ctx, t, backend, executionID, request)
	if _, err := approvals.decide(request.Key, approvalApproved, "operator"); err != nil {
		t.Fatal(err)
	}
	mismatches := []durable.ResumeTarget{
		{CheckpointSequence: target.CheckpointSequence + 1, Phase: target.Phase, OperationID: target.OperationID},
		{CheckpointSequence: target.CheckpointSequence, Phase: target.Phase, OperationID: "turn:0:call:9"},
	}
	for index, mismatch := range mismatches {
		resumer, err := durable.New(backend.reopen(), durable.Options{OwnerID: fmt.Sprintf("stale-%d", index)})
		if err != nil {
			t.Fatal(err)
		}
		_, err = resumer.ResumeWithOptions(ctx, executionID, approvalEngine(executionID, approvals.reopen(), driver, tool.ModeSequential, action), durable.ResumeOptions{Target: mismatch})
		if !errors.Is(err, durable.ErrResumeTargetMismatch) {
			t.Fatalf("ResumeWithOptions(%#v) error = %v", mismatch, err)
		}
		if toolCalls.Load() != 0 || driver.callCount() != 1 {
			t.Fatalf("mismatch effects: tool=%d provider=%d", toolCalls.Load(), driver.callCount())
		}
		execution, loadErr := backend.LoadExecution(ctx, executionID)
		if loadErr != nil || execution.Lease != nil {
			t.Fatalf("mismatch retained lease: %#v, %v", execution, loadErr)
		}
	}
	resumer, err := durable.New(backend.reopen(), durable.Options{OwnerID: "stale-valid"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := resumer.ResumeWithOptions(ctx, executionID, approvalEngine(executionID, approvals.reopen(), driver, tool.ModeSequential, action), durable.ResumeOptions{Target: target})
	if err != nil || result.Text != "transfer complete" || toolCalls.Load() != 1 || driver.callCount() != 2 {
		t.Fatalf("valid result = %#v, error=%v tool=%d provider=%d", result, err, toolCalls.Load(), driver.callCount())
	}
}

func TestApprovalFlow_MultiplePendingCallsSettleOnceEach(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const executionID durable.ExecutionID = "approval-multiple"
	backend := newBackend()
	approvals := newApprovalStore()
	driver := &approvalScriptProvider{responses: [][]provider.Event{
		approvalToolEvents("one", "two"),
		approvalFinalEvents("both approved"),
	}}
	var toolCalls atomic.Int32
	action := approvalTool(t, &toolCalls)
	first, err := durable.New(backend, durable.Options{OwnerID: "multiple-one"})
	if err != nil {
		t.Fatal(err)
	}
	firstRequest := startAndSuspendForApproval(ctx, t, first, executionID, approvalEngine(executionID, approvals, driver, tool.ModeSequential, action), approvals)
	firstTarget := targetForRequest(ctx, t, backend, executionID, firstRequest)
	if _, err := approvals.decide(firstRequest.Key, approvalApproved, "operator"); err != nil {
		t.Fatal(err)
	}

	second, err := durable.New(backend.reopen(), durable.Options{OwnerID: "multiple-two"})
	if err != nil {
		t.Fatal(err)
	}
	resumeDone := make(chan approvalRun, 1)
	go func() {
		result, resumeErr := second.ResumeWithOptions(ctx, executionID, approvalEngine(executionID, approvals.reopen(), driver, tool.ModeSequential, action), durable.ResumeOptions{Target: firstTarget})
		resumeDone <- approvalRun{result: result, err: resumeErr}
	}()
	secondRequest, err := approvals.nextRequest(ctx)
	if err != nil {
		t.Fatalf("nextRequest(second) error = %v", err)
	}
	if secondRequest.Key.OperationID != "turn:0:call:1" || secondRequest.Key == firstRequest.Key {
		t.Fatalf("second request = %#v", secondRequest)
	}
	if err := second.Suspend(ctx, executionID); err != nil {
		t.Fatalf("second Suspend() error = %v", err)
	}
	if outcome := <-resumeDone; !errors.Is(outcome.err, durable.ErrSuspended) {
		t.Fatalf("second ResumeWithOptions() error = %v", outcome.err)
	}
	if toolCalls.Load() != 0 || driver.callCount() != 1 || approvals.requestCount() != 2 {
		t.Fatalf("after second suspension: tool=%d provider=%d requests=%d", toolCalls.Load(), driver.callCount(), approvals.requestCount())
	}
	secondTarget := targetForRequest(ctx, t, backend, executionID, secondRequest)
	if secondTarget.CheckpointSequence <= firstTarget.CheckpointSequence {
		t.Fatalf("checkpoint sequence did not advance on replay: first=%d second=%d", firstTarget.CheckpointSequence, secondTarget.CheckpointSequence)
	}
	if _, err := approvals.decide(secondRequest.Key, approvalApproved, "operator"); err != nil {
		t.Fatal(err)
	}

	third, err := durable.New(backend.reopen(), durable.Options{OwnerID: "multiple-three"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := third.ResumeWithOptions(ctx, executionID, approvalEngine(executionID, approvals.reopen(), driver, tool.ModeSequential, action), durable.ResumeOptions{Target: secondTarget})
	if err != nil {
		t.Fatalf("third ResumeWithOptions() error = %v", err)
	}
	if result.Text != "both approved" || toolCalls.Load() != 2 || driver.callCount() != 2 || approvals.requestCount() != 2 {
		t.Fatalf("result = %#v, tool=%d provider=%d requests=%d", result, toolCalls.Load(), driver.callCount(), approvals.requestCount())
	}
	for _, key := range []approvalKey{firstRequest.Key, secondRequest.Key} {
		record, _ := approvals.lookup(key)
		if record.Status != approvalApproved || len(record.Audit) != 2 {
			t.Fatalf("approval record %v = %#v", key, record)
		}
	}
	assertNoApprovalNotification(t, approvals)
}

func TestApprovalFlow_SuspendRaceCreatesOneRequestAndNoEffect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const executionID durable.ExecutionID = "approval-suspend-race"
	backend := newBackend()
	approvals := newApprovalStore()
	driver := &approvalScriptProvider{responses: [][]provider.Event{approvalToolEvents("race")}}
	var toolCalls atomic.Int32
	action := approvalTool(t, &toolCalls)
	runtime, err := durable.New(backend, durable.Options{OwnerID: "race"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan approvalRun, 1)
	go func() {
		result, runErr := runtime.Start(ctx, executionID, approvalEngine(executionID, approvals, driver, tool.ModeSequential, action), agent.Request{Prompt: "race"}, agent.OutputPolicy{})
		done <- approvalRun{result: result, err: runErr}
	}()
	request, err := approvals.nextRequest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsOut := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			errorsOut <- runtime.Suspend(ctx, executionID)
		}()
	}
	close(start)
	firstErr := <-errorsOut
	secondErr := <-errorsOut
	successes := 0
	loserOK := false
	for _, suspendErr := range []error{firstErr, secondErr} {
		if suspendErr == nil {
			successes++
		} else if errors.Is(suspendErr, durable.ErrBusy) || errors.Is(suspendErr, durable.ErrNotActive) {
			loserOK = true
		}
	}
	if successes != 1 || !loserOK {
		t.Fatalf("Suspend() race errors = [%v, %v]", firstErr, secondErr)
	}
	if outcome := <-done; !errors.Is(outcome.err, durable.ErrSuspended) {
		t.Fatalf("Start() error = %v", outcome.err)
	}
	if approvals.requestCount() != 1 || toolCalls.Load() != 0 || driver.callCount() != 1 {
		t.Fatalf("requests=%d tool=%d provider=%d", approvals.requestCount(), toolCalls.Load(), driver.callCount())
	}
	if _, err := approvals.decide(request.Key, approvalApproved, "operator"); err != nil {
		t.Fatal(err)
	}
	hook := approvalHook{executionID: executionID, store: approvals}
	if err := hook.BeforeToolCall(ctx, &tool.Call{ID: "call-0", Name: "act", OperationID: request.Key.OperationID}); err != nil {
		t.Fatalf("replayed approved hook error = %v", err)
	}
	assertNoApprovalNotification(t, approvals)
}
