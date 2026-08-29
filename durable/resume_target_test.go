package durable_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/Viking602/venat/agent"
	. "github.com/Viking602/venat/durable"
	"github.com/Viking602/venat/durable/internal/testbackend"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

type countingResumeTargetHook struct {
	calls atomic.Int32
}

func (hook *countingResumeTargetHook) TransformContext(_ context.Context, messages []message.Message) ([]message.Message, error) {
	hook.calls.Add(1)
	return messages, nil
}

func (hook *countingResumeTargetHook) BeforeModelCall(context.Context, *provider.Request) error {
	hook.calls.Add(1)
	return nil
}

func (hook *countingResumeTargetHook) BeforeToolCall(context.Context, *tool.Call) error {
	hook.calls.Add(1)
	return nil
}

func (hook *countingResumeTargetHook) AfterToolCall(context.Context, *tool.Result) error {
	hook.calls.Add(1)
	return nil
}

func (hook *countingResumeTargetHook) OnEvent(context.Context, provider.Event) error {
	hook.calls.Add(1)
	return nil
}

func TestRuntime_ResumeTargetReadyMatchAndMismatch(t *testing.T) {
	store := testbackend.New()
	fault := &failSaveBackend{Backend: store, phase: agent.ContinuationReady, afterCommit: true}
	first := newTestRuntime(t, fault, Options{OwnerID: "target-writer"})
	driver := &runtimeProvider{responses: []func(context.Context, provider.Request) (provider.Stream, error){finalEvents("targeted")}}
	if _, err := first.Start(context.Background(), "target-ready", testEngine(driver), testRequest("resume exactly"), agent.OutputPolicy{}); !errors.Is(err, errInjectedBackend) {
		t.Fatalf("Start() error = %v, want injected checkpoint failure", err)
	}
	execution, err := store.LoadExecution(context.Background(), "target-ready")
	if err != nil || execution.Checkpoint == nil {
		t.Fatalf("LoadExecution() = %#v, %v", execution, err)
	}
	sequence := execution.Checkpoint.Sequence
	actualTarget := ResumeTarget{
		CheckpointSequence: sequence,
		Phase:              agent.ContinuationReady,
		OperationID:        "turn:0:model",
	}
	mismatches := []ResumeTarget{
		{CheckpointSequence: sequence + 1},
		{Phase: agent.ContinuationModelComplete},
		{OperationID: "turn:9:model"},
	}
	for index, expected := range mismatches {
		hook := &countingResumeTargetHook{}
		engine := testEngine(driver)
		engine.Hooks = agent.NewHookChain(hook)
		sinkCalls := 0
		resumer := newTestRuntime(t, store.Reopen(), Options{OwnerID: fmt.Sprintf("target-mismatch-%d", index)})
		_, err := resumer.ResumeStreamWithOptions(
			context.Background(),
			"target-ready",
			engine,
			agent.SinkFunc(func(context.Context, agent.Frame) error {
				sinkCalls++
				return nil
			}),
			ResumeOptions{Target: expected},
		)
		var mismatch *ResumeTargetError
		if !errors.Is(err, ErrResumeTargetMismatch) || !errors.As(err, &mismatch) {
			t.Fatalf("ResumeStreamWithOptions(%#v) error = %v", expected, err)
		}
		if mismatch.ExecutionID != "target-ready" || mismatch.Expected != expected || mismatch.Actual != actualTarget || !reflect.DeepEqual(mismatch.AvailableOperationIDs, []string{"turn:0:model"}) {
			t.Fatalf("target mismatch facts = %#v, want expected=%#v actual=%#v", mismatch, expected, actualTarget)
		}
		if driver.callCount() != 0 || hook.calls.Load() != 0 || sinkCalls != 0 {
			t.Fatalf("mismatch effects: provider=%d hook=%d sink=%d", driver.callCount(), hook.calls.Load(), sinkCalls)
		}
		released, loadErr := store.LoadExecution(context.Background(), "target-ready")
		if loadErr != nil || released.Lease != nil || released.Checkpoint == nil || released.Checkpoint.Sequence != sequence {
			t.Fatalf("released execution = %#v, %v", released, loadErr)
		}
	}

	frames := 0
	resumer := newTestRuntime(t, store.Reopen(), Options{OwnerID: "target-match"})
	result, err := resumer.ResumeStreamWithOptions(
		context.Background(),
		"target-ready",
		testEngine(driver),
		agent.SinkFunc(func(context.Context, agent.Frame) error {
			frames++
			return nil
		}),
		ResumeOptions{Target: actualTarget},
	)
	if err != nil {
		t.Fatalf("matching ResumeStreamWithOptions() error = %v", err)
	}
	if result.Text != "targeted" || driver.callCount() != 1 || frames == 0 {
		t.Fatalf("result = %#v, provider calls = %d, frames = %d", result, driver.callCount(), frames)
	}
}

func TestRuntime_ResumeTargetModelCompleteAcceptsPendingToolSlot(t *testing.T) {
	store := testbackend.New()
	fault := &failSaveBackend{Backend: store, phase: agent.ContinuationModelComplete, afterCommit: true}
	first := newTestRuntime(t, fault, Options{OwnerID: "target-tool-writer"})
	driver := &runtimeProvider{responses: []func(context.Context, provider.Request) (provider.Stream, error){
		providerEvents(
			provider.Event{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"query":"one"}`)}},
			provider.Event{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "call-2", Name: "lookup", Arguments: json.RawMessage(`{"query":"two"}`)}},
			provider.Event{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		),
		finalEvents("approved tools complete"),
	}}
	var toolCalls atomic.Int32
	lookup, err := kit.Tool("lookup", func(_ context.Context, input struct {
		Query string `json:"query"`
	},
	) (string, error) {
		toolCalls.Add(1)
		return "found:" + input.Query, nil
	})
	if err != nil {
		t.Fatalf("kit.Tool() error = %v", err)
	}
	if _, err := first.Start(context.Background(), "target-tools", testEngine(driver, lookup), testRequest("look up twice"), agent.OutputPolicy{}); !errors.Is(err, errInjectedBackend) {
		t.Fatalf("Start() error = %v", err)
	}
	execution, err := store.LoadExecution(context.Background(), "target-tools")
	if err != nil || execution.Checkpoint == nil {
		t.Fatalf("LoadExecution() = %#v, %v", execution, err)
	}
	sequence := execution.Checkpoint.Sequence

	hook := &countingResumeTargetHook{}
	mismatchEngine := testEngine(driver, lookup)
	mismatchEngine.Hooks = agent.NewHookChain(hook)
	mismatched := newTestRuntime(t, store.Reopen(), Options{OwnerID: "target-tool-mismatch"})
	_, err = mismatched.ResumeWithOptions(context.Background(), "target-tools", mismatchEngine, ResumeOptions{Target: ResumeTarget{
		CheckpointSequence: sequence,
		Phase:              agent.ContinuationModelComplete,
		OperationID:        "turn:0:call:9",
	}})
	var targetErr *ResumeTargetError
	if !errors.Is(err, ErrResumeTargetMismatch) || !errors.As(err, &targetErr) {
		t.Fatalf("mismatched ResumeWithOptions() error = %v", err)
	}
	if targetErr.Actual.CheckpointSequence != sequence || targetErr.Actual.Phase != agent.ContinuationModelComplete || targetErr.Actual.OperationID != "" || !reflect.DeepEqual(targetErr.AvailableOperationIDs, []string{"turn:0:call:0", "turn:0:call:1"}) {
		t.Fatalf("target mismatch facts = %#v", targetErr)
	}
	if driver.callCount() != 1 || toolCalls.Load() != 0 || hook.calls.Load() != 0 {
		t.Fatalf("mismatch effects: provider=%d tool=%d hook=%d", driver.callCount(), toolCalls.Load(), hook.calls.Load())
	}

	resumer := newTestRuntime(t, store.Reopen(), Options{OwnerID: "target-tool-match"})
	result, err := resumer.ResumeWithOptions(context.Background(), "target-tools", testEngine(driver, lookup), ResumeOptions{Target: ResumeTarget{
		CheckpointSequence: sequence,
		Phase:              agent.ContinuationModelComplete,
		OperationID:        "turn:0:call:1",
	}})
	if err != nil {
		t.Fatalf("matching ResumeWithOptions() error = %v", err)
	}
	if result.Text != "approved tools complete" || driver.callCount() != 2 || toolCalls.Load() != 2 {
		t.Fatalf("result = %#v, provider=%d tool=%d", result, driver.callCount(), toolCalls.Load())
	}
}

func TestRuntime_ResumeTargetDoesNotBypassReconciliation(t *testing.T) {
	store := testbackend.New()
	runtime := newTestRuntime(t, store, Options{OwnerID: "target-reconcile-writer"})
	driver := &runtimeProvider{responses: []func(context.Context, provider.Request) (provider.Stream, error){
		func(context.Context, provider.Request) (provider.Stream, error) {
			return &partialFailureStream{
				events: []provider.Event{{Kind: provider.EventTextDelta, Text: "partial"}},
				err:    errors.New("outcome unknown"),
			}, nil
		},
	}}
	_, err := runtime.Start(context.Background(), "target-reconcile", testEngine(driver), testRequest("work"), agent.OutputPolicy{})
	if !errors.Is(err, ErrReconcileRequired) {
		t.Fatalf("Start() error = %v, want ErrReconcileRequired", err)
	}
	resumer := newTestRuntime(t, store.Reopen(), Options{OwnerID: "target-reconcile-reader"})
	_, err = resumer.ResumeWithOptions(context.Background(), "target-reconcile", testEngine(driver), ResumeOptions{Target: ResumeTarget{
		CheckpointSequence: 999,
		Phase:              agent.ContinuationModelComplete,
		OperationID:        "turn:99:call:0",
	}})
	if !errors.Is(err, ErrReconcileRequired) || errors.Is(err, ErrResumeTargetMismatch) {
		t.Fatalf("ResumeWithOptions() error = %v, want reconciliation precedence", err)
	}
	if driver.callCount() != 1 {
		t.Fatalf("provider calls = %d, want original call only", driver.callCount())
	}
}

func TestRuntime_ResumeTargetValidatesCheckpointBeforeComparison(t *testing.T) {
	store := testbackend.New()
	fault := &failSaveBackend{Backend: store, phase: agent.ContinuationReady, afterCommit: true}
	first := newTestRuntime(t, fault, Options{OwnerID: "target-corrupt-writer"})
	driver := &runtimeProvider{responses: []func(context.Context, provider.Request) (provider.Stream, error){finalEvents("unused")}}
	if _, err := first.Start(context.Background(), "target-corrupt", testEngine(driver), testRequest("persist"), agent.OutputPolicy{}); !errors.Is(err, errInjectedBackend) {
		t.Fatalf("Start() error = %v", err)
	}
	corrupt := newTestRuntime(t, corruptResumeBackend{Backend: store.Reopen()}, Options{OwnerID: "target-corrupt-reader"})
	_, err := corrupt.ResumeWithOptions(context.Background(), "target-corrupt", testEngine(driver), ResumeOptions{Target: ResumeTarget{
		CheckpointSequence: 999,
		Phase:              agent.ContinuationModelComplete,
	}})
	if !errors.Is(err, ErrCorruptCheckpoint) || errors.Is(err, ErrResumeTargetMismatch) {
		t.Fatalf("ResumeWithOptions() error = %v, want checkpoint validation precedence", err)
	}
	if driver.callCount() != 0 {
		t.Fatalf("provider calls = %d, want 0", driver.callCount())
	}
}
