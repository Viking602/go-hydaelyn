package userinput

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Viking602/venat/api"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
)

type IDGenerator func(string) string

type Authorizer func(context.Context, ports.UnitOfWork, api.PolicyRequest) (api.PolicyDecision, error)

type TraceRecorder func(context.Context, ports.UnitOfWork, string, string, string, string) error

type HandlerOptions struct {
	NewID       IDGenerator
	Authorize   Authorizer
	RecordTrace TraceRecorder
}

type SubmitResult struct {
	Item           api.BlackboardItem
	Run            api.Run
	PreviousRun    api.Run
	Task           api.Task
	Envelope       api.TaskEnvelope
	Input          string
	RunTransition  bool
	Redispatched   bool
	TaskTransition bool
}

// NotifyBlackboard implements core.BlackboardNotifier so the runtime can
// fan out the user-input item to subscribers at commit time.
func (r SubmitResult) NotifyBlackboard() []api.BlackboardItem {
	return []api.BlackboardItem{r.Item}
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
		return nil, api.ErrTerminalState
	}
	task, shouldRedispatch, err := loadTask(ctx, uow, cmd)
	if err != nil {
		return nil, err
	}
	if shouldRedispatch && h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, api.PolicyRequest{Operation: api.PolicyOperationDispatch, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: api.SourceIdentity{Type: api.SourceSystem, ID: "user_input"}}); err != nil {
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
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventUserInputSubmitted, Payload: map[string]any{"input": cmd.Input}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return result, nil
}

func loadTask(ctx context.Context, uow ports.UnitOfWork, cmd SubmitUserInputCommand) (api.Task, bool, error) {
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		if errors.Is(err, api.ErrNotFound) {
			return api.Task{}, false, nil
		}
		return api.Task{}, false, err
	}
	shouldRedispatch := task.Status == api.TaskStatusBlocked || task.Status == api.TaskStatusWaitingUserInput
	return task, shouldRedispatch, nil
}

func (h handler) writeItem(ctx context.Context, uow ports.UnitOfWork, cmd SubmitUserInputCommand, now time.Time) (api.BlackboardItem, error) {
	item := api.BlackboardItem{ID: h.options.NewID("bb"), RunID: cmd.RunID, TaskID: cmd.TaskID, Source: api.SourceIdentity{Type: api.SourceSystem, ID: "user"}, Visibility: api.BlackboardVisibilityAgentVisible, Key: "user_input", Payload: cmd.Input, CreatedAt: now}
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return api.BlackboardItem{}, err
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, cmd.RunID, cmd.TaskID, "blackboard.write", "blackboard"); err != nil {
			return api.BlackboardItem{}, err
		}
	}
	if err := appendBlackboardWrittenEvent(ctx, uow, item); err != nil {
		return api.BlackboardItem{}, err
	}
	return item, nil
}

func resumeRun(ctx context.Context, uow ports.UnitOfWork, run api.Run) (api.Run, bool, error) {
	nextRun, err := corestate.TransitionRun(run, api.RunStatusRunning)
	if err != nil {
		return api.Run{}, false, err
	}
	if nextRun.Status == run.Status && nextRun.UpdatedAt.Equal(run.UpdatedAt) {
		return nextRun, false, nil
	}
	if err := uow.Runs().SaveRun(ctx, nextRun); err != nil {
		return api.Run{}, false, err
	}
	if run.Status == nextRun.Status {
		return nextRun, false, nil
	}
	err = uow.Events().AppendEvent(ctx, api.Event{RunID: nextRun.ID, TaskID: nextRun.RootTaskID, Type: api.EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(nextRun.Status), "run": eventpayload.Run(nextRun)}, RecordedAt: time.Now().UTC()})
	return nextRun, true, err
}

func (h handler) redispatchTask(ctx context.Context, uow ports.UnitOfWork, task api.Task) (api.Task, api.TaskEnvelope, bool, error) {
	nextTask, err := corestate.TransitionTask(task, api.TaskStatusDispatched, true)
	if err != nil {
		return api.Task{}, api.TaskEnvelope{}, false, err
	}
	nextTask.Error = ""
	if err := uow.Tasks().SaveTask(ctx, nextTask); err != nil {
		return api.Task{}, api.TaskEnvelope{}, false, err
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, nextTask.RunID, nextTask.ID, "mailbox.dispatch", "mailbox"); err != nil {
			return api.Task{}, api.TaskEnvelope{}, false, err
		}
	}
	env := api.TaskEnvelope{ID: h.options.NewID("env"), RunID: nextTask.RunID, TaskID: nextTask.ID, TargetAgentID: nextTask.OwnerAgentID, TargetComponent: nextTask.OwnerComponent, Type: "TaskEnvelope", Status: "pending", TaskVersion: nextTask.Version, ReadSelectors: slices.Clone(nextTask.ReadSelectors), WriteTargets: slices.Clone(nextTask.WriteTargets), RetryPolicy: nextTask.RetryPolicy, CreatedAt: time.Now().UTC()}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return api.Task{}, api.TaskEnvelope{}, false, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: env.RunID, TaskID: env.TaskID, Type: api.EventTaskDispatched, Payload: map[string]any{"envelope": eventpayload.Envelope(env)}, RecordedAt: time.Now().UTC()}); err != nil {
		return api.Task{}, api.TaskEnvelope{}, false, err
	}
	return nextTask, env, task.Status != nextTask.Status, nil
}

func appendBlackboardWrittenEvent(ctx context.Context, uow ports.UnitOfWork, item api.BlackboardItem) error {
	return uow.Events().AppendEvent(ctx, api.Event{RunID: item.RunID, TaskID: item.TaskID, Type: api.EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: time.Now().UTC()})
}
