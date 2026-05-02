package worker

import (
	"context"
	"encoding/json"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/tool"
)

type GovernedToolBus struct {
	Runner      *hydaelyn.Runner
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
	if d.bus.Runner != nil {
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
	}
	return d.driver.Execute(ctx, call, sink)
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
