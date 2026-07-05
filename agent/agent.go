package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"

	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/skill"
	"github.com/Viking602/go-hydaelyn/stream"
	"github.com/Viking602/go-hydaelyn/tool"
)

var ErrToolBusMissing = errors.New("tool bus missing")

// ErrBudgetExhausted is returned by RunMessages, wrapped with the exhausted
// dimension, when a per-loop budget (MaxTokens/MaxToolCalls/MaxSteps) is hit
// on a turn that would otherwise continue. The accompanying LoopOutput is the
// partial trace accumulated so far. Engine.run maps it to a
// FailureKindBudgetExhausted Result.
var ErrBudgetExhausted = errors.New("agent loop budget exhausted")

// ErrPanicRecovered wraps a panic recovered by RunMessages. The loop invokes
// caller-supplied extension code — guardrails, recorders, sinks, the OnEvent
// callback, the provider stream, and sequential tool drivers — any of which may
// panic on the loop's own goroutine. RunMessages recovers such a panic, returns
// the partial trace accumulated so far, and surfaces it through this sentinel so
// the panic degrades to a typed FailureKindEngineError instead of crashing the
// worker process. Two panic sources are contained one level lower so they carry
// more precise provenance and never reach this defer: hook handlers, guarded by
// hook.Chain (hook.ErrHandlerPanic), and parallel tool drivers, which run on
// bus-spawned goroutines the loop's recover cannot see and are contained by the
// bus itself (tool.ErrToolPanic). errors.Is(err, ErrPanicRecovered) reports
// whether a loop error originated from a panic recovered here.
var ErrPanicRecovered = errors.New("agent loop recovered a panic")

// ErrStepAborted is returned by RunMessages when a StepPolicy deliberately stops
// the loop at a continue boundary — its Next returns StepDecisionFail or itself
// errors. The accompanying LoopOutput is the partial trace accumulated so far.
// Engine.run maps it to a FailureKindStepAborted Result. It is distinct from a
// StepDecisionFinish/Handoff override, which stops the loop cleanly (no error).
var ErrStepAborted = errors.New("agent loop aborted by step policy")

// LoopInput is the message-level input to Engine.RunMessages, the
// low-level loop entry preserved from v0.7. Most callers should use
// Engine.Run(ctx, api.Task, OutputPolicy) Result instead — that is the
// task-level entry the v0.8.0 multi-agent layer schedules against.
type LoopInput struct {
	Model         string
	Messages      []message.Message
	Metadata      map[string]string
	ToolMode      tool.Mode
	MaxIterations int
	OnEvent       func(provider.Event) error

	// Sink, when set, receives a live stream.Frame for every provider
	// event and tool result as the loop runs. It is a transient
	// side-channel: on success the durable LoopOutput is byte-for-byte
	// identical to a run with no Sink, so streaming never changes what the
	// runner persists or replays. A Sink.Emit error is not swallowed — it
	// aborts the current turn and surfaces as the loop error, exactly like a
	// failing OnEvent callback — so a Sink must absorb transient delivery
	// hiccups it can tolerate rather than returning an error for them.
	Sink stream.Sink

	StopSequences  []string
	ThinkingBudget int
	ResponseFormat *provider.ResponseFormat
	// godoc-allow-any: provider-specific request extensions are intentionally open.
	ExtraBody map[string]any

	OutputGuardrails []OutputGuardrail
	OutputRecorder   OutputGuardrailRecorder

	// MaxTokens / MaxToolCalls / MaxSteps are the per-loop budget ceilings.
	// Zero means unbounded on that dimension. They are enforced fail-closed
	// but only on turns that would continue the loop: a run that is about to
	// finish is never failed for a budget it has not yet exceeded. MaxSteps
	// is a hard ceiling (exhausting it fails the run with ErrBudgetExhausted),
	// distinct from MaxIterations, whose soft ceiling yields StopReasonMaxTurns.
	// The token ceiling fails open against providers that report zero usage:
	// a never-incrementing token count can never cross a positive ceiling.
	MaxTokens    int64
	MaxToolCalls int
	MaxSteps     int

	// StepPolicy, when set, is consulted at each continue boundary (after a
	// non-terminal tool turn) so a caller can override the loop's natural
	// decision to iterate again — stopping early, diverting to a handoff, or
	// failing the run once a predicate over the step trace holds. A nil policy
	// leaves the loop's natural control flow unchanged. See StepPolicy.
	StepPolicy StepPolicy

	// Compact, when set, is invoked to shrink the running message history once
	// the per-loop token budget is approached (MaxTokens > 0 and the consumed
	// tokens leave one headroom band or less of it). It replaces the working
	// history with the returned slice before the next turn, so subsequent turns
	// send a smaller prompt. It never runs when MaxTokens is zero, so a run with
	// no token budget never compacts. Engine.Run wires this from the Engine's
	// ContextManager.Compact; the default context managers pass the history
	// through unchanged, so compaction is opt-in via a real compactor.
	//
	// Consumed tokens only grow, so once the loop enters the headroom band the
	// trigger holds for every remaining turn and Compact runs before each one. A
	// compactor must therefore be idempotent — cheap and stable on a history it
	// already compacted — not a one-shot transform.
	//
	// Determinism: the loop triggers Compact deterministically (the same trigger
	// fires on replay), so a deterministic compactor keeps the run
	// replay-faithful (ADR-007) while an LLM-backed one does not. Compact is
	// also responsible for returning a coherent history (for example, not
	// splitting a tool_use from its tool_result); the loop does not police what
	// a compactor returns.
	Compact func(ctx context.Context, history []message.Message) ([]message.Message, error)
}

// LoopOutput is the message-level result from Engine.RunMessages. The
// task-level Result type lives in result.go.
type LoopOutput struct {
	Messages   []message.Message
	Usage      provider.Usage
	StopReason provider.StopReason
	Iterations int
	Thinking   string

	// Steps is the per-iteration trace of the loop, one entry per model
	// turn (including guardrail-retry turns). Steps carry no wall-clock
	// timestamps so a replayed loop reproduces them byte-for-byte (ADR-007).
	Steps []Step
	// ToolCallsUsed is the total number of tool calls dispatched across all
	// iterations. It is the carrier the budget enforcement in Engine.run
	// reads to charge the per-task MaxToolCalls budget.
	ToolCallsUsed int
}

// Engine drives the bounded agent loop. Configure Provider/Tools/Hooks
// at construction; the v0.8.0 fields (Model, ToolMode, LoopPolicy,
// ContextBuilder) bind the defaults Engine.Run (the task-level entry)
// needs. Engine.RunMessages remains available as the low-level
// message-driven entry; it ignores the v0.8.0 fields and reads
// everything it needs from LoopInput.
type Engine struct {
	Provider provider.Driver
	Tools    *tool.Bus
	Hooks    hook.Chain

	Model          string
	ToolMode       tool.Mode
	LoopPolicy     LoopPolicy
	ContextBuilder ContextManager

	// Skills are active reusable instructions Engine.Run injects into task-level
	// context. RunMessages is the low-level message API and does not read this
	// Engine default.
	Skills []skill.Skill

	// The fields below are the engine-level defaults Engine.Run threads into
	// every LoopInput it builds, so the task-level entry can configure the
	// provider request and output handling that previously only RunMessages
	// callers could reach. ResponseFormat and per-request Metadata are
	// deliberately not surfaced here: structured output on the Run path is
	// owned by OutputPolicy, and request Metadata is a per-task value rather
	// than an engine default.

	// ThinkingBudget caps provider reasoning tokens per turn; zero leaves the
	// provider default in place.
	ThinkingBudget int
	// StopSequences are forwarded to every provider turn the loop issues.
	StopSequences []string
	// godoc-allow-any: provider-specific request extensions are intentionally open.
	ExtraBody map[string]any

	// OutputGuardrails run in order against the terminal assistant output and
	// may allow, replace, retry, or block it.
	OutputGuardrails []OutputGuardrail
	// OutputRecorder, when set, receives a decision record for every
	// non-allow guardrail action.
	OutputRecorder OutputGuardrailRecorder

	// StepPolicy, when set, Engine.Run threads into every LoopInput so the loop
	// consults it at each continue boundary. Like OutputGuardrails it is an
	// Engine-level field rather than a Spec field: set it on the built Engine.
	// See StepPolicy and LoopInput.StepPolicy.
	StepPolicy StepPolicy
}

// RunMessages is the low-level loop that drives one LoopInput to
// completion. Engine.Run is the task-level wrapper most callers want.
func (e Engine) RunMessages(ctx context.Context, input LoopInput) (out LoopOutput, err error) {
	if input.MaxIterations <= 0 {
		// Default loop ceiling when the caller sets no bound. 12 sits above
		// OpenAI's default of 10 and well below LangGraph's 25; the prior
		// default of 4 truncated legitimate multi-tool runs. This is the soft
		// ceiling: exhausting it yields StopReasonMaxTurns, which flows through
		// output validation (validate-first) rather than surfacing a failure.
		input.MaxIterations = 12
	}
	if input.ToolMode == "" {
		input.ToolMode = tool.ModeSequential
	}
	current := append([]message.Message{}, input.Messages...)
	totalUsage := provider.Usage{}
	steps := make([]Step, 0, input.MaxIterations)
	toolCallsUsed := 0
	// turnsRun counts the model turns that have actually run (their usage folded
	// into totalUsage). It is set once a turn completes, before the per-turn Step
	// is recorded, so the panic path below reports Iterations consistently with
	// the non-panic returns even when a panic strikes after a turn ran but before
	// its Step exists (a guardrail/recorder panic or a sequential tool driver
	// panic). Deriving Iterations from len(steps) there would under-report a turn
	// whose usage and messages are already in the partial output.
	turnsRun := 0
	// Recover any panic raised by caller-supplied extension code the loop drives
	// on this goroutine (guardrails, recorders, the sink when it emits tool-result
	// frames, and sequential tool drivers) so a misbehaving extension degrades to a
	// typed failure instead of crashing the worker. The accumulated trace is
	// preserved on the returned LoopOutput. Several panic sources never reach this
	// defer and are contained closer to their origin: hook handlers, in hook.Chain;
	// parallel tool drivers, which run on bus-spawned goroutines this recover cannot
	// observe and are contained by the tool bus; and the per-event callbacks
	// (OnEvent, the hook chain, the sink on stream frames) and the provider stream,
	// which panic inside collect — it recovers them itself and returns the partial
	// turn as an ErrPanicRecovered error rather than unwinding here, so the streamed
	// response survives. errors.Is(err, ErrPanicRecovered) holds for all of these.
	defer func() {
		if r := recover(); r != nil {
			out = loopErrorOutput(current, totalUsage, steps, turnsRun, toolCallsUsed)
			err = fmt.Errorf("%w: %v", ErrPanicRecovered, r)
		}
	}()
	for iteration := 0; iteration < input.MaxIterations; iteration++ {
		// A cancelled or expired context ends the loop promptly rather than
		// issuing another model turn; the cause (context.Canceled or
		// context.DeadlineExceeded) flows through loopErrorFailure, which maps a
		// budget-driven deadline to FailureKindBudgetExhausted.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return loopErrorOutput(current, totalUsage, steps, iteration, toolCallsUsed), ctxErr
		}
		// Enforce the per-loop budget before every turn after the first.
		// Reaching iteration N>0 means a prior turn chose to continue, so this
		// is exactly a "will continue" boundary; a run that finished earlier
		// returned before reaching here and is never charged a budget failure.
		if iteration > 0 {
			next, out, stop, preErr := loopTurnPreamble(ctx, input, current, totalUsage, steps, iteration, toolCallsUsed)
			if stop {
				return out, preErr
			}
			current = next
		}
		assistant, usage, stopReason, turnErr := e.runTurn(ctx, current, input)
		if turnErr != nil {
			// A turn can fail after the model already produced content: a per-event
			// callback erroring or panicking mid-stream, which collect surfaces as an
			// error carrying the partial assistant turn. Record that turn so the
			// partial trace keeps the produced response and its usage rather than
			// discarding the completed work along with the failure.
			current, totalUsage, turnsRun = recordIncompleteTurn(current, assistant, totalUsage, usage, iteration, turnsRun)
			return loopErrorOutput(current, totalUsage, steps, turnsRun, toolCallsUsed), turnErr
		}
		totalUsage = totalUsage.Add(usage)
		// The model turn ran and its usage is now counted, so a panic from here on
		// (guardrails, recorders, sink, sequential tool drivers) must report this
		// turn in Iterations rather than only the turns whose Step already exists.
		turnsRun = iteration + 1
		modelCall := &ModelCall{
			Model:        input.Model,
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
			StopReason:   stopReason,
		}
		if len(assistant.ToolCalls) == 0 {
			finalOutput, retryMessages, retryPolicy, guardErr := e.applyOutputGuardrails(ctx, input, current, assistant, iteration+1, totalUsage, stopReason)
			if guardErr != nil {
				return loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed), guardErr
			}
			if len(retryMessages) > 0 {
				// A guardrail asked to retry: record the turn as a continue
				// step and loop again with the retry context appended.
				steps = append(steps, Step{
					Index:      iteration,
					ModelCall:  modelCall,
					Decision:   StepDecisionContinue,
					BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
				})
				current = appendRetryContext(current, assistant, retryMessages, retryPolicy)
				continue
			}
			current = appendFinalAssistant(current, finalOutput)
			steps = append(steps, Step{
				Index:      iteration,
				ModelCall:  modelCall,
				Decision:   StepDecisionFinish,
				BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
			})
			return LoopOutput{
				Messages:      current,
				Usage:         totalUsage,
				StopReason:    stopReason,
				Iterations:    iteration + 1,
				Thinking:      finalOutput.Thinking,
				Steps:         steps,
				ToolCallsUsed: toolCallsUsed,
			}, nil
		}
		// Reaching here means this turn has tool calls: the no-tool-call branch
		// above always returns or continues, so len(assistant.ToolCalls) > 0 is
		// guaranteed and the assistant tool-call message is always recorded
		// before dispatch.
		current = append(current, assistant)
		// Re-check the context before dispatching this turn's tools. collect
		// preserves a turn that completed via EventDone even when the cancellation
		// lands after the terminal event (a context-aware stream can return the
		// context error from Recv), so a tool-use turn can arrive here under an
		// already-cancelled context. A completed final-answer turn is a finished
		// response and was already returned above; a tool-use turn is a request to
		// perform side-effecting work that cancellation must forbid — and the bus
		// dispatches to drivers without a pre-flight context check, so without this
		// guard the work would run on a dead context, relying on every driver to
		// notice. The assistant tool-call message is already appended, so the trace
		// records the request that was deliberately not run.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed), ctxErr
		}
		// Gate the batch against every budget dimension BEFORE preparing or
		// dispatching it. The model turn just ran, so its token spend is now folded
		// into totalUsage: a turn that alone exhausts MaxTokens, or a batch that
		// overflows MaxToolCalls, must abort here — otherwise the whole batch,
		// including side-effecting calls the budget never authorized, executes
		// and the overrun is only caught by the next turn's pre-turn check, one
		// turn too late. The model turn already ran, so it is recorded as a
		// failed step before aborting.
		if dimension := preDispatchBudgetBlock(input, totalUsage, toolCallsUsed, len(steps), len(assistant.ToolCalls)); dimension != "" {
			steps = append(steps, Step{
				Index:      iteration,
				ModelCall:  modelCall,
				Decision:   StepDecisionFail,
				BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
			})
			return budgetAbort(current, totalUsage, steps, iteration+1, toolCallsUsed, dimension)
		}
		// Prepare the batch before charging or dispatching it. prepareToolCalls runs
		// each call's BeforeToolCall hook — the documented place to rewrite a tool
		// name, mapping a model-emitted alias or hallucinated name onto a real tool —
		// and only then checks the prepared name is registered. The bus dispatches
		// the prepared calls, so availability must be judged on them, not the raw
		// model-emitted names: validating the raw names here would reject a name a
		// hook was about to fix. A missing bus or an unregistered prepared tool is
		// FailureKindToolUnavailable; none of these paths runs a driver, so they
		// return before the charge below, leaving ToolCallsUsed untouched for a
		// caller that registers the tool and resumes.
		prepared, terminal, prepErr := e.prepareToolCalls(ctx, assistant.ToolCalls)
		if prepErr != nil {
			return loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed), prepErr
		}
		// Charge this turn's tool batch BEFORE dispatching it, so the count
		// survives every way dispatch can end. A sequential driver runs inline on
		// this goroutine: a panic unwinds straight to the RunMessages recover defer
		// without ever returning here, so a charge placed after dispatch would be
		// skipped and the recovered partial output would under-report. Charging
		// first also covers the returns below — a tool error or recovered parallel
		// panic (dispErr), or a sink Emit failure on a result frame (appendErr).
		// This mirrors the batch-level accounting of the pre-dispatch gate above,
		// which reserves the whole batch against MaxToolCalls, so a caller that
		// persists or resumes from any partial LoopOutput does not under-count and
		// let later work exceed the budget. On the success path the count is
		// unchanged. prepareToolCalls above already rejected any unregistered tool
		// and ran the BeforeToolCall hooks, so this no longer counts a not-found
		// call or a hook rejection; a registered batch that fails partway (a later
		// sequential call left unrun after an earlier driver error) can still be
		// over-counted, but for an upper-bound budget over-counting is the safe
		// direction — it can only stop a resumed run sooner, never let it overrun.
		toolCallsUsed += len(assistant.ToolCalls)
		results, dispErr := e.dispatchPreparedTools(ctx, prepared, input.ToolMode)
		// Append before surfacing a dispatch error: whether a tool driver failed
		// mid-batch or an AfterToolCall rejected a result, dispatchPreparedTools still
		// returns the results that ran and side-effected, so recording that prefix
		// keeps the partial trace from dropping tool results a resuming caller would
		// otherwise replay or leave as a dangling assistant tool call. dispErr is the
		// root cause and is reported ahead of any secondary sink-emit failure on the
		// prefix.
		appendErr := appendToolResults(ctx, &current, results, input.Sink)
		if dispErr != nil {
			return loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed), dispErr
		}
		if appendErr != nil {
			return loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed), appendErr
		}
		decision := StepDecisionContinue
		if terminal {
			decision = StepDecisionFinish
		}
		steps = append(steps, Step{
			Index:      iteration,
			ModelCall:  modelCall,
			ToolCalls:  toolCallTraces(assistant.ToolCalls, results),
			Decision:   decision,
			BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
		})
		if terminal {
			return LoopOutput{
				Messages:      current,
				Usage:         totalUsage,
				StopReason:    provider.StopReasonComplete,
				Iterations:    iteration + 1,
				Thinking:      assistant.Thinking,
				Steps:         steps,
				ToolCallsUsed: toolCallsUsed,
			}, nil
		}
		// Continue boundary: this non-terminal tool turn would loop again. When a
		// StepPolicy is set, let it override that decision once a predicate over
		// the step trace holds — stopping, diverting, or failing the loop. The
		// natural Continue decision is already recorded on the step above;
		// stepPolicyOverride updates it so the trace reflects the decision that
		// actually ended the loop.
		if out, stop, overrideErr := stepPolicyOverride(input, current, totalUsage, steps, assistant.Thinking, iteration+1, toolCallsUsed); stop {
			return out, overrideErr
		}
	}
	return LoopOutput{
		Messages:      current,
		Usage:         totalUsage,
		StopReason:    provider.StopReasonMaxTurns,
		Iterations:    input.MaxIterations,
		Steps:         steps,
		ToolCallsUsed: toolCallsUsed,
	}, nil
}

// loopTurnPreamble runs the per-iteration preamble for turns after the first: it
// stops the loop when a budget dimension is exhausted (budgetAbort) and compacts
// the running history when the token budget is approached. When stop is true the
// loop returns out/err as-is; otherwise next is the history — possibly
// compacted — to drive the upcoming turn with. iteration is the count of model
// turns already issued, matching the budget-abort contract.
func loopTurnPreamble(ctx context.Context, input LoopInput, current []message.Message, usage provider.Usage, steps []Step, iteration, toolCallsUsed int) (next []message.Message, out LoopOutput, stop bool, err error) {
	if _, dimension := budgetRemaining(input, usage, toolCallsUsed, len(steps)); dimension != "" {
		out, err = budgetAbort(current, usage, steps, iteration, toolCallsUsed, dimension)
		return current, out, true, err
	}
	// The budget still has room for this turn. Compacting after the budget check
	// means a turn that would abort never pays for a compaction it cannot use.
	compacted, compactErr := maybeCompactHistory(ctx, input, current, usage)
	if compactErr != nil {
		return current, loopErrorOutput(current, usage, steps, iteration, toolCallsUsed), true, compactErr
	}
	return compacted, LoopOutput{}, false, nil
}

// stepPolicyOverride consults input.StepPolicy at a continue boundary and reports
// whether it stopped the loop. A nil policy, or a Continue/"" decision, returns
// stop=false so the loop keeps iterating. A Finish/Handoff override stops the
// loop cleanly (StopReasonComplete), recording the decision on the final step; a
// Fail decision, or a Next error, stops it with ErrStepAborted. iterations is the
// count of model turns that ran.
func stepPolicyOverride(input LoopInput, current []message.Message, usage provider.Usage, steps []Step, thinking string, iterations, toolCallsUsed int) (out LoopOutput, stop bool, err error) {
	if input.StepPolicy == nil {
		return LoopOutput{}, false, nil
	}
	decision, policyErr := input.StepPolicy.Next(LoopSnapshot{Steps: steps})
	if policyErr != nil {
		// Both errors stay wrapped: ErrStepAborted so the engine classifies the
		// failure, and policyErr so a caller's errors.Is/As still reaches the
		// policy's own sentinel — mirroring how a Compact error keeps its cause.
		return loopErrorOutput(current, usage, steps, iterations, toolCallsUsed), true,
			fmt.Errorf("%w: step policy: %w", ErrStepAborted, policyErr)
	}
	switch decision {
	case StepDecisionFinish, StepDecisionHandoff:
		steps[len(steps)-1].Decision = decision
		return LoopOutput{
			Messages:      current,
			Usage:         usage,
			StopReason:    provider.StopReasonComplete,
			Iterations:    iterations,
			Thinking:      thinking,
			Steps:         steps,
			ToolCallsUsed: toolCallsUsed,
		}, true, nil
	case StepDecisionFail:
		steps[len(steps)-1].Decision = StepDecisionFail
		return loopErrorOutput(current, usage, steps, iterations, toolCallsUsed), true,
			fmt.Errorf("%w: step policy returned fail", ErrStepAborted)
	}
	return LoopOutput{}, false, nil
}

// budgetRemaining returns a copy of input whose budget ceilings are reduced by
// the usage already spent, and reports the first exhausted dimension (empty
// when the loop may still spend). It is the single source of truth for both
// the in-loop "will continue" check and the cross-call repair check. The token
// ceiling fails open: a provider that reports zero usage keeps TotalTokens at
// zero, so the remaining budget never drops to or below zero on that dimension.
func budgetRemaining(input LoopInput, usage provider.Usage, toolCallsUsed, steps int) (LoopInput, string) {
	next := input
	if input.MaxTokens > 0 {
		next.MaxTokens = input.MaxTokens - int64(usage.TotalTokens)
		if next.MaxTokens <= 0 {
			return input, "max tokens"
		}
	}
	if input.MaxToolCalls > 0 {
		next.MaxToolCalls = input.MaxToolCalls - toolCallsUsed
		if next.MaxToolCalls <= 0 {
			return input, "max tool calls"
		}
	}
	if input.MaxSteps > 0 {
		next.MaxSteps = input.MaxSteps - steps
		if next.MaxSteps <= 0 {
			return input, "max steps"
		}
	}
	return next, ""
}

// compactionBudgetHeadroomDivisor sets the token-budget headroom below which the
// loop compacts the running history: when MaxTokens is set and the remaining
// budget falls to MaxTokens/compactionBudgetHeadroomDivisor or less (one band of
// ~20% left), the next turn's history is compacted first. A divisor keeps the
// trigger in integer arithmetic, off the floating-point path.
const compactionBudgetHeadroomDivisor = 5

// maybeCompactHistory returns the history to drive the next turn with, compacted
// when a Compact hook is wired and the per-loop token budget is being approached
// (MaxTokens > 0 and the consumed tokens leave one headroom band or less). It is
// a no-op — returning the input history unchanged — when no Compact is set or no
// token budget bounds the loop, so a run that does not opt into both never
// changes shape. A Compact error aborts the loop rather than silently proceeding
// on an unshrunk history. Callers reach this only after the pre-turn budget check
// has confirmed remaining budget is positive, so the headroom comparison never
// sees a negative remainder.
func maybeCompactHistory(ctx context.Context, input LoopInput, current []message.Message, usage provider.Usage) ([]message.Message, error) {
	if input.Compact == nil || input.MaxTokens <= 0 {
		return current, nil
	}
	remaining := input.MaxTokens - int64(usage.TotalTokens)
	if remaining > input.MaxTokens/compactionBudgetHeadroomDivisor {
		return current, nil
	}
	compacted, err := input.Compact(ctx, current)
	if err != nil {
		return current, fmt.Errorf("agent: compact history: %w", err)
	}
	return compacted, nil
}

// dispatchExceedsToolBudget reports whether executing this turn's tool batch
// would push the running tool-call count past MaxToolCalls. A single model
// turn can emit more parallel calls than the budget allows, so the loop gates
// the batch here rather than discovering the overrun on the next pre-turn
// check — by which point the side-effecting tools have already executed. A
// batch that exactly fills the budget is allowed; the next pre-turn check
// stops the run afterwards.
func dispatchExceedsToolBudget(input LoopInput, toolCallsUsed, batch int) bool {
	return input.MaxToolCalls > 0 && toolCallsUsed+batch > input.MaxToolCalls
}

// preDispatchBudgetBlock reports the budget dimension that forbids dispatching
// this turn's tool batch, or "" when the batch may run. It guards what the
// top-of-loop pre-turn check cannot: the just-completed model turn's token
// spend (totalUsage now includes it, so a single turn that blows MaxTokens is
// caught before its side-effecting tools run rather than at the next pre-turn
// check), and the batch's own tool-call count, which one turn can inflate past
// MaxToolCalls. A non-terminal tool turn is a "will continue" boundary, so any
// exhausted dimension must abort here, before ExecuteBatch. The step ceiling is
// evaluated against the pre-increment count, matching the pre-turn check: this
// turn's step is not yet recorded, so step exhaustion it causes surfaces on the
// next pre-turn check, never prematurely here.
func preDispatchBudgetBlock(input LoopInput, usage provider.Usage, toolCallsUsed, steps, batch int) string {
	if _, dimension := budgetRemaining(input, usage, toolCallsUsed, steps); dimension != "" {
		return dimension
	}
	if dispatchExceedsToolBudget(input, toolCallsUsed, batch) {
		return "max tool calls"
	}
	return ""
}

// budgetAbort builds the partial LoopOutput returned when the loop stops for
// budget reasons, tagging the exhausted dimension on the ErrBudgetExhausted
// chain. iterations is the number of model turns that actually ran.
func budgetAbort(current []message.Message, usage provider.Usage, steps []Step, iterations, toolCallsUsed int, dimension string) (LoopOutput, error) {
	return LoopOutput{
		Messages:      current,
		Usage:         usage,
		StopReason:    provider.StopReasonAborted,
		Iterations:    iterations,
		Steps:         steps,
		ToolCallsUsed: toolCallsUsed,
	}, fmt.Errorf("%w: %s", ErrBudgetExhausted, dimension)
}

// loopErrorOutput builds the partial LoopOutput returned alongside a
// non-budget loop error. It mirrors budgetAbort for the error path: the
// messages, usage, and steps accumulated before the failure are preserved
// (StopReason is StopReasonError) so a scheduler and the durable record keep
// the trace that led up to the error instead of discarding it. iterations is
// the number of model turns that ran before the failure.
func loopErrorOutput(current []message.Message, usage provider.Usage, steps []Step, iterations, toolCallsUsed int) LoopOutput {
	return LoopOutput{
		Messages:      current,
		Usage:         usage,
		StopReason:    provider.StopReasonError,
		Iterations:    iterations,
		Steps:         steps,
		ToolCallsUsed: toolCallsUsed,
	}
}

// recordIncompleteTurn folds a failed turn's partial work into the running trace.
// A turn can fail after the model already streamed content — a per-event callback
// erroring or panicking, which collect surfaces as an error carrying the
// normalized partial assistant turn. When that turn carries content, append it and
// fold its usage so the partial LoopOutput keeps the produced response and its
// token spend, and count the turn in Iterations; an empty turn leaves the trace,
// usage, and turn count untouched so a failure before any content reports nothing
// extra.
func recordIncompleteTurn(current []message.Message, assistant message.Message, totalUsage, usage provider.Usage, iteration, turnsRun int) ([]message.Message, provider.Usage, int) {
	if assistant.Text == "" && assistant.Thinking == "" && assistant.RedactedThinking == "" && len(assistant.ToolCalls) == 0 {
		return current, totalUsage, turnsRun
	}
	return append(current, assistant), totalUsage.Add(usage), iteration + 1
}

// appendRetryContext assembles the messages a guardrail retry feeds back into
// the loop: optionally the rejected assistant output, any replacement
// context, and the retry instructions themselves.
func appendRetryContext(current []message.Message, assistant message.Message, retryMessages []message.Message, retryPolicy RetryPolicy) []message.Message {
	if retryPolicy.IncludeRejectedOutput && (assistant.Text != "" || assistant.Thinking != "") {
		current = append(current, assistant)
	}
	if len(retryPolicy.ReplacementContext) > 0 {
		current = append(current, cloneMessages(retryPolicy.ReplacementContext)...)
	}
	return append(current, retryMessages...)
}

// appendFinalAssistant appends the finalized assistant message to history,
// skipping it when the guardrails left nothing to record.
func appendFinalAssistant(current []message.Message, finalOutput message.Message) []message.Message {
	if finalOutput.Text != "" || finalOutput.Thinking != "" {
		return append(current, finalOutput)
	}
	return current
}

// toolCallTraces builds a replay-safe trace for each tool call in a turn.
// The bus returns results positionally aligned with calls (sequential
// appends in order; parallel writes results[i] for calls[i]), so we pair by
// index rather than ToolCallID — drivers are not required to echo the call
// ID. Per-call timing is intentionally left zero: the bus exposes no
// per-call duration, and a wall-clock reading here would be a
// nondeterministic field on a replayable Step (ADR-007).
func toolCallTraces(calls []message.ToolCall, results []message.ToolResult) []ToolCallTrace {
	if len(calls) == 0 {
		return nil
	}
	traces := make([]ToolCallTrace, 0, len(calls))
	for i, call := range calls {
		trace := ToolCallTrace{
			Name:      call.Name,
			Arguments: call.Arguments,
		}
		if i < len(results) {
			result := results[i]
			trace.Output = result.Structured
			if result.IsError {
				trace.Error = result.Content
			}
		}
		traces = append(traces, trace)
	}
	return traces
}

func (e Engine) applyOutputGuardrails(ctx context.Context, input LoopInput, current []message.Message, assistant message.Message, iteration int, usage provider.Usage, stopReason provider.StopReason) (message.Message, []message.Message, RetryPolicy, error) {
	if len(input.OutputGuardrails) == 0 {
		return assistant, nil, RetryPolicy{}, nil
	}
	candidate := assistant
	for _, guardrail := range input.OutputGuardrails {
		if guardrail == nil {
			continue
		}
		result, err := guardrail.Check(ctx, OutputGuardrailInput{
			Model:         input.Model,
			Messages:      cloneMessages(current),
			Output:        candidate,
			Iteration:     iteration,
			MaxIterations: input.MaxIterations,
			Usage:         usage,
			StopReason:    stopReason,
			Metadata:      cloneStringMap(input.Metadata),
		})
		if err != nil {
			return message.Message{}, nil, RetryPolicy{}, err
		}
		normalized, err := normalizeOutputGuardrailResult(result)
		if err != nil {
			return message.Message{}, nil, RetryPolicy{}, err
		}
		switch normalized.Action {
		case OutputGuardrailActionAllow:
			continue
		case OutputGuardrailActionReplace:
			e.recordOutputGuardrailDecision(ctx, input, guardrail.Name(), normalized.Action, normalized.Reason, iteration, normalized.Metadata)
			candidate = *normalized.Replacement
		case OutputGuardrailActionRetry:
			e.recordOutputGuardrailDecision(ctx, input, guardrail.Name(), normalized.Action, normalized.Reason, iteration, normalized.Metadata)
			if iteration >= input.MaxIterations {
				return message.Message{}, nil, RetryPolicy{}, &OutputGuardrailRetryLimitExceededError{
					Guardrail: guardrail.Name(),
					Output:    candidate,
				}
			}
			return candidate, normalized.RetryMessages, normalized.RetryPolicy, nil
		case OutputGuardrailActionBlock:
			e.recordOutputGuardrailDecision(ctx, input, guardrail.Name(), normalized.Action, normalized.Reason, iteration, normalized.Metadata)
			return message.Message{}, nil, RetryPolicy{}, &OutputGuardrailTripwireTriggeredError{
				Guardrail: guardrail.Name(),
				Reason:    normalized.Reason,
				Output:    candidate,
			}
		}
	}
	return candidate, nil, RetryPolicy{}, nil
}

func (e Engine) recordOutputGuardrailDecision(ctx context.Context, input LoopInput, name string, action OutputGuardrailAction, reason string, iteration int, metadata map[string]string) {
	if input.OutputRecorder == nil {
		return
	}
	merged := cloneStringMap(input.Metadata)
	if merged == nil {
		merged = map[string]string{}
	}
	for key, value := range metadata {
		merged[key] = value
	}
	input.OutputRecorder.RecordOutputGuardrailDecision(ctx, OutputGuardrailDecision{
		GuardrailName: name,
		Action:        action,
		Reason:        reason,
		Iteration:     iteration,
		Metadata:      merged,
	})
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(values map[string]any) map[string]any {
	return maps.Clone(values)
}

// runTurn executes a single model turn: context transform, request assembly,
// provider stream and event collection.
func (e Engine) runTurn(ctx context.Context, current []message.Message, input LoopInput) (message.Message, provider.Usage, provider.StopReason, error) {
	transformed, err := e.Hooks.TransformContext(ctx, current)
	if err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, err
	}
	request := provider.Request{
		Model:          input.Model,
		Messages:       transformed,
		Metadata:       input.Metadata,
		StopSequences:  input.StopSequences,
		ThinkingBudget: input.ThinkingBudget,
		ResponseFormat: input.ResponseFormat,
		ExtraBody:      cloneAnyMap(input.ExtraBody),
	}
	if e.Tools != nil {
		request.Tools = e.Tools.Definitions()
	}
	if err := e.Hooks.BeforeModelCall(ctx, &request); err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, err
	}
	providerStream, err := e.Provider.Stream(ctx, request)
	if err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, err
	}
	return e.collect(ctx, providerStream, input.OnEvent, input.Sink)
}

// prepareToolCalls runs each call's BeforeToolCall hook — which may rewrite the
// tool name — and verifies the resulting name is registered, returning the
// prepared calls the bus will dispatch and whether any targets a terminal tool.
// A nil bus yields ErrToolBusMissing and an unregistered prepared call yields
// ErrToolNotFound; both are FailureKindToolUnavailable. Availability is judged on
// the hook-mutated calls, not the raw model-emitted ones, because the hook is the
// documented place to map a model alias or hallucinated name onto a real tool —
// checking the raw name would reject one a hook was about to fix. None of these
// paths dispatches a driver, so the loop returns before charging them.
func (e Engine) prepareToolCalls(ctx context.Context, calls []message.ToolCall) ([]tool.Call, bool, error) {
	if e.Tools == nil {
		return nil, false, ErrToolBusMissing
	}
	prepared := make([]tool.Call, 0, len(calls))
	terminal := false
	for _, call := range calls {
		item := call
		if err := e.Hooks.BeforeToolCall(ctx, &item); err != nil {
			return nil, false, err
		}
		driver, ok := e.Tools.Driver(item.Name)
		if !ok {
			return nil, false, fmt.Errorf("%w: %s", tool.ErrToolNotFound, item.Name)
		}
		if driver.Definition().Terminal {
			terminal = true
		}
		prepared = append(prepared, item)
	}
	return prepared, terminal, nil
}

// dispatchPreparedTools executes the hook-prepared calls on the bus and runs
// AfterToolCall on each result. prepareToolCalls already validated every call as
// registered, so a dispatch error comes from a driver that actually ran (or, for a
// sequential driver, a panic that unwinds inline to the RunMessages recover) —
// which is why the loop charges the batch before calling this.
//
// Every result ExecuteBatch returns ran to completion and side-effected, even when
// the batch reports an error: a later sequential call failing still yields the
// earlier successes, and a parallel call erroring or panicking still yields the
// completed slots. Those results are post-processed and returned alongside the
// batch error so the caller records them, sparing a resuming caller from replaying
// side-effecting calls or leaving the matching assistant tool calls dangling.
//
// If an AfterToolCall itself fails on one result (an error, or a panic hook.Chain
// converts to ErrHandlerPanic), the results blessed before it are returned with
// that error and the failing result and any after it are dropped — so the returned
// results are always a prefix of what AfterToolCall post-processed. An AfterToolCall
// failure takes precedence over a batch error because it is the earlier hook in the
// pipeline; the loop surfaces whichever non-nil error this returns.
func (e Engine) dispatchPreparedTools(ctx context.Context, prepared []tool.Call, mode tool.Mode) ([]message.ToolResult, error) {
	results, batchErr := e.Tools.ExecuteBatch(ctx, prepared, mode, nil)
	items := make([]message.ToolResult, 0, len(results))
	for _, current := range results {
		item := current
		if err := e.Hooks.AfterToolCall(ctx, &item); err != nil {
			return items, err
		}
		items = append(items, item)
	}
	return items, batchErr
}

// appendToolResults appends each tool result to the running history through the
// caller's slice pointer and, when a sink is set, emits a FrameToolResult for it.
// Tool results are a loop-level enrichment with no provider.Event equivalent. It
// appends each result to *current before emitting it, so the caller's history
// reflects every produced result the instant it exists, not only on return. That
// matters for both sink failure modes the loop's recover defer is meant to
// salvage: a sink Emit that returns an error surfaces it here with *current
// already holding the result, and a sink Emit that panics unwinds straight to the
// recover defer, which reads the same caller variable and still finds the result.
// A by-value return would lose the panic case, because the unwind skips the
// caller's assignment of the return value. The prompt, the assistant tool call,
// and every appended tool result thus survive on the partial LoopOutput rather
// than being discarded.
func appendToolResults(ctx context.Context, current *[]message.Message, results []message.ToolResult, sink stream.Sink) error {
	for _, result := range results {
		*current = append(*current, message.NewToolResult(result))
		if sink == nil {
			continue
		}
		toolResult := result
		if err := sink.Emit(ctx, stream.Frame{Kind: stream.FrameToolResult, ToolResult: &toolResult}); err != nil {
			return err
		}
	}
	return nil
}

func (e Engine) collect(ctx context.Context, providerStream provider.Stream, onEvent func(provider.Event) error, sink stream.Sink) (assistant message.Message, usage provider.Usage, stop provider.StopReason, err error) {
	defer func() { _ = providerStream.Close() }()
	assistant = message.Message{Role: message.RoleAssistant, Kind: message.KindStandard}
	events := make([]provider.Event, 0, 8)
	sawTerminal := false
	// A per-event callback (onEvent or the sink) can panic while handling an
	// event. Each event is recorded before it is delivered, so normalize the
	// events collected so far onto the assistant turn and surface the panic as an
	// ErrPanicRecovered error carrying that partial turn, rather than letting it
	// unwind to the loop's outer recover where the completed response would be
	// lost. errors.Is(err, ErrPanicRecovered) still holds end-to-end.
	defer func() {
		if r := recover(); r != nil {
			usage, _, _ = applyNormalized(&assistant, events)
			stop = provider.StopReasonError
			err = fmt.Errorf("%w: %v", ErrPanicRecovered, r)
		}
	}()
	for {
		// Stop draining the provider stream as soon as the context is done so a
		// cancelled or timed-out turn returns promptly instead of blocking on
		// the next event; the cause is surfaced for loopErrorFailure to classify.
		// Once the provider has delivered its terminal EventDone the response is
		// already complete (EventDone carries the StopReason), so a cancellation
		// landing in the window before io.EOF must not discard it — fall through
		// and normalize the events already collected rather than failing the turn.
		if !sawTerminal {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return assistant, provider.Usage{}, provider.StopReasonError, ctxErr
			}
		}
		event, recvErr := providerStream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				break
			}
			// A context-aware stream (one that wraps a body read) can unblock Recv
			// with the context error rather than io.EOF. If that lands after the
			// terminal EventDone the response is already complete, so treat it like
			// io.EOF: stop draining and normalize the events already collected
			// instead of discarding the finished turn. Only context cancellation is
			// tolerated here — any other post-terminal error is a genuine fault and
			// still propagates.
			if sawTerminal && (errors.Is(recvErr, context.Canceled) || errors.Is(recvErr, context.DeadlineExceeded)) {
				break
			}
			return assistant, provider.Usage{}, provider.StopReasonError, recvErr
		}
		if event.Kind == provider.EventDone {
			sawTerminal = true
		}
		// Record the event before delivering it to the callbacks: a callback that
		// errors or panics must not discard the response already streamed, so both
		// the recover above and the error return below normalize the events held so
		// far rather than starting from an empty turn.
		events = append(events, event)
		if cbErr := e.fanOutEvent(ctx, event, onEvent, sink); cbErr != nil {
			usage, stop, _ = applyNormalized(&assistant, events)
			return assistant, usage, stop, cbErr
		}
	}
	usage, stop, err = applyNormalized(&assistant, events)
	if err != nil {
		return assistant, provider.Usage{}, provider.StopReasonError, err
	}
	return assistant, usage, stop, nil
}

// fanOutEvent delivers one provider event to the caller callback, the hook chain,
// and the sink (frames only), returning the first error. collect records the
// event before calling this, so a callback failure here ends the turn with the
// response streamed so far already preserved for the partial trace.
func (e Engine) fanOutEvent(ctx context.Context, event provider.Event, onEvent func(provider.Event) error, sink stream.Sink) error {
	if onEvent != nil {
		if err := onEvent(event); err != nil {
			return err
		}
	}
	if err := e.Hooks.OnEvent(ctx, event); err != nil {
		return err
	}
	if sink != nil {
		if frame, ok := stream.FrameFromEvent(event); ok {
			if err := sink.Emit(ctx, frame); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyNormalized copies the text, thinking, signatures, and tool calls from the
// events collected so far onto the assistant turn, and reports the turn's usage
// and stop reason. It centralizes the normalization collect performs on both its
// success path and its failure paths (a callback error or a recovered panic),
// where the partial turn must still carry whatever the model already produced. A
// normalize error (a malformed stream prefix) leaves the assistant untouched and
// reports StopReasonError, so a failure path never masks its original cause with a
// normalize error.
func applyNormalized(assistant *message.Message, events []provider.Event) (provider.Usage, provider.StopReason, error) {
	normalized, err := provider.NormalizeEvents(events)
	if err != nil {
		return provider.Usage{}, provider.StopReasonError, err
	}
	assistant.Text = normalized.Text
	assistant.Thinking = normalized.Thinking
	assistant.ThinkingSignature = normalized.Signature
	assistant.RedactedThinking = normalized.RedactedThinking
	assistant.ToolCalls = normalized.ToolCalls
	return normalized.Usage, normalized.StopReason, nil
}
