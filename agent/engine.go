package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/skill"
	"github.com/Viking602/go-hydaelyn/stream"
	"github.com/Viking602/go-hydaelyn/tool"
)

// Run drives the agent loop against one api.Task under the supplied
// OutputPolicy. The typed Failure on the returned Result is the only
// surface a failure crosses to the multi-agent layer (boundaries
// Principle 6); Run intentionally does not return a bare error.
//
// When OutputPolicy.Validate is set with a Schema, Run validates the
// terminal assistant JSON against that schema and, when requested,
// re-prompts the model with validation feedback up to
// MaxRepairAttempts.
func (e Engine) Run(ctx context.Context, task api.Task, policy OutputPolicy) Result {
	return e.run(ctx, task, policy, nil)
}

// RunStream is Run with a live stream.Sink attached: the Sink receives a
// Frame for every provider event and tool result as the loop runs, while
// the returned Result is byte-for-byte identical to Run's. The stream is a
// transient side-channel (final-state-only durability) — it never changes
// what the runner persists or replays. Pass nil to fall back to Run.
func (e Engine) RunStream(ctx context.Context, task api.Task, policy OutputPolicy, sink stream.Sink) Result {
	return e.run(ctx, task, policy, sink)
}

func (e Engine) run(ctx context.Context, task api.Task, policy OutputPolicy, sink stream.Sink) Result {
	runCtx, cancelRun, budgetDriven := e.runContext(ctx, task)
	defer cancelRun()

	messages, err := e.buildContext(runCtx, task)
	if err != nil {
		kind := FailureKindContextBuildFailed
		if budgetDriven && errors.Is(err, context.DeadlineExceeded) && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			kind = FailureKindBudgetExhausted
		}
		return Result{
			Failure: (&AgentFailure{
				Kind:   kind,
				Reason: err.Error(),
			}).WithCause(err),
		}
	}

	maxTokens, maxToolCalls, maxSteps := e.budgetLimits(task)
	input := LoopInput{
		Model:            e.Model,
		Messages:         messages,
		ToolMode:         e.ToolMode,
		MaxIterations:    e.LoopPolicy.MaxIterations,
		MaxTokens:        maxTokens,
		MaxToolCalls:     maxToolCalls,
		MaxSteps:         maxSteps,
		StopSequences:    e.StopSequences,
		ThinkingBudget:   e.ThinkingBudget,
		ExtraBody:        e.ExtraBody,
		OutputGuardrails: e.OutputGuardrails,
		OutputRecorder:   e.OutputRecorder,
		Sink:             sink,
		StepPolicy:       e.StepPolicy,
		Compact:          e.compactor(),
	}

	output, runErr := e.RunMessages(runCtx, input)
	if runErr != nil {
		return Result{
			Messages:   output.Messages,
			Usage:      output.Usage,
			StopReason: output.StopReason,
			Thinking:   output.Thinking,
			Steps:      output.Steps,
			Failure:    loopErrorFailure(runCtx, runErr, budgetDriven),
		}
	}

	result := resultFromLoopOutput(output, 0)
	if !policy.Validate || len(policy.Schema) == 0 {
		return result
	}
	return e.validateAndRepairStructuredOutput(runCtx, input, output, result, policy, budgetDriven)
}

func (e Engine) validateAndRepairStructuredOutput(ctx context.Context, input LoopInput, output LoopOutput, result Result, policy OutputPolicy, budgetDriven bool) Result {
	schema, schemaErr := parseOutputPolicySchema(policy.Schema)
	if schemaErr != nil {
		result.Valid = false
		result.Structured = nil
		result.Failure = schemaInvalidFailure(schemaErr)
		return result
	}
	validationErr := validateResultStructuredOutput(&result, schema)
	if validationErr == nil {
		return result
	}
	if !policy.Repair || policy.MaxRepairAttempts <= 0 {
		result.Failure = schemaInvalidFailure(validationErr)
		return result
	}

	totalUsage := output.Usage
	accumulatedSteps := output.Steps
	toolCallsUsed := output.ToolCallsUsed
	for repairCount := 1; repairCount <= policy.MaxRepairAttempts; repairCount++ {
		// The loop budget spans the whole run, repairs included. Charge what
		// the initial run and prior repairs already spent before issuing the
		// next model call; if nothing is left, fail before calling out rather
		// than overspending the budget on a repair turn.
		repairInput, dimension := budgetRemaining(input, totalUsage, toolCallsUsed, len(accumulatedSteps))
		if dimension != "" {
			result.Usage = totalUsage
			result.Steps = accumulatedSteps
			result.RepairCount = repairCount - 1
			result.Failure = &AgentFailure{
				Kind:      FailureKindBudgetExhausted,
				Reason:    fmt.Sprintf("loop budget exhausted before repair attempt %d: %s", repairCount, dimension),
				Retryable: false,
			}
			return result
		}
		repairInput.Messages = append(cloneMessages(output.Messages), repairInstructionMessage(policy.Schema, validationErr))
		repairOutput, repairErr := e.RunMessages(ctx, repairInput)
		if repairErr != nil {
			return Result{
				Messages:    repairOutput.Messages,
				Usage:       totalUsage.Add(repairOutput.Usage),
				StopReason:  repairOutput.StopReason,
				Thinking:    repairOutput.Thinking,
				Steps:       appendReindexedSteps(accumulatedSteps, repairOutput.Steps),
				RepairCount: repairCount,
				Failure:     loopErrorFailure(ctx, repairErr, budgetDriven),
			}
		}
		totalUsage = totalUsage.Add(repairOutput.Usage)
		toolCallsUsed += repairOutput.ToolCallsUsed
		accumulatedSteps = appendReindexedSteps(accumulatedSteps, repairOutput.Steps)
		repairOutput.Usage = totalUsage
		repairOutput.Steps = accumulatedSteps
		output = repairOutput
		result = resultFromLoopOutput(output, repairCount)
		validationErr = validateResultStructuredOutput(&result, schema)
		if validationErr == nil {
			return result
		}
	}

	result.Failure = &AgentFailure{
		Kind:      FailureKindRepairFailed,
		Reason:    fmt.Sprintf("structured output repair failed after %d attempt(s): %s", policy.MaxRepairAttempts, validationErr),
		Retryable: false,
	}

	return result
}

func resultFromLoopOutput(output LoopOutput, repairCount int) Result {
	return Result{
		Text:        finalAssistantTextFromMessages(output.Messages),
		Thinking:    output.Thinking,
		Usage:       output.Usage,
		StopReason:  output.StopReason,
		Messages:    output.Messages,
		Steps:       output.Steps,
		Valid:       true,
		RepairCount: repairCount,
	}
}

// appendReindexedSteps appends src onto dst, rewriting each appended Step's
// Index so the combined slice stays globally continuous. RunMessages numbers
// steps from zero on every call, so a repair turn's steps would collide with
// the original run's indices without reindexing here.
func appendReindexedSteps(dst, src []Step) []Step {
	for _, step := range src {
		step.Index = len(dst)
		dst = append(dst, step)
	}
	return dst
}

func validateResultStructuredOutput(result *Result, schema outputPolicySchema) error {
	structured, err := validateStructuredOutputAgainstSchema(schema, result.Text)
	if err != nil {
		result.Valid = false
		result.Structured = nil
		return err
	}
	result.Valid = true
	result.Structured = structured
	result.Failure = nil
	return nil
}

func schemaInvalidFailure(err error) *AgentFailure {
	return &AgentFailure{
		Kind:      FailureKindSchemaInvalid,
		Reason:    err.Error(),
		Retryable: false,
	}
}

func repairInstructionMessage(schema []byte, validationErr error) message.Message {
	return message.NewText(message.RoleUser, fmt.Sprintf(
		"Repair the previous JSON output so it satisfies the required JSON Schema. Return only the corrected JSON, with no prose.\n\nValidation error: %s\n\nJSON Schema:\n%s",
		validationErr,
		string(schema),
	))
}

func (e Engine) runContext(ctx context.Context, task api.Task) (context.Context, context.CancelFunc, bool) {
	maxWallClock := e.maxWallClock(task)
	if maxWallClock <= 0 {
		return ctx, func() {}, false
	}
	deadline := time.Now().Add(maxWallClock)
	if parentDeadline, ok := ctx.Deadline(); ok && !deadline.Before(parentDeadline) {
		return ctx, func() {}, false
	}
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	return runCtx, cancel, true
}

func (e Engine) maxWallClock(task api.Task) time.Duration {
	// A present task.Budget is authoritative for the wall-clock dimension too
	// (zero means unbounded), mirroring budgetLimits and the LoopPolicy contract
	// that a per-Task Budget overrides the engine default when present.
	if task.Budget != nil {
		return task.Budget.MaxWallClock
	}
	// On the engine side a structured LoopPolicy.Budget likewise wins over the
	// bare legacy LoopPolicy.MaxWallClock: a present Budget is authoritative, so
	// its zero wall-clock means unbounded rather than inheriting the legacy
	// deadline. The legacy field applies only when no Budget is set at all.
	if e.LoopPolicy.Budget != nil {
		return e.LoopPolicy.Budget.MaxWallClock
	}
	return e.LoopPolicy.MaxWallClock
}

// budgetLimits resolves the token, tool-call, and step ceilings the loop
// enforces. A present task.Budget is authoritative: its fields are used as-is
// and a zero dimension means unbounded (the api.TaskBudget contract), so it is
// never backfilled from the engine default — otherwise a task that caps only
// one dimension would silently inherit the engine's caps on the others. The
// engine's LoopPolicy.Budget supplies all three ceilings only when the task
// carries no Budget of its own. This mirrors maxWallClock and the LoopPolicy
// contract that a per-Task Budget overrides the engine default when present.
func (e Engine) budgetLimits(task api.Task) (maxTokens int64, maxToolCalls, maxSteps int) {
	if task.Budget != nil {
		return task.Budget.MaxTokens, task.Budget.MaxToolCalls, task.Budget.MaxSteps
	}
	if e.LoopPolicy.Budget != nil {
		return e.LoopPolicy.Budget.MaxTokens, e.LoopPolicy.Budget.MaxToolCalls, e.LoopPolicy.Budget.MaxSteps
	}
	return 0, 0, 0
}

// loopErrorFailure classifies a RunMessages error into the typed AgentFailure
// that crosses the agent → multiagent boundary. An explicit loop-budget
// exhaustion, or a wall-clock deadline on a budget-driven run, surfaces as a
// budget failure. A tool the bus cannot serve — a name the model invoked that
// is not registered (tool.ErrToolNotFound) or a missing bus altogether
// (ErrToolBusMissing) — surfaces as FailureKindToolUnavailable, marked
// retryable and escalatable so a scheduler applies the documented "retry with
// backoff, then escalate" path (spec 03 §dispatch policy) instead of treating
// an unavailable tool as an opaque engine error. An output guardrail that
// refuses the terminal output — a tripwire block or a retry the loop could not
// satisfy within MaxIterations — surfaces as FailureKindUnsafeAction, marked
// escalatable but not retryable, matching the spec's "request human approval;
// do not retry automatically" semantics: the guardrail already withheld the
// output, so an automated re-run is not the right next step. Anything else is a
// generic engine error. The Reason and cause mirror the source error so
// errors.Is / errors.As still walk the chain across the boundary.
func loopErrorFailure(ctx context.Context, err error, budgetDriven bool) *AgentFailure {
	failure := &AgentFailure{Reason: err.Error()}
	var tripwire *OutputGuardrailTripwireTriggeredError
	var retryLimit *OutputGuardrailRetryLimitExceededError
	switch {
	case errors.Is(err, ErrBudgetExhausted):
		failure.Kind = FailureKindBudgetExhausted
	case budgetDriven && errors.Is(err, context.DeadlineExceeded) && errors.Is(ctx.Err(), context.DeadlineExceeded):
		failure.Kind = FailureKindBudgetExhausted
	case errors.Is(err, ErrToolBusMissing) || errors.Is(err, tool.ErrToolNotFound):
		failure.Kind = FailureKindToolUnavailable
		failure.Retryable = true
		failure.Escalatable = true
	case errors.As(err, &tripwire), errors.As(err, &retryLimit):
		failure.Kind = FailureKindUnsafeAction
		failure.Escalatable = true
	case errors.Is(err, ErrStepAborted):
		// A StepPolicy that stops the loop with a fail decision (or whose Next
		// errors) is a deliberate control-flow choice, not a transient fault, so
		// it is neither retryable nor escalatable: re-running would meet the same
		// predicate. A Finish/Handoff override never reaches here — it ends the
		// loop with no error.
		failure.Kind = FailureKindStepAborted
	default:
		failure.Kind = FailureKindEngineError
	}
	return failure.WithCause(err)
}

func (e Engine) buildContext(ctx context.Context, task api.Task) ([]message.Message, error) {
	var (
		messages []message.Message
		err      error
	)
	if e.ContextBuilder != nil {
		messages, err = e.ContextBuilder.Build(ctx, task)
	} else {
		messages, err = defaultContextBuilder{}.Build(ctx, task)
	}
	if err != nil {
		return nil, err
	}
	return injectSkillMessages(messages, e.Skills), nil
}

func injectSkillMessages(messages []message.Message, skills []skill.Skill) []message.Message {
	if len(skills) == 0 {
		return messages
	}
	section := skill.RenderSystemSection(skills)
	if section == "" {
		return messages
	}
	insertAt := 0
	for insertAt < len(messages) && messages[insertAt].Role == message.RoleSystem {
		insertAt++
	}
	skillMessage := message.NewText(message.RoleSystem, section)
	out := make([]message.Message, 0, len(messages)+1)
	out = append(out, messages[:insertAt]...)
	out = append(out, skillMessage)
	out = append(out, messages[insertAt:]...)
	return out
}

// compactor returns the ContextManager.Compact bound as the loop's compaction
// hook, or nil when no ContextManager is configured (the loop then never
// compacts). The default context managers pass the history through unchanged, so
// wiring this is a no-op until a caller supplies a ContextManager whose Compact
// actually reshapes the history.
func (e Engine) compactor() func(context.Context, []message.Message) ([]message.Message, error) {
	if e.ContextBuilder == nil {
		return nil
	}
	return e.ContextBuilder.Compact
}

type defaultContextBuilder struct{}

func (defaultContextBuilder) Build(_ context.Context, task api.Task) ([]message.Message, error) {
	goal := strings.TrimSpace(task.Goal)
	if goal == "" {
		goal = "Complete the assigned task and return a concise result."
	}
	return []message.Message{
		message.NewText(message.RoleSystem, "You are a Hydaelyn agent."),
		message.NewText(message.RoleUser, goal),
	}, nil
}

func (defaultContextBuilder) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
}

func finalAssistantTextFromMessages(messages []message.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role == message.RoleAssistant && strings.TrimSpace(m.Text) != "" {
			return m.Text
		}
	}
	return ""
}
