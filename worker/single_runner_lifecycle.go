package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Viking602/venat/api"
)

// Suspend stops a locally executing run at a durable checkpoint or pauses a
// queued run. It never steals a task lease owned by another process.
func (s *SingleRunner) Suspend(ctx context.Context, runID string) error {
	return s.interrupt(ctx, runID, ErrSingleRunSuspended)
}

// Cancel stops a locally executing run and records terminal task/run state. It
// never steals a task lease owned by another process.
func (s *SingleRunner) Cancel(ctx context.Context, runID string) error {
	return s.interrupt(ctx, runID, ErrSingleRunCancelled)
}

// Report commits a host-authored report when setup fails before Execute starts.
// It acquires the pending task atomically, submits the report under that lease,
// and reconciles task, run, and admission state through the same terminal path
// used by Execute.
func (s *SingleRunner) Report(ctx context.Context, runID string, report api.TypedReport) (SingleRun, error) {
	if err := s.validate(runID); err != nil {
		return SingleRun{}, err
	}
	if err := ctx.Err(); err != nil {
		return SingleRun{}, err
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.active[runID] != nil {
		return SingleRun{}, fmt.Errorf("%w: %s", ErrSingleRunAlreadyExecuting, runID)
	}
	state, err := s.load(ctx, runID)
	if err != nil {
		return SingleRun{}, err
	}
	if terminalTaskStatus(state.Task.Status) {
		if err := s.reconcileTerminal(ctx, &state); err != nil {
			return SingleRun{}, err
		}
		return s.load(ctx, runID)
	}
	if state.Envelope.ID == "" {
		return SingleRun{}, fmt.Errorf("%w: no pending envelope for run %q", ErrSingleRunNotResumable, runID)
	}
	if count := s.Runner.ActiveLeaseCountContext(ctx, runID, state.Task.ID); count > 0 {
		return SingleRun{}, fmt.Errorf("%w: run %s has %d active leases", ErrSingleRunNotOwned, runID, count)
	}
	ttl := s.Worker.executeEnvelopeTTL(ExecuteEnvelopeRequest{})
	lease, acquired, err := s.Runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: runID, TaskID: state.Task.ID, EnvelopeID: state.Envelope.ID,
		HolderType: api.HolderAgent, HolderID: s.Worker.AgentID, TTL: ttl,
	})
	if err != nil {
		return SingleRun{}, fmt.Errorf("worker: acquire single-run report lease: %w", err)
	}
	if !acquired {
		return SingleRun{}, fmt.Errorf("%w: report lease was not granted for run %s", ErrSingleRunNotOwned, runID)
	}
	leaseHandled := false
	defer func() {
		if leaseHandled {
			return
		}
		_ = s.Runner.ReleaseTaskExecution(context.WithoutCancel(ctx), api.ReleaseTaskExecutionCommand{
			LeaseID: lease.ID, HolderID: s.Worker.AgentID,
		})
	}()
	if err := s.Runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
		RunID: runID, TaskID: state.Task.ID, LeaseID: lease.ID,
		HolderType: api.HolderAgent, HolderID: s.Worker.AgentID, TaskVersion: lease.TaskVersion,
		Report: report,
	}); err != nil {
		return SingleRun{}, fmt.Errorf("worker: submit single-run report: %w", err)
	}
	leaseHandled = true
	if _, err := s.Runner.Recover(ctx, runID); err != nil {
		return SingleRun{}, fmt.Errorf("worker: recover reported single run: %w", err)
	}
	state, err = s.load(ctx, runID)
	if err != nil {
		return SingleRun{}, err
	}
	if err := s.reconcileTerminal(ctx, &state); err != nil {
		return SingleRun{}, err
	}
	return s.load(ctx, runID)
}

func (s *SingleRunner) interrupt(ctx context.Context, runID string, cause error) error {
	if err := s.validate(runID); err != nil {
		return err
	}
	s.lifecycleMu.Lock()
	if entry := s.active[runID]; entry != nil {
		entry.cancel(cause)
		s.lifecycleMu.Unlock()
		select {
		case <-entry.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer s.lifecycleMu.Unlock()
	state, err := s.load(ctx, runID)
	if err != nil {
		return err
	}
	if count := s.Runner.ActiveLeaseCountContext(ctx, runID, state.Task.ID); count > 0 {
		return fmt.Errorf("%w: run %s has %d active leases", ErrSingleRunNotOwned, runID, count)
	}
	if terminalRunStatus(state.Run.Status) {
		return api.ErrTerminalState
	}
	if errors.Is(cause, ErrSingleRunCancelled) {
		return s.cancelPersisted(ctx, &state)
	}
	return s.suspendPersisted(ctx, &state, true)
}

func (s *SingleRunner) reserveAdmission(ctx context.Context, runID string) (api.AdmissionReservation, error) {
	if !RequiresAdmission(s.Governance) {
		return api.AdmissionReservation{}, nil
	}
	decision, err := s.Admission.Reserve(ctx, RunAdmissionRequest{
		AgentID: s.Worker.AgentID, AgentVersion: s.AgentVersion,
		RunID: runID, Governance: s.Governance,
	})
	if err != nil {
		return api.AdmissionReservation{}, fmt.Errorf("worker: reserve single-run admission: %w", err)
	}
	if !decision.Allowed {
		return api.AdmissionReservation{}, &AdmissionDeniedError{Reason: decision.Reason, Usage: decision.Usage}
	}
	if decision.Reservation.ID == "" {
		return api.AdmissionReservation{}, fmt.Errorf("%w: admission controller returned an empty reservation", ErrSingleRunInvalid)
	}
	return decision.Reservation, nil
}

func (s *SingleRunner) abortStart(ctx context.Context, runID string, runCreated bool, reservation api.AdmissionReservation, cause error) error {
	var cleanupErr error
	if runCreated {
		cleanupErr = errors.Join(cleanupErr, s.Runner.TransitionRun(ctx, api.TransitionRunCommand{
			RunID: runID, To: api.RunStatusFailed,
		}))
	}
	if reservation.ID != "" && !terminalAdmissionState(reservation.State) {
		_, err := s.transitionAdmission(ctx, reservation, api.AdmissionReleased)
		cleanupErr = errors.Join(cleanupErr, err)
	}
	return errors.Join(cause, cleanupErr)
}

func (s *SingleRunner) activateAdmission(ctx context.Context, reservation api.AdmissionReservation) (api.AdmissionReservation, error) {
	if reservation.ID == "" {
		return reservation, nil
	}
	current, err := s.expireAdmissionIfDue(ctx, reservation)
	if err != nil {
		return current, err
	}
	switch current.State {
	case api.AdmissionActive:
		return current, nil
	case api.AdmissionReserved, api.AdmissionSuspended:
		return s.transitionAdmission(ctx, current, api.AdmissionActive)
	default:
		return api.AdmissionReservation{}, fmt.Errorf("%w: admission reservation is %s", ErrSingleRunNotResumable, current.State)
	}
}

func (s *SingleRunner) expireAdmissionIfDue(
	ctx context.Context,
	reservation api.AdmissionReservation,
) (api.AdmissionReservation, error) {
	if reservation.ID == "" || terminalAdmissionState(reservation.State) ||
		reservation.ExpiresAt.IsZero() || reservation.ExpiresAt.After(s.currentTime()) {
		return reservation, nil
	}
	expired, err := s.transitionAdmission(ctx, reservation, api.AdmissionExpired)
	if err != nil {
		return api.AdmissionReservation{}, fmt.Errorf("worker: expire overdue single-run admission: %w", err)
	}
	return expired, fmt.Errorf(
		"%w: admission reservation %q expired at %s",
		ErrSingleRunNotResumable,
		reservation.ID,
		reservation.ExpiresAt.UTC().Format(time.RFC3339Nano),
	)
}

func (s *SingleRunner) suspendAdmission(ctx context.Context, reservation api.AdmissionReservation) (api.AdmissionReservation, error) {
	if reservation.ID == "" || reservation.State != api.AdmissionActive {
		return reservation, nil
	}
	return s.transitionAdmission(ctx, reservation, api.AdmissionSuspended)
}

func (s *SingleRunner) settleAdmission(ctx context.Context, reservation api.AdmissionReservation) (api.AdmissionReservation, error) {
	if reservation.ID == "" || terminalAdmissionState(reservation.State) {
		return reservation, nil
	}
	if reservation.State == api.AdmissionReserved {
		return s.transitionAdmission(ctx, reservation, api.AdmissionReleased)
	}
	return s.transitionAdmission(ctx, reservation, api.AdmissionSettled)
}

func (s *SingleRunner) transitionAdmission(
	ctx context.Context,
	reservation api.AdmissionReservation,
	to api.AdmissionState,
) (api.AdmissionReservation, error) {
	for range 2 {
		at := s.currentTime()
		if at.Before(reservation.UpdatedAt) {
			at = reservation.UpdatedAt
		}
		transition := api.AdmissionTransition{
			ReservationID: reservation.ID, ExpectedVersion: reservation.Version,
			To: to, At: at,
		}
		if to == api.AdmissionActive || to == api.AdmissionSuspended {
			lifetime := reservation.ExpiresAt.Sub(reservation.CreatedAt)
			if lifetime <= 0 {
				lifetime = defaultAdmissionTTL
			}
			transition.ExpiresAt = at.Add(lifetime)
		}
		decision, err := s.Admission.Transition(ctx, transition)
		if err != nil {
			return api.AdmissionReservation{}, fmt.Errorf("worker: transition single-run admission to %s: %w", to, err)
		}
		if decision.Allowed {
			return decision.Reservation, nil
		}
		if decision.Reason != api.AdmissionDeniedVersionConflict {
			return api.AdmissionReservation{}, &AdmissionDeniedError{Reason: decision.Reason, Usage: decision.Usage}
		}
		current, loadErr := s.Runner.LoadAdmissionReservation(ctx, reservation.ID)
		if loadErr != nil {
			return api.AdmissionReservation{}, fmt.Errorf("worker: reload admission after version conflict: %w", loadErr)
		}
		if current.State == to {
			return current, nil
		}
		reservation = current
	}
	return api.AdmissionReservation{}, fmt.Errorf("%w: repeated admission version conflict", ErrSingleRunAlreadyExecuting)
}

func (s *SingleRunner) finishExecution(ctx context.Context, state *SingleRun, outcome ExecutionOutcome) error {
	current, err := s.load(ctx, state.Run.ID)
	if err != nil {
		if outcome.State == "" {
			_, suspendErr := s.suspendAdmission(ctx, state.Admission)
			return errors.Join(err, suspendErr)
		}
		return err
	}
	*state = current
	switch outcome.State {
	case ExecutionCancelled:
		return s.cancelPersisted(ctx, state)
	case ExecutionSuspended:
		manual := outcome.Suspension != nil && outcome.Suspension.Kind == SuspensionRequested
		return s.suspendPersisted(ctx, state, manual)
	case ExecutionCompleted, ExecutionFailed:
		if !terminalTaskStatus(state.Task.Status) {
			updated, suspendErr := s.suspendAdmission(ctx, state.Admission)
			state.Admission = updated
			return errors.Join(
				fmt.Errorf("worker: execution ended without terminal task state %s", state.Task.Status),
				suspendErr,
			)
		}
		return s.reconcileTerminal(ctx, state)
	default:
		updated, suspendErr := s.suspendAdmission(ctx, state.Admission)
		state.Admission = updated
		return suspendErr
	}
}

func (s *SingleRunner) reconcileTerminal(ctx context.Context, state *SingleRun) error {
	var admission api.AdmissionReservation
	var admissionErr error
	switch state.Task.Status {
	case api.TaskStatusCompleted:
		admission, admissionErr = s.settleAdmission(ctx, state.Admission)
		state.Admission = admission
		return errors.Join(admissionErr, s.finalizeRun(ctx, state.Run.ID, api.RunStatusCompleted))
	case api.TaskStatusFailed:
		admission, admissionErr = s.settleAdmission(ctx, state.Admission)
		state.Admission = admission
		return errors.Join(admissionErr, s.finalizeRun(ctx, state.Run.ID, api.RunStatusFailed))
	case api.TaskStatusCancelled:
		admission, admissionErr = s.settleAdmission(ctx, state.Admission)
		state.Admission = admission
		return errors.Join(admissionErr, s.finalizeRun(ctx, state.Run.ID, api.RunStatusCancelled))
	case api.TaskStatusBlocked:
		admission, admissionErr = s.suspendAdmission(ctx, state.Admission)
		state.Admission = admission
		return errors.Join(admissionErr, s.finalizeRun(ctx, state.Run.ID, api.RunStatusBlocked))
	default:
		return nil
	}
}

func (s *SingleRunner) suspendPersisted(ctx context.Context, state *SingleRun, manual bool) error {
	updated, admissionErr := s.suspendAdmission(ctx, state.Admission)
	state.Admission = updated
	if !manual {
		return admissionErr
	}
	var lifecycleErr error
	lifecycleErr = errors.Join(lifecycleErr, s.retirePendingEnvelopes(ctx, state.Run.ID, state.Task.ID, "suspended"))
	switch state.Task.Status {
	case api.TaskStatusCreated, api.TaskStatusPlanned, api.TaskStatusValidated, api.TaskStatusRouted,
		api.TaskStatusWaitingDependency, api.TaskStatusDispatched, api.TaskStatusRunning, api.TaskStatusBlocked:
		lifecycleErr = errors.Join(lifecycleErr, s.Runner.TransitionTask(ctx, api.TransitionTaskCommand{
			RunID: state.Run.ID, TaskID: state.Task.ID, To: api.TaskStatusPaused,
		}))
	}
	switch state.Run.Status {
	case api.RunStatusCreated, api.RunStatusPlanning, api.RunStatusValidating, api.RunStatusRouting,
		api.RunStatusDispatching, api.RunStatusRunning, api.RunStatusExecuting:
		lifecycleErr = errors.Join(lifecycleErr, s.Runner.TransitionRun(ctx, api.TransitionRunCommand{
			RunID: state.Run.ID, To: api.RunStatusBlocked,
		}))
	}
	return errors.Join(admissionErr, lifecycleErr)
}

func (s *SingleRunner) cancelPersisted(ctx context.Context, state *SingleRun) error {
	if terminalRunStatus(state.Run.Status) {
		return nil
	}
	var lifecycleErr error
	lifecycleErr = errors.Join(lifecycleErr, s.retirePendingEnvelopes(ctx, state.Run.ID, state.Task.ID, "cancelled"))
	if !terminalTaskStatus(state.Task.Status) {
		lifecycleErr = errors.Join(lifecycleErr, s.Runner.TransitionTask(ctx, api.TransitionTaskCommand{
			RunID: state.Run.ID, TaskID: state.Task.ID, To: api.TaskStatusCancelled,
		}))
	}
	lifecycleErr = errors.Join(lifecycleErr, s.finalizeRun(ctx, state.Run.ID, api.RunStatusCancelled))
	updated, admissionErr := s.settleAdmission(ctx, state.Admission)
	state.Admission = updated
	return errors.Join(lifecycleErr, admissionErr)
}

func (s *SingleRunner) retirePendingEnvelopes(ctx context.Context, runID, taskID, status string) error {
	envelopes, err := s.Runner.ListEnvelopes(ctx, runID)
	if err != nil {
		return err
	}
	var updateErr error
	for _, envelope := range envelopes {
		if envelope.TaskID != taskID || envelope.Status != "pending" {
			continue
		}
		envelope.Status = status
		updateErr = errors.Join(updateErr, s.Runner.UpdateEnvelope(ctx, envelope))
	}
	return updateErr
}

func (s *SingleRunner) finalizeRun(ctx context.Context, runID string, target api.RunStatus) error {
	run, err := s.Runner.Run(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status == target || terminalRunStatus(run.Status) {
		return nil
	}
	if target == api.RunStatusCompleted && run.Status != api.RunStatusComposingResponse {
		if err := s.Runner.TransitionRun(ctx, api.TransitionRunCommand{
			RunID: runID, To: api.RunStatusComposingResponse,
		}); err != nil {
			return fmt.Errorf("worker: compose single-run response: %w", err)
		}
	}
	if err := s.Runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: runID, To: target}); err != nil {
		return fmt.Errorf("worker: finalize single run as %s: %w", target, err)
	}
	return nil
}

func (s *SingleRunner) resultForTerminal(ctx context.Context, state SingleRun) (SingleRunResult, error) {
	current, err := s.load(ctx, state.Run.ID)
	if err != nil {
		return SingleRunResult{}, err
	}
	outcome := ExecutionOutcome{RunID: current.Run.ID, TaskID: current.Task.ID}
	switch current.Task.Status {
	case api.TaskStatusCompleted:
		outcome.State = ExecutionCompleted
	case api.TaskStatusCancelled:
		outcome.State = ExecutionCancelled
	case api.TaskStatusFailed:
		outcome.State = ExecutionFailed
	default:
		outcome.State = ExecutionSuspended
	}
	return SingleRunResult{
		Execution: outcome, Run: current.Run, Task: current.Task, Admission: current.Admission,
	}, nil
}

func (s *SingleRunner) currentTime() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func terminalAdmissionState(state api.AdmissionState) bool {
	switch state {
	case api.AdmissionSettled, api.AdmissionReleased, api.AdmissionExpired:
		return true
	default:
		return false
	}
}

func terminalRunStatus(status api.RunStatus) bool {
	switch status {
	case api.RunStatusCompleted, api.RunStatusFailed, api.RunStatusCancelled:
		return true
	default:
		return false
	}
}

func terminalTaskStatus(status api.TaskStatus) bool {
	switch status {
	case api.TaskStatusCompleted, api.TaskStatusFailed, api.TaskStatusCancelled:
		return true
	default:
		return false
	}
}
