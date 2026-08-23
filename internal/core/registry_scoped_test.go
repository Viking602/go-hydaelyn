package core

import (
	"errors"
	"sync"
	"testing"

	"github.com/Viking602/venat/internal/core/model"
)

func TestScopedToolDefinitionsDoNotOverwriteAcrossConcurrentTasks(t *testing.T) {
	rt := NewMemoryRuntime()
	definitions := map[string]model.Tool{
		"sensitive": {
			Name:               "shared",
			EffectType:         model.ToolEffectExternalSideEffect,
			RequiresActionTask: true,
			RiskLevel:          "high",
		},
		"benign": {
			Name:       "shared",
			EffectType: model.ToolEffectReadOnly,
			RiskLevel:  "low",
		},
	}
	var registrations sync.WaitGroup
	for taskID, definition := range definitions {
		registrations.Add(1)
		go func() {
			defer registrations.Done()
			_ = rt.RegisterToolForInvocation("run-1", taskID, model.HolderAgent, "agent-a", definition)
		}()
	}
	registrations.Wait()

	sensitive, ok := rt.toolForInvocation("run-1", "sensitive", model.HolderAgent, "agent-a", "shared")
	if !ok {
		t.Fatal("sensitive task definition missing")
	}
	if sensitive.EffectType != model.ToolEffectExternalSideEffect || !sensitive.RequiresActionTask {
		t.Fatalf("sensitive definition downgraded: %#v", sensitive)
	}
	benign, ok := rt.toolForInvocation("run-1", "benign", model.HolderAgent, "agent-a", "shared")
	if !ok {
		t.Fatal("benign task definition missing")
	}
	if benign.EffectType != model.ToolEffectReadOnly || benign.RequiresActionTask {
		t.Fatalf("benign definition changed: %#v", benign)
	}
}

func TestAgentScopedLookupDoesNotUseGlobalOrOtherTaskTool(t *testing.T) {
	rt := NewMemoryRuntime()
	_ = rt.RegisterTool(model.Tool{Name: "shared", EffectType: model.ToolEffectReadOnly})
	_ = rt.RegisterToolForInvocation("run-1", "task-1", model.HolderAgent, "agent-a", model.Tool{
		Name: "shared", EffectType: model.ToolEffectExternalSideEffect,
	})
	if _, ok := rt.toolForInvocation("run-1", "task-2", model.HolderAgent, "agent-a", "shared"); ok {
		t.Fatal("agent lookup used another task's definition")
	}
	if _, ok := rt.toolForInvocation("run-2", "task-1", model.HolderAgent, "agent-a", "shared"); ok {
		t.Fatal("agent lookup used the global tool definition")
	}
}

func TestRemoveToolsForInvocationLeavesOtherTaskDefinitionsIntact(t *testing.T) {
	rt := NewMemoryRuntime()
	_ = rt.RegisterToolForInvocation("run-1", "task-1", model.HolderAgent, "agent-a", model.Tool{Name: "shared"})
	_ = rt.RegisterToolForInvocation("run-1", "task-2", model.HolderAgent, "agent-a", model.Tool{Name: "shared"})

	rt.RemoveToolsForInvocation("run-1", "task-1", model.HolderAgent, "agent-a")

	if _, ok := rt.toolForInvocation("run-1", "task-1", model.HolderAgent, "agent-a", "shared"); ok {
		t.Fatal("removed invocation still has scoped metadata")
	}
	if _, ok := rt.toolForInvocation("run-1", "task-2", model.HolderAgent, "agent-a", "shared"); !ok {
		t.Fatal("removing one invocation removed another task's metadata")
	}
}

func TestRegisterRejectsEmptyIdentity(t *testing.T) {
	rt := NewMemoryRuntime()
	if err := rt.RegisterAgent(AgentProfile{}); !errors.Is(err, model.ErrInvalidCommand) {
		t.Fatalf("RegisterAgent() error = %v, want ErrInvalidCommand", err)
	}
	if err := rt.RegisterTool(model.Tool{}); !errors.Is(err, model.ErrInvalidCommand) {
		t.Fatalf("RegisterTool() error = %v, want ErrInvalidCommand", err)
	}
	if err := rt.RegisterToolForInvocation("", "task", model.HolderAgent, "agent", model.Tool{Name: "t"}); !errors.Is(err, model.ErrInvalidCommand) {
		t.Fatalf("RegisterToolForInvocation() error = %v, want ErrInvalidCommand", err)
	}
}
