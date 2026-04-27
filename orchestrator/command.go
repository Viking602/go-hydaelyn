package orchestrator

import (
	"context"
	"errors"
)

func (StartRunCommand) CommandName() string               { return "run.start" }
func (CreateTaskCommand) CommandName() string             { return "task.create" }
func (TransitionRunCommand) CommandName() string          { return "run.transition" }
func (TransitionTaskCommand) CommandName() string         { return "task.transition" }
func (AdvanceRunCommand) CommandName() string             { return "run.advance" }
func (DispatchTaskCommand) CommandName() string           { return "task.dispatch" }
func (AcquireTaskExecutionCommand) CommandName() string   { return "task_execution.acquire" }
func (HeartbeatTaskExecutionCommand) CommandName() string { return "task_execution.heartbeat" }
func (ReleaseTaskExecutionCommand) CommandName() string   { return "task_execution.release" }
func (AckEnvelopeCommand) CommandName() string            { return "mailbox.ack" }
func (DeadLetterCommand) CommandName() string             { return "mailbox.dead_letter" }
func (SubmitTypedReportCommand) CommandName() string      { return "report.submit_typed" }
func (SubmitUserInputCommand) CommandName() string        { return "user_input.submit" }
func (HandoffCommand) CommandName() string                { return "handoff.request" }
func (SubmitResponseOutputCommand) CommandName() string   { return "response.submit_output" }
func (PublishResponseCommand) CommandName() string        { return "response.publish" }

func (r *Runtime) ExecuteCommand(ctx context.Context, command RuntimeCommand) (any, error) {
	uow, err := r.Begin(ctx)
	if err != nil {
		return nil, err
	}
	cursor := r.captureStoreCursor()
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	result, handled, err := r.executeCommand(ctx, command)
	if err != nil {
		return nil, err
	}
	if !handled {
		return nil, ErrInvalidCommand
	}
	if err := r.persistCommandDelta(ctx, uow, cursor); err != nil {
		return nil, err
	}
	if err := uow.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}

type runtimeStoreCursor struct {
	eventCounts      map[string]int
	blackboardCounts map[string]int
	traceCounts      map[string]int
	envelopeIDs      map[string]struct{}
	messageIDs       map[string]struct{}
}

type runtimeStoreDelta struct {
	runs       []Run
	tasks      []Task
	events     []Event
	blackboard []BlackboardItem
	envelopes  []TaskEnvelope
	messages   []UserMessage
	spans      []TraceSpan
}

func (r *Runtime) captureStoreCursor() runtimeStoreCursor {
	r.mu.Lock()
	defer r.mu.Unlock()
	cursor := runtimeStoreCursor{
		eventCounts:      map[string]int{},
		blackboardCounts: map[string]int{},
		traceCounts:      map[string]int{},
		envelopeIDs:      map[string]struct{}{},
		messageIDs:       map[string]struct{}{},
	}
	for runID, events := range r.events {
		cursor.eventCounts[runID] = len(events)
	}
	for runID, items := range r.blackboard {
		cursor.blackboardCounts[runID] = len(items)
	}
	for runID, spans := range r.traceSpans {
		cursor.traceCounts[runID] = len(spans)
	}
	for id := range r.envelopes {
		cursor.envelopeIDs[id] = struct{}{}
	}
	for id := range r.messages {
		cursor.messageIDs[id] = struct{}{}
	}
	return cursor
}

func (r *Runtime) persistCommandDelta(ctx context.Context, uow UnitOfWork, cursor runtimeStoreCursor) error {
	if _, ok := uow.(runtimeUnitOfWork); ok {
		return nil
	}
	delta := r.collectStoreDelta(cursor)
	for _, run := range delta.runs {
		if err := uow.Runs().SaveRun(ctx, run); err != nil {
			return err
		}
	}
	for _, task := range delta.tasks {
		if err := uow.Tasks().SaveTask(ctx, task); err != nil {
			return err
		}
	}
	for _, event := range delta.events {
		if err := uow.Events().AppendEvent(ctx, event); err != nil {
			return err
		}
	}
	for _, item := range delta.blackboard {
		if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
			return err
		}
	}
	for _, env := range delta.envelopes {
		if err := persistEnvelope(ctx, uow.MailboxOutbox(), cursor, env); err != nil {
			return err
		}
	}
	for _, message := range delta.messages {
		if err := persistMessage(ctx, uow.UserMessages(), cursor, message); err != nil {
			return err
		}
	}
	for _, span := range delta.spans {
		if err := uow.Trace().SaveTraceSpan(ctx, span); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) collectStoreDelta(cursor runtimeStoreCursor) runtimeStoreDelta {
	r.mu.Lock()
	defer r.mu.Unlock()
	delta := runtimeStoreDelta{}
	for _, run := range r.runs {
		delta.runs = append(delta.runs, run)
	}
	for _, tasks := range r.tasks {
		for _, task := range tasks {
			delta.tasks = append(delta.tasks, task)
		}
	}
	for runID, events := range r.events {
		start := cursor.eventCounts[runID]
		if start < len(events) {
			delta.events = append(delta.events, events[start:]...)
		}
	}
	for runID, items := range r.blackboard {
		start := cursor.blackboardCounts[runID]
		if start < len(items) {
			delta.blackboard = append(delta.blackboard, items[start:]...)
		}
	}
	for _, env := range r.envelopes {
		delta.envelopes = append(delta.envelopes, env)
	}
	for _, message := range r.messages {
		delta.messages = append(delta.messages, message)
	}
	for runID, spans := range r.traceSpans {
		start := cursor.traceCounts[runID]
		if start < len(spans) {
			delta.spans = append(delta.spans, spans[start:]...)
		}
	}
	return delta
}

func persistEnvelope(ctx context.Context, store MailboxOutboxStore, cursor runtimeStoreCursor, env TaskEnvelope) error {
	if _, existed := cursor.envelopeIDs[env.ID]; existed {
		if err := store.UpdateEnvelope(ctx, env); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		} else if err == nil {
			return nil
		}
	}
	return store.QueueEnvelope(ctx, env)
}

func persistMessage(ctx context.Context, store UserMessageStore, cursor runtimeStoreCursor, message UserMessage) error {
	if _, existed := cursor.messageIDs[message.ID]; existed {
		if err := store.UpdateMessage(ctx, message); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		} else if err == nil {
			return nil
		}
	}
	return store.QueueMessage(ctx, message)
}

func (r *Runtime) executeCommand(ctx context.Context, command RuntimeCommand) (any, bool, error) {
	for _, executor := range []func(context.Context, RuntimeCommand) (any, bool, error){
		r.executeRunTaskCommand,
		r.executeMailboxCommand,
		r.executeReportResponseCommand,
		r.executeApprovalActionCommand,
		r.executeTraceCommand,
	} {
		result, handled, err := executor(ctx, command)
		if handled || err != nil {
			return result, handled, err
		}
	}
	return nil, false, nil
}

func (r *Runtime) executeRunTaskCommand(ctx context.Context, command RuntimeCommand) (any, bool, error) {
	switch cmd := command.(type) {
	case StartRunCommand:
		run, task, err := r.StartRun(ctx, cmd)
		return []any{run, task}, true, err
	case CreateTaskCommand:
		task, err := r.CreateTask(ctx, cmd)
		return task, true, err
	case TransitionRunCommand:
		return nil, true, r.TransitionRun(ctx, cmd)
	case TransitionTaskCommand:
		return nil, true, r.TransitionTask(ctx, cmd)
	case AdvanceRunCommand:
		run, err := r.AdvanceRun(ctx, cmd)
		return run, true, err
	default:
		return nil, false, nil
	}
}

func (r *Runtime) executeMailboxCommand(ctx context.Context, command RuntimeCommand) (any, bool, error) {
	switch cmd := command.(type) {
	case DispatchTaskCommand:
		env, err := r.DispatchTask(ctx, cmd)
		return env, true, err
	case AcquireTaskExecutionCommand:
		lease, acquired, err := r.AcquireTaskExecution(ctx, cmd)
		return struct {
			Lease    TaskExecutionLease
			Acquired bool
		}{Lease: lease, Acquired: acquired}, true, err
	case HeartbeatTaskExecutionCommand:
		return nil, true, r.HeartbeatTaskExecution(ctx, cmd)
	case ReleaseTaskExecutionCommand:
		return nil, true, r.ReleaseTaskExecution(ctx, cmd)
	case AckEnvelopeCommand:
		return nil, true, r.AckEnvelope(ctx, cmd)
	case DeadLetterCommand:
		return nil, true, r.DeadLetter(ctx, cmd)
	default:
		return nil, false, nil
	}
}

func (r *Runtime) executeReportResponseCommand(ctx context.Context, command RuntimeCommand) (any, bool, error) {
	switch cmd := command.(type) {
	case SubmitTypedReportCommand:
		return nil, true, r.SubmitTypedReport(ctx, cmd)
	case SubmitUserInputCommand:
		return nil, true, r.SubmitUserInput(ctx, cmd)
	case HandoffCommand:
		return nil, true, r.RequestHandoff(ctx, cmd)
	case SubmitResponseOutputCommand:
		return nil, true, r.SubmitResponseOutput(ctx, cmd)
	case PublishResponseCommand:
		return nil, true, r.PublishResponse(ctx, cmd)
	default:
		return nil, false, nil
	}
}

func (r *Runtime) executeApprovalActionCommand(ctx context.Context, command RuntimeCommand) (any, bool, error) {
	switch cmd := command.(type) {
	case RequestApprovalCommand:
		approval, token, err := r.RequestApproval(ctx, cmd)
		return []any{approval, token}, true, err
	case DecideApprovalCommand:
		return nil, true, r.DecideApproval(ctx, cmd)
	case RecoverResumeTokenCommand:
		token, err := r.RecoverResumeToken(ctx, cmd)
		return token, true, err
	case StartActionAttemptCommand:
		attempt, err := r.StartActionAttempt(ctx, cmd)
		return attempt, true, err
	case CompleteActionAttemptCommand:
		attempt, err := r.CompleteActionAttempt(ctx, cmd)
		return attempt, true, err
	default:
		return nil, false, nil
	}
}

func (r *Runtime) executeTraceCommand(ctx context.Context, command RuntimeCommand) (any, bool, error) {
	switch cmd := command.(type) {
	case StartTraceSpanCommand:
		span, err := r.StartTraceSpan(ctx, cmd)
		return span, true, err
	case EndTraceSpanCommand:
		return nil, true, r.EndTraceSpan(ctx, cmd)
	default:
		return nil, false, nil
	}
}
