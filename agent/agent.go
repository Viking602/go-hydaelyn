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
	// side-channel: the durable LoopOutput is unaffected by it, so
	// streaming never changes what the runner persists or replays.
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
func (e Engine) RunMessages(ctx context.Context, input LoopInput) (LoopOutput, error) {
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
	for iteration := 0; iteration < input.MaxIterations; iteration++ {
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
		assistant, usage, stopReason, err := e.runTurn(ctx, current, input)
		if err != nil {
			return LoopOutput{}, err
		}
		totalUsage = totalUsage.Add(usage)
		modelCall := &ModelCall{
			Model:        input.Model,
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
			StopReason:   stopReason,
		}
		if len(assistant.ToolCalls) == 0 {
			finalOutput, retryMessages, retryPolicy, err := e.applyOutputGuardrails(ctx, input, current, assistant, iteration+1, totalUsage, stopReason)
			if err != nil {
				return LoopOutput{}, err
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
		if assistant.Text != "" || len(assistant.ToolCalls) > 0 {
			current = append(current, assistant)
		}
		if e.Tools == nil {
			return LoopOutput{}, ErrToolBusMissing
		}
		// Gate the batch against every budget dimension BEFORE dispatching it.
		// The model turn just ran, so its token spend is now folded into
		// totalUsage: a turn that alone exhausts MaxTokens, or a batch that
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
		results, terminal, err := e.executeTools(ctx, assistant.ToolCalls, input.ToolMode)
		if err != nil {
			return LoopOutput{}, err
		}
		current, err = appendToolResults(ctx, current, results, input.Sink)
		if err != nil {
			return LoopOutput{}, err
		}
		toolCallsUsed += len(assistant.ToolCalls)
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

func (e Engine) executeTools(ctx context.Context, calls []message.ToolCall, mode tool.Mode) ([]message.ToolResult, bool, error) {
	prepared := make([]tool.Call, 0, len(calls))
	terminal := false
	for _, call := range calls {
		item := call
		if err := e.Hooks.BeforeToolCall(ctx, &item); err != nil {
			return nil, false, err
		}
		if e.Tools != nil && e.Tools.IsTerminal(item.Name) {
			terminal = true
		}
		prepared = append(prepared, item)
	}
	results, err := e.Tools.ExecuteBatch(ctx, prepared, mode, nil)
	if err != nil {
		return nil, false, err
	}
	items := make([]message.ToolResult, 0, len(results))
	for _, current := range results {
		item := current
		if err := e.Hooks.AfterToolCall(ctx, &item); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	return items, terminal, nil
}

// appendToolResults appends each tool result to the running history and,
// when a sink is set, emits a FrameToolResult for it. Tool results are a
// loop-level enrichment with no provider.Event equivalent.
func appendToolResults(ctx context.Context, current []message.Message, results []message.ToolResult, sink stream.Sink) ([]message.Message, error) {
	for _, result := range results {
		current = append(current, message.NewToolResult(result))
		if sink == nil {
			continue
		}
		toolResult := result
		if err := sink.Emit(ctx, stream.Frame{Kind: stream.FrameToolResult, ToolResult: &toolResult}); err != nil {
			return nil, err
		}
	}
	return current, nil
}

func (e Engine) collect(ctx context.Context, providerStream provider.Stream, onEvent func(provider.Event) error, sink stream.Sink) (message.Message, provider.Usage, provider.StopReason, error) {
	defer func() { _ = providerStream.Close() }()
	assistant := message.Message{Role: message.RoleAssistant, Kind: message.KindStandard}
	events := make([]provider.Event, 0, 8)
	for {
		event, err := providerStream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return message.Message{}, provider.Usage{}, provider.StopReasonError, err
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
	assistant.ToolCalls = normalized.ToolCalls
	return assistant, normalized.Usage, normalized.StopReason, nil
}
