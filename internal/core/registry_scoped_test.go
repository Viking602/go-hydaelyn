package core

import (
	"errors"
	"sync"
	"testing"

	"github.com/Viking602/venat/api"
)

func TestScopedToolDefinitionsDoNotOverwriteAcrossConcurrentTasks(t *testing.T) {
	rt := NewMemoryRuntime()
	definitions := map[string]api.Tool{
		"sensitive": {
			Name:               "shared",
			EffectType:         api.ToolEffectExternalSideEffect,
			RequiresActionTask: true,
			RiskLevel:          "high",
		},
		"benign": {
			Name:       "shared",
			EffectType: api.ToolEffectReadOnly,
			RiskLevel:  "low",
		},
	}
	var registrations sync.WaitGroup
	for taskID, definition := range definitions {
		registrations.Add(1)
		go func() {
			defer registrations.Done()
			_ = rt.RegisterToolForInvocation("run-1", taskID, api.HolderAgent, "agent-a", definition)
		}()
	}
	registrations.Wait()

	sensitive, ok := rt.toolForInvocation("run-1", "sensitive", api.HolderAgent, "agent-a", "shared")
	if !ok {
		t.Fatal("sensitive task definition missing")
	}
	if sensitive.EffectType != api.ToolEffectExternalSideEffect || !sensitive.RequiresActionTask {
		t.Fatalf("sensitive definition downgraded: %#v", sensitive)
	}
	benign, ok := rt.toolForInvocation("run-1", "benign", api.HolderAgent, "agent-a", "shared")
	if !ok {
		t.Fatal("benign task definition missing")
	}
	if benign.EffectType != api.ToolEffectReadOnly || benign.RequiresActionTask {
		t.Fatalf("benign definition changed: %#v", benign)
	}
}

func TestAgentScopedLookupDoesNotUseGlobalOrOtherTaskTool(t *testing.T) {
	rt := NewMemoryRuntime()
	_ = rt.RegisterTool(api.Tool{Name: "shared", EffectType: api.ToolEffectReadOnly})
	_ = rt.RegisterToolForInvocation("run-1", "task-1", api.HolderAgent, "agent-a", api.Tool{
		Name: "shared", EffectType: api.ToolEffectExternalSideEffect,
	})
	if _, ok := rt.toolForInvocation("run-1", "task-2", api.HolderAgent, "agent-a", "shared"); ok {
		t.Fatal("agent lookup used another task's definition")
	}
	if _, ok := rt.toolForInvocation("run-2", "task-1", api.HolderAgent, "agent-a", "shared"); ok {
		t.Fatal("agent lookup used the global tool definition")
	}
}

func TestRemoveToolsForInvocationLeavesOtherTaskDefinitionsIntact(t *testing.T) {
	rt := NewMemoryRuntime()
	_ = rt.RegisterToolForInvocation("run-1", "task-1", api.HolderAgent, "agent-a", api.Tool{Name: "shared"})
	_ = rt.RegisterToolForInvocation("run-1", "task-2", api.HolderAgent, "agent-a", api.Tool{Name: "shared"})

	rt.RemoveToolsForInvocation("run-1", "task-1", api.HolderAgent, "agent-a")

	if _, ok := rt.toolForInvocation("run-1", "task-1", api.HolderAgent, "agent-a", "shared"); ok {
		t.Fatal("removed invocation still has scoped metadata")
	}
	if _, ok := rt.toolForInvocation("run-1", "task-2", api.HolderAgent, "agent-a", "shared"); !ok {
		t.Fatal("removing one invocation removed another task's metadata")
	}
}

func TestRegisterRejectsEmptyIdentity(t *testing.T) {
	rt := NewMemoryRuntime()
	if err := rt.RegisterAgent(AgentProfile{}); !errors.Is(err, api.ErrInvalidCommand) {
		t.Fatalf("RegisterAgent() error = %v, want ErrInvalidCommand", err)
	}
	if err := rt.RegisterTool(api.Tool{}); !errors.Is(err, api.ErrInvalidCommand) {
		t.Fatalf("RegisterTool() error = %v, want ErrInvalidCommand", err)
	}
	if err := rt.RegisterToolForInvocation("", "task", api.HolderAgent, "agent", api.Tool{Name: "t"}); !errors.Is(err, api.ErrInvalidCommand) {
		t.Fatalf("RegisterToolForInvocation() error = %v, want ErrInvalidCommand", err)
	}
}
