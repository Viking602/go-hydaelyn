package worker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

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
		IdempotencyKey: call.ID,
		InputHash:      fmt.Sprintf("%x", sha256.Sum256(call.Arguments)),
	})
	if err != nil {
		return tool.Result{}, err
	}
	if attempt.RequiresReconcile || attempt.Status == api.ActionAttemptUnknown {
		return tool.Result{}, venat.ErrActionReconcileRequired
	}
	if attempt.AttemptID != requestedAttemptID && !toolDefinitionIdempotent(d.definition) {
		return tool.Result{}, venat.ErrActionReconcileRequired
	}

	result, executeErr := d.driver.Execute(ctx, call, sink)
	status := api.ActionAttemptSucceeded
	requiresReconcile := false
	switch {
	case executeErr != nil:
		status = api.ActionAttemptUnknown
		requiresReconcile = true
	case result.IsError:
		status = api.ActionAttemptFailed
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

func requiresActionAttempt(def tool.Definition) bool {
	return def.RequiresActionTask ||
		def.RequiresApproval ||
		def.Security.RequiresApproval ||
		def.EffectType == tool.EffectWrite ||
		def.EffectType == tool.EffectExternalSideEffect
}

func toolDefinitionIdempotent(def tool.Definition) bool {
	return def.Idempotent || def.Security.Idempotent
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
