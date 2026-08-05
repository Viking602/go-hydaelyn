package memory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Viking602/venat/internal/core/model"
)

type agentProfileStore UnitOfWork

type capabilityStore UnitOfWork

type (
	agentDefinitionStore UnitOfWork
	usageStore           UnitOfWork
)

type deadLetterStore UnitOfWork

func (s *agentProfileStore) uow() *UnitOfWork    { return (*UnitOfWork)(s) }
func (s *capabilityStore) uow() *UnitOfWork      { return (*UnitOfWork)(s) }
func (s *usageStore) uow() *UnitOfWork           { return (*UnitOfWork)(s) }
func (s *agentDefinitionStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }
func (s *deadLetterStore) uow() *UnitOfWork      { return (*UnitOfWork)(s) }

// AgentProfileStore

func (s *agentProfileStore) SaveAgentProfile(_ context.Context, profile model.AgentProfile) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(profile.ID) == "" {
		return fmt.Errorf("agent profile ID required: %w", model.ErrInvalidCommand)
	}
	u.staged.AgentProfiles[profile.ID] = profile
	return nil
}

func (s *agentProfileStore) LoadAgentProfile(_ context.Context, id string) (model.AgentProfile, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.AgentProfile{}, err
	}
	profile, ok := u.staged.AgentProfiles[id]
	if !ok {
		return model.AgentProfile{}, model.ErrNotFound
	}
	return profile, nil
}

func (s *agentProfileStore) ListAgentProfiles(_ context.Context, sel model.AgentSelector) ([]model.AgentProfile, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	var out []model.AgentProfile
	for _, profile := range u.staged.AgentProfiles {
		if !matchAgentSelector(profile, sel) {
			continue
		}
		out = append(out, profile)
		if sel.Limit > 0 && len(out) >= sel.Limit {
			break
		}
	}
	slices.SortFunc(out, func(a, b model.AgentProfile) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func matchAgentSelector(p model.AgentProfile, sel model.AgentSelector) bool {
	if len(sel.IDs) > 0 && !slices.Contains(sel.IDs, p.ID) {
		return false
	}
	if len(sel.Roles) > 0 && !slices.Contains(sel.Roles, p.Role) {
		return false
	}
	if len(sel.Groups) > 0 {
		matched := false
		for _, g := range sel.Groups {
			if slices.Contains(p.Groups, g) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// CapabilityStore

func capabilityKey(name, agentID string) string { return name + "|" + agentID }

func (s *capabilityStore) SaveCapability(_ context.Context, capability model.Capability) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(capability.Name) == "" {
		return fmt.Errorf("capability name required: %w", model.ErrInvalidCommand)
	}
	u.staged.Capabilities[capabilityKey(capability.Name, capability.AgentID)] = capability
	return nil
}

func (s *capabilityStore) LoadCapability(_ context.Context, name string, agentID string) (model.Capability, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.Capability{}, err
	}
	capability, ok := u.staged.Capabilities[capabilityKey(name, agentID)]
	if !ok {
		return model.Capability{}, model.ErrNotFound
	}
	return capability, nil
}

func (s *capabilityStore) ListCapabilities(_ context.Context, sel model.CapabilitySelector) ([]model.Capability, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	var out []model.Capability
	for _, cap := range u.staged.Capabilities {
		if !matchCapabilitySelector(cap, sel) {
			continue
		}
		out = append(out, cap)
		if sel.Limit > 0 && len(out) >= sel.Limit {
			break
		}
	}
	slices.SortFunc(out, func(a, b model.Capability) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.AgentID, b.AgentID)
	})
	return out, nil
}

func matchCapabilitySelector(c model.Capability, sel model.CapabilitySelector) bool {
	if len(sel.Names) > 0 && !slices.Contains(sel.Names, c.Name) {
		return false
	}
	if len(sel.AgentIDs) > 0 && !slices.Contains(sel.AgentIDs, c.AgentID) {
		return false
	}
	if len(sel.Tags) > 0 {
		matched := false
		for _, t := range sel.Tags {
			if slices.Contains(c.Tags, t) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// AgentDefinitionStore

func agentDefinitionKey(definitionID, version string) string {
	return definitionID + "\x00" + version
}

func (s *agentDefinitionStore) SaveAgentDefinitionSnapshot(_ context.Context, snapshot model.AgentDefinitionSnapshot) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(snapshot.DefinitionID) == "" || strings.TrimSpace(snapshot.Version) == "" {
		return fmt.Errorf("agent definition ID and version required: %w", model.ErrInvalidCommand)
	}
	if len(snapshot.Definition) == 0 {
		return fmt.Errorf("agent definition payload required: %w", model.ErrInvalidCommand)
	}
	sum := sha256.Sum256(snapshot.Definition)
	if snapshot.Digest != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("agent definition digest mismatch: %w", model.ErrInvalidCommand)
	}
	key := agentDefinitionKey(snapshot.DefinitionID, snapshot.Version)
	if existing, ok := u.staged.AgentDefinitions[key]; ok {
		if existing.Digest == snapshot.Digest && bytes.Equal(existing.Definition, snapshot.Definition) {
			return nil
		}
		return model.ErrDefinitionVersionConflict
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC()
	}
	snapshot.Definition = append([]byte(nil), snapshot.Definition...)
	u.staged.AgentDefinitions[key] = snapshot
	return nil
}

func (s *agentDefinitionStore) LoadAgentDefinitionSnapshot(_ context.Context, definitionID, version string) (model.AgentDefinitionSnapshot, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.AgentDefinitionSnapshot{}, err
	}
	snapshot, ok := u.staged.AgentDefinitions[agentDefinitionKey(definitionID, version)]
	if !ok {
		return model.AgentDefinitionSnapshot{}, model.ErrNotFound
	}
	snapshot.Definition = append([]byte(nil), snapshot.Definition...)
	return snapshot, nil
}

func (s *agentDefinitionStore) ListAgentDefinitionSnapshots(_ context.Context, selector model.AgentDefinitionSnapshotSelector) ([]model.AgentDefinitionSnapshot, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	out := make([]model.AgentDefinitionSnapshot, 0, len(u.staged.AgentDefinitions))
	for _, snapshot := range u.staged.AgentDefinitions {
		if len(selector.DefinitionIDs) > 0 && !slices.Contains(selector.DefinitionIDs, snapshot.DefinitionID) {
			continue
		}
		if len(selector.Versions) > 0 && !slices.Contains(selector.Versions, snapshot.Version) {
			continue
		}
		if !selector.Since.IsZero() && snapshot.CreatedAt.Before(selector.Since) {
			continue
		}
		snapshot.Definition = append([]byte(nil), snapshot.Definition...)
		out = append(out, snapshot)
	}
	slices.SortFunc(out, func(a, b model.AgentDefinitionSnapshot) int {
		if byID := strings.Compare(a.DefinitionID, b.DefinitionID); byID != 0 {
			return byID
		}
		return strings.Compare(a.Version, b.Version)
	})
	if selector.Limit > 0 && len(out) > selector.Limit {
		out = out[:selector.Limit]
	}
	return out, nil
}

// UsageStore

func (s *usageStore) AppendUsage(_ context.Context, rec model.UsageRecord) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if rec.ID == "" {
		rec.ID = u.nextID("usage")
	}
	rec = normalizeUsageRecord(rec)
	if existing, ok := u.staged.UsageRecords[rec.ID]; ok {
		existing = normalizeUsageRecord(existing)
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = existing.CreatedAt
		}
		if reflect.DeepEqual(existing, rec) {
			return nil
		}
		return model.ErrIdempotencyConflict
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	u.staged.UsageRecords[rec.ID] = rec
	return nil
}

func normalizeUsageRecord(rec model.UsageRecord) model.UsageRecord {
	if rec.Kind == "" {
		rec.Kind = model.UsageKindLegacyExecution
	}
	return rec
}

func (s *usageStore) QueryUsage(_ context.Context, sel model.UsageSelector) ([]model.UsageRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	var out []model.UsageRecord
	for _, rec := range u.staged.UsageRecords {
		if !matchUsageSelector(rec, sel) {
			continue
		}
		out = append(out, rec)
	}
	slices.SortFunc(out, func(a, b model.UsageRecord) int {
		if a.CreatedAt.Before(b.CreatedAt) {
			return -1
		}
		if a.CreatedAt.After(b.CreatedAt) {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	if sel.Limit > 0 && len(out) > sel.Limit {
		out = out[:sel.Limit]
	}
	return out, nil
}

func (s *usageStore) SumCredits(ctx context.Context, sel model.UsageSelector) (int64, error) {
	sel.Limit = 0
	records, err := s.QueryUsage(ctx, sel)
	if err != nil {
		return 0, err
	}
	var sum int64
	for _, r := range records {
		sum += r.Credits
	}
	return sum, nil
}

func matchUsageSelector(r model.UsageRecord, sel model.UsageSelector) bool {
	if sel.RunID != "" && r.RunID != sel.RunID {
		return false
	}
	if sel.TaskID != "" && r.TaskID != sel.TaskID {
		return false
	}
	if sel.AgentID != "" && r.AgentID != sel.AgentID {
		return false
	}
	if sel.Kind != "" && normalizeUsageRecord(r).Kind != sel.Kind {
		return false
	}
	if sel.Provider != "" && r.Provider != sel.Provider {
		return false
	}
	if sel.ToolName != "" && r.ToolName != sel.ToolName {
		return false
	}
	if !sel.Since.IsZero() && r.CreatedAt.Before(sel.Since) {
		return false
	}
	if !sel.Until.IsZero() && r.CreatedAt.After(sel.Until) {
		return false
	}
	return true
}

// DeadLetterStore

func (s *deadLetterStore) AppendDeadLetter(_ context.Context, entry model.DeadLetterEntry) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if entry.ID == "" {
		entry.ID = u.nextID("deadletter")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	u.staged.DeadLetters[entry.ID] = entry
	return nil
}

func (s *deadLetterStore) ListDeadLetters(_ context.Context, sel model.DeadLetterSelector) ([]model.DeadLetterEntry, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	var out []model.DeadLetterEntry
	for _, e := range u.staged.DeadLetters {
		if sel.RunID != "" && e.RunID != sel.RunID {
			continue
		}
		if sel.TaskID != "" && e.TaskID != sel.TaskID {
			continue
		}
		if !sel.Since.IsZero() && e.CreatedAt.Before(sel.Since) {
			continue
		}
		if !sel.Until.IsZero() && e.CreatedAt.After(sel.Until) {
			continue
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b model.DeadLetterEntry) int {
		if a.CreatedAt.Before(b.CreatedAt) {
			return -1
		}
		if a.CreatedAt.After(b.CreatedAt) {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	if sel.Limit > 0 && len(out) > sel.Limit {
		out = out[:sel.Limit]
	}
	return out, nil
}

// Requeue is not supported by the memory provider — it reports
// SupportsDeadLetterRequeue=false in StoreCapabilities. Returning an error
// here is the contracted behavior for providers that don't implement
// re-queue.
func (s *deadLetterStore) Requeue(context.Context, string) error {
	return fmt.Errorf("memory provider: dead-letter requeue not supported: %w", model.ErrInvalidCommand)
}
