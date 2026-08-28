package agent

import (
	"context"
	"errors"
	"time"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/session"
)

// Prompt appends msgs to the main lane and drives the run to a terminal
// outcome. Only one drive may be in flight; concurrent callers get ErrLaneBusy.
func (h *Harness) Prompt(ctx context.Context, msgs ...message.Message) (RunOutcome, error) {
	h.lifecycleMu.Lock()
	if err := h.entryLocked(); err != nil {
		h.lifecycleMu.Unlock()
		return RunOutcome{}, err
	}
	if len(msgs) == 0 {
		h.lifecycleMu.Unlock()
		return RunOutcome{}, ErrInvalidMessage
	}
	for _, msg := range msgs {
		if msg.Role == "" {
			h.lifecycleMu.Unlock()
			return RunOutcome{}, ErrInvalidMessage
		}
	}
	driveCtx, release := h.beginDriveLocked(ctx)
	h.lifecycleMu.Unlock()

	h.laneMu.Lock()
	op, st, err := h.startRunLocked(driveCtx, msgs)
	h.laneMu.Unlock()
	if err != nil {
		release()
		return RunOutcome{}, err
	}
	defer h.finishDrive(op.OperationID, release)
	return h.drive(driveCtx, op, st)
}

// Resume continues the operation recorded in the store, typically after the
// process that started it went away. It reports ErrNothingToResume when the
// lane is idle.
func (h *Harness) Resume(ctx context.Context) (RunOutcome, error) {
	h.lifecycleMu.Lock()
	if err := h.entryLocked(); err != nil {
		h.lifecycleMu.Unlock()
		return RunOutcome{}, err
	}
	driveCtx, release := h.beginDriveLocked(ctx)
	h.lifecycleMu.Unlock()

	h.laneMu.Lock()
	idle, op, st, err := h.restoreMain(driveCtx)
	h.laneMu.Unlock()
	switch {
	case err != nil:
		release()
		return RunOutcome{}, err
	case idle:
		release()
		return RunOutcome{}, ErrNothingToResume
	}
	defer h.finishDrive(op.OperationID, release)
	return h.drive(driveCtx, op, st)
}

// LastResult returns the terminal record of the most recent operation.
func (h *Harness) LastResult(ctx context.Context) (session.LaneLastResult, bool, error) {
	release, err := h.beginRead()
	if err != nil {
		return session.LaneLastResult{}, false, err
	}
	defer release()
	h.laneMu.Lock()
	defer h.laneMu.Unlock()
	return readReg[session.LaneLastResult](ctx, h, session.NSLaneLastResult, harnessLaneMain)
}

// startRunLocked writes the prompt entries and the operation registers that
// make the run resumable. The caller must hold laneMu and must already have
// claimed the lane.
func (h *Harness) startRunLocked(ctx context.Context, msgs []message.Message) (Operation, RunState, error) {
	state, stateReg, err := h.loadLaneStateRecord(ctx)
	if err != nil {
		return Operation{}, RunState{}, err
	}
	if state.CurrentOperationID != "" {
		return Operation{}, RunState{}, ErrLaneBusy
	}
	leafID, leafReg, leafExists, err := h.loadLeafRecord(ctx)
	if err != nil {
		return Operation{}, RunState{}, err
	}

	opID := h.sess.IDs().Next()
	parent := leafID
	promptIDs := make([]string, 0, len(msgs))
	writes := make([]session.Write, 0, len(msgs)+4)
	var lastID string
	for _, msg := range msgs {
		id := h.sess.IDs().Next()
		msg.ID = id
		writes = append(writes, session.InsertEntry{Entry: session.Entry{
			ID:       id,
			ParentID: parent,
			Type:     session.EntryMessage,
			Message:  msg,
		}})
		promptIDs = append(promptIDs, id)
		parent = id
		lastID = id
	}
	op := Operation{
		OperationID:  opID,
		Lane:         harnessLaneMain,
		SourceLeafID: leafID,
		Intent:       OperationIntent{Kind: harnessKindRun, PromptEntryIDs: promptIDs},
	}
	st := RunState{
		Kind: harnessKindRun,
		Phase: RunPhase{
			Kind:           harnessPhaseCheckpoint,
			Continuation:   harnessNeedAssistant,
			TriggerEntryID: lastID,
		},
	}
	meta, err := setReg(session.NSOpMeta, opID, op)
	if err != nil {
		return Operation{}, RunState{}, h.fault(err)
	}
	meta.CompareSeq = true
	stateWrite, err := setReg(session.NSOpState, opID, st)
	if err != nil {
		return Operation{}, RunState{}, h.fault(err)
	}
	stateWrite.CompareSeq = true
	leafWrite, err := setReg(session.NSLaneLeaf, harnessLaneMain, lastID)
	if err != nil {
		return Operation{}, RunState{}, h.fault(err)
	}
	leafWrite.CompareSeq = true
	if leafExists {
		leafWrite.ExpectedSeq = leafReg.Seq
	}
	state.CurrentOperationID = opID
	state.OwnerID = h.ownerID
	state.LeaseExpiresAt = time.Now().Add(h.opts.LeaseTTL).UnixMilli()
	laneWrite, err := setReg(session.NSLaneState, harnessLaneMain, state)
	if err != nil {
		return Operation{}, RunState{}, h.fault(err)
	}
	laneWrite.CompareSeq = true
	laneWrite.ExpectedSeq = stateReg.Seq
	writes = append(writes, meta, stateWrite, leafWrite, laneWrite)
	if err := h.commit(ctx, writes); err != nil {
		if errors.Is(err, session.ErrConflict) {
			return Operation{}, RunState{}, ErrLaneBusy
		}
		return Operation{}, RunState{}, err
	}
	return op, st, nil
}
