package worker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/stream"
)

var (
	ErrSingleRunInvalid          = errors.New("worker: invalid single run")
	ErrSingleRunAlreadyExecuting = errors.New("worker: single run already executing")
	ErrSingleRunNotResumable     = errors.New("worker: single run is not resumable")
	ErrSingleRunNotOwned         = errors.New("worker: single run execution is owned elsewhere")
	ErrSingleRunSuspended        = errors.New("worker: single run suspended")
	ErrSingleRunCancelled        = errors.New("worker: single run cancelled")
	ErrAdmissionDenied           = errors.New("worker: admission denied")
)

const (
	singleRunDefinitionIDMetadata      = "venat.agent_definition.id"
	singleRunDefinitionVersionMetadata = "venat.agent_definition.version"
	singleRunStartDigestMetadata       = "venat.single_run.start_digest"
)

// AdmissionDeniedError reports the aggregate guard that rejected a new or
// resumed run. No Run is created when Start returns this error.
type AdmissionDeniedError struct {
	Reason api.AdmissionDenialReason
	Usage  api.AdmissionUsage
}

func (e *AdmissionDeniedError) Error() string {
	return fmt.Sprintf("%v: %s", ErrAdmissionDenied, e.Reason)
}

func (e *AdmissionDeniedError) Unwrap() error { return ErrAdmissionDenied }

// StartSingleRunRequest is the durable input for one agent execution. RunID is
// required and makes retries explicit; root and worker task IDs default to
// deterministic values derived from it.
type StartSingleRunRequest struct {
	RunID      string
	RootTaskID string
	TaskID     string
	Request    string
	Metadata   map[string]string

	Goal               string
	Input              json.RawMessage
	AllowsAction       bool
	Tags               []string
	CompletionCriteria []string
	ReadSelectors      []api.BlackboardSelector
	WriteTargets       []string
	RetryPolicy        api.RetryPolicy
	Budget             *api.TaskBudget
	InputSchema        json.RawMessage
	OutputSchema       json.RawMessage
	ResourceClaims     []api.ResourceClaimSpec
}

// ExecuteSingleRunRequest supplies transient execution inputs. Durable task,
// envelope, checkpoint, admission, and run state are always reloaded by RunID.
type ExecuteSingleRunRequest struct {
	RunID    string
	Messages []message.Message
	Sink     stream.Sink
	// Engine overrides the coordinator's engine for this execution only. The
	// durable worker identity and governance remain coordinator-owned.
	Engine *agent.Engine
	// OnLeaseAcquired runs before the agent starts and on every retry lease.
	OnLeaseAcquired func(api.TaskExecutionLease) error
	TTL             time.Duration
}

// SingleRun is a current durable snapshot of one coordinated execution.
type SingleRun struct {
	Run       api.Run                  `json:"run"`
	RootTask  api.Task                 `json:"rootTask"`
	Task      api.Task                 `json:"task"`
	Envelope  api.TaskEnvelope         `json:"envelope,omitempty"`
	Admission api.AdmissionReservation `json:"admission,omitempty"`
}

// SingleRunResult combines the worker outcome with the reconciled durable
// state observed after lifecycle handling.
type SingleRunResult struct {
	Execution ExecutionOutcome         `json:"execution"`
	Run       api.Run                  `json:"run"`
	Task      api.Task                 `json:"task"`
	Admission api.AdmissionReservation `json:"admission,omitempty"`
}

type activeSingleRun struct {
	cancel     context.CancelCauseFunc
	done       chan struct{}
	stopParent func() bool
}

// SingleRunner coordinates one AgentWorker with Run/Task/Envelope durability
// and optional aggregate admission. It is safe for concurrent lifecycle calls;
// do not copy it after first use.
type SingleRunner struct {
	Runner       *venat.Runner
	Worker       AgentWorker
	Admission    AdmissionController
	AgentVersion string
	Governance   api.GovernancePolicy
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
	Now          func() time.Time

	definitionID  string
	restoreWorker func(context.Context, string, string) (AgentWorker, error)
	lifecycleMu   sync.Mutex
	active        map[string]*activeSingleRun
}

// NewSingleRunner builds a coordinator from an installed immutable definition.
func NewSingleRunner(deployment *DeployedDefinition) *SingleRunner {
	if deployment == nil {
		return &SingleRunner{}
	}
	return &SingleRunner{
		Runner:        deployment.Worker.Runner,
		Worker:        deployment.Worker,
		Admission:     deployment.Admission,
		AgentVersion:  deployment.Definition.Version,
		Governance:    deployment.Definition.Governance,
		InputSchema:   append(json.RawMessage(nil), deployment.Definition.InputSchema...),
		OutputSchema:  append(json.RawMessage(nil), deployment.Definition.OutputSchema...),
		definitionID:  deployment.Definition.ID,
		restoreWorker: deployment.restoreWorker,
	}
}

// Start atomically reserves aggregate capacity, then creates and dispatches one
// durable worker task. Any setup failure terminalizes the partial run and
// releases its reservation.
func (s *SingleRunner) Start(ctx context.Context, request StartSingleRunRequest) (SingleRun, error) {
	if err := s.validate(request.RunID); err != nil {
		return SingleRun{}, err
	}
	if err := ctx.Err(); err != nil {
		return SingleRun{}, err
	}
	request, err := normalizeSingleRunStartRequest(request)
	if err != nil {
		return SingleRun{}, err
	}
	metadata, err := s.startMetadata(request)
	if err != nil {
		return SingleRun{}, err
	}
	existingRun, existingState, runExists, err := s.inspectSingleRunStart(ctx, request, metadata)
	if err != nil {
		return SingleRun{}, err
	}
	if existingState != nil {
		return *existingState, nil
	}

	reservation, err := s.admissionForStart(ctx, request.RunID, runExists)
	if err != nil {
		return SingleRun{}, err
	}
	return s.startAfterAdmission(ctx, request, metadata, existingRun, runExists, reservation)
}

func (s *SingleRunner) startAfterAdmission(
	ctx context.Context,
	request StartSingleRunRequest,
	metadata map[string]string,
	existingRun api.Run,
	runExists bool,
	reservation api.AdmissionReservation,
) (SingleRun, error) {
	runCreated := false
	cleanupReservation := reservation
	if runExists {
		cleanupReservation = api.AdmissionReservation{}
	}
	abort := func(cause error) (SingleRun, error) {
		return SingleRun{}, s.abortStart(
			context.WithoutCancel(ctx),
			request.RunID,
			runCreated,
			cleanupReservation,
			cause,
		)
	}

	run, root, created, setupErr := s.runAndRootForStart(ctx, request, metadata, existingRun, runExists)
	runCreated = created
	if setupErr != nil {
		return abort(setupErr)
	}
	cleanupReservation = startOwnedReservation(cleanupReservation, runCreated)
	if err := s.advanceSingleRunToRunning(ctx, run.ID); err != nil {
		return abort(err)
	}

	task, taskErr := s.Runner.Task(ctx, run.ID, request.TaskID)
	if errors.Is(taskErr, api.ErrNotFound) {
		task, taskErr = s.Runner.CreateTask(ctx, s.taskCommand(request, run.ID, root.ID))
	}
	if taskErr != nil {
		return abort(fmt.Errorf("worker: create single-run task: %w", taskErr))
	}
	state, stateErr := s.load(ctx, run.ID)
	if stateErr != nil {
		return abort(fmt.Errorf("worker: reload single-run task: %w", stateErr))
	}
	if state.Task.Status != api.TaskStatusCreated || state.Envelope.ID != "" {
		return state, nil
	}
	envelope, err := s.dispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: s.Worker.AgentID,
	})
	if err != nil {
		return abort(fmt.Errorf("worker: dispatch single-run task: %w", err))
	}
	state, err = s.load(ctx, run.ID)
	if err != nil {
		return abort(fmt.Errorf("worker: reload started single run: %w", err))
	}
	state.Envelope = envelope
	state.Admission = reservation
	return state, nil
}

func normalizeSingleRunStartRequest(request StartSingleRunRequest) (StartSingleRunRequest, error) {
	request.RunID = strings.TrimSpace(request.RunID)
	if request.RootTaskID == "" {
		request.RootTaskID = request.RunID + "-root"
	}
	if request.TaskID == "" {
		request.TaskID = request.RunID + "-task"
	}
	if request.RootTaskID == request.TaskID {
		return StartSingleRunRequest{}, fmt.Errorf("%w: root and worker task IDs must differ", ErrSingleRunInvalid)
	}
	return request, nil
}

func (s *SingleRunner) inspectSingleRunStart(
	ctx context.Context,
	request StartSingleRunRequest,
	metadata map[string]string,
) (api.Run, *SingleRun, bool, error) {
	existing, err := s.Runner.Run(ctx, request.RunID)
	if errors.Is(err, api.ErrNotFound) {
		return api.Run{}, nil, false, nil
	}
	if err != nil {
		return api.Run{}, nil, false, fmt.Errorf("worker: inspect single run before start: %w", err)
	}
	if existing.RootTaskID != request.RootTaskID ||
		existing.Request != request.Request ||
		existing.AgentVersion != s.AgentVersion ||
		!maps.Equal(existing.Metadata, metadata) {
		return api.Run{}, nil, false, fmt.Errorf(
			"%w: run %q was started with different durable input",
			api.ErrIdempotencyConflict,
			request.RunID,
		)
	}
	state, err := s.load(ctx, request.RunID)
	if err == nil {
		if state.Task.Status != api.TaskStatusCreated || state.Envelope.ID != "" {
			return existing, &state, true, nil
		}
		return existing, nil, true, nil
	}
	if !errors.Is(err, ErrSingleRunInvalid) {
		return api.Run{}, nil, false, err
	}
	return existing, nil, true, nil
}

func (s *SingleRunner) admissionForStart(
	ctx context.Context,
	runID string,
	runExists bool,
) (api.AdmissionReservation, error) {
	if runExists && RequiresAdmission(s.Governance) {
		return s.loadAdmission(ctx, runID)
	}
	return s.reserveAdmission(ctx, runID)
}

func (s *SingleRunner) runAndRootForStart(
	ctx context.Context,
	request StartSingleRunRequest,
	metadata map[string]string,
	existing api.Run,
	runExists bool,
) (api.Run, api.Task, bool, error) {
	if runExists {
		root, err := s.Runner.Task(ctx, request.RunID, request.RootTaskID)
		if err != nil {
			return api.Run{}, api.Task{}, false, fmt.Errorf("worker: load single-run root task: %w", err)
		}
		return existing, root, false, nil
	}
	started, err := s.Runner.StartRunWithResult(ctx, api.StartRunCommand{
		RunID: request.RunID, RootTaskID: request.RootTaskID,
		Request: request.Request, AgentVersion: s.AgentVersion, Metadata: metadata,
	})
	if err != nil {
		return api.Run{}, api.Task{}, false, fmt.Errorf("worker: start single run: %w", err)
	}
	return started.Run, started.RootTask, started.Created, nil
}

func startOwnedReservation(
	reservation api.AdmissionReservation,
	runCreated bool,
) api.AdmissionReservation {
	if !runCreated {
		return api.AdmissionReservation{}
	}
	return reservation
}

func (s *SingleRunner) startMetadata(request StartSingleRunRequest) (map[string]string, error) {
	metadata := maps.Clone(request.Metadata)
	if metadata == nil {
		metadata = make(map[string]string, 3)
	}
	delete(metadata, singleRunStartDigestMetadata)
	if s.definitionID != "" {
		metadata[singleRunDefinitionIDMetadata] = s.definitionID
		metadata[singleRunDefinitionVersionMetadata] = s.AgentVersion
	}
	identity := struct {
		RunID      string                `json:"runId"`
		RootTaskID string                `json:"rootTaskId"`
		Request    string                `json:"request"`
		Metadata   map[string]string     `json:"metadata"`
		Task       api.CreateTaskCommand `json:"task"`
	}{
		RunID: request.RunID, RootTaskID: request.RootTaskID,
		Request: request.Request, Metadata: metadata,
		Task: s.taskCommand(request, request.RunID, request.RootTaskID),
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return nil, fmt.Errorf("worker: encode single-run start identity: %w", err)
	}
	metadata[singleRunStartDigestMetadata] = fmt.Sprintf("%x", sha256.Sum256(encoded))
	return metadata, nil
}

func (s *SingleRunner) taskCommand(request StartSingleRunRequest, runID, rootTaskID string) api.CreateTaskCommand {
	goal := request.Goal
	if strings.TrimSpace(goal) == "" {
		goal = request.Request
	}
	inputSchema := request.InputSchema
	if len(inputSchema) == 0 {
		inputSchema = s.InputSchema
	}
	outputSchema := request.OutputSchema
	if len(outputSchema) == 0 {
		outputSchema = s.OutputSchema
	}
	return api.CreateTaskCommand{
		RunID: runID, TaskID: request.TaskID, ParentTaskID: rootTaskID,
		Type: api.TaskTypeWorker, Goal: goal, Input: append(json.RawMessage(nil), request.Input...),
		AssignedAgentID: s.Worker.AgentID, OwnerAgentID: s.Worker.AgentID,
		AllowsAction: request.AllowsAction, Tags: slices.Clone(request.Tags),
		CompletionCriteria: slices.Clone(request.CompletionCriteria),
		ReadSelectors:      cloneBlackboardSelectors(request.ReadSelectors),
		WriteTargets:       slices.Clone(request.WriteTargets), RetryPolicy: request.RetryPolicy,
		Budget:         cloneTaskBudget(singleRunBudget(request.Budget, s.Governance.Budget)),
		InputSchema:    append(json.RawMessage(nil), inputSchema...),
		OutputSchema:   append(json.RawMessage(nil), outputSchema...),
		ResourceClaims: slices.Clone(request.ResourceClaims),
	}
}

func (s *SingleRunner) advanceSingleRunToRunning(ctx context.Context, runID string) error {
	path := []api.RunStatus{
		api.RunStatusCreated,
		api.RunStatusPlanning,
		api.RunStatusValidating,
		api.RunStatusRouting,
		api.RunStatusDispatching,
		api.RunStatusRunning,
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		run, err := s.Runner.Run(ctx, runID)
		if err != nil {
			return fmt.Errorf("worker: load single run while advancing: %w", err)
		}
		index := slices.Index(path, run.Status)
		if index < 0 {
			return fmt.Errorf(
				"%w: run %q cannot start from status %s",
				ErrSingleRunInvalid,
				runID,
				run.Status,
			)
		}
		if index == len(path)-1 {
			return nil
		}
		next := path[index+1]
		if err := s.Runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: runID, To: next}); err != nil {
			latest, loadErr := s.Runner.Run(ctx, runID)
			if loadErr == nil && latest.Status != run.Status {
				continue
			}
			return fmt.Errorf("worker: advance single run to %s: %w", next, errors.Join(err, loadErr))
		}
	}
}

// Resume runs durable recovery, repairs a manually suspended or orphaned
// dispatch, and returns the envelope ready for Execute. Approval, user-input,
// and reconciliation suspensions must first be resolved through their owning
// Runner APIs.
func (s *SingleRunner) Resume(ctx context.Context, runID string) (SingleRun, error) {
	if err := s.validate(runID); err != nil {
		return SingleRun{}, err
	}
	if _, err := s.Runner.Recover(ctx, runID); err != nil {
		return SingleRun{}, fmt.Errorf("worker: recover single run: %w", err)
	}
	state, err := s.load(ctx, runID)
	if err != nil {
		return SingleRun{}, err
	}
	if terminalRunStatus(state.Run.Status) || terminalTaskStatus(state.Task.Status) {
		if err := s.reconcileTerminal(ctx, &state); err != nil {
			return SingleRun{}, err
		}
		return s.load(ctx, runID)
	}
	state.Admission, err = s.expireAdmissionIfDue(ctx, state.Admission)
	if err != nil {
		return SingleRun{}, err
	}
	if state.Admission.ID != "" && terminalAdmissionState(state.Admission.State) {
		return SingleRun{}, fmt.Errorf("%w: admission reservation is %s", ErrSingleRunNotResumable, state.Admission.State)
	}

	switch state.Run.Status {
	case api.RunStatusWaitingApproval, api.RunStatusWaitingUserInput, api.RunStatusReconcileRequired:
		return SingleRun{}, fmt.Errorf("%w: resolve run state %s before resume", ErrSingleRunNotResumable, state.Run.Status)
	case api.RunStatusBlocked:
		if err := s.Runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: runID, To: api.RunStatusRunning}); err != nil {
			return SingleRun{}, fmt.Errorf("worker: resume blocked run: %w", err)
		}
	case api.RunStatusRunning, api.RunStatusExecuting:
	default:
		return SingleRun{}, fmt.Errorf("%w: run state %s", ErrSingleRunNotResumable, state.Run.Status)
	}

	if state.Envelope.ID == "" {
		envelope, dispatchErr := s.dispatchTask(ctx, api.DispatchTaskCommand{
			RunID: runID, TaskID: state.Task.ID, TargetAgentID: s.Worker.AgentID,
		})
		if dispatchErr != nil {
			return SingleRun{}, fmt.Errorf("worker: redispatch single run: %w", dispatchErr)
		}
		state.Envelope = envelope
	}
	return s.load(ctx, runID)
}

// Execute activates admission, runs the durable worker path, and reconciles the
// task outcome into run and admission terminal state. Caller cancellation is a
// resumable suspension; Cancel records an explicit terminal cancellation.
func (s *SingleRunner) Execute(ctx context.Context, request ExecuteSingleRunRequest) (SingleRunResult, error) {
	if err := s.validate(request.RunID); err != nil {
		return SingleRunResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return SingleRunResult{}, err
	}
	state, err := s.load(ctx, request.RunID)
	if err != nil {
		return SingleRunResult{}, err
	}
	if terminalTaskStatus(state.Task.Status) {
		if err := s.reconcileTerminal(ctx, &state); err != nil {
			return SingleRunResult{}, err
		}
		return s.resultForTerminal(ctx, state)
	}
	if state.Envelope.ID == "" {
		return SingleRunResult{}, fmt.Errorf("%w: no pending envelope for run %q", ErrSingleRunNotResumable, request.RunID)
	}
	worker, err := s.executionWorker(ctx, state, request.Engine)
	if err != nil {
		return SingleRunResult{}, err
	}
	worker.Runner = s.Runner

	s.lifecycleMu.Lock()
	if s.active == nil {
		s.active = make(map[string]*activeSingleRun)
	}
	if _, exists := s.active[request.RunID]; exists {
		s.lifecycleMu.Unlock()
		return SingleRunResult{}, fmt.Errorf("%w: %s", ErrSingleRunAlreadyExecuting, request.RunID)
	}
	activated, activateErr := s.activateAdmission(ctx, state.Admission)
	if activateErr != nil {
		s.lifecycleMu.Unlock()
		return SingleRunResult{}, activateErr
	}
	state.Admission = activated
	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	entry := &activeSingleRun{cancel: cancel, done: make(chan struct{})}
	entry.stopParent = context.AfterFunc(ctx, func() {
		cancel(fmt.Errorf("%w: %v", ErrSingleRunSuspended, context.Cause(ctx)))
	})
	s.active[request.RunID] = entry
	s.lifecycleMu.Unlock()

	outcome, runErr := worker.ExecuteContinuing(runCtx, ExecuteEnvelopeRequest{
		Envelope: state.Envelope, TTL: request.TTL,
		Messages: append([]message.Message(nil), request.Messages...), Sink: request.Sink,
		OnLeaseAcquired: request.OnLeaseAcquired,
	})
	lifecycleErr := s.finishExecution(context.WithoutCancel(ctx), &state, outcome)
	s.finishActive(request.RunID, entry)

	current, loadErr := s.load(context.WithoutCancel(ctx), request.RunID)
	result := SingleRunResult{Execution: outcome}
	if loadErr == nil {
		result.Run = current.Run
		result.Task = current.Task
		result.Admission = current.Admission
	}
	return result, errors.Join(runErr, lifecycleErr, loadErr)
}

func (s *SingleRunner) executionWorker(
	ctx context.Context,
	state SingleRun,
	override *agent.Engine,
) (AgentWorker, error) {
	if s.definitionID == "" {
		worker := s.Worker
		if override != nil {
			worker.Engine = *override
			worker.Model = override.Model
		}
		return worker, nil
	}
	if override != nil {
		return AgentWorker{}, fmt.Errorf("%w: immutable definition execution does not accept an engine override", ErrSingleRunInvalid)
	}
	definitionID := state.Run.Metadata[singleRunDefinitionIDMetadata]
	version := state.Run.Metadata[singleRunDefinitionVersionMetadata]
	if definitionID == "" || version == "" {
		return AgentWorker{}, fmt.Errorf("%w: run %q has no recorded definition revision", ErrSingleRunNotResumable, state.Run.ID)
	}
	if definitionID != s.definitionID || definitionID != s.Worker.AgentID {
		return AgentWorker{}, fmt.Errorf(
			"%w: run %q belongs to definition %q, coordinator owns %q",
			ErrSingleRunNotOwned, state.Run.ID, definitionID, s.definitionID,
		)
	}
	if version == s.AgentVersion {
		return s.Worker, nil
	}
	if s.restoreWorker == nil {
		return AgentWorker{}, fmt.Errorf("%w: definition %q version %q cannot be restored", ErrSingleRunNotResumable, definitionID, version)
	}
	return s.restoreWorker(ctx, definitionID, version)
}

func (s *SingleRunner) validate(runID string) error {
	if s == nil || s.Runner == nil {
		return ErrRunnerMissing
	}
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("%w: run ID is required", ErrSingleRunInvalid)
	}
	if strings.TrimSpace(s.Worker.AgentID) == "" {
		return ErrAgentIDMissing
	}
	if s.Worker.Runner != nil && s.Worker.Runner != s.Runner {
		return fmt.Errorf("%w: coordinator and worker use different runners", ErrSingleRunInvalid)
	}
	if s.Governance.Budget.MaxCredits > 0 {
		return fmt.Errorf("%w: governance budget maxCredits is unsupported", ErrSingleRunInvalid)
	}
	if s.Governance.Budget.MaxActionCalls > 0 {
		return fmt.Errorf("%w: governance budget maxActionCalls is unsupported", ErrSingleRunInvalid)
	}
	if RequiresAdmission(s.Governance) && s.Admission == nil {
		return ErrAdmissionControllerMissing
	}
	return nil
}

func (s *SingleRunner) load(ctx context.Context, runID string) (SingleRun, error) {
	run, err := s.Runner.Run(ctx, runID)
	if err != nil {
		return SingleRun{}, fmt.Errorf("worker: load single run: %w", err)
	}
	tasks, err := s.Runner.ListTasks(ctx, runID)
	if err != nil {
		return SingleRun{}, fmt.Errorf("worker: list single-run tasks: %w", err)
	}
	root, workerTask, workers := selectSingleRunTasks(run, tasks, s.Worker.AgentID)
	if root.ID == "" || workers != 1 {
		return SingleRun{}, fmt.Errorf("%w: run %q has root=%t matching-worker-tasks=%d", ErrSingleRunInvalid, runID, root.ID != "", workers)
	}
	envelopes, err := s.Runner.ListEnvelopes(ctx, runID)
	if err != nil {
		return SingleRun{}, fmt.Errorf("worker: list single-run envelopes: %w", err)
	}
	state := SingleRun{
		Run: run, RootTask: root, Task: workerTask,
		Envelope: latestPendingEnvelope(envelopes, workerTask.ID),
	}
	if RequiresAdmission(s.Governance) {
		state.Admission, err = s.loadAdmission(ctx, runID)
		if err != nil {
			return SingleRun{}, err
		}
	}
	return state, nil
}

func selectSingleRunTasks(run api.Run, tasks []api.Task, agentID string) (api.Task, api.Task, int) {
	var root api.Task
	var workerTask api.Task
	workers := 0
	for _, task := range tasks {
		if task.ID == run.RootTaskID {
			root = task
			continue
		}
		if task.Type != api.TaskTypeWorker {
			continue
		}
		if task.AssignedAgentID != "" && task.AssignedAgentID != agentID && task.OwnerAgentID != agentID {
			continue
		}
		workerTask = task
		workers++
	}
	return root, workerTask, workers
}

func latestPendingEnvelope(envelopes []api.TaskEnvelope, taskID string) api.TaskEnvelope {
	var latest api.TaskEnvelope
	for _, envelope := range envelopes {
		if envelope.TaskID != taskID || envelope.Status != "pending" {
			continue
		}
		if latest.ID == "" || envelope.TaskVersion > latest.TaskVersion ||
			(envelope.TaskVersion == latest.TaskVersion && envelope.CreatedAt.After(latest.CreatedAt)) {
			latest = envelope
		}
	}
	return latest
}

func (s *SingleRunner) loadAdmission(ctx context.Context, runID string) (api.AdmissionReservation, error) {
	reservations, err := s.Runner.ListAdmissionReservations(ctx, api.AdmissionReservationSelector{
		AgentIDs: []string{s.Worker.AgentID}, RunIDs: []string{runID},
	})
	if err != nil {
		return api.AdmissionReservation{}, fmt.Errorf("worker: list single-run admission: %w", err)
	}
	var latest api.AdmissionReservation
	for _, reservation := range reservations {
		if latest.ID == "" || reservation.CreatedAt.After(latest.CreatedAt) {
			latest = reservation
		}
	}
	if latest.ID == "" {
		return api.AdmissionReservation{}, fmt.Errorf("%w: admission reservation missing", ErrSingleRunInvalid)
	}
	return latest, nil
}

func (s *SingleRunner) finishActive(runID string, entry *activeSingleRun) {
	s.lifecycleMu.Lock()
	if entry.stopParent != nil {
		entry.stopParent()
	}
	delete(s.active, runID)
	close(entry.done)
	s.lifecycleMu.Unlock()
}

func singleRunBudget(explicit *api.TaskBudget, governance api.Budget) *api.TaskBudget {
	var requested api.TaskBudget
	if explicit != nil {
		requested = *explicit
	}
	budget := &api.TaskBudget{
		MaxTokens:    intersectBudgetInt64(requested.MaxTokens, governance.MaxTokens),
		MaxWallClock: intersectBudgetDuration(requested.MaxWallClock, governance.MaxRuntime),
		MaxToolCalls: intersectBudgetInt(requested.MaxToolCalls, governance.MaxToolCalls),
		MaxSteps:     intersectBudgetInt(requested.MaxSteps, governance.MaxModelCalls),
	}
	if budget.MaxTokens == 0 && budget.MaxWallClock == 0 &&
		budget.MaxToolCalls == 0 && budget.MaxSteps == 0 {
		return nil
	}
	return budget
}

func intersectBudgetInt64(requested, ceiling int64) int64 {
	if requested < 0 {
		requested = 0
	}
	if ceiling < 0 {
		ceiling = 0
	}
	if requested == 0 {
		return ceiling
	}
	if ceiling == 0 || requested < ceiling {
		return requested
	}
	return ceiling
}

func intersectBudgetInt(requested, ceiling int) int {
	if requested < 0 {
		requested = 0
	}
	if ceiling < 0 {
		ceiling = 0
	}
	if requested == 0 {
		return ceiling
	}
	if ceiling == 0 || requested < ceiling {
		return requested
	}
	return ceiling
}

func intersectBudgetDuration(requested, ceiling time.Duration) time.Duration {
	if requested < 0 {
		requested = 0
	}
	if ceiling < 0 {
		ceiling = 0
	}
	if requested == 0 {
		return ceiling
	}
	if ceiling == 0 || requested < ceiling {
		return requested
	}
	return ceiling
}

func cloneTaskBudget(budget *api.TaskBudget) *api.TaskBudget {
	if budget == nil {
		return nil
	}
	cloned := *budget
	return &cloned
}

func (s *SingleRunner) dispatchTask(ctx context.Context, command api.DispatchTaskCommand) (api.TaskEnvelope, error) {
	for {
		envelope, err := s.Runner.DispatchTask(ctx, command)
		if err == nil {
			return envelope, nil
		}
		if !errors.Is(err, api.ErrIdempotencyConflict) {
			return api.TaskEnvelope{}, err
		}
		if envelope, found, loadErr := taskEnvelope(ctx, s.Runner, command.RunID, command.TaskID, "pending"); loadErr != nil {
			return api.TaskEnvelope{}, loadErr
		} else if found {
			return envelope, nil
		}
		if err := ctx.Err(); err != nil {
			return api.TaskEnvelope{}, err
		}
	}
}

func cloneBlackboardSelectors(selectors []api.BlackboardSelector) []api.BlackboardSelector {
	if selectors == nil {
		return nil
	}
	cloned := make([]api.BlackboardSelector, len(selectors))
	for i, selector := range selectors {
		cloned[i] = selector
		cloned[i].SourceTypes, cloned[i].SourceIDs = cloneSelectorSources(selector)
	}
	return cloned
}

func cloneSelectorSources(selector api.BlackboardSelector) ([]api.SourceType, []string) {
	sourceTypes := slices.Clone(selector.SourceTypes)
	sourceIDs := slices.Clone(selector.SourceIDs)
	for _, agentID := range deprecatedSelectorAgentIDs(selector) {
		if !slices.Contains(sourceTypes, api.SourceAgent) {
			sourceTypes = append(sourceTypes, api.SourceAgent)
		}
		if !slices.Contains(sourceIDs, agentID) {
			sourceIDs = append(sourceIDs, agentID)
		}
	}
	return sourceTypes, sourceIDs
}

func deprecatedSelectorAgentIDs(selector api.BlackboardSelector) []string {
	//lint:ignore SA1019 SourceAgentIDs is normalized at the worker boundary for backward compatibility.
	return slices.Clone(selector.SourceAgentIDs)
}
