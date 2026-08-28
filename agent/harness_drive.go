package agent

import (
	"context"
	"fmt"
	rand "math/rand/v2"
	"time"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/session"
)

// harnessRetryMaxDelay caps the exponential backoff between provider attempts.
const harnessRetryMaxDelay = 30 * time.Second

func (h *Harness) drive(ctx context.Context, op Operation, st RunState) (RunOutcome, error) {
	for {
		switch {
		case st.Phase.Kind == harnessPhaseCheckpoint && st.Phase.Continuation == harnessNeedAssistant:
			next, err := h.commitReady(ctx, op, st)
			if err != nil {
				return RunOutcome{}, err
			}
			st = next
		case st.Phase.Kind == harnessPhaseAssistant && st.generationStatus() == harnessGenReady:
			next, out, done, err := h.runReady(ctx, op, st)
			if err != nil || done {
				return out, err
			}
			st = next
		case st.Phase.Kind == harnessPhaseAssistant && st.generationStatus() == harnessGenEffectPending:
			next, out, done, err := h.recoverUnknownEffect(ctx, op, st)
			if err != nil || done {
				return out, err
			}
			st = next
		case st.Phase.Kind == harnessPhaseAssistant && st.generationStatus() == harnessGenRetryWait:
			next, err := h.waitRetry(ctx, op, st)
			if err != nil {
				return RunOutcome{}, err
			}
			st = next
		case st.Phase.Kind == harnessPhaseCheckpoint && st.Phase.Continuation == harnessMayFinish:
			return h.terminal(ctx, op, st, harnessOutcomeCompleted, nil)
		default:
			return RunOutcome{}, h.lockedFault(fmt.Errorf("agent: unhandled run phase %s/%s", st.Phase.Kind, st.generationStatus()))
		}
	}
}

func (st RunState) generationStatus() string {
	if st.Phase.Generation == nil {
		return ""
	}
	return st.Phase.Generation.Status
}

func (h *Harness) lockedFault(err error) error {
	return h.fault(err)
}

func (h *Harness) commitReady(ctx context.Context, op Operation, st RunState) (RunState, error) {
	h.laneMu.Lock()
	defer h.laneMu.Unlock()
	if err := h.guardOpen(); err != nil {
		return RunState{}, err
	}
	cfg, ok, err := readReg[session.LaneConfiguration](ctx, h, session.NSLaneConfig, harnessLaneMain)
	if err != nil {
		return RunState{}, err
	}
	if !ok {
		return RunState{}, h.fault(missingRegister(session.NSLaneConfig, harnessLaneMain))
	}
	st.Phase.Kind = harnessPhaseAssistant
	st.Phase.Continuation = ""
	st.Phase.Generation = &Generation{
		Status:      harnessGenReady,
		Context:     GenerationContext{Model: cfg.Model, MaxAttempts: h.opts.Retry.MaxAttempts, BaseDelayMs: h.opts.Retry.BaseDelayMs},
		NextAttempt: 1,
	}
	write, err := setReg(session.NSOpState, op.OperationID, st)
	if err != nil {
		return RunState{}, h.fault(err)
	}
	if err := h.commitOwned(ctx, op.OperationID, []session.Write{write}, false); err != nil {
		return RunState{}, err
	}
	return st, nil
}

func (h *Harness) runReady(ctx context.Context, op Operation, st RunState) (RunState, RunOutcome, bool, error) {
	h.laneMu.Lock()
	if err := h.guardOpen(); err != nil {
		h.laneMu.Unlock()
		return RunState{}, RunOutcome{}, true, err
	}
	gen := st.Phase.Generation
	if gen == nil {
		h.laneMu.Unlock()
		return RunState{}, RunOutcome{}, true, h.fault(fmt.Errorf("agent: unhandled run phase %s/%s", st.Phase.Kind, st.generationStatus()))
	}
	// Pin the reserved ids to the trigger entry's millisecond so they sort
	// after it even when the clock has moved backwards since the prompt.
	ms := time.Now().UnixMilli()
	if at, ok := session.TimestampMs(st.Phase.TriggerEntryID); ok {
		ms = at
	}
	rID := h.sess.IDs().Next(ms)
	uID := h.sess.IDs().Next(ms)
	gen.Status = harnessGenEffectPending
	if gen.NextAttempt < 1 {
		gen.NextAttempt = 1
	}
	gen.Attempt = gen.NextAttempt
	gen.ResponseEntryID = rID
	gen.UsageID = uID
	st.Phase.Generation = gen
	write, err := setReg(session.NSOpState, op.OperationID, st)
	if err != nil {
		h.laneMu.Unlock()
		return RunState{}, RunOutcome{}, true, h.fault(err)
	}
	if err := h.commitOwned(ctx, op.OperationID, []session.Write{write}, false); err != nil {
		h.laneMu.Unlock()
		return RunState{}, RunOutcome{}, true, err
	}
	leafID, err := h.loadLeaf(ctx)
	if err != nil {
		h.laneMu.Unlock()
		return RunState{}, RunOutcome{}, true, err
	}
	model := gen.Context.Model
	h.laneMu.Unlock()

	collected, leaseErr := h.runStreamWithLease(ctx, op.OperationID, leafID, model)

	h.laneMu.Lock()
	defer h.laneMu.Unlock()
	if err := h.guardOpen(); err != nil {
		return st, RunOutcome{}, true, err
	}
	// Everything past this point records an effect that already happened: the
	// provider stream ran, and the store still says a generation is in flight.
	// Those writes must land even though ctx may already be done, so they run
	// detached. Cancellation is still reported to the caller below.
	settle, cancelSettle := context.WithTimeout(context.WithoutCancel(ctx), h.opts.LeaseTTL)
	defer cancelSettle()
	if err := ctx.Err(); err != nil {
		next, out, done, rerr := h.recoverUnknownEffectLocked(settle, op, st)
		if rerr != nil {
			return next, RunOutcome{}, true, rerr
		}
		if done {
			return next, out, true, nil
		}
		return next, RunOutcome{}, true, err
	}
	if leaseErr != nil {
		return st, RunOutcome{}, true, leaseErr
	}
	if err := collected.storageErr; err != nil {
		// Reading the branch failed, so the provider was never called. Grade it
		// like any other storage failure instead of recording a verdict for a
		// turn that did not happen: a corrupt tree must retire the harness, not
		// settle as a provider error that the next prompt would record again.
		// The reservation stays in effect_pending and Resume picks it up.
		return st, RunOutcome{}, true, h.storageErr(err)
	}
	return h.settleLocked(settle, op, st, collected)
}

func (h *Harness) runStreamWithLease(ctx context.Context, operationID, leafID, model string) (streamCollect, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	leaseDone := make(chan error, 1)
	go func() {
		leaseDone <- h.renewLeaseUntilCancelled(streamCtx, operationID, cancel)
	}()
	collected := h.runStream(streamCtx, leafID, model)
	cancel()
	return collected, <-leaseDone
}

func (h *Harness) renewLeaseUntilCancelled(ctx context.Context, operationID string, cancel context.CancelFunc) error {
	interval := max(time.Millisecond, h.opts.LeaseTTL/3)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			h.laneMu.Lock()
			err := h.guardOpen()
			if err == nil {
				err = h.commitOwned(ctx, operationID, nil, false)
			}
			h.laneMu.Unlock()
			if err != nil {
				cancel()
				return err
			}
		}
	}
}

func (h *Harness) waitWithLease(ctx context.Context, operationID string, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	interval := max(time.Millisecond, h.opts.LeaseTTL/3)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := h.guardOpen(); err != nil {
				return err
			}
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-ticker.C:
			h.laneMu.Lock()
			err := h.guardOpen()
			if err == nil {
				err = h.commitOwned(ctx, operationID, nil, false)
			}
			h.laneMu.Unlock()
			if err != nil {
				return err
			}
		}
	}
}

func (h *Harness) recoverUnknownEffect(ctx context.Context, op Operation, st RunState) (RunState, RunOutcome, bool, error) {
	h.laneMu.Lock()
	defer h.laneMu.Unlock()
	if err := h.guardOpen(); err != nil {
		return RunState{}, RunOutcome{}, true, err
	}
	return h.recoverUnknownEffectLocked(ctx, op, st)
}

// recoverUnknownEffectLocked settles a generation whose provider call may or
// may not have reached the model. Attempts left mean a fresh reservation;
// otherwise the run terminates as interrupted.
func (h *Harness) recoverUnknownEffectLocked(ctx context.Context, op Operation, st RunState) (RunState, RunOutcome, bool, error) {
	gen := st.Phase.Generation
	if gen == nil {
		return RunState{}, RunOutcome{}, true, h.fault(fmt.Errorf("agent: unhandled run phase %s/%s", st.Phase.Kind, st.generationStatus()))
	}
	if gen.Attempt < gen.Context.MaxAttempts {
		gen.Status = harnessGenReady
		gen.NextAttempt = gen.Attempt + 1
		gen.ResponseEntryID = ""
		gen.UsageID = ""
		st.Phase.Generation = gen
		write, err := setReg(session.NSOpState, op.OperationID, st)
		if err != nil {
			return RunState{}, RunOutcome{}, true, h.fault(err)
		}
		if err := h.commitOwned(ctx, op.OperationID, []session.Write{write}, false); err != nil {
			return RunState{}, RunOutcome{}, true, err
		}
		return st, RunOutcome{}, false, nil
	}
	return h.materializeInterruptedLocked(ctx, op, st)
}

func (h *Harness) materializeInterruptedLocked(ctx context.Context, op Operation, st RunState) (RunState, RunOutcome, bool, error) {
	gen := st.Phase.Generation
	msg := message.NewText(message.RoleAssistant, "")
	msg.ID = gen.ResponseEntryID
	entry := session.Entry{
		ID:           gen.ResponseEntryID,
		Type:         session.EntryMessage,
		Message:      msg,
		StopReason:   string(provider.StopReasonError),
		ErrorMessage: "interrupted",
	}
	leaf, err := h.loadLeaf(ctx)
	if err != nil {
		return st, RunOutcome{}, true, err
	}
	entry.ParentID = leaf
	usage := session.InsertUsage{Row: session.UsageRow{ID: gen.UsageID, EntryID: gen.ResponseEntryID}}
	st.LatestAssistantEntryID = gen.ResponseEntryID
	out, err := h.settleTerminalLocked(ctx, op, st, entry, usage, session.OperationError{
		Code:    harnessCodeInterrupted,
		Message: "interrupted",
	})
	return st, out, true, err
}

func (h *Harness) waitRetry(ctx context.Context, op Operation, st RunState) (RunState, error) {
	gen := st.Phase.Generation
	if gen == nil {
		return RunState{}, h.lockedFault(fmt.Errorf("agent: unhandled run phase %s/%s", st.Phase.Kind, st.generationStatus()))
	}
	if err := h.waitWithLease(ctx, op.OperationID, time.Until(time.UnixMilli(gen.NotBefore))); err != nil {
		return RunState{}, err
	}
	h.laneMu.Lock()
	defer h.laneMu.Unlock()
	if err := h.guardOpen(); err != nil {
		return RunState{}, err
	}
	gen.Status = harnessGenReady
	gen.ResponseEntryID = ""
	gen.UsageID = ""
	gen.NotBefore = 0
	st.Phase.Generation = gen
	write, err := setReg(session.NSOpState, op.OperationID, st)
	if err != nil {
		return RunState{}, h.fault(err)
	}
	if err := h.commitOwned(ctx, op.OperationID, []session.Write{write}, false); err != nil {
		return RunState{}, err
	}
	return st, nil
}

// retryDelay returns the wait before retrying attempt, which is 1-based and
// counts the call that just failed. The base doubles per prior attempt up to
// harnessRetryMaxDelay, then half of it is randomized so a fleet of harnesses
// riding out one provider outage does not retry in lockstep. A base of zero
// means the caller opted out of waiting.
func retryDelay(baseMs, attempt int) time.Duration {
	if baseMs <= 0 {
		return 0
	}
	delay := time.Duration(baseMs) * time.Millisecond
	for i := 1; i < attempt && delay < harnessRetryMaxDelay; i++ {
		delay *= 2
	}
	delay = min(delay, harnessRetryMaxDelay)
	half := delay / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func (h *Harness) settleLocked(ctx context.Context, op Operation, st RunState, got streamCollect) (RunState, RunOutcome, bool, error) {
	gen := st.Phase.Generation
	if gen == nil {
		return RunState{}, RunOutcome{}, true, h.fault(fmt.Errorf("agent: unhandled run phase %s/%s", st.Phase.Kind, st.generationStatus()))
	}
	leaf, err := h.loadLeaf(ctx)
	if err != nil {
		return RunState{}, RunOutcome{}, true, err
	}
	msg := message.Message{
		ID:                gen.ResponseEntryID,
		Role:              message.RoleAssistant,
		Kind:              message.KindStandard,
		Text:              got.text,
		Thinking:          got.thinking,
		ThinkingSignature: got.signature,
		RedactedThinking:  got.redacted,
		ProviderState:     got.providerState,
	}
	entry := session.Entry{
		ID:         gen.ResponseEntryID,
		ParentID:   leaf,
		Type:       session.EntryMessage,
		Message:    msg,
		StopReason: string(got.stop),
	}
	usage := session.InsertUsage{Row: session.UsageRow{
		ID:      gen.UsageID,
		EntryID: gen.ResponseEntryID,
		Usage:   got.usage,
	}}

	if got.sawTool || got.stop == provider.StopReasonToolUse {
		entry.StopReason = string(provider.StopReasonError)
		entry.ErrorMessage = "tool calls are not supported"
		entry.Message.ToolCalls = nil
		st.LatestAssistantEntryID = gen.ResponseEntryID
		out, err := h.settleTerminalLocked(ctx, op, st, entry, usage, session.OperationError{
			Code:    harnessCodeTools,
			Message: "tool calls are not supported",
		})
		return st, out, true, err
	}
	retryable := got.err != nil && provider.IsRetryableError(got.err)
	if retryable && gen.Attempt < gen.Context.MaxAttempts {
		entry.StopReason = string(provider.StopReasonError)
		if got.err != nil {
			entry.ErrorMessage = got.err.Error()
		}
		gen.Status = harnessGenRetryWait
		gen.NextAttempt = gen.Attempt + 1
		gen.NotBefore = time.Now().Add(retryDelay(gen.Context.BaseDelayMs, gen.Attempt)).UnixMilli()
		st.Phase.Generation = gen
		if err := h.commitSettlement(ctx, op, st, entry, usage, gen.ResponseEntryID); err != nil {
			return RunState{}, RunOutcome{}, true, err
		}
		return st, RunOutcome{}, false, nil
	}

	success := got.stop == provider.StopReasonComplete || got.stop == provider.StopReasonLength ||
		(got.stop == "" && got.text != "" && got.err == nil)
	if success {
		st.Phase.Kind = harnessPhaseCheckpoint
		st.Phase.Continuation = harnessMayFinish
		st.Phase.Generation = nil
		st.LatestAssistantEntryID = gen.ResponseEntryID
		if err := h.commitSettlement(ctx, op, st, entry, usage, gen.ResponseEntryID); err != nil {
			return RunState{}, RunOutcome{}, true, err
		}
		return st, RunOutcome{}, false, nil
	}

	entry.StopReason = string(provider.StopReasonError)
	if got.err != nil {
		entry.ErrorMessage = got.err.Error()
	}
	st.LatestAssistantEntryID = gen.ResponseEntryID
	out, err := h.settleTerminalLocked(ctx, op, st, entry, usage, session.OperationError{
		Code:    harnessCodeProvider,
		Message: entry.ErrorMessage,
	})
	return st, out, true, err
}

func (h *Harness) commitSettlement(ctx context.Context, op Operation, st RunState, entry session.Entry, usage session.InsertUsage, leafID string) error {
	stateWrite, err := setReg(session.NSOpState, op.OperationID, st)
	if err != nil {
		return h.fault(err)
	}
	leafWrite, err := setReg(session.NSLaneLeaf, harnessLaneMain, leafID)
	if err != nil {
		return h.fault(err)
	}
	return h.commitOwned(ctx, op.OperationID, []session.Write{
		session.InsertEntry{Entry: entry},
		usage,
		stateWrite,
		leafWrite,
	}, false)
}

func (h *Harness) settleTerminalLocked(ctx context.Context, op Operation, st RunState, entry session.Entry, usage session.InsertUsage, ferr session.OperationError) (RunOutcome, error) {
	leafWrite, err := setReg(session.NSLaneLeaf, harnessLaneMain, entry.ID)
	if err != nil {
		return RunOutcome{}, h.fault(err)
	}
	writes := []session.Write{
		session.InsertEntry{Entry: entry},
		usage,
		leafWrite,
	}
	return h.terminalWrites(ctx, op, st, harnessOutcomeFailed, &ferr, writes, entry.ID)
}

func (h *Harness) terminal(ctx context.Context, op Operation, st RunState, outcome string, ferr *session.OperationError) (RunOutcome, error) {
	h.laneMu.Lock()
	defer h.laneMu.Unlock()
	if err := h.guardOpen(); err != nil {
		return RunOutcome{}, err
	}
	leafID, err := h.loadLeaf(ctx)
	if err != nil {
		return RunOutcome{}, err
	}
	return h.terminalWrites(ctx, op, st, outcome, ferr, nil, leafID)
}

func (h *Harness) terminalWrites(ctx context.Context, op Operation, st RunState, outcome string, ferr *session.OperationError, prefix []session.Write, leafID string) (RunOutcome, error) {
	writes := append([]session.Write{}, prefix...)
	writes = append(writes,
		session.DeleteRegister{Namespace: session.NSOpMeta, Key: op.OperationID},
		session.DeleteRegister{Namespace: session.NSOpState, Key: op.OperationID},
	)
	last := session.LaneLastResult{
		OperationID:           op.OperationID,
		Kind:                  harnessKindRun,
		LeafID:                leafID,
		FinalAssistantEntryID: st.LatestAssistantEntryID,
		Outcome:               outcome,
		Error:                 ferr,
	}
	lastWrite, err := setReg(session.NSLaneLastResult, harnessLaneMain, last)
	if err != nil {
		return RunOutcome{}, h.fault(err)
	}
	writes = append(writes, lastWrite)
	if err := h.commitOwned(ctx, op.OperationID, writes, true); err != nil {
		return RunOutcome{}, err
	}
	return h.outcomeFrom(ctx, last)
}

func (h *Harness) outcomeFrom(ctx context.Context, last session.LaneLastResult) (RunOutcome, error) {
	out := RunOutcome{
		OperationID: last.OperationID,
		Kind:        last.Outcome,
		LeafID:      last.LeafID,
		Error:       last.Error,
	}
	if last.FinalAssistantEntryID == "" {
		return out, nil
	}
	entries, err := h.sess.Storage().GetEntries(ctx, []string{last.FinalAssistantEntryID})
	if err != nil {
		return RunOutcome{}, h.storageErr(err)
	}
	if entry, ok := entries[last.FinalAssistantEntryID]; ok {
		msg := entry.Message
		out.FinalMessage = &msg
	}
	return out, nil
}

func (h *Harness) guardOpen() error {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.closed {
		return ErrHarnessClosed
	}
	if h.faulted {
		return ErrHarnessFault
	}
	return nil
}
