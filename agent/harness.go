package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Viking602/venat/session"
)

// Harness drives one durable single-agent lane against session.Storage.
// It is experimental and deliberately narrower than Engine: no tools, hooks,
// or budgets. See docs/adr/ADR-028-agent-harness-and-session.md.
type Harness struct {
	sess    *session.Session
	opts    HarnessOptions
	ownerID string

	// lifecycleMu guards only process-local lifecycle state. It is never held
	// across storage or provider calls, so Close can always observe its context
	// and cancel an in-flight drive.
	lifecycleMu sync.Mutex
	laneMu      sync.Mutex
	closed      bool
	faulted     bool
	driving     bool
	runCancel   context.CancelFunc
	active      int
	idle        chan struct{}
}

func OpenHarness(ctx context.Context, store session.Storage, opts HarnessOptions) (*Harness, error) {
	if opts.Provider == nil || opts.Model == "" {
		return nil, ErrMissingIdentities
	}
	if opts.Retry.MaxAttempts < 1 {
		opts.Retry.MaxAttempts = 1
	}
	if opts.LeaseTTL <= 0 {
		opts.LeaseTTL = 30 * time.Second
	}
	sess := session.New(store)
	if err := sess.EnsureMain(ctx, session.LaneConfiguration{Model: opts.Model}); err != nil {
		return nil, err
	}
	return &Harness{sess: sess, opts: opts, ownerID: sess.IDs().Next()}, nil
}

func (h *Harness) Session() *session.Session { return h.sess }

// Close stops the in-flight drive and waits for it to stop writing. Storage is
// caller-owned so a closed harness may be replaced by OpenHarness over the same
// store; that hand-off is only safe once no drive can still commit. ctx bounds
// the wait — on expiry Close reports ctx.Err() and the drive may still be
// running, so the store must not be reused.
func (h *Harness) Close(ctx context.Context) error {
	h.lifecycleMu.Lock()
	h.closed = true
	cancel := h.runCancel
	active := h.active
	idle := h.idle
	h.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if active == 0 {
		return nil
	}
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Harness) commit(ctx context.Context, writes []session.Write) error {
	if _, err := h.sess.Commit(ctx, writes); err != nil {
		return h.storageErr(err)
	}
	return nil
}

// claimLane installs this Harness as the durable owner of an existing
// operation. The register sequence makes two concurrent claimers mutually
// exclusive; an unexpired owner cannot be displaced.
func (h *Harness) claimLane(ctx context.Context, state session.LaneState, reg session.Register) error {
	now := time.Now()
	if state.OwnerID != "" && state.OwnerID != h.ownerID && state.LeaseExpiresAt > now.UnixMilli() {
		return ErrLaneBusy
	}
	state.OwnerID = h.ownerID
	state.LeaseExpiresAt = now.Add(h.opts.LeaseTTL).UnixMilli()
	write, err := setReg(session.NSLaneState, harnessLaneMain, state)
	if err != nil {
		return h.fault(err)
	}
	write.CompareSeq = true
	write.ExpectedSeq = reg.Seq
	if err := h.commit(ctx, []session.Write{write}); err != nil {
		if errors.Is(err, session.ErrConflict) {
			return ErrLaneBusy
		}
		return err
	}
	return nil
}

// commitOwned applies one operation transition and renews its durable lane
// lease in the same atomic commit. A stale owner cannot write after another
// Harness recovered the operation.
func (h *Harness) commitOwned(ctx context.Context, operationID string, writes []session.Write, release bool) error {
	state, reg, ok, err := readRegRecord[session.LaneState](ctx, h, session.NSLaneState, harnessLaneMain)
	if err != nil {
		return err
	}
	if !ok {
		return h.fault(missingRegister(session.NSLaneState, harnessLaneMain))
	}
	now := time.Now()
	if state.CurrentOperationID != operationID || state.OwnerID != h.ownerID || state.LeaseExpiresAt <= now.UnixMilli() {
		return ErrLaneBusy
	}
	if release {
		state = session.LaneState{}
	} else {
		state.LeaseExpiresAt = now.Add(h.opts.LeaseTTL).UnixMilli()
	}
	laneWrite, err := setReg(session.NSLaneState, harnessLaneMain, state)
	if err != nil {
		return h.fault(err)
	}
	laneWrite.CompareSeq = true
	laneWrite.ExpectedSeq = reg.Seq
	writes = append(writes, laneWrite)
	if err := h.commit(ctx, writes); err != nil {
		if errors.Is(err, session.ErrConflict) {
			return ErrLaneBusy
		}
		return err
	}
	return nil
}

func (h *Harness) finishDrive(operationID string, release func()) {
	h.laneMu.Lock()
	h.relinquishLane(operationID)
	h.laneMu.Unlock()
	release()
}

// relinquishLane is a bounded best-effort handoff. A failed release is still
// safe: the durable lease expires and a later Harness can recover it.
func (h *Harness) relinquishLane(operationID string) {
	timeout := min(time.Second, h.opts.LeaseTTL)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	reg, ok, err := h.sess.Storage().GetRegister(ctx, session.NSLaneState, harnessLaneMain)
	if err != nil || !ok {
		return
	}
	state, err := session.UnmarshalRegister[session.LaneState](reg.Value)
	if err != nil || state.CurrentOperationID != operationID || state.OwnerID != h.ownerID {
		return
	}
	state.OwnerID = ""
	state.LeaseExpiresAt = 0
	write, err := setReg(session.NSLaneState, harnessLaneMain, state)
	if err != nil {
		return
	}
	write.CompareSeq = true
	write.ExpectedSeq = reg.Seq
	_, _ = h.sess.Commit(ctx, []session.Write{write})
}

// fault retires the harness. Reserve it for a broken invariant: state that
// cannot be decoded, a register that the session guarantees but does not have,
// or a phase the state machine cannot reach. Once faulted every later call
// fails fast rather than writing on top of state nobody can interpret.
func (h *Harness) fault(err error) error {
	h.lifecycleMu.Lock()
	h.faulted = true
	h.lifecycleMu.Unlock()
	if err == nil {
		return ErrHarnessFault
	}
	return fmt.Errorf("%w: %w", ErrHarnessFault, err)
}

// storageErr classifies a failure that came back from the store. Corruption
// retires the harness; anything else — a cancelled context, a closed store, a
// transport hiccup — is transient and returned unchanged so the caller can
// drive the same lane again.
func (h *Harness) storageErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, session.ErrCorrupt),
		errors.Is(err, session.ErrDuplicateID),
		errors.Is(err, session.ErrInvalidWrite),
		errors.Is(err, session.ErrNotFound):
		return h.fault(err)
	default:
		return err
	}
}

func missingRegister(namespace, key string) error {
	return fmt.Errorf("%w: %s/%s", ErrRegisterMissing, namespace, key)
}

// entryLocked validates and claims a public drive entry. The caller holds
// lifecycleMu.
func (h *Harness) entryLocked() error {
	if h.closed {
		return ErrHarnessClosed
	}
	if h.faulted {
		return ErrHarnessFault
	}
	if h.driving {
		return ErrLaneBusy
	}
	return nil
}

// beginDriveLocked claims the process-local lane and registers the cancellable
// point used by Close. The caller holds lifecycleMu.
func (h *Harness) beginDriveLocked(ctx context.Context) (context.Context, func()) {
	driveCtx, cancel := context.WithCancel(ctx)
	h.driving = true
	h.runCancel = cancel
	if h.active == 0 {
		h.idle = make(chan struct{})
	}
	h.active++
	var once sync.Once
	return driveCtx, func() {
		once.Do(func() {
			cancel()
			h.lifecycleMu.Lock()
			h.driving = false
			h.runCancel = nil
			h.active--
			if h.active == 0 {
				close(h.idle)
			}
			h.lifecycleMu.Unlock()
		})
	}
}

func (h *Harness) beginRead() (func(), error) {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.closed {
		return nil, ErrHarnessClosed
	}
	if h.faulted {
		return nil, ErrHarnessFault
	}
	if h.active == 0 {
		h.idle = make(chan struct{})
	}
	h.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			h.lifecycleMu.Lock()
			h.active--
			if h.active == 0 {
				close(h.idle)
			}
			h.lifecycleMu.Unlock()
		})
	}, nil
}

// readReg decodes one register. A read failure stays transient; a value that
// will not decode is corruption. Callers must hold laneMu.
func readReg[T any](ctx context.Context, h *Harness, namespace, key string) (T, bool, error) {
	value, _, ok, err := readRegRecord[T](ctx, h, namespace, key)
	return value, ok, err
}

func readRegRecord[T any](ctx context.Context, h *Harness, namespace, key string) (T, session.Register, bool, error) {
	var zero T
	reg, ok, err := h.sess.Storage().GetRegister(ctx, namespace, key)
	if err != nil {
		return zero, session.Register{}, false, h.storageErr(err)
	}
	if !ok {
		return zero, session.Register{}, false, nil
	}
	value, err := session.UnmarshalRegister[T](reg.Value)
	if err != nil {
		return zero, session.Register{}, false, h.fault(fmt.Errorf("%s/%s: %w", namespace, key, err))
	}
	return value, reg, true, nil
}

func setReg(namespace, key string, v any) (session.SetRegister, error) {
	raw, err := session.MarshalRegister(v)
	if err != nil {
		return session.SetRegister{}, err
	}
	return session.SetRegister{Namespace: namespace, Key: key, Value: raw}, nil
}

func decodeLeaf(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	return session.UnmarshalRegister[string](raw)
}

// loadLeaf reads the lane leaf. An absent register is the empty branch, not an
// error: EnsureMain seeds it with null before the first prompt.
func (h *Harness) loadLeaf(ctx context.Context) (string, error) {
	leaf, _, _, err := h.loadLeafRecord(ctx)
	return leaf, err
}

func (h *Harness) loadLeafRecord(ctx context.Context) (string, session.Register, bool, error) {
	reg, ok, err := h.sess.Storage().GetRegister(ctx, session.NSLaneLeaf, harnessLaneMain)
	if err != nil {
		return "", session.Register{}, false, h.storageErr(err)
	}
	if !ok {
		return "", session.Register{}, false, nil
	}
	leaf, err := decodeLeaf(reg.Value)
	if err != nil {
		return "", session.Register{}, false, h.fault(fmt.Errorf("%s/%s: %w", session.NSLaneLeaf, harnessLaneMain, err))
	}
	return leaf, reg, true, nil
}

func (h *Harness) loadLaneStateRecord(ctx context.Context) (session.LaneState, session.Register, error) {
	state, reg, ok, err := readRegRecord[session.LaneState](ctx, h, session.NSLaneState, harnessLaneMain)
	if err != nil {
		return session.LaneState{}, session.Register{}, err
	}
	if !ok {
		return session.LaneState{}, session.Register{}, h.fault(missingRegister(session.NSLaneState, harnessLaneMain))
	}
	return state, reg, nil
}
