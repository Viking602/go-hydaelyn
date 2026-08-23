package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Viking602/venat/api"
)

type handoffStore UnitOfWork

type teamStateStore UnitOfWork

type agentInstanceStore UnitOfWork

func (s *handoffStore) uow() *UnitOfWork       { return (*UnitOfWork)(s) }
func (s *teamStateStore) uow() *UnitOfWork     { return (*UnitOfWork)(s) }
func (s *agentInstanceStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func handoffKey(runID, handoffID string) string { return runID + "|" + handoffID }

// HandoffStore

func (s *handoffStore) SaveHandoff(_ context.Context, record api.HandoffRecord) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.RunID) == "" {
		return fmt.Errorf("handoff ID and run ID required: %w", api.ErrInvalidCommand)
	}
	key := handoffKey(record.RunID, record.ID)
	if _, exists := u.staged.Handoffs[key]; exists {
		return fmt.Errorf("handoff %s already recorded for run %s (append-only store): %w", record.ID, record.RunID, api.ErrInvalidCommand)
	}
	u.staged.Handoffs[key] = record
	return nil
}

func (s *handoffStore) LoadHandoff(_ context.Context, runID, handoffID string) (api.HandoffRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return api.HandoffRecord{}, err
	}
	record, ok := u.staged.Handoffs[handoffKey(runID, handoffID)]
	if !ok {
		return api.HandoffRecord{}, api.ErrNotFound
	}
	return record, nil
}

func (s *handoffStore) ListHandoffs(_ context.Context, sel api.HandoffSelector) ([]api.HandoffRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	matches := func(record api.HandoffRecord) bool {
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
	out := make([]api.HandoffRecord, 0)
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

func (s *teamStateStore) SaveTeamState(_ context.Context, record api.TeamStateRecord) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(record.RunID) == "" {
		return fmt.Errorf("team state run ID required: %w", api.ErrInvalidCommand)
	}
	u.staged.TeamStates[record.RunID] = record
	return nil
}

func (s *teamStateStore) LoadTeamState(_ context.Context, runID string) (api.TeamStateRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return api.TeamStateRecord{}, err
	}
	record, ok := u.staged.TeamStates[runID]
	if !ok {
		return api.TeamStateRecord{}, api.ErrNotFound
	}
	return record, nil
}

// AgentInstanceStore

func (s *agentInstanceStore) SaveAgentInstance(_ context.Context, record api.AgentInstanceRecord) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("agent instance ID required: %w", api.ErrInvalidCommand)
	}
	u.staged.AgentInstances[record.ID] = record
	return nil
}

func (s *agentInstanceStore) LoadAgentInstance(_ context.Context, id string) (api.AgentInstanceRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return api.AgentInstanceRecord{}, err
	}
	record, ok := u.staged.AgentInstances[id]
	if !ok {
		return api.AgentInstanceRecord{}, api.ErrNotFound
	}
	return record, nil
}

func (s *agentInstanceStore) ListAgentInstances(_ context.Context, sel api.AgentInstanceSelector) ([]api.AgentInstanceRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	out := make([]api.AgentInstanceRecord, 0)
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
