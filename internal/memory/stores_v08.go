package memory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Viking602/venat/api"
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

func (s *agentProfileStore) SaveAgentProfile(_ context.Context, profile api.AgentProfile) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if strings.TrimSpace(profile.ID) == "" {
		return fmt.Errorf("agent profile ID required: %w", api.ErrInvalidCommand)
	}
	u.staged.AgentProfiles[profile.ID] = profile
	return nil
}

func (s *agentProfileStore) LoadAgentProfile(_ context.Context, id string) (api.AgentProfile, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return api.AgentProfile{}, err
	}
	profile, ok := u.staged.AgentProfiles[id]
	if !ok {
		return api.AgentProfile{}, api.ErrNotFound
	}
	return profile, nil
}

func (s *agentProfileStore) ListAgentProfiles(_ context.Context, sel api.AgentSelector) ([]api.AgentProfile, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	var out []api.AgentProfile
	for _, profile := range u.staged.AgentProfiles {
		if !matchAgentSelector(profile, sel) {
			continue
		}
		out = append(out, profile)
		if sel.Limit > 0 && len(out) >= sel.Limit {
			break
		}
	}
	slices.SortFunc(out, func(a, b api.AgentProfile) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func matchAgentSelector(p api.AgentProfile, sel api.AgentSelector) bool {
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

func (s *capabilityStore) SaveCapability(_ context.Context, capability api.Capability) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if err := api.ValidateCapabilityName(capability.Name); err != nil {
		return err
	}
	u.staged.Capabilities[capabilityKey(capability.Name, capability.AgentID)] = capability
	return nil
}

func (s *capabilityStore) LoadCapability(_ context.Context, name string, agentID string) (api.Capability, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return api.Capability{}, err
	}
	capability, ok := u.staged.Capabilities[capabilityKey(name, agentID)]
	if !ok {
		return api.Capability{}, api.ErrNotFound
	}
	return capability, nil
}

func (s *capabilityStore) ListCapabilities(_ context.Context, sel api.CapabilitySelector) ([]api.Capability, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	var out []api.Capability
	for _, cap := range u.staged.Capabilities {
		if !matchCapabilitySelector(cap, sel) {
			continue
		}
		out = append(out, cap)
		if sel.Limit > 0 && len(out) >= sel.Limit {
			break
		}
	}
	slices.SortFunc(out, func(a, b api.Capability) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.AgentID, b.AgentID)
	})
	return out, nil
}

func matchCapabilitySelector(c api.Capability, sel api.CapabilitySelector) bool {
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

func (s *agentDefinitionStore) SaveAgentDefinitionSnapshot(_ context.Context, snapshot api.AgentDefinitionSnapshot) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	definitionID := strings.TrimSpace(snapshot.Definition.ID)
	version := strings.TrimSpace(snapshot.Definition.Version)
	if definitionID == "" || version == "" {
		return fmt.Errorf("agent definition ID and version required: %w", api.ErrInvalidCommand)
	}
	payload, err := json.Marshal(snapshot.Definition)
	if err != nil {
		return fmt.Errorf("encode agent definition: %w", err)
	}
	sum := sha256.Sum256(payload)
	if snapshot.Digest != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("agent definition digest mismatch: %w", api.ErrInvalidCommand)
	}
	key := agentDefinitionKey(definitionID, version)
	if existing, ok := u.staged.AgentDefinitions[key]; ok {
		existingPayload, marshalErr := json.Marshal(existing.Definition)
		if marshalErr != nil {
			return fmt.Errorf("encode stored agent definition: %w", marshalErr)
		}
		if existing.Digest == snapshot.Digest && bytes.Equal(existingPayload, payload) {
			return nil
		}
		return api.ErrDefinitionVersionConflict
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC()
	}
	snapshot.Definition = cloneAgentDefinition(snapshot.Definition)
	u.staged.AgentDefinitions[key] = snapshot
	return nil
}

func (s *agentDefinitionStore) LoadAgentDefinitionSnapshot(_ context.Context, definitionID, version string) (api.AgentDefinitionSnapshot, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return api.AgentDefinitionSnapshot{}, err
	}
	snapshot, ok := u.staged.AgentDefinitions[agentDefinitionKey(definitionID, version)]
	if !ok {
		return api.AgentDefinitionSnapshot{}, api.ErrNotFound
	}
	snapshot.Definition = cloneAgentDefinition(snapshot.Definition)
	return snapshot, nil
}

func (s *agentDefinitionStore) ListAgentDefinitionSnapshots(_ context.Context, selector api.AgentDefinitionSnapshotSelector) ([]api.AgentDefinitionSnapshot, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	out := make([]api.AgentDefinitionSnapshot, 0, len(u.staged.AgentDefinitions))
	for _, snapshot := range u.staged.AgentDefinitions {
		if len(selector.DefinitionIDs) > 0 && !slices.Contains(selector.DefinitionIDs, snapshot.Definition.ID) {
			continue
		}
		if len(selector.Versions) > 0 && !slices.Contains(selector.Versions, snapshot.Definition.Version) {
			continue
		}
		if !selector.Since.IsZero() && snapshot.CreatedAt.Before(selector.Since) {
			continue
		}
		snapshot.Definition = cloneAgentDefinition(snapshot.Definition)
		out = append(out, snapshot)
	}
	slices.SortFunc(out, func(a, b api.AgentDefinitionSnapshot) int {
		if byID := strings.Compare(a.Definition.ID, b.Definition.ID); byID != 0 {
			return byID
		}
		return strings.Compare(a.Definition.Version, b.Definition.Version)
	})
	if selector.Limit > 0 && len(out) > selector.Limit {
		out = out[:selector.Limit]
	}
	return out, nil
}

// UsageStore

func (s *usageStore) AppendUsage(_ context.Context, rec api.UsageRecord) error {
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
		return api.ErrIdempotencyConflict
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	u.staged.UsageRecords[rec.ID] = rec
	return nil
}

func normalizeUsageRecord(rec api.UsageRecord) api.UsageRecord {
	if rec.Kind == "" {
		rec.Kind = api.UsageKindLegacyExecution
	}
	return rec
}

func (s *usageStore) QueryUsage(_ context.Context, sel api.UsageSelector) ([]api.UsageRecord, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	var out []api.UsageRecord
	for _, rec := range u.staged.UsageRecords {
		if !matchUsageSelector(rec, sel) {
			continue
		}
		out = append(out, rec)
	}
	slices.SortFunc(out, func(a, b api.UsageRecord) int {
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

func (s *usageStore) SumCredits(ctx context.Context, sel api.UsageSelector) (int64, error) {
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

func matchUsageSelector(r api.UsageRecord, sel api.UsageSelector) bool {
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

func (s *deadLetterStore) AppendDeadLetter(_ context.Context, entry api.DeadLetterEntry) error {
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

func (s *deadLetterStore) ListDeadLetters(_ context.Context, sel api.DeadLetterSelector) ([]api.DeadLetterEntry, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	var out []api.DeadLetterEntry
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
	slices.SortFunc(out, func(a, b api.DeadLetterEntry) int {
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
	return fmt.Errorf("memory provider: dead-letter requeue not supported: %w", api.ErrInvalidCommand)
}
