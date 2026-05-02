package userinput

import (
	"context"
	"errors"
	"slices"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	corestate "github.com/Viking602/go-hydaelyn/internal/core/state"
	"github.com/Viking602/go-hydaelyn/internal/eventpayload"
)

type IDGenerator func(string) string

type Authorizer func(context.Context, ports.UnitOfWork, model.PolicyRequest) (model.PolicyDecision, error)

type TraceRecorder func(context.Context, ports.UnitOfWork, string, string, string, string) error

type HandlerOptions struct {
	NewID       IDGenerator
	Authorize   Authorizer
	RecordTrace TraceRecorder
}

type SubmitResult struct {
	Item           model.BlackboardItem
	Run            model.Run
	PreviousRun    model.Run
	Task           model.Task
	Envelope       model.TaskEnvelope
	Input          string
	RunTransition  bool
	Redispatched   bool
	TaskTransition bool
}

// NotifyBlackboard implements core.BlackboardNotifier so the runtime can
// fan out the user-input item to subscribers at commit time.
func (r SubmitResult) NotifyBlackboard() []model.BlackboardItem {
	return []model.BlackboardItem{r.Item}
}

func RegisterHandlers(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[SubmitUserInputCommand](bus, handler{options: options})
}

type handler struct{ options HandlerOptions }

func (handler) Name() string { return SubmitUserInputCommand{}.CommandName() }

func (h handler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd SubmitUserInputCommand) (any, error) {
	run, err := uow.Runs().LoadRun(ctx, cmd.RunID)
	if err != nil {
		return nil, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return nil, model.ErrTerminalState
	}
	task, shouldRedispatch, err := loadTask(ctx, uow, cmd)
	if err != nil {
		return nil, err
	}
	if shouldRedispatch && h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationDispatch, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: model.SourceIdentity{Type: model.SourceSystem, ID: "user_input"}}); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	item, err := h.writeItem(ctx, uow, cmd, now)
	if err != nil {
		return nil, err
	}
	result := SubmitResult{Item: item, PreviousRun: run, Input: cmd.Input}
	nextRun, runTransition, err := resumeRun(ctx, uow, run)
	if err != nil {
		return nil, err
	}
	result.Run = nextRun
	result.RunTransition = runTransition
	if shouldRedispatch {
		nextTask, env, taskTransition, err := h.redispatchTask(ctx, uow, task)
		if err != nil {
			return nil, err
		}
		result.Task = nextTask
		result.Envelope = env
		result.Redispatched = true
		result.TaskTransition = taskTransition
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventUserInputSubmitted, Payload: map[string]any{"input": cmd.Input}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return result, nil
}

func loadTask(ctx context.Context, uow ports.UnitOfWork, cmd SubmitUserInputCommand) (model.Task, bool, error) {
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.Task{}, false, nil
		}
		return model.Task{}, false, err
	}
	shouldRedispatch := task.Status == model.TaskStatusBlocked || task.Status == model.TaskStatusWaitingUserInput
	return task, shouldRedispatch, nil
}

func (h handler) writeItem(ctx context.Context, uow ports.UnitOfWork, cmd SubmitUserInputCommand, now time.Time) (model.BlackboardItem, error) {
	item := model.BlackboardItem{ID: h.options.NewID("bb"), RunID: cmd.RunID, TaskID: cmd.TaskID, Source: model.SourceIdentity{Type: model.SourceSystem, ID: "user"}, Visibility: model.BlackboardVisibilityAgentVisible, Key: "user_input", Payload: cmd.Input, CreatedAt: now}
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return model.BlackboardItem{}, err
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, cmd.RunID, cmd.TaskID, "blackboard.write", "blackboard"); err != nil {
			return model.BlackboardItem{}, err
		}
	}
	if err := appendBlackboardWrittenEvent(ctx, uow, item); err != nil {
		return model.BlackboardItem{}, err
	}
	return item, nil
}

func resumeRun(ctx context.Context, uow ports.UnitOfWork, run model.Run) (model.Run, bool, error) {
	nextRun, err := corestate.TransitionRun(run, model.RunStatusRunning)
	if err != nil {
		return model.Run{}, false, err
	}
	if nextRun.Status == run.Status && nextRun.UpdatedAt.Equal(run.UpdatedAt) {
		return nextRun, false, nil
	}
	if err := uow.Runs().SaveRun(ctx, nextRun); err != nil {
		return model.Run{}, false, err
	}
	if run.Status == nextRun.Status {
		return nextRun, false, nil
	}
	err = uow.Events().AppendEvent(ctx, model.Event{RunID: nextRun.ID, TaskID: nextRun.RootTaskID, Type: model.EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(nextRun.Status), "run": eventpayload.Run(nextRun)}, RecordedAt: time.Now().UTC()})
	return nextRun, true, err
}

func (h handler) redispatchTask(ctx context.Context, uow ports.UnitOfWork, task model.Task) (model.Task, model.TaskEnvelope, bool, error) {
	nextTask, err := corestate.TransitionTask(task, model.TaskStatusDispatched, true)
	if err != nil {
		return model.Task{}, model.TaskEnvelope{}, false, err
	}
	nextTask.Error = ""
	if err := uow.Tasks().SaveTask(ctx, nextTask); err != nil {
		return model.Task{}, model.TaskEnvelope{}, false, err
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, nextTask.RunID, nextTask.ID, "mailbox.dispatch", "mailbox"); err != nil {
			return model.Task{}, model.TaskEnvelope{}, false, err
		}
	}
	env := model.TaskEnvelope{ID: h.options.NewID("env"), RunID: nextTask.RunID, TaskID: nextTask.ID, TargetAgentID: nextTask.OwnerAgentID, TargetComponent: nextTask.OwnerComponent, Type: "TaskEnvelope", Status: "pending", TaskVersion: nextTask.Version, ReadSelectors: slices.Clone(nextTask.ReadSelectors), WriteTargets: slices.Clone(nextTask.WriteTargets), RetryPolicy: nextTask.RetryPolicy, CreatedAt: time.Now().UTC()}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return model.Task{}, model.TaskEnvelope{}, false, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: model.EventTaskDispatched, Payload: map[string]any{"envelope": eventpayload.Envelope(env)}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.Task{}, model.TaskEnvelope{}, false, err
	}
	return nextTask, env, task.Status != nextTask.Status, nil
}

func appendBlackboardWrittenEvent(ctx context.Context, uow ports.UnitOfWork, item model.BlackboardItem) error {
	return uow.Events().AppendEvent(ctx, model.Event{RunID: item.RunID, TaskID: item.TaskID, Type: model.EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: time.Now().UTC()})
}
