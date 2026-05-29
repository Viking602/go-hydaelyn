package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/stream"
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

	input := LoopInput{
		Model:         e.Model,
		Messages:      messages,
		ToolMode:      e.ToolMode,
		MaxIterations: e.LoopPolicy.MaxIterations,
		Sink:          sink,
	}

	output, runErr := e.RunMessages(runCtx, input)
	if runErr != nil {
		kind := FailureKindEngineError
		if budgetDriven && errors.Is(runErr, context.DeadlineExceeded) && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			kind = FailureKindBudgetExhausted
		}
		return Result{
			Messages:   output.Messages,
			Usage:      output.Usage,
			StopReason: output.StopReason,
			Thinking:   output.Thinking,
			Steps:      output.Steps,
			Failure: (&AgentFailure{
				Kind:   kind,
				Reason: runErr.Error(),
			}).WithCause(runErr),
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
	for repairCount := 1; repairCount <= policy.MaxRepairAttempts; repairCount++ {
		repairInput := input
		repairInput.Messages = append(cloneMessages(output.Messages), repairInstructionMessage(policy.Schema, validationErr))
		repairOutput, repairErr := e.RunMessages(ctx, repairInput)
		if repairErr != nil {
			kind := FailureKindEngineError
			if budgetDriven && errors.Is(repairErr, context.DeadlineExceeded) && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				kind = FailureKindBudgetExhausted
			}
			return Result{
				Messages:    repairOutput.Messages,
				Usage:       totalUsage.Add(repairOutput.Usage),
				StopReason:  repairOutput.StopReason,
				Thinking:    repairOutput.Thinking,
				Steps:       appendReindexedSteps(accumulatedSteps, repairOutput.Steps),
				RepairCount: repairCount,
				Failure: (&AgentFailure{
					Kind:   kind,
					Reason: repairErr.Error(),
				}).WithCause(repairErr),
			}
		}
		totalUsage = totalUsage.Add(repairOutput.Usage)
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
	if task.Budget != nil && task.Budget.MaxWallClock > 0 {
		return task.Budget.MaxWallClock
	}
	if e.LoopPolicy.Budget != nil && e.LoopPolicy.Budget.MaxWallClock > 0 {
		return e.LoopPolicy.Budget.MaxWallClock
	}
	if e.LoopPolicy.MaxWallClock > 0 {
		return e.LoopPolicy.MaxWallClock
	}
	return 0
}

func (e Engine) buildContext(ctx context.Context, task api.Task) ([]message.Message, error) {
	if e.ContextBuilder != nil {
		return e.ContextBuilder.Build(ctx, task)
	}
	return defaultContextBuilder{}.Build(ctx, task)
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
