package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"sync"

	"github.com/Viking602/venat/hook"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/skill"
	"github.com/Viking602/venat/stream"
	"github.com/Viking602/venat/tool"
)

var ErrToolBusMissing = errors.New("tool bus missing")

const (
	maxProviderTurnEvents = 65_536
	maxProviderTurnBytes  = 64 << 20
)

// ErrProviderTurnLimit reports a provider stream that exceeded the
// provider-neutral per-turn event or decoded-byte ceiling.
var ErrProviderTurnLimit = errors.New("provider turn exceeds safe stream limits")

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
	Model       string
	Messages    []message.Message
	Temperature float64
	TopP        float64
	// ModelMaxTokens caps output tokens for each provider request. MaxTokens
	// below remains the cumulative loop-spend budget.
	ModelMaxTokens int
	Metadata       map[string]string
	ToolMode       tool.Mode
	MaxIterations  int
	// UnlimitedIterations explicitly disables the model-turn ceiling. When
	// false, an unset MaxIterations retains the conservative default of 12.
	UnlimitedIterations bool
	// OperationTurn is the next durable tool-call turn ordinal. Checkpoint
	// recovery restores it so compaction cannot reuse a prior operation ID.
	OperationTurn int
	OnEvent       func(provider.Event) error

	// Sink, when set, receives a live stream.Frame for every provider
	// event and tool result as the loop runs. It is a transient
	// side-channel: on success the durable LoopOutput is byte-for-byte
	// identical to a run with no Sink, so streaming never changes what the
	// runner persists or replays. A Sink.Emit error is not swallowed — it
	// aborts the current turn and surfaces as the loop error, exactly like a
	// failing OnEvent callback — so a Sink must absorb transient delivery
	// hiccups it can tolerate rather than returning an error for them.
	Sink              stream.Sink
	Control           TurnControl
	ContextTransition ContextTransition
	// AppliedControlIDs come only from the latest host-owned durable
	// checkpoint. They acknowledge controls already embedded in that checkpoint
	// before a resumed loop reserves new work.
	AppliedControlIDs        []string
	externallyAccountedUsage *provider.Usage

	StopSequences  []string
	ThinkingBudget int
	ResponseFormat *provider.ResponseFormat
	// godoc-allow-any: provider-specific request extensions are intentionally open.
	ExtraBody         map[string]any
	PromptCacheKey    string
	ServiceTier       string
	ParallelToolCalls *bool
	NativeToolHost    provider.NativeToolHost
	ContextUsage      provider.ContextUsageObserver

	OutputGuardrails []OutputGuardrail
	OutputRecorder   OutputGuardrailRecorder

	// MaxTokens / MaxToolCalls / MaxSteps are the per-loop budget ceilings.
	// Zero means unbounded on that dimension. They are enforced fail-closed
	// but only on turns that would continue the loop: a run that is about to
	// finish is never failed for a budget it has not yet exceeded. MaxSteps
	// is a hard ceiling (exhausting it fails the run with ErrBudgetExhausted),
	// distinct from MaxIterations, whose soft ceiling yields StopReasonMaxTurns.
	// A successful turn that reports no usage fails closed under a positive
	// token ceiling before a final answer is accepted or tools are dispatched.
	MaxTokens    int64
	MaxToolCalls int
	MaxSteps     int

	// ContextTokenTarget is the usable token allowance for message history in
	// one provider request, after the caller reserves room for output, tools,
	// schemas, reasoning, and provider framing. It is independent of MaxTokens,
	// which remains the cumulative run-spend ceiling. When positive, the loop
	// prepares context before every model turn, including the first.
	ContextTokenTarget int

	// StepPolicy, when set, is consulted at each continue boundary (after a
	// non-terminal tool turn) so a caller can override the loop's natural
	// decision to iterate again — stopping early, diverting to a handoff, or
	// failing the run once a predicate over the step trace holds. A nil policy
	// leaves the loop's natural control flow unchanged. See StepPolicy.
	StepPolicy StepPolicy

	// StepRecorder, when set, receives each Step exactly once after its final
	// decision is known. A recording failure aborts the loop while preserving
	// the finalized step in the returned partial trace.
	StepRecorder StepRecorder

	// CheckpointRecorder, when set, receives the provider-neutral transcript and
	// cumulative budget at each completed model-turn boundary. Recording failure
	// aborts before the loop advances, so a retry never skips unpersisted work.
	CheckpointRecorder CheckpointRecorder

	// Compact is the source-compatible history compaction hook. With no
	// ContextTokenTarget it retains the legacy trigger: the loop invokes it once
	// cumulative MaxTokens spend enters the final headroom band. With a positive
	// target and no CompactTo, the loop invokes Compact before every request as a
	// best-effort fallback, including when MaxTokens is zero; because Compact does
	// not receive the target it cannot guarantee a fit. Engine.Run wires this from
	// ContextManager.Compact.
	//
	// Consumed tokens only grow, so once the loop enters the headroom band the
	// trigger holds for every remaining turn and Compact runs before each one. A
	// compactor must therefore be idempotent — cheap and stable on a history it
	// already compacted — not a one-shot transform.
	//
	// Determinism: the loop triggers Compact deterministically (the same trigger
	// fires on replay), so a deterministic compactor keeps the run
	// replay-faithful (ADR-007) while an LLM-backed one does not. After Compact
	// returns successfully, the loop validates that its output contains only
	// complete tool turns and rejects malformed or split exchanges.
	Compact func(ctx context.Context, history []message.Message) ([]message.Message, error)

	// CompactTo is the token-aware context preparation hook. When
	// ContextTokenTarget is positive, the loop invokes CompactTo before every
	// provider request and prefers it over Compact. The implementation owns
	// model-specific token estimation and should return history unchanged when it
	// already fits. Engine.Run wires this from TargetContextManager.
	CompactTo func(ctx context.Context, history []message.Message, targetTokens int) ([]message.Message, error)
}

// LoopOutput is the message-level result from Engine.RunMessages. The
// task-level Result type lives in result.go.
type LoopOutput struct {
	Messages   []message.Message
	Usage      provider.Usage
	StopReason provider.StopReason
	// ExternallyAccountedUsage is included in Usage for in-memory budget
	// enforcement but was already durably recorded by a child scheduler.
	ExternallyAccountedUsage provider.Usage
	Iterations               int
	Thinking                 string

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
	Temperature    float64
	TopP           float64
	ModelMaxTokens int
	ToolMode       tool.Mode
	LoopPolicy     LoopPolicy
	ContextBuilder ContextManager
	// OperationTurn seeds the next durable tool-call turn ordinal for resumed
	// task executions. New executions leave it at zero.
	OperationTurn int

	// Skills are active reusable instructions Engine.Run injects into task-level
	// context. RunMessages is the low-level message API and does not read this
	// Engine default.
	Skills []skill.Skill

	// AvailableSkills are disclosed as metadata and activated on demand. Build
	// resolves them from Spec.AvailableSkills; direct Engine construction may
	// also supply validated skills here.
	AvailableSkills []skill.Skill

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
	ExtraBody         map[string]any
	PromptCacheKey    string
	ServiceTier       string
	ParallelToolCalls *bool
	NativeToolHost    provider.NativeToolHost
	ContextUsage      provider.ContextUsageObserver
	Control           TurnControl
	ContextTransition ContextTransition
	AppliedControlIDs []string
	// SubagentScheduler routes agent-as-tool child executions through an
	// application-owned durable scheduler. A nil scheduler keeps the embedded
	// synchronous execution used by small in-process applications.
	SubagentScheduler SubagentScheduler

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

	// StepRecorder, when set, Engine.Run threads into every LoopInput so each
	// finalized step can be persisted before the loop advances.
	StepRecorder StepRecorder
	// CheckpointRecorder persists provider-neutral message/tool-result state at
	// each completed turn so another worker execution can resume from it.
	CheckpointRecorder CheckpointRecorder
}

// RunMessages is the low-level loop that drives one LoopInput to
// completion. Engine.Run is the task-level wrapper most callers want.
func (e Engine) RunMessages(ctx context.Context, input LoopInput) (out LoopOutput, err error) {
	controlSession, controlErr := attachTurnControlSession(ctx, &input)
	if controlErr != nil {
		return LoopOutput{Messages: message.CloneMessages(input.Messages)}, controlErr
	}
	defer func() {
		err = errors.Join(err, releaseTurnControlSession(context.WithoutCancel(ctx), controlSession))
	}()
	input, stepCapacity := normalizeIterationPolicy(input)
	if input.ToolMode == "" {
		input.ToolMode = tool.ModeSequential
	}
	input.OperationTurn = max(input.OperationTurn, nextToolOperationTurn(input.Messages))
	current := message.CloneMessages(input.Messages)
	totalUsage := provider.Usage{}
	externallyAccountedUsage := provider.Usage{}
	input.externallyAccountedUsage = &externallyAccountedUsage
	steps := make([]Step, 0, stepCapacity)
	lastModelCall := (*ModelCall)(nil)
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
		out.ExternallyAccountedUsage = externallyAccountedUsage
	}()
	defer recoverRunMessagesPanic(
		ctx,
		input,
		&out,
		&err,
		&current,
		&totalUsage,
		&steps,
		&lastModelCall,
		&turnsRun,
		&toolCallsUsed,
	)
	for iteration := 0; iterationAllowed(input, iteration); iteration++ {
		// A cancelled or expired context ends the loop promptly rather than
		// issuing another model turn; the cause (context.Canceled or
		// context.DeadlineExceeded) flows through loopErrorFailure, which maps a
		// budget-driven deadline to FailureKindBudgetExhausted.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return loopErrorOutput(current, totalUsage, steps, iteration, toolCallsUsed), ctxErr
		}
		var stopForControl bool
		current, steps, stopForControl, controlErr = handleBeforeModelControl(
			ctx, input, current, totalUsage, steps, iteration, toolCallsUsed,
		)
		if stopForControl {
			return loopErrorOutput(current, totalUsage, steps, iteration, toolCallsUsed), controlErr
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
		} else if input.ContextTokenTarget > 0 {
			prepared, prepareErr := maybeCompactHistory(ctx, input, current, totalUsage)
			if prepareErr != nil {
				return loopErrorOutput(current, totalUsage, steps, iteration, toolCallsUsed), prepareErr
			}
			current = prepared
		}
		assistant, usage, stopReason, identity, opened, turnErr := e.runTurn(ctx, current, input)
		if turnErr != nil {
			resume, failOut, failErr := e.handleTurnError(
				ctx, input, turnErr, assistant, usage, stopReason, identity, opened, iteration,
				&current, &totalUsage, &turnsRun, &steps, toolCallsUsed,
			)
			if resume {
				continue
			}
			return failOut, failErr
		}
		totalUsage = totalUsage.Add(usage)
		// The model turn ran and its usage is now counted, so a panic from here on
		// (guardrails, recorders, sink, sequential tool drivers) must report this
		// turn in Iterations rather than only the turns whose Step already exists.
		turnsRun = iteration + 1
		modelCall := newModelCall(identity, usage, stopReason)
		lastModelCall = modelCall
		if input.MaxTokens > 0 && usage.TotalTokens == 0 {
			current = append(current, assistant)
			steps = append(steps, Step{
				Index:      iteration,
				ModelCall:  modelCall,
				Decision:   StepDecisionFail,
				BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
			})
			if recordErr := recordFinalizedStep(ctx, input.StepRecorder, steps); recordErr != nil {
				return loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed), recordErr
			}
			return budgetAbort(current, totalUsage, steps, iteration+1, toolCallsUsed, "max tokens")
		}
		if len(assistant.ToolCalls) == 0 {
			var retry bool
			current, steps, out, retry, err = e.finalizeNoToolStep(
				ctx, input, current, assistant, modelCall, totalUsage, steps,
				stopReason, iteration, toolCallsUsed,
			)
			if retry {
				continue
			}
			return out, err
		}
		var stop bool
		out, stop, err = e.runToolStep(
			ctx, &input, &current, assistant, modelCall, &totalUsage, &externallyAccountedUsage,
			&steps, iteration, &toolCallsUsed,
		)
		if stop {
			return out, err
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

func handleBeforeModelControl(
	ctx context.Context,
	input LoopInput,
	current []message.Message,
	totalUsage provider.Usage,
	steps []Step,
	iteration int,
	toolCallsUsed int,
) ([]message.Message, []Step, bool, error) {
	controlBatch, err := drainTurnControl(ctx, input.Control, TurnBoundaryBeforeModel)
	if err != nil {
		return current, steps, true, err
	}
	current = append(current, controlMessages(controlBatch)...)
	if !controlAborts(controlBatch) {
		return current, steps, false, nil
	}
	steps = append(steps, Step{
		Index:      iteration,
		Decision:   StepDecisionFail,
		BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
	})
	if recordErr := recordTurnBoundary(ctx, input, current, totalUsage, steps, toolCallsUsed); recordErr != nil {
		return current, steps, true, errors.Join(ErrTurnControlAbort, recordErr)
	}
	return current, steps, true, ErrTurnControlAbort
}

func recoverRunMessagesPanic(
	ctx context.Context,
	input LoopInput,
	out *LoopOutput,
	runErr *error,
	current *[]message.Message,
	totalUsage *provider.Usage,
	steps *[]Step,
	lastModelCall **ModelCall,
	turnsRun *int,
	toolCallsUsed *int,
) {
	panicValue := recover()
	if panicValue == nil {
		return
	}
	last := *lastModelCall
	if last != nil && (len(*steps) == 0 || (*steps)[len(*steps)-1].ModelCall != last) {
		*steps = append(*steps, Step{
			Index:      *turnsRun - 1,
			ModelCall:  last,
			Decision:   StepDecisionFail,
			BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: *toolCallsUsed},
		})
		if recordErr := recordRecoveredStep(ctx, input.StepRecorder, *steps); recordErr != nil {
			*runErr = errors.Join(*runErr, recordErr)
		}
	}
	*out = loopErrorOutput(*current, *totalUsage, *steps, *turnsRun, *toolCallsUsed)
	*runErr = errors.Join(*runErr, fmt.Errorf("%w: %v", ErrPanicRecovered, panicValue))
}

func newModelCall(identity provider.StreamIdentity, usage provider.Usage, stopReason provider.StopReason) *ModelCall {
	return &ModelCall{
		Provider:                      identity.Provider.Name,
		Model:                         identity.Model,
		InputTokens:                   usage.InputTokens,
		CachedInputTokens:             usage.CachedInputTokens,
		CachedInputTokensReported:     usage.CachedInputTokensReported,
		CacheWriteInputTokens:         usage.CacheWriteInputTokens,
		CacheWriteInputTokensReported: usage.CacheWriteInputTokensReported,
		OutputTokens:                  usage.OutputTokens,
		ReasoningTokens:               usage.ReasoningTokens,
		TotalTokens:                   usage.TotalTokens,
		StopReason:                    stopReason,
	}
}

// handleTurnError finalizes a failed model turn. It reports resume=true when
// the loop should continue at the next iteration (a stream-rule interrupt);
// otherwise it returns the output and error RunMessages must surface.
func (e Engine) handleTurnError(
	ctx context.Context,
	input LoopInput,
	turnErr error,
	assistant message.Message,
	usage provider.Usage,
	stopReason provider.StopReason,
	identity provider.StreamIdentity,
	opened bool,
	iteration int,
	current *[]message.Message,
	totalUsage *provider.Usage,
	turnsRun *int,
	steps *[]Step,
	toolCallsUsed int,
) (bool, LoopOutput, error) {
	var streamInterrupt *StreamRuleInterruptError
	if errors.As(turnErr, &streamInterrupt) {
		if streamInterrupt.KeepPartial {
			*current, *totalUsage, *turnsRun = recordIncompleteTurn(*current, assistant, *totalUsage, usage, iteration, *turnsRun)
		} else {
			*totalUsage = totalUsage.Add(usage)
			*turnsRun = max(*turnsRun, iteration+1)
		}
		if opened {
			*steps = append(*steps, Step{
				Index:      iteration,
				ModelCall:  newModelCall(identity, usage, provider.StopReasonAborted),
				Decision:   StepDecisionContinue,
				BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
			})
			if recordErr := recordFinalizedStep(ctx, input.StepRecorder, *steps); recordErr != nil {
				return false, loopErrorOutput(*current, *totalUsage, *steps, *turnsRun, toolCallsUsed), errors.Join(turnErr, recordErr)
			}
		}
		return true, LoopOutput{}, nil
	}
	// A turn can fail after its stream opens: preserve a failed ModelCall
	// with the selected stream identity so durable usage cannot fall back
	// to the wrapper's primary metadata.
	*current, *totalUsage, *turnsRun = recordIncompleteTurn(*current, assistant, *totalUsage, usage, iteration, *turnsRun)
	if opened {
		*turnsRun = max(*turnsRun, iteration+1)
		*steps = append(*steps, Step{
			Index:      iteration,
			ModelCall:  newModelCall(identity, usage, stopReason),
			Decision:   StepDecisionFail,
			BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
		})
		if recordErr := recordFinalizedStep(ctx, input.StepRecorder, *steps); recordErr != nil {
			return false, loopErrorOutput(*current, *totalUsage, *steps, *turnsRun, toolCallsUsed), errors.Join(turnErr, recordErr)
		}
	}
	return false, loopErrorOutput(*current, *totalUsage, *steps, *turnsRun, toolCallsUsed), turnErr
}

func (e Engine) runToolStep(
	ctx context.Context,
	input *LoopInput,
	current *[]message.Message,
	assistant message.Message,
	modelCall *ModelCall,
	totalUsage *provider.Usage,
	externallyAccountedUsage *provider.Usage,
	steps *[]Step,
	iteration int,
	toolCallsUsed *int,
) (LoopOutput, bool, error) {
	for index := range assistant.ToolCalls {
		assistant.ToolCalls[index].OperationID = fmt.Sprintf("turn:%d:call:%d", input.OperationTurn, index)
	}
	input.OperationTurn++
	*current = append(*current, assistant)

	// A completed tool-use turn is only a request for side effects. Refuse to
	// dispatch it when cancellation landed after the provider's terminal event.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return loopErrorOutput(*current, *totalUsage, *steps, iteration+1, *toolCallsUsed), true, ctxErr
	}

	beforeResults, controlBatch, controlErr := prepareBeforeToolControl(
		ctx, input.Control, assistant.ToolCalls, current, input.Sink,
	)
	if controlErr != nil {
		return loopErrorOutput(*current, *totalUsage, *steps, iteration+1, *toolCallsUsed), true, controlErr
	}
	if len(controlBatch) > 0 {
		*current = append(*current, controlMessages(controlBatch)...)
		if controlAborts(controlBatch) {
			nextSteps, out, abortErr := abortToolControl(
				ctx, *input, *current, *totalUsage, *steps, modelCall,
				assistant.ToolCalls, beforeResults, iteration, *toolCallsUsed,
			)
			*steps = nextSteps
			return out, true, abortErr
		}
		nextSteps, out, stop, err := finalizeToolStep(
			ctx, *input, *current, *totalUsage, *steps, assistant, modelCall,
			beforeResults, false, iteration, *toolCallsUsed,
		)
		*steps = nextSteps
		return out, stop, err
	}

	// Reserve the whole batch before hooks or drivers run. This prevents one
	// side-effecting batch from crossing a token or tool-call ceiling.
	if dimension := preDispatchBudgetBlock(*input, *totalUsage, *toolCallsUsed, len(*steps), len(assistant.ToolCalls)); dimension != "" {
		*steps = append(*steps, Step{
			Index:      iteration,
			ModelCall:  modelCall,
			Decision:   StepDecisionFail,
			BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: *toolCallsUsed},
		})
		if recordErr := recordFinalizedStep(ctx, input.StepRecorder, *steps); recordErr != nil {
			return loopErrorOutput(*current, *totalUsage, *steps, iteration+1, *toolCallsUsed), true, recordErr
		}
		out, err := budgetAbort(*current, *totalUsage, *steps, iteration+1, *toolCallsUsed, dimension)
		return out, true, err
	}

	// Hooks may rewrite tool names, so validate the prepared calls. Charge the
	// registered batch before dispatch so panics and partial failures cannot
	// under-report usage.
	prepared, terminal, prepErr := e.prepareToolCalls(ctx, assistant.ToolCalls)
	if prepErr != nil {
		return loopErrorOutput(*current, *totalUsage, *steps, iteration+1, *toolCallsUsed), true, prepErr
	}
	*toolCallsUsed += len(assistant.ToolCalls)
	var childUsage, childExternallyAccounted provider.Usage
	var childUsageMu sync.Mutex
	toolCtx := withParentUsageSink(ctx, func(usage, external provider.Usage) {
		childUsageMu.Lock()
		defer childUsageMu.Unlock()
		childUsage = childUsage.Add(usage)
		childExternallyAccounted = childExternallyAccounted.Add(external)
	})
	if input.MaxTokens > 0 {
		remaining := input.MaxTokens - int64(totalUsage.TotalTokens)
		if remaining < 0 {
			remaining = 0
		}
		toolCtx = withParentTokenBudget(toolCtx, remaining)
	}
	toolCtx, stopControlWatch := controlledToolContext(toolCtx, input.Control)
	results, dispatchErr := e.dispatchPreparedTools(toolCtx, prepared, input.ToolMode)
	stopControlWatch()
	*totalUsage = totalUsage.Add(childUsage)
	*externallyAccountedUsage = externallyAccountedUsage.Add(childExternallyAccounted)

	postToolControl, controlErr := drainTurnControl(ctx, input.Control, TurnBoundaryAfterTools)
	if controlErr != nil {
		return loopErrorOutput(*current, *totalUsage, *steps, iteration+1, *toolCallsUsed), true, controlErr
	}
	if len(postToolControl) > 0 && dispatchErr != nil {
		notExecuted, allNotExecuted := tool.NotExecutedCallIDs(dispatchErr)
		results = completeCancelledToolResults(prepared, results, notExecuted)
		if allNotExecuted {
			dispatchErr = nil
		}
	}

	// Preserve every result that ran before reporting the dispatch error. A
	// steer closes only slots the tool bus proves never started. Running or
	// otherwise unknown outcomes remain errors for durable reconciliation.
	appendErr := appendToolResults(ctx, current, results, input.Sink)
	if executionErr := errors.Join(dispatchErr, appendErr); executionErr != nil {
		return loopErrorOutput(*current, *totalUsage, *steps, iteration+1, *toolCallsUsed), true, executionErr
	}
	transitioned, transitionErr := applyContextTransition(ctx, input.ContextTransition, *current, results)
	if transitionErr != nil {
		return loopErrorOutput(*current, *totalUsage, *steps, iteration+1, *toolCallsUsed), true, transitionErr
	}
	*current = transitioned
	if len(postToolControl) > 0 {
		*current = append(*current, controlMessages(postToolControl)...)
		terminal = false
		if controlAborts(postToolControl) {
			nextSteps, out, abortErr := abortToolControl(
				ctx, *input, *current, *totalUsage, *steps, modelCall,
				assistant.ToolCalls, results, iteration, *toolCallsUsed,
			)
			*steps = nextSteps
			return out, true, abortErr
		}
	}
	nextSteps, out, stop, err := finalizeToolStep(
		ctx, *input, *current, *totalUsage, *steps, assistant, modelCall,
		results, terminal, iteration, *toolCallsUsed,
	)
	*steps = nextSteps
	return out, stop, err
}

// abortToolControl records the failed step for a control abort and finalizes
// the turn boundary before surfacing ErrTurnControlAbort.
func abortToolControl(
	ctx context.Context,
	input LoopInput,
	current []message.Message,
	totalUsage provider.Usage,
	steps []Step,
	modelCall *ModelCall,
	calls []message.ToolCall,
	results []tool.Result,
	iteration int,
	toolCallsUsed int,
) ([]Step, LoopOutput, error) {
	steps = append(steps, Step{
		Index:      iteration,
		ModelCall:  modelCall,
		ToolCalls:  toolCallTraces(calls, results),
		Decision:   StepDecisionFail,
		BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
	})
	abortErr := error(ErrTurnControlAbort)
	if err := recordTurnBoundary(ctx, input, current, totalUsage, steps, toolCallsUsed); err != nil {
		abortErr = errors.Join(ErrTurnControlAbort, err)
	}
	return steps, loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed), abortErr
}

// applyContextTransition hands the post-batch history to the host transition
// and installs the cloned result. History passes through unchanged when no
// transition is configured.
func applyContextTransition(ctx context.Context, transition ContextTransition, current []message.Message, results []tool.Result) ([]message.Message, error) {
	if transition == nil {
		return current, nil
	}
	transitioned, err := transition.Apply(ctx, message.CloneMessages(current), append([]tool.Result(nil), results...))
	if err != nil {
		return nil, err
	}
	return message.CloneMessages(transitioned), nil
}

func prepareBeforeToolControl(
	ctx context.Context,
	control TurnControl,
	calls []message.ToolCall,
	current *[]message.Message,
	sink stream.Sink,
) ([]tool.Result, []ControlMessage, error) {
	controlBatch, err := drainTurnControl(ctx, control, TurnBoundaryBeforeTools)
	if err != nil || len(controlBatch) == 0 {
		return nil, controlBatch, err
	}
	skipped := make([]tool.Call, len(calls))
	for index, call := range calls {
		skipped[index] = tool.Call{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
	}
	results := cancelledToolResults(skipped)
	if err := appendToolResults(ctx, current, results, sink); err != nil {
		return nil, nil, err
	}
	return results, controlBatch, nil
}

func normalizeIterationPolicy(input LoopInput) (LoopInput, int) {
	if input.UnlimitedIterations {
		return input, 16
	}
	if input.MaxIterations <= 0 {
		// The conservative default remains in place unless a caller explicitly
		// opts into unlimited interactive turns.
		input.MaxIterations = 12
	}
	return input, input.MaxIterations
}

func iterationAllowed(input LoopInput, iteration int) bool {
	return input.UnlimitedIterations || iteration < input.MaxIterations
}

func (e Engine) finalizeNoToolStep(
	ctx context.Context,
	input LoopInput,
	current []message.Message,
	assistant message.Message,
	modelCall *ModelCall,
	totalUsage provider.Usage,
	steps []Step,
	stopReason provider.StopReason,
	iteration int,
	toolCallsUsed int,
) ([]message.Message, []Step, LoopOutput, bool, error) {
	finalOutput, retryMessages, retryPolicy, guardErr := e.applyOutputGuardrails(
		ctx, input, current, assistant, iteration+1, totalUsage, stopReason,
	)
	if guardErr != nil {
		return current, steps,
			loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed),
			false, guardErr
	}
	if len(retryMessages) > 0 {
		steps = append(steps, Step{
			Index:      iteration,
			ModelCall:  modelCall,
			Decision:   StepDecisionContinue,
			BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
		})
		current = appendRetryContext(current, assistant, retryMessages, retryPolicy)
		if recordErr := recordTurnBoundary(ctx, input, current, totalUsage, steps, toolCallsUsed); recordErr != nil {
			return current, steps,
				loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed),
				false, recordErr
		}
		return current, steps, LoopOutput{}, true, nil
	}
	controlBatch, controlErr := drainTurnControl(ctx, input.Control, TurnBoundaryAfterAnswer)
	if controlErr != nil {
		return current, steps,
			loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed),
			false, controlErr
	}
	if controlAborts(controlBatch) {
		steps = append(steps, Step{
			Index:      iteration,
			ModelCall:  modelCall,
			Decision:   StepDecisionFail,
			BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
		})
		if recordErr := recordTurnBoundary(ctx, input, current, totalUsage, steps, toolCallsUsed); recordErr != nil {
			return current, steps,
				loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed),
				false, errors.Join(ErrTurnControlAbort, recordErr)
		}
		return current, steps,
			loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed),
			false, ErrTurnControlAbort
	}
	if messages := controlMessages(controlBatch); len(messages) > 0 {
		current = appendFinalAssistant(current, finalOutput)
		current = append(current, messages...)
		steps = append(steps, Step{
			Index:      iteration,
			ModelCall:  modelCall,
			Decision:   StepDecisionContinue,
			BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
		})
		if recordErr := recordTurnBoundary(ctx, input, current, totalUsage, steps, toolCallsUsed); recordErr != nil {
			return current, steps,
				loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed),
				false, recordErr
		}
		return current, steps, LoopOutput{}, true, nil
	}
	current = appendFinalAssistant(current, finalOutput)
	steps = append(steps, Step{
		Index:      iteration,
		ModelCall:  modelCall,
		Decision:   StepDecisionFinish,
		BudgetUsed: BudgetUsage{Tokens: int64(totalUsage.TotalTokens), ToolCalls: toolCallsUsed},
	})
	if recordErr := recordTurnBoundary(ctx, input, current, totalUsage, steps, toolCallsUsed); recordErr != nil {
		return current, steps,
			loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed),
			false, recordErr
	}
	return current, steps, LoopOutput{
		Messages:      current,
		Usage:         totalUsage,
		StopReason:    stopReason,
		Iterations:    iteration + 1,
		Thinking:      finalOutput.Thinking,
		Steps:         steps,
		ToolCallsUsed: toolCallsUsed,
	}, false, nil
}

func finalizeToolStep(
	ctx context.Context,
	input LoopInput,
	current []message.Message,
	totalUsage provider.Usage,
	steps []Step,
	assistant message.Message,
	modelCall *ModelCall,
	results []tool.Result,
	terminal bool,
	iteration int,
	toolCallsUsed int,
) ([]Step, LoopOutput, bool, error) {
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
		if recordErr := recordTurnBoundary(ctx, input, current, totalUsage, steps, toolCallsUsed); recordErr != nil {
			return steps, loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed), true, recordErr
		}
		return steps, LoopOutput{
			Messages:      current,
			Usage:         totalUsage,
			StopReason:    provider.StopReasonComplete,
			Iterations:    iteration + 1,
			Thinking:      assistant.Thinking,
			Steps:         steps,
			ToolCallsUsed: toolCallsUsed,
		}, true, nil
	}

	// Continue boundary: let StepPolicy mutate the latest decision before
	// persisting that finalized decision exactly once.
	policyOut, stop, policyErr := stepPolicyOverride(
		input, current, totalUsage, steps, assistant.Thinking, iteration+1, toolCallsUsed,
	)
	recordErr := recordTurnBoundary(ctx, input, current, totalUsage, steps, toolCallsUsed)
	if policyErr != nil && recordErr != nil {
		return steps, policyOut, true, errors.Join(policyErr, recordErr)
	}
	if recordErr != nil {
		return steps, loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed), true, recordErr
	}
	if stop {
		return steps, policyOut, true, policyErr
	}
	return steps, LoopOutput{}, false, nil
}

// recordFinalizedStep invokes recorder for the latest finalized step. The caller
// appends the step before calling this helper so every failure returns the step
// in the partial trace.
func recordFinalizedStep(ctx context.Context, recorder StepRecorder, steps []Step) error {
	if recorder == nil {
		return nil
	}
	step := steps[len(steps)-1]
	if err := recorder.RecordStep(ctx, step); err != nil {
		return fmt.Errorf("agent: record step %d: %w", step.Index, err)
	}
	return nil
}

func recordRecoveredStep(ctx context.Context, recorder StepRecorder, steps []Step) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrPanicRecovered, r)
		}
	}()
	return recordFinalizedStep(ctx, recorder, steps)
}

func nextToolOperationTurn(messages []message.Message) int {
	next := 0
	for _, msg := range messages {
		for _, call := range msg.ToolCalls {
			var turn, callIndex int
			if count, err := fmt.Sscanf(call.OperationID, "turn:%d:call:%d", &turn, &callIndex); err == nil && count == 2 && turn >= next {
				next = turn + 1
			}
		}
	}
	return next
}

func recordTurnBoundary(
	ctx context.Context,
	input LoopInput,
	messages []message.Message,
	usage provider.Usage,
	steps []Step,
	toolCallsUsed int,
) error {
	if err := recordFinalizedStep(ctx, input.StepRecorder, steps); err != nil {
		return err
	}
	session, _ := input.Control.(*turnControlSession)
	if input.CheckpointRecorder != nil {
		step := steps[len(steps)-1]
		checkpoint := TurnCheckpoint{
			Messages:                 message.CloneMessages(messages),
			Usage:                    usage,
			Step:                     step,
			ToolCallsUsed:            toolCallsUsed,
			NextOperationTurn:        input.OperationTurn,
			ExternallyAccountedUsage: dereferenceUsage(input.externallyAccountedUsage),
			AppliedControlIDs:        sessionPendingIDs(session),
			ControlAborted:           session != nil && session.hasAbort(),
		}
		if err := input.CheckpointRecorder.RecordCheckpoint(ctx, checkpoint); err != nil {
			return fmt.Errorf("agent: record checkpoint after step %d: %w", step.Index, err)
		}
	}
	if session != nil {
		return session.acknowledgePending(ctx)
	}
	return nil
}

func dereferenceUsage(usage *provider.Usage) provider.Usage {
	if usage == nil {
		return provider.Usage{}
	}
	return *usage
}

// loopTurnPreamble runs the per-iteration preamble for turns after the first: it
// stops the loop when a budget dimension is exhausted (budgetAbort), then
// prepares the running history for the upcoming request. When stop is true the
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
// the in-loop "will continue" check and the cross-call repair check. A
// positive token ceiling also rejects a successful zero-usage turn in
// RunMessages before it can continue.
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

// maybeCompactHistory returns the history to drive the next turn. A positive
// ContextTokenTarget invokes token-targeted preparation on every request,
// preferring CompactTo and falling back to Compact for source-compatible
// managers. With no target it preserves the legacy behavior: Compact runs only
// when cumulative MaxTokens spend enters its final headroom band. A compaction
// error or incomplete tool turn aborts rather than sending malformed history.
func maybeCompactHistory(ctx context.Context, input LoopInput, current []message.Message, usage provider.Usage) ([]message.Message, error) {
	if input.ContextTokenTarget > 0 {
		if input.CompactTo == nil && input.Compact == nil {
			return current, nil
		}
	} else {
		if input.Compact == nil || input.MaxTokens <= 0 {
			return current, nil
		}
		remaining := input.MaxTokens - int64(usage.TotalTokens)
		if remaining > input.MaxTokens/compactionBudgetHeadroomDivisor {
			return current, nil
		}
	}

	compactionInput, err := cacheSafeCompactionInput(current)
	if err != nil {
		return current, fmt.Errorf("agent: compact history: %w", err)
	}
	var compacted []message.Message
	if input.ContextTokenTarget > 0 && input.CompactTo != nil {
		compacted, err = input.CompactTo(ctx, compactionInput, input.ContextTokenTarget)
	} else {
		compacted, err = input.Compact(ctx, compactionInput)
	}
	if err != nil {
		return current, fmt.Errorf("agent: compact history: %w", err)
	}
	if err := message.ValidateCompleteTurns(compacted); err != nil {
		return current, fmt.Errorf("agent: compact history: %w", err)
	}
	if err := validateCachePrefixPreserved(current, compacted); err != nil {
		return current, fmt.Errorf("agent: compact history: %w", err)
	}
	return compacted, nil
}

func validateCachePrefixPreserved(before, after []message.Message) error {
	boundary, err := message.CachePrefixBoundary(before)
	if err != nil {
		return err
	}
	if boundary == 0 {
		return nil
	}
	if len(after) < boundary {
		return errors.New("compaction removed an explicit cache prefix")
	}
	for index := range boundary {
		if !reflect.DeepEqual(before[index], after[index]) {
			return fmt.Errorf("compaction changed explicit cache prefix at message %d", index)
		}
	}
	return nil
}

// cacheSafeCompactionInput isolates histories carrying an explicit cache prefix
// before calling extension code. Without a deep copy, an in-place compactor can
// mutate both the source and returned aliases and defeat the post-call integrity
// comparison. Histories without a cache boundary retain the allocation-free
// legacy path.
func cacheSafeCompactionInput(messages []message.Message) ([]message.Message, error) {
	boundary, err := message.CachePrefixBoundary(messages)
	if err != nil {
		return nil, err
	}
	if boundary == 0 {
		return messages, nil
	}

	return message.CloneMessages(messages), nil
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
// normalized partial assistant turn. When that turn carries content or usage,
// append it and fold its usage so the partial LoopOutput keeps the produced
// response and its token spend, and count the turn in Iterations; an empty turn
// leaves the trace, usage, and turn count untouched so a failure before any
// content or usage reports nothing extra.
func recordIncompleteTurn(current []message.Message, assistant message.Message, totalUsage, usage provider.Usage, iteration, turnsRun int) ([]message.Message, provider.Usage, int) {
	if assistant.Text == "" && assistant.Thinking == "" && assistant.RedactedThinking == "" && len(assistant.ToolCalls) == 0 && usage == (provider.Usage{}) {
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
			ID:        call.ID,
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
			Model:               input.Model,
			Messages:            cloneMessages(current),
			Output:              candidate,
			Iteration:           iteration,
			MaxIterations:       input.MaxIterations,
			UnlimitedIterations: input.UnlimitedIterations,
			Usage:               usage,
			StopReason:          stopReason,
			Metadata:            cloneStringMap(input.Metadata),
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
			if !input.UnlimitedIterations && iteration >= input.MaxIterations {
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

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// runTurn executes a single model turn: context transform, request assembly,
// provider stream and event collection.
func (e Engine) runTurn(ctx context.Context, current []message.Message, input LoopInput) (message.Message, provider.Usage, provider.StopReason, provider.StreamIdentity, bool, error) {
	transformed, err := e.Hooks.TransformContext(ctx, current)
	if err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, provider.StreamIdentity{}, false, err
	}
	request := provider.Request{
		Model:             input.Model,
		Messages:          transformed,
		Temperature:       input.Temperature,
		TopP:              input.TopP,
		MaxTokens:         input.ModelMaxTokens,
		Metadata:          input.Metadata,
		StopSequences:     input.StopSequences,
		ThinkingBudget:    input.ThinkingBudget,
		ResponseFormat:    input.ResponseFormat,
		PromptCacheKey:    input.PromptCacheKey,
		ServiceTier:       input.ServiceTier,
		ParallelToolCalls: cloneBoolPointer(input.ParallelToolCalls),
		NativeToolHost:    input.NativeToolHost,
		ContextUsage:      input.ContextUsage,
		ExtraBody:         cloneAnyMap(input.ExtraBody),
	}
	if e.Tools != nil {
		request.Tools = e.Tools.Definitions()
	}
	if err := e.Hooks.BeforeModelCall(ctx, &request); err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, provider.StreamIdentity{}, false, err
	}
	if err := provider.ValidateExtraBody(request.ExtraBody); err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, provider.StreamIdentity{}, false, err
	}
	providerStream, err := e.Provider.Stream(ctx, request)
	if err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, provider.StreamIdentity{}, false, err
	}
	assistant, usage, stop, collectErr := e.collect(ctx, providerStream, input.OnEvent, input.Sink)
	identity := provider.StreamIdentity{
		Provider: e.Provider.Metadata(),
		Model:    request.Model,
	}
	if identified, ok := providerStream.(provider.IdentifiedStream); ok {
		identity = identified.Identity()
		if identity.Model == "" {
			identity.Model = request.Model
		}
	}
	return assistant, usage, stop, identity, true, collectErr
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
		operationID := item.OperationID
		if err := e.Hooks.BeforeToolCall(ctx, &item); err != nil {
			return nil, false, err
		}
		item.OperationID = operationID
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

// ResumeToolCalls executes tool calls restored from a durable checkpoint
// through the same preparation, validation, and hook pipeline as a live turn.
// The caller remains responsible for appending the returned results to history.
func (e Engine) ResumeToolCalls(ctx context.Context, calls []message.ToolCall) ([]message.ToolResult, error) {
	prepared, _, err := e.prepareToolCalls(ctx, calls)
	if err != nil {
		return nil, err
	}
	return e.dispatchPreparedTools(ctx, prepared, e.ToolMode)
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
	for index, current := range results {
		item := current
		if index < len(prepared) {
			if item.ToolCallID == "" {
				item.ToolCallID = prepared[index].ID
			}
			if item.Name == "" {
				item.Name = prepared[index].Name
			}
		}
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
	eventBytes := 0
	// A per-event callback (onEvent or the sink) can panic while handling an
	// event. Each event is recorded before it is delivered, so normalize the
	// events collected so far onto the assistant turn and surface the panic as an
	// ErrPanicRecovered error carrying that partial turn, rather than letting it
	// unwind to the loop's outer recover where the completed response would be
	// lost. errors.Is(err, ErrPanicRecovered) still holds end-to-end.
	defer func() {
		if r := recover(); r != nil {
			usage, _, _ = applyNormalized(&assistant, events, false)
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
				partialUsage, partialStop, _ := applyNormalized(&assistant, events, false)
				if partialStop == "" {
					partialStop = provider.StopReasonError
				}
				return assistant, partialUsage, partialStop, ctxErr
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
			partialUsage, partialStop, _ := applyNormalized(&assistant, events, false)
			if partialStop == "" {
				partialStop = provider.StopReasonError
			}
			return assistant, partialUsage, partialStop, recvErr
		}
		if event.Kind == provider.EventDone {
			sawTerminal = true
		}
		if len(events) >= maxProviderTurnEvents {
			partialUsage, partialStop, _ := applyNormalized(&assistant, events, false)
			return assistant, partialUsage, partialStop, fmt.Errorf(
				"%w: more than %d events",
				ErrProviderTurnLimit,
				maxProviderTurnEvents,
			)
		}
		size := providerEventSize(event)
		if size > maxProviderTurnBytes-eventBytes {
			partialUsage, partialStop, _ := applyNormalized(&assistant, events, false)
			return assistant, partialUsage, partialStop, fmt.Errorf(
				"%w: more than %d bytes",
				ErrProviderTurnLimit,
				maxProviderTurnBytes,
			)
		}
		eventBytes += size
		// Record the event before delivering it to the callbacks: a callback that
		// errors or panics must not discard the response already streamed, so both
		// the recover above and the error return below normalize the events held so
		// far rather than starting from an empty turn.
		events = append(events, event)
		if cbErr := e.fanOutEvent(ctx, event, onEvent, sink); cbErr != nil {
			usage, stop, _ = applyNormalized(&assistant, events, false)
			return assistant, usage, stop, cbErr
		}
	}
	usage, stop, err = applyNormalized(&assistant, events, true)
	if err != nil {
		return assistant, provider.Usage{}, provider.StopReasonError, err
	}
	return assistant, usage, stop, nil
}

func providerEventSize(event provider.Event) int {
	size := len(event.Text) + len(event.Thinking) + len(event.Signature) +
		len(event.RedactedThinking) + len(event.ProviderState)
	if event.ToolCall != nil {
		size += len(event.ToolCall.ID) + len(event.ToolCall.Name) + len(event.ToolCall.Arguments)
	}
	if event.ToolCallDelta != nil {
		size += len(event.ToolCallDelta.ID) + len(event.ToolCallDelta.Name) +
			len(event.ToolCallDelta.ArgumentsDelta)
	}
	size += len(event.Response.ID) + len(event.Response.Model)
	for key, value := range event.Response.Headers {
		size += len(key) + len(value)
	}
	if event.Err != nil {
		size += len(event.Err.Error())
	}
	return size
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

// applyNormalized copies text, thinking, signatures, tool calls, and opaque
// provider state from the events collected so far onto the assistant turn,
// and reports the turn's usage and stop reason. It centralizes the
// success path and its failure paths (a callback error or a recovered panic),
// where the partial turn must still carry whatever the model already produced. A
// normalize error (a malformed stream prefix) leaves the assistant untouched and
// reports StopReasonError, so a failure path never masks its original cause with a
// normalize error.
func applyNormalized(assistant *message.Message, events []provider.Event, requireTerminal bool) (provider.Usage, provider.StopReason, error) {
	var (
		normalized provider.NormalizedResponse
		err        error
	)
	if requireTerminal {
		normalized, err = provider.NormalizeEvents(events)
	} else {
		normalized, err = provider.NormalizePartialEvents(events)
	}
	if err != nil {
		return provider.Usage{}, provider.StopReasonError, err
	}
	assistant.Content = message.CloneContent(normalized.Content)
	assistant.ToolCalls = normalized.ToolCalls
	assistant.ProviderState = normalized.ProviderState
	assistant.Response = message.CloneResponseMetadata(normalized.Response)
	assistant.SyncLegacyContent()
	return normalized.Usage, normalized.StopReason, nil
}
