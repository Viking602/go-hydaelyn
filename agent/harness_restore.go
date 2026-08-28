package agent

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/session"
)

// restoreMain rebuilds the in-flight operation from the store. A read that
// fails transiently is returned as-is so the caller can retry; only state that
// contradicts the session invariants retires the harness. Callers must hold
// laneMu.
func (h *Harness) restoreMain(ctx context.Context) (bool, Operation, RunState, error) {
	state, stateReg, leafID, err := h.loadLaneRegisters(ctx)
	if err != nil {
		return false, Operation{}, RunState{}, err
	}
	if state.CurrentOperationID == "" {
		if state.OwnerID != "" || state.LeaseExpiresAt != 0 {
			return false, Operation{}, RunState{}, h.fault(fmt.Errorf("idle lane retains an owner lease"))
		}
		// An idle lane still has to have an intact leaf before it can be
		// prompted again.
		if err := h.lookupEntries(ctx, newEntryRefs(leafID)); err != nil {
			return false, Operation{}, RunState{}, err
		}
		return true, Operation{}, RunState{}, nil
	}
	if err := h.claimLane(ctx, state, stateReg); err != nil {
		return false, Operation{}, RunState{}, err
	}
	op, st, err := h.loadOperation(ctx, state.CurrentOperationID)
	if err != nil {
		h.relinquishLane(state.CurrentOperationID)
		return false, Operation{}, RunState{}, err
	}
	if err := h.lookupEntries(ctx, operationRefs(leafID, op, st)); err != nil {
		h.relinquishLane(state.CurrentOperationID)
		return false, Operation{}, RunState{}, err
	}
	if err := h.validateOperationState(ctx, leafID, op, st); err != nil {
		h.relinquishLane(state.CurrentOperationID)
		return false, Operation{}, RunState{}, err
	}
	return false, op, st, nil
}

// loadLaneRegisters reads the lane registers EnsureMain guarantees. An absent
// config or state register means the session was tampered with, not that the
// lane is empty; only the leaf is legitimately absent before the first prompt.
func (h *Harness) loadLaneRegisters(ctx context.Context) (session.LaneState, session.Register, string, error) {
	_, ok, err := readReg[session.LaneConfiguration](ctx, h, session.NSLaneConfig, harnessLaneMain)
	if err != nil {
		return session.LaneState{}, session.Register{}, "", err
	}
	if !ok {
		return session.LaneState{}, session.Register{}, "", h.fault(missingRegister(session.NSLaneConfig, harnessLaneMain))
	}
	state, stateReg, err := h.loadLaneStateRecord(ctx)
	if err != nil {
		return session.LaneState{}, session.Register{}, "", err
	}
	leafID, err := h.loadLeaf(ctx)
	if err != nil {
		return session.LaneState{}, session.Register{}, "", err
	}
	return state, stateReg, leafID, nil
}

// loadOperation reads the registers describing the operation the lane state
// points at. Anything missing or mistyped there is a dangling pointer, so it
// retires the harness rather than reporting an empty lane.
func (h *Harness) loadOperation(ctx context.Context, operationID string) (Operation, RunState, error) {
	op, ok, err := readReg[Operation](ctx, h, session.NSOpMeta, operationID)
	if err != nil {
		return Operation{}, RunState{}, err
	}
	if !ok {
		return Operation{}, RunState{}, h.fault(missingRegister(session.NSOpMeta, operationID))
	}
	st, ok, err := readReg[RunState](ctx, h, session.NSOpState, operationID)
	if err != nil {
		return Operation{}, RunState{}, err
	}
	if !ok {
		return Operation{}, RunState{}, h.fault(missingRegister(session.NSOpState, operationID))
	}
	if op.OperationID != operationID || op.Lane != harnessLaneMain || op.Intent.Kind != harnessKindRun || st.Kind != harnessKindRun {
		return Operation{}, RunState{}, h.fault(fmt.Errorf("operation %s does not describe its main-lane run", operationID))
	}
	return op, st, nil
}

// entryRefs collects the entries a restore has to validate: every id the
// registers point at, which of those must already exist, and the reserved
// response id, which is checked only if it happens to be there.
type entryRefs struct {
	ids        []string
	required   map[string]struct{}
	responseID string
}

func newEntryRefs(leafID string) entryRefs {
	refs := entryRefs{required: map[string]struct{}{}}
	refs.add(leafID, true)
	return refs
}

func (r *entryRefs) add(id string, required bool) {
	if id == "" {
		return
	}
	r.ids = append(r.ids, id)
	if required {
		r.required[id] = struct{}{}
	}
}

func operationRefs(leafID string, op Operation, st RunState) entryRefs {
	refs := newEntryRefs(leafID)
	refs.add(op.SourceLeafID, true)
	for _, id := range op.Intent.PromptEntryIDs {
		refs.add(id, true)
	}
	refs.add(st.Phase.TriggerEntryID, true)
	refs.add(st.LatestAssistantEntryID, true)
	if gen := st.Phase.Generation; gen != nil {
		refs.responseID = gen.ResponseEntryID
		// While the id is only reserved the entry may not have been written
		// yet; once the generation moved past that state it must exist.
		reserved := gen.Status == harnessGenReady || gen.Status == harnessGenEffectPending
		refs.add(gen.ResponseEntryID, !reserved)
	}
	return refs
}

func (h *Harness) lookupEntries(ctx context.Context, refs entryRefs) error {
	if len(refs.ids) == 0 {
		return nil
	}
	entries, err := h.sess.Storage().GetEntries(ctx, refs.ids)
	if err != nil {
		return h.storageErr(err)
	}
	for id := range refs.required {
		entry, ok := entries[id]
		if !ok {
			return h.fault(fmt.Errorf("register references missing entry %s", id))
		}
		if entry.Type != session.EntryMessage {
			return h.fault(fmt.Errorf("entry %s is not a message", id))
		}
	}
	if refs.responseID != "" {
		if entry, ok := entries[refs.responseID]; ok && entry.Type != session.EntryMessage {
			return h.fault(fmt.Errorf("response entry %s is not a message", refs.responseID))
		}
	}
	return nil
}

func (h *Harness) validateOperationState(ctx context.Context, leafID string, op Operation, st RunState) error {
	lastPrompt, err := h.validateOperationPhase(ctx, leafID, op, st)
	if err != nil {
		return err
	}
	return h.validatePromptAncestry(ctx, leafID, op, lastPrompt)
}

func (h *Harness) validateOperationPhase(ctx context.Context, leafID string, op Operation, st RunState) (string, error) {
	if len(op.Intent.PromptEntryIDs) == 0 {
		return "", h.fault(fmt.Errorf("operation %s has no prompt entries", op.OperationID))
	}
	lastPrompt := op.Intent.PromptEntryIDs[len(op.Intent.PromptEntryIDs)-1]
	if st.Phase.TriggerEntryID != lastPrompt {
		return "", h.fault(fmt.Errorf("operation %s trigger does not match its last prompt", op.OperationID))
	}
	switch {
	case st.Phase.Kind == harnessPhaseCheckpoint && st.Phase.Continuation == harnessNeedAssistant:
		if st.Phase.Generation != nil || st.LatestAssistantEntryID != "" || leafID != lastPrompt {
			return "", h.fault(fmt.Errorf("operation %s has an invalid initial checkpoint", op.OperationID))
		}
	case st.Phase.Kind == harnessPhaseCheckpoint && st.Phase.Continuation == harnessMayFinish:
		if st.Phase.Generation != nil || st.LatestAssistantEntryID == "" || leafID != st.LatestAssistantEntryID {
			return "", h.fault(fmt.Errorf("operation %s has an invalid terminal checkpoint", op.OperationID))
		}
	case st.Phase.Kind == harnessPhaseAssistant && st.Phase.Continuation == "" && st.Phase.Generation != nil:
		if st.LatestAssistantEntryID != "" {
			return "", h.fault(fmt.Errorf("operation %s has a terminal assistant during generation", op.OperationID))
		}
		if err := h.validateGenerationState(ctx, leafID, op.OperationID, *st.Phase.Generation); err != nil {
			return "", err
		}
	default:
		return "", h.fault(fmt.Errorf("operation %s has unreachable phase %q/%q", op.OperationID, st.Phase.Kind, st.Phase.Continuation))
	}
	return lastPrompt, nil
}

func (h *Harness) validatePromptAncestry(ctx context.Context, leafID string, op Operation, _ string) error {
	branch, err := h.sess.Storage().ScanBranch(ctx, leafID)
	if err != nil {
		return h.storageErr(err)
	}
	entries := make(map[string]session.Entry, len(branch))
	for _, entry := range branch {
		if _, duplicate := entries[entry.ID]; duplicate {
			return h.fault(fmt.Errorf("operation %s branch repeats entry %s", op.OperationID, entry.ID))
		}
		entries[entry.ID] = entry
	}
	expectedParent := op.SourceLeafID
	for _, promptID := range op.Intent.PromptEntryIDs {
		entry, ok := entries[promptID]
		if !ok || entry.ParentID != expectedParent {
			return h.fault(fmt.Errorf("operation %s prompt chain is broken at %s", op.OperationID, promptID))
		}
		expectedParent = promptID
	}
	if op.SourceLeafID != "" {
		if _, ok := entries[op.SourceLeafID]; !ok {
			return h.fault(fmt.Errorf("operation %s source leaf is not an ancestor", op.OperationID))
		}
	}
	return nil
}

func (h *Harness) validateGenerationState(ctx context.Context, leafID, operationID string, gen Generation) error {
	if gen.Context.Model == "" || gen.Context.MaxAttempts < 1 || gen.NextAttempt < 1 {
		return h.fault(fmt.Errorf("operation %s has invalid generation context", operationID))
	}
	switch gen.Status {
	case harnessGenReady:
		return h.validateReadyGeneration(operationID, gen)
	case harnessGenEffectPending:
		return h.validatePendingGeneration(ctx, operationID, gen)
	case harnessGenRetryWait:
		return h.validateRetryGeneration(ctx, leafID, operationID, gen)
	default:
		return h.fault(fmt.Errorf("operation %s has unknown generation status %q", operationID, gen.Status))
	}
}

func (h *Harness) validateReadyGeneration(operationID string, gen Generation) error {
	if gen.ResponseEntryID != "" || gen.UsageID != "" || gen.NotBefore != 0 {
		return h.fault(fmt.Errorf("operation %s ready generation retains settlement ids", operationID))
	}
	return nil
}

func (h *Harness) validatePendingGeneration(ctx context.Context, operationID string, gen Generation) error {
	if gen.Attempt < 1 || gen.Attempt > gen.Context.MaxAttempts || gen.ResponseEntryID == "" || gen.UsageID == "" {
		return h.fault(fmt.Errorf("operation %s has invalid pending generation", operationID))
	}
	return h.requireGenerationArtifacts(ctx, gen, false)
}

func (h *Harness) validateRetryGeneration(ctx context.Context, leafID, operationID string, gen Generation) error {
	if gen.Attempt < 1 || gen.Attempt >= gen.Context.MaxAttempts || gen.NextAttempt != gen.Attempt+1 ||
		gen.ResponseEntryID == "" || gen.UsageID == "" || gen.NotBefore == 0 || leafID != gen.ResponseEntryID {
		return h.fault(fmt.Errorf("operation %s has invalid retry generation", operationID))
	}
	return h.requireGenerationArtifacts(ctx, gen, true)
}

func (h *Harness) requireGenerationArtifacts(ctx context.Context, gen Generation, required bool) error {
	entries, err := h.sess.Storage().GetEntries(ctx, []string{gen.ResponseEntryID})
	if err != nil {
		return h.storageErr(err)
	}
	usage, err := h.sess.Storage().GetUsage(ctx, []string{gen.UsageID})
	if err != nil {
		return h.storageErr(err)
	}
	entry, hasEntry := entries[gen.ResponseEntryID]
	row, hasUsage := usage[gen.UsageID]
	if !required {
		if hasEntry || hasUsage {
			return h.fault(fmt.Errorf("reserved generation artifacts already exist"))
		}
		return nil
	}
	if !hasEntry || entry.Type != session.EntryMessage || !hasUsage || row.EntryID != entry.ID {
		return h.fault(fmt.Errorf("generation settlement artifacts are incomplete"))
	}
	return nil
}
