package hydaelyn

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/flow"
	"github.com/Viking602/go-hydaelyn/policy"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/skill"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/transport/mcp"
)

type allowAPIEngine struct{}

func (allowAPIEngine) Authorize(context.Context, api.PolicyRequest) (api.PolicyDecision, error) {
	return api.PolicyDecision{Effect: api.PolicyEffectAllow}, nil
}

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
	var _ = skill.Skill{Name: "code-review", Description: "Review code"}
	var _ = skill.DiscoveryOptions{TrustProject: true}
	var _ = agent.Spec{AvailableSkills: []string{"code-review"}}
	if agent.SkillActivationToolName == "" || agent.SkillResourceToolName == "" {
		t.Fatal("skill runtime tool names must be exported")
	}
	skills := skill.NewRegistry()
	if err := skill.Register(skills, skill.Skill{Name: "code-review", Description: "Review code"}); err != nil {
		t.Fatalf("skill.Register() error = %v", err)
	}
	if got := len(skills.List()); got != 1 {
		t.Fatalf("skill registry List() length = %d, want 1", got)
	}
	_ = api.Tool{Name: "write", EffectType: api.ToolEffectWrite, RequiresActionTask: true}

	runner := New()
	run, err := runner.QueueRun(context.Background(), api.StartRunCommand{Request: "primary runner smoke"})
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

func TestRunnerConstructionModes(t *testing.T) {
	development := NewDevelopment()
	if development.Mode() != api.RuntimeModeDevelopment {
		t.Fatalf("NewDevelopment mode = %q", development.Mode())
	}

	if _, err := NewProduction(api.Config{}); !errors.Is(err, api.ErrInvalidConfiguration) {
		t.Fatalf("NewProduction(empty) error = %v", err)
	}
	if _, err := NewProduction(api.Config{StoreProvider: stubStoreProvider{}}); !errors.Is(err, api.ErrInvalidConfiguration) {
		t.Fatalf("NewProduction(without policy) error = %v", err)
	}
	var nilProvider *stubStoreProvider
	if _, err := NewProduction(api.Config{StoreProvider: nilProvider, PolicyEngine: allowAPIEngine{}}); !errors.Is(err, api.ErrInvalidConfiguration) {
		t.Fatalf("NewProduction(typed-nil provider) error = %v", err)
	}
	var nilPolicy *allowAPIEngine
	if _, err := NewProduction(api.Config{StoreProvider: stubStoreProvider{}, PolicyEngine: nilPolicy}); !errors.Is(err, api.ErrInvalidConfiguration) {
		t.Fatalf("NewProduction(typed-nil policy) error = %v", err)
	}
	production, err := NewProduction(api.Config{StoreProvider: stubStoreProvider{}, PolicyEngine: allowAPIEngine{}})
	if err != nil {
		t.Fatalf("NewProduction() error = %v", err)
	}
	if production.Mode() != api.RuntimeModeProduction {
		t.Fatalf("NewProduction mode = %q", production.Mode())
	}

	// Legacy construction remains source-compatible during the pre-v1 migration.
	if legacy := New(api.Config{PolicyEngine: allowAPIEngine{}}); legacy.Mode() != api.RuntimeModeDevelopment {
		t.Fatalf("legacy New mode = %q", legacy.Mode())
	}
	_ = DefaultConfig()
}

type stubStoreProvider struct{}

func (stubStoreProvider) Begin(context.Context) (api.UnitOfWork, error) { return nil, nil }
