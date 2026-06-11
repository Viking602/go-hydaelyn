package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

type handoffStore UnitOfWork

type teamStateStore UnitOfWork

type agentInstanceStore UnitOfWork

func (s *handoffStore) uow() *UnitOfWork       { return (*UnitOfWork)(s) }
func (s *teamStateStore) uow() *UnitOfWork     { return (*UnitOfWork)(s) }
func (s *agentInstanceStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func handoffKey(runID, handoffID string) string { return runID + "|" + handoffID }

// HandoffStore

func (s *handoffStore) SaveHandoff(_ context.Context, record model.HandoffRecord) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.RunID) == "" {
		return fmt.Errorf("handoff ID and run ID required: %w", model.ErrInvalidCommand)
	}
	key := handoffKey(record.RunID, record.ID)
	if _, exists := u.staged.Handoffs[key]; exists {
		return fmt.Errorf("handoff %s already recorded for run %s (append-only store): %w", record.ID, record.RunID, model.ErrInvalidCommand)
	}
	u.staged.Handoffs[key] = record
	return nil
}

func (s *handoffStore) LoadHandoff(_ context.Context, runID, handoffID string) (model.HandoffRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.HandoffRecord{}, err
	}
	record, ok := u.staged.Handoffs[handoffKey(runID, handoffID)]
	if !ok {
		return model.HandoffRecord{}, model.ErrNotFound
	}
	return record, nil
}

func (s *handoffStore) ListHandoffs(_ context.Context, sel model.HandoffSelector) ([]model.HandoffRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	matches := func(record model.HandoffRecord) bool {
		if sel.RunID != "" && record.RunID != sel.RunID {
			return false
		}
		if sel.From != "" && record.From != sel.From {
			return false
		}
		if sel.To != "" && record.To != sel.To {
			return false
		}
		if !sel.Since.IsZero() && record.CreatedAt.Before(sel.Since) {
			return false
		}
		return true
	}
	out := make([]model.HandoffRecord, 0)
	for _, record := range u.staged.Handoffs {
		if matches(record) {
			out = append(out, record)
		}
	}
	// Spec 07 §"HandoffStore": List MUST return handoffs in ID-ascending
	// order (scheduler-derived ULIDs ⇒ wall-clock monotonic), independent
	// of persistence order — globally, not per run, so cross-run listings
	// stay in time order too. RunID only breaks exact-ID ties.
	sort.Slice(out, func(a, b int) bool {
		if out[a].ID != out[b].ID {
			return out[a].ID < out[b].ID
		}
		return out[a].RunID < out[b].RunID
	})
	return out, nil
}

// TeamStateStore

func (s *teamStateStore) SaveTeamState(_ context.Context, record model.TeamStateRecord) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(record.RunID) == "" {
		return fmt.Errorf("team state run ID required: %w", model.ErrInvalidCommand)
	}
	u.staged.TeamStates[record.RunID] = record
	return nil
}

func (s *teamStateStore) LoadTeamState(_ context.Context, runID string) (model.TeamStateRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.TeamStateRecord{}, err
	}
	record, ok := u.staged.TeamStates[runID]
	if !ok {
		return model.TeamStateRecord{}, model.ErrNotFound
	}
	return record, nil
}

// AgentInstanceStore

func (s *agentInstanceStore) SaveAgentInstance(_ context.Context, record model.AgentInstanceRecord) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("agent instance ID required: %w", model.ErrInvalidCommand)
	}
	u.staged.AgentInstances[record.ID] = record
	return nil
}

func (s *agentInstanceStore) LoadAgentInstance(_ context.Context, id string) (model.AgentInstanceRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.AgentInstanceRecord{}, err
	}
	record, ok := u.staged.AgentInstances[id]
	if !ok {
		return model.AgentInstanceRecord{}, model.ErrNotFound
	}
	return record, nil
}

func (s *agentInstanceStore) ListAgentInstances(_ context.Context, sel model.AgentInstanceSelector) ([]model.AgentInstanceRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	out := make([]model.AgentInstanceRecord, 0)
	for _, record := range u.staged.AgentInstances {
		if sel.RunID != "" && record.RunID != sel.RunID {
			continue
		}
		if sel.ClassName != "" && record.ClassName != sel.ClassName {
			continue
		}
		if sel.State != "" && record.State != sel.State {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}
