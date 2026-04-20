package host

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/internal/middleware"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func (r *Runtime) createSession(ctx context.Context, params session.CreateParams) (session.Session, error) {
	var current session.Session
	err := r.runStage(ctx, &middleware.Envelope{
		Stage:     middleware.StageMemory,
		Operation: "create",
		TeamID:    params.TeamID,
		AgentID:   params.AgentID,
		Request:   params,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		next, runErr := r.storage.Sessions().Create(ctx, params)
		if runErr != nil {
			return runErr
		}
		current = next
		envelope.Response = next
		return nil
	})
	return current, err
}

func (r *Runtime) loadSession(ctx context.Context, sessionID string) (session.Snapshot, error) {
	var snapshot session.Snapshot
	err := r.runStage(ctx, &middleware.Envelope{
		Stage:     middleware.StageMemory,
		Operation: "load",
		Request:   sessionID,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		next, runErr := r.storage.Sessions().Load(ctx, sessionID)
		if runErr != nil {
			return runErr
		}
		snapshot = next
		envelope.TeamID = next.Session.TeamID
		envelope.AgentID = next.Session.AgentID
		envelope.Response = next
		return nil
	})
	return snapshot, err
}

func (r *Runtime) appendSessionMessages(ctx context.Context, sessionID string, messages ...message.Message) ([]session.Entry, error) {
	var entries []session.Entry
	err := r.runStage(ctx, &middleware.Envelope{
		Stage:     middleware.StageMemory,
		Operation: "append",
		Request:   messages,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		next, runErr := r.storage.Sessions().Append(ctx, sessionID, messages...)
		if runErr != nil {
			return runErr
		}
		entries = next
		envelope.Response = next
		return nil
	})
	return entries, err
}

func (r *Runtime) runStage(ctx context.Context, envelope *middleware.Envelope, next middleware.Next) error {
	if envelope == nil {
		envelope = &middleware.Envelope{}
	}
	finalReached := false
	err := r.middlewares.Handle(ctx, envelope, func(ctx context.Context, envelope *middleware.Envelope) error {
		finalReached = true
		return next(ctx, envelope)
	})
	if err != nil && !finalReached {
		r.RecordPolicyOutcome(ctx, storage.PolicyOutcomeEvent{
			SchemaVersion: storage.PolicyOutcomeEventSchemaVersion,
			Layer:         "stage",
			Stage:         string(envelope.Stage),
			Operation:     envelope.Operation,
			Action:        "block",
			Policy:        "stage.middleware",
			Outcome:       "blocked",
			Severity:      "error",
			Message:       err.Error(),
			Blocking:      true,
			RunID:         envelope.Metadata["runId"],
			TeamID:        envelope.TeamID,
			TaskID:        envelope.TaskID,
			AgentID:       envelope.AgentID,
			Reference:     string(envelope.Stage),
			Timestamp:     time.Now().UTC(),
			Evidence: &storage.PolicyOutcomeEvidence{
				Metadata: cloneStringMap(envelope.Metadata),
			},
		})
	}
	return err
}

func phaseStage(phase team.Phase) middleware.Stage {
	switch phase {
	case team.PhasePlanning:
		return middleware.StagePlanner
	case team.PhaseVerify:
		return middleware.StageVerify
	case team.PhaseSynthesize:
		return middleware.StageSynthesize
	default:
		return middleware.StageTeam
	}
}

func runnableTaskSet(state team.RunState) ([]team.Task, map[string]struct{}) {
	runnable := state.RunnableTasks()
	items := make(map[string]struct{}, len(runnable))
	for _, task := range runnable {
		items[task.ID] = struct{}{}
	}
	return runnable, items
}
