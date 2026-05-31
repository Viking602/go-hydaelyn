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
	// on this goroutine (guardrails, recorders, sinks, the OnEvent callback, the
	// provider stream, and sequential tool drivers) so a misbehaving extension
	// degrades to a typed failure instead of crashing the worker. The accumulated
	// trace is preserved on the returned LoopOutput. Two panic sources never
	// reach this defer and are contained closer to their origin: hook handlers,
	// in hook.Chain, and parallel tool drivers, which run on bus-spawned
	// goroutines this recover cannot observe and are contained by the tool bus.
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
			if _, dimension := budgetRemaining(input, totalUsage, toolCallsUsed, len(steps)); dimension != "" {
				// The current turn never ran, so iteration is the count of
				// model turns actually issued.
				return budgetAbort(current, totalUsage, steps, iteration, toolCallsUsed, dimension)
			}
		}
		assistant, usage, stopReason, turnErr := e.runTurn(ctx, current, input)
		if turnErr != nil {
			return loopErrorOutput(current, totalUsage, steps, iteration, toolCallsUsed), turnErr
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
		if dispErr != nil {
			return loopErrorOutput(current, totalUsage, steps, iteration+1, toolCallsUsed), dispErr
		}
		var appendErr error
		current, appendErr = appendToolResults(ctx, current, results, input.Sink)
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
// registered, so an error here comes from a driver that actually ran (or, for a
// sequential driver, a panic that unwinds inline to the RunMessages recover) —
// which is why the loop charges the batch before calling this.
func (e Engine) dispatchPreparedTools(ctx context.Context, prepared []tool.Call, mode tool.Mode) ([]message.ToolResult, error) {
	results, err := e.Tools.ExecuteBatch(ctx, prepared, mode, nil)
	if err != nil {
		return nil, err
	}
	items := make([]message.ToolResult, 0, len(results))
	for _, current := range results {
		item := current
		if err := e.Hooks.AfterToolCall(ctx, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// appendToolResults appends each tool result to the running history and,
// when a sink is set, emits a FrameToolResult for it. Tool results are a
// loop-level enrichment with no provider.Event equivalent. On a sink Emit
// failure it returns the history accumulated so far — not nil — so the caller's
// partial-trace error path preserves the prompt, the assistant tool call, and
// every tool result already appended (including the one whose streaming just
// failed) rather than discarding the turn. Each result is appended before its
// Emit, so it is real accumulated state; only the side-channel delivery hiccuped.
func appendToolResults(ctx context.Context, current []message.Message, results []message.ToolResult, sink stream.Sink) ([]message.Message, error) {
	for _, result := range results {
		current = append(current, message.NewToolResult(result))
		if sink == nil {
			continue
		}
		toolResult := result
		if err := sink.Emit(ctx, stream.Frame{Kind: stream.FrameToolResult, ToolResult: &toolResult}); err != nil {
			return current, err
		}
	}
	return current, nil
}

func (e Engine) collect(ctx context.Context, providerStream provider.Stream, onEvent func(provider.Event) error, sink stream.Sink) (message.Message, provider.Usage, provider.StopReason, error) {
	defer func() { _ = providerStream.Close() }()
	assistant := message.Message{Role: message.RoleAssistant, Kind: message.KindStandard}
	events := make([]provider.Event, 0, 8)
	sawTerminal := false
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
				return message.Message{}, provider.Usage{}, provider.StopReasonError, ctxErr
			}
		}
		event, err := providerStream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			// A context-aware stream (one that wraps a body read) can unblock Recv
			// with the context error rather than io.EOF. If that lands after the
			// terminal EventDone the response is already complete, so treat it like
			// io.EOF: stop draining and normalize the events already collected
			// instead of discarding the finished turn. Only context cancellation is
			// tolerated here — any other post-terminal error is a genuine fault and
			// still propagates.
			if sawTerminal && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				break
			}
			return message.Message{}, provider.Usage{}, provider.StopReasonError, err
		}
		if event.Kind == provider.EventDone {
			sawTerminal = true
		}
		if onEvent != nil {
			if err := onEvent(event); err != nil {
				return message.Message{}, provider.Usage{}, provider.StopReasonError, err
			}
		}
		if err := e.Hooks.OnEvent(ctx, event); err != nil {
			return message.Message{}, provider.Usage{}, provider.StopReasonError, err
		}
		if sink != nil {
			if frame, ok := stream.FrameFromEvent(event); ok {
				if err := sink.Emit(ctx, frame); err != nil {
					return message.Message{}, provider.Usage{}, provider.StopReasonError, err
				}
			}
		}
		events = append(events, event)
	}
	normalized, err := provider.NormalizeEvents(events)
	if err != nil {
		return message.Message{}, provider.Usage{}, provider.StopReasonError, err
	}
	assistant.Text = normalized.Text
	assistant.Thinking = normalized.Thinking
	assistant.ThinkingSignature = normalized.Signature
	assistant.RedactedThinking = normalized.RedactedThinking
	assistant.ToolCalls = normalized.ToolCalls
	return assistant, normalized.Usage, normalized.StopReason, nil
}
