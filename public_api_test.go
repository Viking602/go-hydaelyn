package hydaelyn

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/flow"
	"github.com/Viking602/go-hydaelyn/policy"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/transport/mcp"
)

func TestPublicAPISmoke(t *testing.T) {
	var _ agent.Engine
	var _ agent.AgentProfile
	var _ blackboard.Item
	var _ blackboard.Selector
	var _ flow.Flow
	var _ policy.Engine = policy.EngineFunc(func(context.Context, policy.Request) (policy.Decision, error) {
		return policy.Decision{Effect: policy.EffectAllow}, nil
	})
	var _ provider.Driver
	var _ mcp.Gateway
	var _ tool.Mode
	_ = Tool{Name: "write", EffectType: tool.EffectWrite, RequiresActionTask: true}

	runner := New(Config{})
	var _ *Runtime = runner
	run, err := runner.QueueRun(context.Background(), StartRunCommand{Request: "primary runtime smoke"})
	if err != nil {
		t.Fatalf("QueueRun() error = %v", err)
	}
	if run.ID == "" {
		t.Fatalf("QueueRun() returned empty run: %#v", run)
	}
	if events, err := runner.RunEvents(context.Background(), run.ID); err != nil || len(events) == 0 {
		t.Fatalf("RunEvents() returned no events for queued run")
	}
}
