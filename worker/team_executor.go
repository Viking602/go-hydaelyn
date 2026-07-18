package worker

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/multiagent"
)

// RunnerExecutor persists a multi-agent dispatch through Runner before
// executing it with the bounded agent engine.
type RunnerExecutor struct {
	Runner         *hydaelyn.Runner
	Classes        map[string]multiagent.AgentClass
	BuildDeps      agent.BuildDeps
	DecorateEngine func(agent.Engine, multiagent.Dispatch, multiagent.AgentClass) agent.Engine
	TTL            time.Duration
}

func (e RunnerExecutor) Execute(ctx context.Context, dispatch multiagent.Dispatch) (api.TypedReport, error) {
	if e.Runner == nil {
		return api.TypedReport{}, ErrRunnerMissing
	}
	if err := multiagent.ValidateDispatch(dispatch); err != nil {
		return api.TypedReport{}, err
	}
	dispatch = dispatchWithTaskInput(dispatch)
	instanceClassName, class, err := e.resolveClass(dispatch)
	if err != nil {
		return api.TypedReport{}, err
	}
	engine, err := agent.Build(class.ToSpec(), e.BuildDeps)
	if err != nil {
		return api.TypedReport{}, err
	}
	if e.DecorateEngine != nil {
		engine = e.DecorateEngine(engine, dispatch, class)
	}
	e.Runner.RegisterAgent(api.AgentProfile{ID: dispatch.To})
	task, err := e.ensureTask(ctx, dispatch)
	if err != nil {
		return api.TypedReport{}, err
	}
	if task.Status == api.TaskStatusCompleted && task.Result != nil {
		if err := e.persistInstance(ctx, dispatch, instanceClassName, multiagent.InstanceStateFinished, multiagent.EventAgentInstanceFinished); err != nil {
			return *task.Result, err
		}
		return *task.Result, nil
	}
	if err := e.persistInstance(ctx, dispatch, instanceClassName, multiagent.InstanceStateRunning, multiagent.EventAgentInstanceCreated); err != nil {
		return api.TypedReport{}, err
	}
	envelope, ok, err := taskEnvelope(ctx, e.Runner, task.RunID, task.ID, "pending")
	if err != nil {
		return api.TypedReport{}, err
	}
	if !ok {
		envelope, err = e.Runner.DispatchTask(ctx, api.DispatchTaskCommand{
			RunID:         task.RunID,
			TaskID:        task.ID,
			TargetAgentID: dispatch.To,
		})
		if err != nil {
			return api.TypedReport{}, err
		}
	}
	runErr := (AgentWorker{
		Runner:  e.Runner,
		Engine:  engine,
		AgentID: dispatch.To,
		Model:   class.Model,
		TTL:     e.TTL,
	}).ExecuteEnvelope(ctx, ExecuteEnvelopeRequest{Envelope: envelope, TTL: e.TTL})
	persisted, loadErr := e.Runner.Task(ctx, task.RunID, task.ID)
	if loadErr != nil {
		return api.TypedReport{}, errors.Join(runErr, loadErr)
	}
	state := multiagent.InstanceStateFinished
	if runErr != nil {
		state = multiagent.InstanceStateFailed
	}
	if err := e.persistInstance(ctx, dispatch, instanceClassName, state, multiagent.EventAgentInstanceFinished); err != nil {
		return reportValue(persisted.Result), errors.Join(runErr, err)
	}
	return reportValue(persisted.Result), runErr
}

func dispatchWithTaskInput(dispatch multiagent.Dispatch) multiagent.Dispatch {
	if len(dispatch.Task.Input) > 0 {
		return dispatch
	}
	dispatch.Task.Input = dispatch.Input
	if len(dispatch.Task.Input) == 0 && dispatch.Handoff != nil {
		dispatch.Task.Input = dispatch.Handoff.Payload
	}
	return dispatch
}

func (e RunnerExecutor) resolveClass(dispatch multiagent.Dispatch) (string, multiagent.AgentClass, error) {
	instanceClassName := dispatch.ClassName
	if instanceClassName == "" {
		instanceClassName = strings.TrimPrefix(dispatch.Task.ID, dispatch.Task.RunID+"-")
	}
	agentClassName := dispatch.AgentClassName
	if agentClassName == "" {
		agentClassName = instanceClassName
	}
	class, ok := e.Classes[agentClassName]
	if !ok {
		return "", multiagent.AgentClass{}, fmt.Errorf("worker: agent class %q not found", agentClassName)
	}
	return instanceClassName, class, nil
}

func (e RunnerExecutor) ensureTask(ctx context.Context, dispatch multiagent.Dispatch) (api.Task, error) {
	existing, err := e.Runner.Task(ctx, dispatch.Task.RunID, dispatch.Task.ID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, api.ErrNotFound) {
		return api.Task{}, err
	}
	return e.Runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        dispatch.Task.RunID,
		TaskID:       dispatch.Task.ID,
		Type:         dispatch.Task.Type,
		Goal:         dispatch.Task.Goal,
		Input:        dispatch.Task.Input,
		OwnerAgentID: dispatch.To,
		InputSchema:  dispatch.Task.InputSchema,
		OutputSchema: dispatch.Task.OutputSchema,
		Budget:       dispatch.Task.Budget,
	})
}

func taskEnvelope(ctx context.Context, runner *hydaelyn.Runner, runID, taskID string, statuses ...string) (api.TaskEnvelope, bool, error) {
	uow, err := runner.Begin(ctx)
	if err != nil {
		return api.TaskEnvelope{}, false, err
	}
	envelopes, err := uow.MailboxOutbox().ListEnvelopes(ctx, runID)
	if err != nil {
		_ = uow.Rollback(ctx)
		return api.TaskEnvelope{}, false, err
	}
	if err := uow.Rollback(ctx); err != nil {
		return api.TaskEnvelope{}, false, err
	}
	for _, envelope := range envelopes {
		if envelope.TaskID != taskID {
			continue
		}
		for _, status := range statuses {
			if envelope.Status == status {
				return envelope, true, nil
			}
		}
	}
	return api.TaskEnvelope{}, false, nil
}

func (e RunnerExecutor) persistInstance(ctx context.Context, dispatch multiagent.Dispatch, className string, state multiagent.InstanceState, eventType api.EventType) error {
	uow, err := e.Runner.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	createdAt := time.Now().UTC()
	if existing, loadErr := uow.AgentInstances().LoadAgentInstance(ctx, dispatch.To); loadErr == nil {
		createdAt = existing.CreatedAt
		if existing.State == string(state) {
			return nil
		}
	} else if !errors.Is(loadErr, api.ErrNotFound) {
		return loadErr
	}
	record := api.AgentInstanceRecord{
		ID:        dispatch.To,
		ClassName: className,
		RunID:     dispatch.Task.RunID,
		TaskID:    dispatch.Task.ID,
		State:     string(state),
		CreatedAt: createdAt,
	}
	if eventType == multiagent.EventAgentInstanceCreated && dispatch.Handoff != nil {
		handoff := handoffRecord(dispatch)
		if _, loadErr := uow.Handoffs().LoadHandoff(ctx, handoff.RunID, handoff.ID); errors.Is(loadErr, api.ErrNotFound) {
			if err := uow.Handoffs().SaveHandoff(ctx, handoff); err != nil {
				return err
			}
			if err := uow.Events().AppendEvent(ctx, api.Event{
				RunID:      dispatch.Task.RunID,
				TaskID:     dispatch.Task.ID,
				Type:       multiagent.EventTypedHandoff,
				Payload:    map[string]any{"handoffId": handoff.ID, "from": handoff.From, "to": handoff.To},
				RecordedAt: time.Now().UTC(),
			}); err != nil {
				return err
			}
		} else if loadErr != nil {
			return loadErr
		}
	}
	if err := uow.AgentInstances().SaveAgentInstance(ctx, record); err != nil {
		return err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{
		RunID:      dispatch.Task.RunID,
		TaskID:     dispatch.Task.ID,
		Type:       eventType,
		Payload:    map[string]any{"instanceId": dispatch.To, "className": className, "state": string(state)},
		RecordedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	if eventType == multiagent.EventAgentInstanceCreated {
		if err := uow.Events().AppendEvent(ctx, api.Event{
			RunID:      dispatch.Task.RunID,
			TaskID:     dispatch.Task.ID,
			Type:       multiagent.EventDispatchEmitted,
			Payload:    map[string]any{"instanceId": dispatch.To, "className": className},
			RecordedAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
	}
	if err := uow.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func handoffRecord(dispatch multiagent.Dispatch) api.HandoffRecord {
	handoff := *dispatch.Handoff
	if handoff.RunID == "" {
		handoff.RunID = dispatch.Task.RunID
	}
	if handoff.To == "" {
		handoff.To = dispatch.To
	}
	if handoff.CreatedAt.IsZero() {
		handoff.CreatedAt = time.Now().UTC()
	}
	if handoff.ID == "" {
		sum := sha256.Sum256([]byte(handoff.RunID + "\x00" + handoff.From + "\x00" + handoff.To + "\x00" + dispatch.Task.ID + "\x00" + string(handoff.Payload)))
		handoff.ID = fmt.Sprintf("handoff-%x", sum[:8])
	}
	return api.HandoffRecord{
		ID:                   handoff.ID,
		RunID:                handoff.RunID,
		From:                 handoff.From,
		To:                   handoff.To,
		Reason:               handoff.Reason,
		Payload:              handoff.Payload,
		EvidenceIDs:          handoff.EvidenceIDs,
		RequiredOutputSchema: handoff.RequiredOutputSchema,
		CreatedAt:            handoff.CreatedAt,
	}
}

func reportValue(report *api.TypedReport) api.TypedReport {
	if report == nil {
		return api.TypedReport{}
	}
	return *report
}
