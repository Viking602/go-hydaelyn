package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/hook"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/skill"
	"github.com/Viking602/venat/tool"
)

// ErrProviderResolverMissing is returned by Build when BuildDeps carries no
// provider.Resolver — the Engine cannot issue a model call without one.
var ErrProviderResolverMissing = errors.New("agent: build deps missing provider resolver")

// ErrSkillRegistryMissing is returned by Build when Spec.Skills names active
// skills but BuildDeps carries no skill registry to resolve them.
var ErrSkillRegistryMissing = errors.New("agent: build deps missing skill registry")

// Spec is the neutral, executable declaration of a single agent: how to run one
// bounded loop. It says nothing about how the agent is used — the same Spec can
// be materialized and then driven as a standalone agent, wrapped as a subagent
// tool (AsTool), or executed as a member of a multi-agent team. Positioning is
// the caller's choice, never a property of the Spec.
//
// Build is the sole materialization path from a Spec to an Engine. Keeping
// construction in one place means every usage — single agent, subagent, team
// member — resolves models, selects tools, and wires instructions identically.
//
// Spec anchor: docs/adr/ADR-018-self-sufficient-agent-layer.md.
type Spec struct {
	// Instructions is the agent's system prompt. When BuildDeps supplies no
	// ContextManager, Build wires a default one that seeds the loop with
	// Instructions as the system message and the task goal as the user message.
	Instructions string

	// Skills names reusable instruction bundles resolved against BuildDeps.Skills
	// and injected into Engine.Run task context.
	Skills []string

	// AvailableSkills names reusable instruction bundles disclosed to the model
	// as a compact catalog and activated on demand. Unlike Skills, their bodies
	// are not injected before activation.
	AvailableSkills []string

	// Provider optionally pins resolution to a driver Metadata().Name. Model is
	// the model name sent to that driver. FallbackModel is resolved separately
	// and tried only when the primary stream cannot be opened.
	Provider      string
	Model         string
	FallbackModel string

	// Temperature, TopP, and MaxTokens are forwarded to every provider request.
	// Zero leaves the selected provider's default in place.
	Temperature float64
	TopP        float64
	MaxTokens   int

	// Tools names the tools this agent may call. Build selects exactly this
	// subset from BuildDeps.Tools; an empty slice yields a tool-less agent.
	Tools []string

	// LoopPolicy bounds one Engine.Run (iterations, wall-clock, budget). A
	// per-Task Budget still overrides it at run time.
	LoopPolicy LoopPolicy

	// ThinkingBudget caps provider reasoning tokens per turn; zero leaves the
	// provider default in place. StopSequences are forwarded to every turn.
	ThinkingBudget int
	StopSequences  []string

	// godoc-allow-any: provider-specific request extensions are intentionally open.
	ExtraBody map[string]any

	// InputSchema and OutputSchema are the declared typed-handoff contract.
	// They travel with the Spec for callers that create tasks or advertise the
	// agent; Build does not bake them into the Engine, because input/output
	// validation is a per-task concern (api.Task.OutputSchema drives the
	// OutputPolicy at Run time), not an Engine field.
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
}

// BuildDeps carries the live runtime dependencies a Spec cannot hold by value:
// the provider resolver, the master tool registry, the hook chain, and an
// optional ContextManager override. These are wired once per deployment and
// reused across every Build.
type BuildDeps struct {
	// Providers resolves Spec.Model to the driver that serves it. Required.
	// A single-provider deployment passes provider.Single(driver).
	Providers provider.Resolver

	// Tools is the master registry Build selects each Spec's named subset from.
	// May be nil only when every Spec being built declares no tools.
	Tools *tool.Bus

	// Skills is the registry Build uses to resolve Spec.Skills. It is required
	// only when a Spec declares one or more skill names.
	Skills *skill.Registry

	// Hooks is the hook chain installed on the materialized Engine. The zero
	// value is a valid empty chain.
	Hooks hook.Chain

	// ContextManager, when set, overrides the default instructions-based
	// context builder for every Engine built with these deps.
	ContextManager ContextManager
}

// Build materializes a Spec into an Engine using the supplied dependencies. It
// is the single construction entry point for the agent layer.
//
// Build resolves Spec.Model through deps.Providers to pick the driver, selects
// the named tool subset from deps.Tools (failing if a named tool is absent, so
// a misdeclared tool fails at construction rather than mid-run), and wires
// Spec.Instructions into a default ContextManager unless deps overrides it. A
// missing resolver or an unservable model fails here rather than at the first
// model call.
func Build(spec Spec, deps BuildDeps) (Engine, error) {
	if deps.Providers == nil {
		return Engine{}, ErrProviderResolverMissing
	}
	driver, err := provider.Resolve(deps.Providers, spec.Provider, spec.Model)
	if err != nil {
		return Engine{}, fmt.Errorf("agent: resolve provider %q for model %q: %w", spec.Provider, spec.Model, err)
	}
	if spec.FallbackModel != "" {
		fallback, fallbackErr := provider.Resolve(deps.Providers, "", spec.FallbackModel)
		if fallbackErr != nil {
			return Engine{}, fmt.Errorf("agent: resolve fallback model %q: %w", spec.FallbackModel, fallbackErr)
		}
		driver = provider.ModelFallback(driver, fallback, spec.FallbackModel)
	}

	var bus *tool.Bus
	if len(spec.Tools) > 0 {
		if deps.Tools == nil {
			return Engine{}, fmt.Errorf("agent: spec lists %d tool(s) but build deps carry no tool bus", len(spec.Tools))
		}
		for _, name := range spec.Tools {
			if _, ok := deps.Tools.Driver(name); !ok {
				return Engine{}, fmt.Errorf("agent: tool %q named by spec is not registered in build deps", name)
			}
		}
		bus = deps.Tools.Subset(spec.Tools)
	}

	var activeSkills, availableSkills []skill.Skill
	if len(spec.Skills) > 0 || len(spec.AvailableSkills) > 0 {
		if deps.Skills == nil {
			return Engine{}, fmt.Errorf("%w: spec lists %d skill(s)", ErrSkillRegistryMissing, len(spec.Skills)+len(spec.AvailableSkills))
		}
	}
	if len(spec.Skills) > 0 {
		activeSkills, err = deps.Skills.Resolve(spec.Skills...)
		if err != nil {
			return Engine{}, wrapSkillResolveError(err)
		}
	}
	if len(spec.AvailableSkills) > 0 {
		availableSkills, err = deps.Skills.Resolve(spec.AvailableSkills...)
		if err != nil {
			return Engine{}, wrapSkillResolveError(err)
		}
		active := make(map[string]struct{}, len(activeSkills))
		for _, current := range activeSkills {
			active[current.Name] = struct{}{}
		}
		filtered := availableSkills[:0]
		for _, current := range availableSkills {
			if _, alreadyActive := active[current.Name]; !alreadyActive {
				filtered = append(filtered, current)
			}
		}
		availableSkills = filtered
	}

	contextManager := deps.ContextManager
	if contextManager == nil {
		contextManager = instructionsContext{instructions: spec.Instructions}
	}

	return Engine{
		Provider:        driver,
		Tools:           bus,
		Hooks:           deps.Hooks,
		Model:           spec.Model,
		Temperature:     spec.Temperature,
		TopP:            spec.TopP,
		ModelMaxTokens:  spec.MaxTokens,
		LoopPolicy:      spec.LoopPolicy,
		ContextBuilder:  contextManager,
		Skills:          activeSkills,
		AvailableSkills: availableSkills,
		ThinkingBudget:  spec.ThinkingBudget,
		StopSequences:   spec.StopSequences,
		ExtraBody:       spec.ExtraBody,
	}, nil
}

func wrapSkillResolveError(err error) error {
	var missing *skill.NotFoundError
	if errors.As(err, &missing) {
		return fmt.Errorf("agent: resolve skill %q: %w", missing.Name, err)
	}
	return fmt.Errorf("agent: resolve skills: %w", err)
}

// instructionsContext is the default ContextManager Build installs when none is
// supplied: it seeds the loop with the Spec's instructions as the system
// message and the task goal as the user message. Compact is a pass-through;
// tightening it lands when LoopPolicy.MaxTokens is wired.
type instructionsContext struct {
	instructions string
}

func (c instructionsContext) Build(_ context.Context, task api.Task) ([]message.Message, error) {
	system := strings.TrimSpace(c.instructions)
	if system == "" {
		system = "You are a Venat agent."
	}
	goal := strings.TrimSpace(task.Goal)
	if goal == "" {
		goal = "Complete the assigned task and return a concise result."
	}
	return []message.Message{
		message.NewText(message.RoleSystem, system),
		message.NewText(message.RoleUser, goal),
	}, nil
}

func (instructionsContext) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
}
