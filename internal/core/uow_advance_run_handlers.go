package core

import (
	"context"
	"errors"
	"slices"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerAdvanceRunUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[AdvanceRunCommand](runtime.commandBus, advanceRunHandler{runtime: runtime})
}

type advanceRunHandler struct{ runtime *Runtime }

func (advanceRunHandler) Name() string { return AdvanceRunCommand{}.CommandName() }

type advanceRunResult struct {
	Run        Run
	Runs       []Run
	Tasks      []Task
	Envelopes  []TaskEnvelope
	Events     []Event
	TraceSpans []TraceSpan
}

func (h advanceRunHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd AdvanceRunCommand) (any, error) {
	run, err := uow.Runs().LoadRun(ctx, cmd.RunID)
	if err != nil {
		return nil, err
	}
	if isTerminalRun(run.Status) {
		return nil, ErrTerminalState
	}
	pipeline := h.runtime.currentPipeline()
	m := &advanceRunResult{}
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
	run, err = h.transitionRun(ctx, uow, m, run, RunStatusDispatching)
	if err != nil {
		return nil, err
	}
	if err := h.dispatchRouting(ctx, uow, m, pipeline, run, routing); err != nil {
		return nil, err
	}
	run, err = h.transitionRun(ctx, uow, m, run, RunStatusRunning)
	if err != nil {
		return nil, err
	}
	if err := pipeline.TaskMonitor.Advance(ctx, run); err != nil {
		return nil, err
	}
	m.Run = run
	return *m, nil
}

func (h advanceRunHandler) createPipelinePlan(ctx context.Context, uow ports.FullUnitOfWork, m *advanceRunResult, pipeline PipelineComponents, run Run) (Run, TodoPlan, error) {
	run, err := h.transitionRun(ctx, uow, m, run, RunStatusPlanning)
	if err != nil {
		return Run{}, TodoPlan{}, err
	}
	intent, err := pipeline.IntentAnalyzer.AnalyzeIntent(ctx, run)
	if err != nil {
		return Run{}, TodoPlan{}, err
	}
	if err := h.emit(ctx, uow, m, Event{RunID: run.ID, TaskID: run.RootTaskID, Type: EventIntentAnalyzed, Payload: map[string]any{"summary": intent.Summary}, RecordedAt: time.Now().UTC()}); err != nil {
		return Run{}, TodoPlan{}, err
	}
	plan, err := pipeline.Planner.CreatePlan(ctx, intent)
	if err != nil {
		return Run{}, TodoPlan{}, err
	}
	if plan.RunID == "" {
		plan.RunID = run.ID
	}
	if len(plan.Tasks) == 0 {
		root, err := uow.Tasks().LoadTask(ctx, run.ID, run.RootTaskID)
		if err != nil {
			return Run{}, TodoPlan{}, err
		}
		plan.Tasks = []Task{root}
	}
	if err := h.preparePlanTasks(ctx, uow, m, run, plan); err != nil {
		return Run{}, TodoPlan{}, err
	}
	if err := h.emit(ctx, uow, m, Event{RunID: run.ID, TaskID: run.RootTaskID, Type: EventPlanCreated, Payload: map[string]any{"taskCount": len(plan.Tasks)}, RecordedAt: time.Now().UTC()}); err != nil {
		return Run{}, TodoPlan{}, err
	}
	return run, plan, nil
}

func (h advanceRunHandler) preparePlanTasks(ctx context.Context, uow ports.FullUnitOfWork, m *advanceRunResult, run Run, plan TodoPlan) error {
	for _, planned := range plan.Tasks {
		if planned.ID == "" || planned.ID == run.RootTaskID {
			continue
		}
		planned = normalizePlannedTask(run.ID, planned)
		if _, err := uow.Tasks().LoadTask(ctx, run.ID, planned.ID); err == nil {
			continue
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := h.saveTask(ctx, uow, m, planned); err != nil {
			return err
		}
		if err := h.emit(ctx, uow, m, Event{RunID: run.ID, TaskID: planned.ID, Type: EventTaskCreated, Payload: taskEventPayload(planned), RecordedAt: time.Now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func (h advanceRunHandler) validatePipelinePlan(ctx context.Context, uow ports.FullUnitOfWork, m *advanceRunResult, pipeline PipelineComponents, run Run, plan TodoPlan) (Run, error) {
	run, err := h.transitionRun(ctx, uow, m, run, RunStatusValidating)
	if err != nil {
		return Run{}, err
	}
	if err := pipeline.Validator.ValidatePlan(ctx, plan); err != nil {
		return Run{}, err
	}
	if err := h.emit(ctx, uow, m, Event{RunID: run.ID, TaskID: run.RootTaskID, Type: EventPlanValidated, Payload: map[string]any{"valid": true}, RecordedAt: time.Now().UTC()}); err != nil {
		return Run{}, err
	}
	for _, planned := range plan.Tasks {
		task, err := uow.Tasks().LoadTask(ctx, run.ID, planned.ID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return Run{}, err
		}
		if task.Status == TaskStatusPlanned {
			next, err := transitionTaskPure(task, TaskStatusValidated, true)
			if err != nil {
				return Run{}, err
			}
			if err := h.saveTask(ctx, uow, m, next); err != nil {
				return Run{}, err
			}
		}
	}
	return run, nil
}

func (h advanceRunHandler) routePipelinePlan(ctx context.Context, uow ports.FullUnitOfWork, m *advanceRunResult, pipeline PipelineComponents, run Run, plan TodoPlan) (Run, RoutingPlan, error) {
	run, err := h.transitionRun(ctx, uow, m, run, RunStatusRouting)
	if err != nil {
		return Run{}, RoutingPlan{}, err
	}
	routing, err := pipeline.Router.RouteTasks(ctx, plan)
	if err != nil {
		return Run{}, RoutingPlan{}, err
	}
	if routing.RunID == "" {
		routing.RunID = run.ID
	}
	if len(routing.Routes) == 0 {
		for _, task := range plan.Tasks {
			routing.Routes = append(routing.Routes, TaskRoute{TaskID: task.ID, TargetAgentID: task.OwnerAgentID, TargetComponent: task.OwnerComponent})
		}
	}
	if err := h.emit(ctx, uow, m, Event{RunID: run.ID, TaskID: run.RootTaskID, Type: EventRoutingPlanCreated, Payload: map[string]any{"routeCount": len(routing.Routes)}, RecordedAt: time.Now().UTC()}); err != nil {
		return Run{}, RoutingPlan{}, err
	}
	for _, route := range routing.Routes {
		task, err := uow.Tasks().LoadTask(ctx, run.ID, route.TaskID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return Run{}, RoutingPlan{}, err
		}
		if task.Status == TaskStatusValidated {
			next, err := transitionTaskPure(task, TaskStatusRouted, true)
			if err != nil {
				return Run{}, RoutingPlan{}, err
			}
			if err := h.saveTask(ctx, uow, m, next); err != nil {
				return Run{}, RoutingPlan{}, err
			}
		}
	}
	return run, routing, nil
}

func (h advanceRunHandler) dispatchRouting(ctx context.Context, uow ports.FullUnitOfWork, m *advanceRunResult, pipeline PipelineComponents, run Run, routing RoutingPlan) error {
	envelopes, err := pipeline.Dispatcher.Dispatch(ctx, routing)
	if err != nil {
		return err
	}
	for _, env := range envelopes {
		task, err := uow.Tasks().LoadTask(ctx, run.ID, env.TaskID)
		if errors.Is(err, ErrNotFound) || isTerminalTask(task.Status) {
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
			byID := make(map[string]Task, len(tasks))
			for _, item := range tasks {
				byID[item.ID] = item
			}
			ready, _ := dependencyGate(task, byID)
			if !ready {
				continue
			}
		}
		if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationDispatch, RunID: run.ID, TaskID: task.ID, Actor: SourceIdentity{Type: SourceComponent, ID: "dispatcher"}}); err != nil {
			return err
		}
		if err := h.recordTrace(ctx, uow, m, run.ID, task.ID, "mailbox.dispatch", "mailbox"); err != nil {
			return err
		}
		next, err := transitionTaskPure(task, TaskStatusDispatched, false)
		if err != nil {
			return err
		}
		if err := h.saveTask(ctx, uow, m, next); err != nil {
			return err
		}
		env = normalizePipelineEnvelope(run.ID, next, env)
		if env.ID == "" {
			env.ID = h.runtime.newID("env")
		}
		if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
			return err
		}
		m.Envelopes = append(m.Envelopes, env)
		if err := h.emit(ctx, uow, m, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventTaskDispatched, Payload: map[string]any{"envelope": envPayload(env)}, RecordedAt: time.Now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func normalizePipelineEnvelope(runID string, task Task, env TaskEnvelope) TaskEnvelope {
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
	if env.RetryPolicy == (RetryPolicy{}) {
		env.RetryPolicy = task.RetryPolicy
	}
	env.CreatedAt = time.Now().UTC()
	return env
}

func (h advanceRunHandler) transitionRun(ctx context.Context, uow ports.FullUnitOfWork, m *advanceRunResult, run Run, status RunStatus) (Run, error) {
	next, err := transitionRunPure(run, status)
	if err != nil {
		return Run{}, err
	}
	if err := uow.Runs().SaveRun(ctx, next); err != nil {
		return Run{}, err
	}
	m.Runs = append(m.Runs, next)
	if run.Status != next.Status {
		if err := h.emit(ctx, uow, m, Event{RunID: next.ID, TaskID: next.RootTaskID, Type: EventRunStatusChanged, Payload: map[string]any{"from": string(run.Status), "to": string(next.Status), "run": runPayload(next)}, RecordedAt: time.Now().UTC()}); err != nil {
			return Run{}, err
		}
	}
	return next, nil
}

func (h advanceRunHandler) saveTask(ctx context.Context, uow ports.FullUnitOfWork, m *advanceRunResult, task Task) error {
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return err
	}
	m.Tasks = append(m.Tasks, task)
	return nil
}

func (h advanceRunHandler) emit(ctx context.Context, uow ports.FullUnitOfWork, m *advanceRunResult, event Event) error {
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	if err := uow.Events().AppendEvent(ctx, event); err != nil {
		return err
	}
	m.Events = append(m.Events, event)
	return nil
}

func (h advanceRunHandler) recordTrace(ctx context.Context, uow ports.FullUnitOfWork, m *advanceRunResult, runID, taskID, name, component string) error {
	now := time.Now().UTC()
	span := TraceSpan{RunID: runID, TaskID: taskID, Name: name, Component: component, Status: TraceSpanEnded, StartedAt: now, EndedAt: now}
	if err := uow.Trace().SaveTraceSpan(ctx, span); err != nil {
		return err
	}
	m.TraceSpans = append(m.TraceSpans, span)
	return nil
}
