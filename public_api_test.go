package hydaelyn

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/mcp"
	"github.com/Viking602/go-hydaelyn/observe"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/internal/plugin"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/internal/security"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/tool/kit"
)

func TestPublicAPISmoke(t *testing.T) {
	var _ agent.Engine
	var _ blackboard.State
	var _ capability.Call
	var _ capability.Policy
	var _ host.Runtime
	var _ mcp.Gateway
	var _ observe.Observer = observe.NewMemoryObserver()
	var _ planner.Plan
	var _ plugin.Spec
	var _ security.Context
	var _ scheduler.TaskLease
	var _ team.RunState
	var _ tool.Mode
	_ = kit.Profile("researcher")

	runner := host.New(host.Config{})
	runner.RegisterCapability(capability.TypeSearch, "web", func(context.Context, capability.Call) (capability.Result, error) {
		return capability.Result{Output: "ok"}, nil
	})
	if _, err := runner.InvokeCapability(context.Background(), capability.Call{Type: capability.TypeSearch, Name: "web"}); err != nil {
		t.Fatalf("InvokeCapability() error = %v", err)
	}
}
