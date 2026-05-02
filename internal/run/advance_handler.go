package run

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

type PipelineProvider func() ports.PipelineComponents

type Authorizer func(context.Context, ports.UnitOfWork, model.PolicyRequest) (model.PolicyDecision, error)

type AdvanceHandlerOptions struct {
	NewID     IDGenerator
	Pipeline  PipelineProvider
	Authorize Authorizer
}

func RegisterAdvanceHandler(bus *commandbus.Bus, options AdvanceHandlerOptions) {
	commandbus.Register[AdvanceRunCommand](bus, advanceRunHandler{options: options})
}

type AdvanceResult struct {
	Run        model.Run
	Runs       []model.Run
	Tasks      []model.Task
	Envelopes  []model.TaskEnvelope
	Events     []model.Event
	TraceSpans []model.TraceSpan
}

type advanceRunHandler struct{ options AdvanceHandlerOptions }

func (advanceRunHandler) Name() string { return AdvanceRunCommand{}.CommandName() }

func (h advanceRunHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd AdvanceRunCommand) (any, error) {
	run, err := uow.Runs().LoadRun(ctx, cmd.RunID)
	if err != nil {
		return nil, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return nil, model.ErrTerminalState
	}
	pipeline := h.options.Pipeline()
	m := &AdvanceResult{}
	if err := h.recordTrace(ctx, uow, m, run.ID, run.RootTaskID, "runtime.pipeline", "orchestrator"); err != nil {
		return nil, err
	}
	run, plan, err := h.createPipelinePlan(ctx, uow, m, pipeline, run)
	if err != nil {
		return nil, err
	}
	run, err = h.validatePipelinePlan(ctx, uow, m, pipeline, run, plan)
	if err != nil {
		return nil, err
	}
	run, routing, err := h.routePipelinePlan(ctx, uow, m, pipeline, run, plan)
	if err != nil {
		return nil, err
	}
	run, err = h.transitionRun(ctx, uow, m, run, model.RunStatusDispatching)
	if err != nil {
		return nil, err
	}
	if err := h.dispatchRouting(ctx, uow, m, pipeline, run, routing); err != nil {
		return nil, err
	}
	run, err = h.transitionRun(ctx, uow, m, run, model.RunStatusRunning)
	if err != nil {
		return nil, err
	}
	if err := pipeline.TaskMonitor.Advance(ctx, run); err != nil {
		return nil, err
	}
	m.Run = run
	return *m, nil
}

func (h advanceRunHandler) createPipelinePlan(ctx context.Context, uow ports.UnitOfWork, m *AdvanceResult, pipeline ports.PipelineComponents, run model.Run) (model.Run, model.TodoPlan, error) {
	run, err := h.transitionRun(ctx, uow, m, run, model.RunStatusPlanning)
	if err != nil {
		return model.Run{}, model.TodoPlan{}, err
	}
	intent, err := pipeline.IntentAnalyzer.AnalyzeIntent(ctx, run)
	if err != nil {
		return model.Run{}, model.TodoPlan{}, err
	}
	if err := h.emit(ctx, uow, m, model.Event{RunID: run.ID, TaskID: run.RootTaskID, Type: model.EventIntentAnalyzed, Payload: map[string]any{"summary": intent.Summary}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.Run{}, model.TodoPlan{}, err
	}
	plan, err := pipeline.Planner.CreatePlan(ctx, intent)
	if err != nil {
		return model.Run{}, model.TodoPlan{}, err
	}
	if plan.RunID == "" {
		plan.RunID = run.ID
	}
	if len(plan.Tasks) == 0 {
		root, err := uow.Tasks().LoadTask(ctx, run.ID, run.RootTaskID)
		if err != nil {
			return model.Run{}, model.TodoPlan{}, err
		}
		plan.Tasks = []model.Task{root}
	}
	if err := h.preparePlanTasks(ctx, uow, m, run, plan); err != nil {
		return model.Run{}, model.TodoPlan{}, err
	}
	if err := h.emit(ctx, uow, m, model.Event{RunID: run.ID, TaskID: run.RootTaskID, Type: model.EventPlanCreated, Payload: map[string]any{"taskCount": len(plan.Tasks)}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.Run{}, model.TodoPlan{}, err
	}
	return run, plan, nil
}

func (h advanceRunHandler) preparePlanTasks(ctx context.Context, uow ports.UnitOfWork, m *AdvanceResult, run model.Run, plan model.TodoPlan) error {
	for _, planned := range plan.Tasks {
		if planned.ID == "" || planned.ID == run.RootTaskID {
			continue
		}
		planned = normalizePlannedTask(run.ID, planned)
		if _, err := uow.Tasks().LoadTask(ctx, run.ID, planned.ID); err == nil {
			continue
		} else if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		if err := h.saveTask(ctx, uow, m, planned); err != nil {
			return err
		}
		if err := h.emit(ctx, uow, m, model.Event{RunID: run.ID, TaskID: planned.ID, Type: model.EventTaskCreated, Payload: eventpayload.Task(planned), RecordedAt: time.Now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func (h advanceRunHandler) validatePipelinePlan(ctx context.Context, uow ports.UnitOfWork, m *AdvanceResult, pipeline ports.PipelineComponents, run model.Run, plan model.TodoPlan) (model.Run, error) {
	run, err := h.transitionRun(ctx, uow, m, run, model.RunStatusValidating)
	if err != nil {
		return model.Run{}, err
	}
	if err := pipeline.Validator.ValidatePlan(ctx, plan); err != nil {
		return model.Run{}, err
	}
	if err := h.emit(ctx, uow, m, model.Event{RunID: run.ID, TaskID: run.RootTaskID, Type: model.EventPlanValidated, Payload: map[string]any{"valid": true}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.Run{}, err
	}
	for _, planned := range plan.Tasks {
		task, err := uow.Tasks().LoadTask(ctx, run.ID, planned.ID)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			return model.Run{}, err
		}
		if task.Status == model.TaskStatusPlanned {
			next, err := corestate.TransitionTask(task, model.TaskStatusValidated, true)
			if err != nil {
				return model.Run{}, err
			}
			if err := h.saveTask(ctx, uow, m, next); err != nil {
				return model.Run{}, err
			}
		}
	}
	return run, nil
}

func (h advanceRunHandler) routePipelinePlan(ctx context.Context, uow ports.UnitOfWork, m *AdvanceResult, pipeline ports.PipelineComponents, run model.Run, plan model.TodoPlan) (model.Run, model.RoutingPlan, error) {
	run, err := h.transitionRun(ctx, uow, m, run, model.RunStatusRouting)
	if err != nil {
		return model.Run{}, model.RoutingPlan{}, err
	}
	routing, err := pipeline.Router.RouteTasks(ctx, plan)
	if err != nil {
		return model.Run{}, model.RoutingPlan{}, err
	}
	if routing.RunID == "" {
		routing.RunID = run.ID
	}
	if len(routing.Routes) == 0 {
		for _, task := range plan.Tasks {
			routing.Routes = append(routing.Routes, model.TaskRoute{TaskID: task.ID, TargetAgentID: task.OwnerAgentID, TargetComponent: task.OwnerComponent})
		}
	}
	if err := h.emit(ctx, uow, m, model.Event{RunID: run.ID, TaskID: run.RootTaskID, Type: model.EventRoutingPlanCreated, Payload: map[string]any{"routeCount": len(routing.Routes)}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.Run{}, model.RoutingPlan{}, err
	}
	for _, route := range routing.Routes {
		task, err := uow.Tasks().LoadTask(ctx, run.ID, route.TaskID)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			return model.Run{}, model.RoutingPlan{}, err
		}
		if task.Status == model.TaskStatusValidated {
			next, err := corestate.TransitionTask(task, model.TaskStatusRouted, true)
			if err != nil {
				return model.Run{}, model.RoutingPlan{}, err
			}
			if err := h.saveTask(ctx, uow, m, next); err != nil {
				return model.Run{}, model.RoutingPlan{}, err
			}
		}
	}
	return run, routing, nil
}

func (h advanceRunHandler) dispatchRouting(ctx context.Context, uow ports.UnitOfWork, m *AdvanceResult, pipeline ports.PipelineComponents, run model.Run, routing model.RoutingPlan) error {
	envelopes, err := pipeline.Dispatcher.Dispatch(ctx, routing)
	if err != nil {
		return err
	}
	for _, env := range envelopes {
		task, err := uow.Tasks().LoadTask(ctx, run.ID, env.TaskID)
		if errors.Is(err, model.ErrNotFound) || corestate.IsTerminalTask(task.Status) {
			continue
		}
		if err != nil {
			return err
		}
		if len(task.DependsOn) > 0 {
			tasks, err := uow.Tasks().ListTasks(ctx, run.ID)
			if err != nil {
				return err
			}
			byID := make(map[string]model.Task, len(tasks))
			for _, item := range tasks {
				byID[item.ID] = item
			}
			ready, _ := corestate.DependencyGate(task, byID)
			if !ready {
				continue
			}
		}
		if h.options.Authorize != nil {
			if _, err := h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationDispatch, RunID: run.ID, TaskID: task.ID, Actor: model.SourceIdentity{Type: model.SourceComponent, ID: "dispatcher"}}); err != nil {
				return err
			}
		}
		if err := h.recordTrace(ctx, uow, m, run.ID, task.ID, "mailbox.dispatch", "mailbox"); err != nil {
			return err
		}
		next, err := corestate.TransitionTask(task, model.TaskStatusDispatched, false)
		if err != nil {
			return err
		}
		if err := h.saveTask(ctx, uow, m, next); err != nil {
			return err
		}
		env = normalizePipelineEnvelope(run.ID, next, env)
		if env.ID == "" {
			env.ID = h.options.NewID("env")
		}
		if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
			return err
		}
		m.Envelopes = append(m.Envelopes, env)
		if err := h.emit(ctx, uow, m, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: model.EventTaskDispatched, Payload: map[string]any{"envelope": eventpayload.Envelope(env)}, RecordedAt: time.Now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func normalizePipelineEnvelope(runID string, task model.Task, env model.TaskEnvelope) model.TaskEnvelope {
	env.RunID = runID
	env.TaskID = task.ID
	env.TargetAgentID = firstNonEmpty(env.TargetAgentID, task.OwnerAgentID)
	env.TargetComponent = firstNonEmpty(env.TargetComponent, task.OwnerComponent)
	if env.Type == "" {
		env.Type = "TaskEnvelope"
	}
	if env.Status == "" {
		env.Status = "pending"
	}
	env.TaskVersion = task.Version
	env.ReadSelectors = slices.Clone(task.ReadSelectors)
	env.WriteTargets = slices.Clone(task.WriteTargets)
	if env.RetryPolicy == (model.RetryPolicy{}) {
		env.RetryPolicy = task.RetryPolicy
	}
	env.CreatedAt = time.Now().UTC()
	return env
}

func (h advanceRunHandler) transitionRun(ctx context.Context, uow ports.UnitOfWork, m *AdvanceResult, run model.Run, status model.RunStatus) (model.Run, error) {
	next, err := corestate.TransitionRun(run, status)
	if err != nil {
		return model.Run{}, err
	}
	if err := uow.Runs().SaveRun(ctx, next); err != nil {
		return model.Run{}, err
	}
	m.Runs = append(m.Runs, next)
	if run.Status != next.Status {
		if err := h.emit(ctx, uow, m, model.Event{RunID: next.ID, TaskID: next.RootTaskID, Type: model.EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(next.Status), "run": eventpayload.Run(next)}, RecordedAt: time.Now().UTC()}); err != nil {
			return model.Run{}, err
		}
	}
	return next, nil
}

func (h advanceRunHandler) saveTask(ctx context.Context, uow ports.UnitOfWork, m *AdvanceResult, task model.Task) error {
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return err
	}
	m.Tasks = append(m.Tasks, task)
	return nil
}

func (h advanceRunHandler) emit(ctx context.Context, uow ports.UnitOfWork, m *AdvanceResult, event model.Event) error {
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	if err := uow.Events().AppendEvent(ctx, event); err != nil {
		return err
	}
	m.Events = append(m.Events, event)
	return nil
}

func (h advanceRunHandler) recordTrace(ctx context.Context, uow ports.UnitOfWork, m *AdvanceResult, runID, taskID, name, component string) error {
	now := time.Now().UTC()
	span := model.TraceSpan{RunID: runID, TaskID: taskID, Name: name, Component: component, Status: model.TraceSpanEnded, StartedAt: now, EndedAt: now}
	if err := uow.Trace().SaveTraceSpan(ctx, span); err != nil {
		return err
	}
	m.TraceSpans = append(m.TraceSpans, span)
	return nil
}

func normalizePlannedTask(runID string, task model.Task) model.Task {
	if task.RunID == "" {
		task.RunID = runID
	}
	if task.Status == "" {
		task.Status = model.TaskStatusPlanned
	}
	if task.Version == 0 {
		task.Version = 1
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	task.UpdatedAt = task.CreatedAt
	return task
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
