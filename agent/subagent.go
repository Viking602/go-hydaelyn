package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

// DefaultSubagentMaxDepth bounds how deep a chain of subagents may nest when a
// SubagentDef does not set its own MaxDepth. It is a safety cap against runaway
// recursion (an agent that calls a subagent that calls back into an equivalent
// subagent), not a feature limit; raise it per SubagentDef.MaxDepth when a
// workload genuinely needs deeper delegation.
const DefaultSubagentMaxDepth = 4

// SubagentDef is the parent-facing declaration of a subagent: the tool name and
// description the parent model sees, the input contract, and the guards that
// keep a delegated child subordinate to its parent. It is deliberately lighter
// than multiagent.AgentClass.
//
// This is the "agent as a tool" positioning (ADR-018): a subagent has no
// independent identity, no lease, and no blackboard presence. From outside the
// parent, the whole delegation is one of the parent's tool calls — it counts as
// one entry in the parent's MaxToolCalls budget and appears as a single tool
// observation in the parent's Step trace. It is the opposite of a multiagent
// team member, which is an independently scheduled peer with its own identity
// and governance.
type SubagentDef struct {
	// Name and Description are advertised to the parent model as the tool's
	// identity and purpose.
	Name        string
	Description string

	// InputSchema is advertised on the tool definition and, when it constrains
	// the shape (a type, properties, required fields, or item schema),
	// validates the parent's arguments before the child runs. An empty schema
	// accepts any JSON arguments.
	InputSchema tool.Schema

	// MaxDepth caps subagent nesting depth measured from the top-level parent.
	// Zero falls back to DefaultSubagentMaxDepth.
	MaxDepth int

	// Budget, when set, is the explicit per-call budget the child Engine.Run is
	// bound by. When nil, the child runs under its own Engine LoopPolicy. The
	// child's token spend is bounded here but is not folded back into the
	// parent's token budget (see ADR-018 §"Known limitation").
	Budget *api.TaskBudget

	// Effect raises the advertised effect floor for the delegation. AsTool
	// already aggregates the effect, approval, and action-task metadata of the
	// child engine's tools onto Definition(), so the wrapper is never advertised
	// as safer than the most dangerous tool the child can call — a tool-less,
	// pure-reasoning child aggregates to read-only, which is the genuinely safe
	// case. Set Effect when a child's tools are registered lazily or are
	// otherwise not statically visible and you need to assert a higher floor;
	// the advertised effect is the maximum of this floor and the aggregated
	// child effect, so it can only raise the risk, never lower it.
	Effect tool.EffectType
}

// AsTool wraps an already-materialized child Engine as a tool.Driver the parent
// agent can invoke from within its own loop. The child Engine is built
// independently (e.g. via Build) and may run on a different model than the
// parent; AsTool consumes the Engine, not a declaration, so the subagent path
// has zero dependency on Spec, AgentClass, or the multiagent layer.
//
// The returned driver runs the child synchronously and in-memory:
//
//   - The parent's tool-call arguments map to the child task's goal. When the
//     arguments are a JSON object with a string "input" field, that field is
//     the goal; otherwise the raw arguments JSON becomes the goal.
//   - A child success returns a tool result whose Content is the child's text
//     and whose Structured carries any structured child output. When the child
//     instead submits its answer through a terminal tool — so it completes with
//     no trailing assistant text — the wrapper surfaces that terminal tool
//     result's content and structured payload instead of a blank result, and
//     carries its IsError status so a rejected submission stays an error. A
//     non-terminal tool observation is never promoted to the final answer.
//   - A child that stops on its turn budget (StopReasonMaxTurns) without any
//     final answer returns an error tool result, so a truncated, non-converged
//     run is not reported to the parent as a completed delegation.
//   - A child failure (a non-nil Result.Failure) returns an error tool result
//     (IsError) carrying the failure reason and its typed classification, never
//     a Go error — so a subagent failure never hard-aborts the parent loop. The
//     parent observes the error result and decides what to do next.
//   - Nesting past the effective MaxDepth returns an error tool result rather
//     than recursing.
func AsTool(child Engine, def SubagentDef) tool.Driver {
	return &subagentTool{child: child, def: def}
}

type subagentTool struct {
	child Engine
	def   SubagentDef
}

func (s *subagentTool) Definition() tool.Definition {
	risk := s.childRisk()
	return tool.Definition{
		Name:               s.def.Name,
		Description:        s.def.Description,
		InputSchema:        s.def.InputSchema,
		EffectType:         risk.effect,
		RequiresApproval:   risk.requiresApproval,
		RequiresActionTask: risk.requiresActionTask,
		RiskLevel:          risk.riskLevel,
		PolicyTags:         risk.policyTags,
	}
}

// childRisk aggregates the governance-relevant metadata of every tool the child
// engine may call, so the delegation advertises a risk at least as high as the
// most dangerous action the child can take. A subagent is never safer than its
// child: advertising a fixed read-only effect would let the parent's tool-gate
// wave the whole delegation through without the approval a side-effecting child
// tool would otherwise demand, because the runner derives the persisted tool
// policy from Definition() and only gates write/external/action-task tools. A
// tool-less (pure-reasoning) child aggregates to read-only — the genuinely safe
// case — and SubagentDef.Effect can raise the floor when the child's tools are
// not statically visible.
func (s *subagentTool) childRisk() subagentRisk {
	risk := subagentRisk{effect: s.def.Effect}
	if s.child.Tools != nil {
		for _, def := range s.child.Tools.Definitions() {
			risk.absorb(def)
		}
	}
	if effectRank(risk.effect) == 0 {
		risk.effect = tool.EffectReadOnly
	}
	slices.Sort(risk.policyTags)
	return risk
}

// subagentRisk accumulates the worst-case governance metadata across the child
// engine's tools.
type subagentRisk struct {
	effect             tool.EffectType
	requiresApproval   bool
	requiresActionTask bool
	riskLevel          string
	policyTags         []string
}

func (r *subagentRisk) absorb(def tool.Definition) {
	requiresApproval := def.RequiresApproval || def.Security.RequiresApproval
	effect := def.EffectType
	if effect == "" && requiresApproval {
		// Mirror worker.toolDefinitionToRunnerTool: an approval-gated tool that
		// declares no effect is treated as an external side effect, so the
		// aggregate ranks it the same way the runner's tool-gate will.
		effect = tool.EffectExternalSideEffect
	}
	if effectRank(effect) > effectRank(r.effect) {
		r.effect = effect
	}
	r.requiresApproval = r.requiresApproval || requiresApproval
	r.requiresActionTask = r.requiresActionTask || def.RequiresActionTask
	if riskLevelRank(def.RiskLevel) > riskLevelRank(r.riskLevel) {
		r.riskLevel = def.RiskLevel
	}
	if rl := def.Security.RiskLevel; riskLevelRank(rl) > riskLevelRank(r.riskLevel) {
		r.riskLevel = rl
	}
	for _, tag := range def.PolicyTags {
		if !slices.Contains(r.policyTags, tag) {
			r.policyTags = append(r.policyTags, tag)
		}
	}
}

// effectRank orders tool effects by escalating risk so aggregation can take the
// maximum. An empty effect ranks with read-only.
func effectRank(e tool.EffectType) int {
	switch e {
	case tool.EffectExternalSideEffect:
		return 2
	case tool.EffectWrite:
		return 1
	default:
		return 0
	}
}

// riskLevelRank orders the common free-form risk-level strings so aggregation
// can keep the highest. Unknown values rank with the unset level.
func riskLevelRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium", "moderate":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func (s *subagentTool) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	maxDepth := s.def.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultSubagentMaxDepth
	}
	if depth := subagentDepth(ctx); depth >= maxDepth {
		return subagentErrorResult(call, s.def.Name,
			fmt.Sprintf("subagent %q refused: max nesting depth %d reached", s.def.Name, maxDepth)), nil
	}

	if err := s.validateArguments(call.Arguments); err != nil {
		return subagentErrorResult(call, s.def.Name,
			fmt.Sprintf("subagent %q input rejected: %v", s.def.Name, err)), nil
	}

	task := api.Task{
		Goal:   subagentGoal(call.Arguments),
		Budget: s.def.Budget,
	}
	childCtx := withSubagentDepth(ctx, subagentDepth(ctx)+1)
	result := s.child.Run(childCtx, task, OutputPolicy{})
	if result.Failure != nil {
		return subagentFailureResult(call, s.def.Name, result.Failure), nil
	}
	return s.subagentSuccessResult(call, result), nil
}

// validateArguments checks the parent's arguments against the input schema when
// the schema constrains a shape. An unconstrained (empty) schema accepts any
// arguments, including none.
func (s *subagentTool) validateArguments(args json.RawMessage) error {
	if !schemaConstrains(s.def.InputSchema) {
		return nil
	}
	raw, err := json.Marshal(s.def.InputSchema)
	if err != nil {
		return fmt.Errorf("input schema is not serializable: %w", err)
	}
	schema, err := parseOutputPolicySchema(raw)
	if err != nil {
		return fmt.Errorf("input schema is invalid: %w", err)
	}
	argsText := string(args)
	if len(args) == 0 {
		argsText = "{}"
	}
	_, err = validateStructuredOutputAgainstSchema(schema, argsText)
	return err
}

// schemaConstrains reports whether a tool schema actually restricts the input.
// An all-zero schema imposes no constraint, so validation is skipped for it.
func schemaConstrains(schema tool.Schema) bool {
	return schema.Type != "" ||
		len(schema.Properties) > 0 ||
		len(schema.Required) > 0 ||
		schema.Items != nil ||
		len(schema.Enum) > 0
}

// subagentGoal derives the child task goal from the parent's tool arguments. A
// JSON object with a string "input" field uses that field; anything else uses
// the raw arguments JSON, so a structured payload reaches the child intact.
func subagentGoal(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(args, &object); err == nil {
		if raw, ok := object["input"]; ok {
			var text string
			if json.Unmarshal(raw, &text) == nil {
				return text
			}
		}
	}
	return string(args)
}

func (s *subagentTool) subagentSuccessResult(call tool.Call, result Result) tool.Result {
	content := result.Text
	structured := result.Structured
	isError := false
	// When the child finished through a terminal tool, that tool call is its
	// final act, so the wrapper reflects it: carry the terminal result's error
	// status, and fall back to its content/structured payload when the child left
	// no trailing assistant text/output (it submitted its answer through the tool,
	// so result.Text is empty and — under the empty per-delegation OutputPolicy —
	// result.Structured is nil). A terminal submit tool that rejects its input
	// returns an error result; without carrying IsError the parent would read a
	// failed submission as a completed delegation. A non-terminal tool observation
	// is mid-run state, never the child's conclusion, so it is never promoted.
	if final := s.childTerminalToolResult(result); final != nil {
		if content == "" {
			content = final.Content
		}
		if len(structured) == 0 {
			structured = final.Structured
		}
		isError = final.IsError
	}
	// With no assistant text and no terminal answer, a child that stopped on its
	// turn budget (StopReasonMaxTurns) did not converge — it ran out of
	// iterations mid-task. Surface that as an error result so the parent does not
	// mistake a truncated run for a completed, empty delegation; without it,
	// AsTool would drop the stop reason and report success. Failure-bearing stops
	// (aborted/error) never reach here — they become a failure result upstream.
	if content == "" && len(structured) == 0 && result.StopReason == provider.StopReasonMaxTurns {
		return subagentErrorResult(call, s.def.Name,
			fmt.Sprintf("subagent %q stopped without a final answer (%s): the child ran out of iterations", s.def.Name, result.StopReason))
	}
	return tool.Result{
		ToolCallID: call.ID,
		Name:       s.def.Name,
		Content:    content,
		Structured: structured,
		IsError:    isError,
	}
}

// childTerminalToolResult returns the terminal tool result the child's run
// ended on, or nil when the child did not finish through a terminal tool. It
// scans the child's messages back-to-front for the last tool result whose tool
// the child bus marks Terminal. A non-terminal tool observation is mid-run
// state, not the child's final answer, so it is never returned — and a result
// whose tool is not statically known to be terminal (absent bus, tool gone) is
// treated as non-terminal rather than guessed.
func (s *subagentTool) childTerminalToolResult(result Result) *message.ToolResult {
	if s.child.Tools == nil {
		return nil
	}
	for i := len(result.Messages) - 1; i >= 0; i-- {
		m := result.Messages[i]
		if m.Role != message.RoleTool || m.ToolResult == nil {
			continue
		}
		if driver, ok := s.child.Tools.Driver(m.ToolResult.Name); ok && driver.Definition().Terminal {
			return m.ToolResult
		}
	}
	return nil
}

func subagentFailureResult(call tool.Call, name string, failure *AgentFailure) tool.Result {
	// Serialize the typed classification (kind/reason/retryable/escalatable)
	// alongside the human-readable reason so a parent hook or downstream reader
	// can branch on the failure mode, mirroring how the multiagent path carries
	// AgentFailure across the boundary.
	structured, _ := json.Marshal(failure)
	return tool.Result{
		ToolCallID: call.ID,
		Name:       name,
		Content:    failure.Error(),
		Structured: structured,
		IsError:    true,
	}
}

func subagentErrorResult(call tool.Call, name, reason string) tool.Result {
	return tool.Result{
		ToolCallID: call.ID,
		Name:       name,
		Content:    reason,
		IsError:    true,
	}
}

// subagentDepthKey carries the current subagent nesting depth through context so
// AsTool can enforce MaxDepth across nested delegations without threading a
// counter through every call signature.
type subagentDepthKey struct{}

func subagentDepth(ctx context.Context) int {
	if depth, ok := ctx.Value(subagentDepthKey{}).(int); ok {
		return depth
	}
	return 0
}

func withSubagentDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, subagentDepthKey{}, depth)
}
