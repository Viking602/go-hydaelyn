package worker

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/tool"
)

type GovernedToolBus struct {
	Runner      *venat.Runner
	Bus         *tool.Bus
	RunID       string
	TaskID      string
	LeaseID     string
	HolderType  api.HolderType
	HolderID    string
	TaskVersion int
}

func (b GovernedToolBus) ToolBus() *tool.Bus {
	if b.Bus == nil {
		return tool.NewBus()
	}
	drivers := make([]tool.Driver, 0)
	for _, def := range b.Bus.Definitions() {
		driver, ok := b.Bus.Driver(def.Name)
		if !ok {
			continue
		}
		if b.Runner != nil {
			b.Runner.RegisterTool(toolDefinitionToRunnerTool(def))
		}
		drivers = append(drivers, governedToolDriver{bus: b, driver: driver, definition: def})
	}
	return tool.NewBus(drivers...)
}

func (b GovernedToolBus) Execute(ctx context.Context, call tool.Call, sink tool.UpdateSink) (tool.Result, error) {
	if b.Bus == nil {
		return tool.Result{}, tool.ErrToolNotFound
	}
	driver, ok := b.Bus.Driver(call.Name)
	if !ok {
		return tool.Result{}, tool.ErrToolNotFound
	}
	def := driver.Definition()
	if b.Runner != nil {
		b.Runner.RegisterTool(toolDefinitionToRunnerTool(def))
	}
	return governedToolDriver{bus: b, driver: driver, definition: def}.Execute(ctx, call, sink)
}

type governedToolDriver struct {
	bus        GovernedToolBus
	driver     tool.Driver
	definition tool.Definition
}

func (d governedToolDriver) Definition() tool.Definition {
	return d.definition
}

func (d governedToolDriver) prepare(
	ctx context.Context,
	call tool.Call,
	sink tool.UpdateSink,
) (tool.Call, func(context.Context) (tool.Result, error), tool.Result, bool, error) {
	execute := func(runCtx context.Context) (tool.Result, error) {
		return d.driver.Execute(runCtx, call, sink)
	}
	preparing, ok := d.driver.(tool.PreparingDriver)
	if !ok {
		return call, execute, tool.Result{}, false, nil
	}
	prepared, err := preparing.Prepare(ctx, call, sink)
	if prepared.Call.ID != "" {
		call = prepared.Call
	}
	if err != nil {
		return call, nil, prepared.Result, false, err
	}
	if prepared.Complete {
		return call, nil, prepared.Result, true, nil
	}
	if prepared.Execute == nil {
		return call, nil, tool.Result{}, false, fmt.Errorf("worker: guarded tool %q returned an empty prepared execution", call.Name)
	}
	return call, prepared.Execute, tool.Result{}, false, nil
}

func (d governedToolDriver) Execute(ctx context.Context, call tool.Call, sink tool.UpdateSink) (tool.Result, error) {
	if d.bus.Runner == nil {
		return d.driver.Execute(ctx, call, sink)
	}
	_, err := d.bus.Runner.InvokeTool(ctx, api.ToolInvocation{
		RunID:       d.bus.RunID,
		TaskID:      d.bus.TaskID,
		LeaseID:     d.bus.LeaseID,
		HolderType:  d.bus.HolderType,
		HolderID:    d.bus.HolderID,
		TaskVersion: d.bus.TaskVersion,
		ToolName:    call.Name,
		Input:       rawToolInput(call.Arguments),
	})
	if err != nil {
		return tool.Result{}, err
	}
	if !requiresActionAttempt(d.definition) {
		return d.driver.Execute(ctx, call, sink)
	}
	if call.ID == "" {
		return tool.Result{}, fmt.Errorf("worker: guarded tool %q requires a call ID", call.Name)
	}
	call, execute, preparedResult, complete, err := d.prepare(ctx, call, sink)
	if err != nil || complete {
		return preparedResult, err
	}
	canonicalArguments, err := canonicalToolArguments(call.Arguments)
	if err != nil {
		return tool.Result{}, fmt.Errorf("worker: canonicalize guarded tool %q arguments: %w", call.Name, err)
	}
	inputHash := fmt.Sprintf("%x", sha256.Sum256(canonicalArguments))
	idempotencyKey := call.ID
	if !d.definition.Idempotent {
		// The agent loop assigns a stable logical slot before dispatch. A
		// provider may regenerate its call ID after a crash, but the same turn
		// and call position retain OperationID. InputHash then rejects a changed
		// operation at that slot instead of replaying it under a new key.
		if call.OperationID != "" {
			idempotencyKey = "operation:" + call.OperationID
		} else {
			// Compatibility for direct callers that do not run through Engine.
			idempotencyKey = "input:" + inputHash
		}
	}
	requestedAttemptID, err := newAttemptID()
	if err != nil {
		return tool.Result{}, err
	}
	attempt, err := d.bus.Runner.StartActionAttempt(ctx, api.StartActionAttemptCommand{
		AttemptID:      requestedAttemptID,
		ActionID:       call.ID,
		RunID:          d.bus.RunID,
		TaskID:         d.bus.TaskID,
		LeaseID:        d.bus.LeaseID,
		HolderType:     d.bus.HolderType,
		HolderID:       d.bus.HolderID,
		TaskVersion:    d.bus.TaskVersion,
		ToolName:       call.Name,
		IdempotencyKey: idempotencyKey,
		InputHash:      inputHash,
	})
	if err != nil {
		return tool.Result{}, err
	}
	if attempt.AttemptID != requestedAttemptID {
		if result, resolved := terminalAttemptOutput(call, attempt); resolved {
			return result, nil
		}
		return tool.Result{}, venat.ErrActionReconcileRequired
	}
	if attempt.RequiresReconcile || attempt.Status == api.ActionAttemptUnknown {
		return tool.Result{}, venat.ErrActionReconcileRequired
	}

	result, executeErr := execute(ctx)
	encodedResult, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		executeErr = errors.Join(executeErr, fmt.Errorf("worker: encode guarded tool %q result: %w", call.Name, encodeErr))
	}
	status, requiresReconcile := actionAttemptCompletion(result, executeErr)
	if requiresReconcile {
		encodedResult = nil
	}
	_, completeErr := d.bus.Runner.CompleteActionAttempt(context.WithoutCancel(ctx), api.CompleteActionAttemptCommand{
		RunID:             d.bus.RunID,
		TaskID:            d.bus.TaskID,
		LeaseID:           d.bus.LeaseID,
		HolderType:        d.bus.HolderType,
		HolderID:          d.bus.HolderID,
		TaskVersion:       d.bus.TaskVersion,
		AttemptID:         attempt.AttemptID,
		Status:            status,
		ExternalResultRef: result.Content,
		ToolResult:        encodedResult,
		RequiresReconcile: requiresReconcile,
	})
	if executeErr != nil {
		return result, errors.Join(executeErr, completeErr)
	}
	if completeErr != nil {
		return result, completeErr
	}
	return result, nil
}

func actionAttemptCompletion(result tool.Result, executeErr error) (api.ActionAttemptStatus, bool) {
	switch {
	case errors.Is(executeErr, tool.ErrNotExecuted):
		return api.ActionAttemptFailed, false
	case executeErr != nil:
		return api.ActionAttemptUnknown, true
	case result.IsError:
		return api.ActionAttemptFailed, false
	default:
		return api.ActionAttemptSucceeded, false
	}
}

func terminalAttemptOutput(call tool.Call, attempt api.ActionAttempt) (tool.Result, bool) {
	switch attempt.Status {
	case api.ActionAttemptSucceeded, api.ActionAttemptFailed, api.ActionAttemptTimeout, api.ActionAttemptCancelled:
		if len(attempt.ToolResult) > 0 {
			var result tool.Result
			if err := json.Unmarshal(attempt.ToolResult, &result); err != nil {
				return tool.Result{}, false
			}
			result.ToolCallID = call.ID
			return result, true
		}
		content := attempt.ExternalResultRef
		if content == "" {
			content = "durable action " + string(attempt.Status)
		}
		return tool.Result{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    content,
			IsError:    attempt.Status != api.ActionAttemptSucceeded,
		}, true
	default:
		return tool.Result{}, false
	}
}

func requiresActionAttempt(def tool.Definition) bool {
	return def.RequiresActionTask ||
		def.RequiresApproval ||
		def.Security.RequiresApproval ||
		def.EffectType == tool.EffectWrite ||
		def.EffectType == tool.EffectExternalSideEffect
}

func newAttemptID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("worker: generate action attempt ID: %w", err)
	}
	return fmt.Sprintf("attempt-%x", random[:]), nil
}

func toolDefinitionToRunnerTool(def tool.Definition) api.Tool {
	requiresApproval := def.RequiresApproval || def.Security.RequiresApproval
	effect := def.EffectType
	if effect == "" && requiresApproval {
		effect = tool.EffectExternalSideEffect
	}
	return api.Tool{
		Name:               def.Name,
		EffectType:         api.ToolEffectType(effect),
		RequiresActionTask: def.RequiresActionTask || requiresApproval,
		RiskLevel:          firstNonEmpty(def.RiskLevel, def.Security.RiskLevel),
		Idempotent:         def.Idempotent || def.Security.Idempotent,
		Timeout:            def.Timeout,
		RetryPolicy: api.RetryPolicy{
			MaxAttempts: def.RetryPolicy.MaxAttempts,
			Backoff:     def.RetryPolicy.Backoff,
		},
		PolicyTags: def.PolicyTags,
		Metadata:   def.Metadata,
	}
}

func canonicalToolArguments(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return []byte("null"), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return json.Marshal(value)
}

func rawToolInput(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return string(raw)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
