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
	"time"

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
	UsagePricer UsagePricer
}

func (b GovernedToolBus) registerTool(def tool.Definition) {
	if b.Runner == nil {
		return
	}
	registered := toolDefinitionToRunnerTool(def)
	if b.HolderType == api.HolderAgent || (b.HolderType != "" && b.HolderID != "") {
		b.Runner.RegisterToolForInvocation(b.RunID, b.TaskID, b.HolderType, b.HolderID, registered)
		return
	}
	b.Runner.RegisterTool(registered)
}

func (b GovernedToolBus) ToolBus() *tool.Bus {
	if b.Bus == nil {
		return tool.NewBus()
	}
	return b.Bus.MapDrivers(func(def tool.Definition, driver tool.Driver) tool.Driver {
		b.registerTool(def)
		return governedToolDriver{bus: b, driver: driver, definition: def}
	})
}

func (b GovernedToolBus) Execute(ctx context.Context, call tool.Call, sink tool.UpdateSink) (tool.Result, error) {
	if b.Bus == nil {
		return tool.Result{}, tool.ErrToolNotFound
	}
	return b.ToolBus().Execute(ctx, call, sink)
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
	authorization, err := d.bus.Runner.InvokeTool(ctx, api.ToolInvocation{
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
		if replayErr := d.actionReplayError(ctx, call); replayErr != nil {
			return tool.Result{}, errors.Join(err, replayErr)
		}
		return tool.Result{}, err
	}
	if !requiresActionAttempt(d.definition) {
		result, executeErr := d.driver.Execute(ctx, call, sink)
		enforced, _, enforcementErr := d.enforceResult(ctx, authorization.Decision, result)
		return enforced, errors.Join(executeErr, enforcementErr)
	}
	return d.executeAction(ctx, call, sink, authorization.Decision)
}

func (d governedToolDriver) executeAction(
	ctx context.Context,
	call tool.Call,
	sink tool.UpdateSink,
	decision api.PolicyDecision,
) (tool.Result, error) {
	if call.ID == "" {
		return tool.Result{}, fmt.Errorf("worker: guarded tool %q requires a call ID", call.Name)
	}
	call, execute, preparedResult, complete, err := d.prepare(ctx, call, sink)
	if err != nil {
		return tool.Result{}, err
	}
	if complete {
		enforced, _, enforcementErr := d.enforceResult(ctx, decision, preparedResult)
		return enforced, enforcementErr
	}
	attempt, requestedAttemptID, idempotencyKey, err := d.startActionAttempt(ctx, call)
	if err != nil {
		return tool.Result{}, err
	}
	if attempt.AttemptID != requestedAttemptID {
		if result, resolved := terminalAttemptOutput(call, attempt); resolved {
			enforced, _, enforcementErr := d.enforceResult(ctx, decision, result)
			return enforced, enforcementErr
		}
		return tool.Result{}, venat.ErrActionReconcileRequired
	}
	if attempt.RequiresReconcile || attempt.Status == api.ActionAttemptUnknown {
		return tool.Result{}, venat.ErrActionReconcileRequired
	}
	return d.completeActionAttempt(ctx, call, execute, decision, attempt, idempotencyKey)
}

func (d governedToolDriver) startActionAttempt(
	ctx context.Context,
	call tool.Call,
) (api.ActionAttempt, string, string, error) {
	idempotencyKey, inputHash, err := actionAttemptIdentity(call, d.definition)
	if err != nil {
		return api.ActionAttempt{}, "", "", fmt.Errorf("worker: canonicalize guarded tool %q arguments: %w", call.Name, err)
	}
	requestedAttemptID, err := newAttemptID()
	if err != nil {
		return api.ActionAttempt{}, "", "", err
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
	return attempt, requestedAttemptID, idempotencyKey, err
}

func (d governedToolDriver) actionReplayError(ctx context.Context, call tool.Call) error {
	if !requiresActionAttempt(d.definition) {
		return nil
	}
	idempotencyKey, inputHash, err := actionAttemptIdentity(call, d.definition)
	if err != nil || idempotencyKey == "" {
		return nil
	}
	attempts, err := d.bus.Runner.ListActionAttempts(ctx, api.ActionAttemptSelector{
		RunID: d.bus.RunID, TaskID: d.bus.TaskID, ToolName: call.Name,
	})
	if err != nil {
		return nil
	}
	for _, attempt := range attempts {
		if attempt.IdempotencyKey != idempotencyKey {
			continue
		}
		if attempt.InputHash != inputHash {
			return venat.ErrIdempotencyConflict
		}
		if attempt.RequiresReconcile || attempt.Status == api.ActionAttemptUnknown {
			return venat.ErrActionReconcileRequired
		}
	}
	return nil
}

func actionAttemptIdentity(call tool.Call, definition tool.Definition) (string, string, error) {
	canonicalArguments, err := canonicalToolArguments(call.Arguments)
	if err != nil {
		return "", "", err
	}
	idempotencyKey := call.ID
	if !definition.Idempotent && call.OperationID != "" {
		// The agent loop assigns a stable logical slot before dispatch. A
		// provider may regenerate its call ID after a crash, but the same turn
		// and call position retain OperationID. InputHash then rejects a changed
		// operation at that slot instead of replaying it under a new key. Direct
		// callers have no durable slot, so their distinct call IDs remain
		// distinct even when their arguments match.
		idempotencyKey = "operation:" + call.OperationID
	}
	return idempotencyKey, fmt.Sprintf("%x", sha256.Sum256(canonicalArguments)), nil
}

func (d governedToolDriver) completeActionAttempt(
	ctx context.Context,
	call tool.Call,
	execute func(context.Context) (tool.Result, error),
	decision api.PolicyDecision,
	attempt api.ActionAttempt,
	idempotencyKey string,
) (tool.Result, error) {
	startedAt := time.Now()
	result, executeErr := execute(ctx)
	status, requiresReconcile := actionAttemptCompletion(result, executeErr)
	enforcedResult, encodedResult, enforcementErr := d.enforceResult(ctx, decision, result)
	if enforcementErr != nil && status == api.ActionAttemptSucceeded {
		status = api.ActionAttemptUnknown
		requiresReconcile = true
	}
	if requiresReconcile || enforcementErr != nil {
		encodedResult = nil
	}
	usageRecord := d.actionUsageRecord(ctx, call, attempt, idempotencyKey, status, startedAt)
	pricedUsage, priceErr := priceUsageRecord(ctx, d.bus.UsagePricer, usageRecord)
	if priceErr != nil {
		status = api.ActionAttemptUnknown
		requiresReconcile = true
		encodedResult = nil
		usageRecord = pricedUsage
		usageRecord.Metadata["status"] = string(status)
	} else {
		usageRecord = pricedUsage
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
		ExternalResultRef: enforcedResult.Content,
		ToolResult:        encodedResult,
		RequiresReconcile: requiresReconcile,
		UsageRecord:       &usageRecord,
	})
	if executeErr != nil {
		return enforcedResult, errors.Join(executeErr, enforcementErr, priceErr, completeErr)
	}
	if enforcementErr != nil {
		return tool.Result{}, errors.Join(enforcementErr, priceErr, completeErr)
	}
	if completeErr != nil || priceErr != nil {
		return enforcedResult, errors.Join(completeErr, priceErr)
	}
	return enforcedResult, nil
}

func (d governedToolDriver) actionUsageRecord(
	_ context.Context,
	call tool.Call,
	attempt api.ActionAttempt,
	idempotencyKey string,
	status api.ActionAttemptStatus,
	startedAt time.Time,
) api.UsageRecord {
	usageKey := idempotencyKey
	if usageKey == "" {
		usageKey = attempt.AttemptID
	}
	agentID := ""
	if d.bus.HolderType == api.HolderAgent {
		agentID = d.bus.HolderID
	}
	return api.UsageRecord{
		ID:    stableUsageID(d.bus.RunID, d.bus.TaskID, d.bus.HolderID, string(api.UsageKindActionCall), usageKey),
		RunID: d.bus.RunID, TaskID: d.bus.TaskID, AgentID: agentID,
		Kind: api.UsageKindActionCall, ToolName: call.Name,
		DurationMS: time.Since(startedAt).Milliseconds(),
		Metadata:   map[string]string{"attemptId": attempt.AttemptID, "status": string(status)},
	}
}

func (d governedToolDriver) enforceResult(
	ctx context.Context,
	decision api.PolicyDecision,
	result tool.Result,
) (tool.Result, json.RawMessage, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return tool.Result{}, nil, fmt.Errorf("worker: encode guarded tool %q result: %w", d.definition.Name, err)
	}
	enforced, err := d.bus.Runner.EnforceToolResult(ctx, api.ToolResultEnforcementRequest{
		RunID:      d.bus.RunID,
		TaskID:     d.bus.TaskID,
		Decision:   decision,
		ToolResult: encoded,
	})
	if err != nil {
		return tool.Result{}, nil, err
	}
	var out tool.Result
	if err := json.Unmarshal(enforced.ToolResult, &out); err != nil {
		return tool.Result{}, nil, fmt.Errorf("worker: decode enforced tool %q result: %w", d.definition.Name, err)
	}
	return out, enforced.ToolResult, nil
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
			if result.Name == "" {
				result.Name = call.Name
			}
			result.IsError = result.IsError || attempt.Status != api.ActionAttemptSucceeded
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
