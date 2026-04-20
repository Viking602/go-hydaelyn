package host

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/middleware"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
)

type StartTeamRequest struct {
	TeamID            string
	Pattern           string
	Planner           string
	SupervisorProfile string
	WorkerProfiles    []string
	Input             map[string]any
	Metadata          map[string]string
	Agent             AgentOptions
}

func (r *Runtime) StartTeam(ctx context.Context, request StartTeamRequest) (team.RunState, error) {
	var state team.RunState
	err := r.runStage(ctx, &middleware.Envelope{
		Stage:     middleware.StageTeam,
		Operation: "start",
		Metadata:  cloneStringMap(request.Metadata),
		Request:   request,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		next, runErr := r.startTeamPrepared(ctx, request, true)
		if runErr != nil {
			return runErr
		}
		state = next
		envelope.TeamID = next.ID
		envelope.Response = next
		return nil
	})
	return state, err
}

func (r *Runtime) ResumeTeam(ctx context.Context, teamID string) (team.RunState, error) {
	var state team.RunState
	err := r.runStage(ctx, &middleware.Envelope{
		Stage:     middleware.StageTeam,
		Operation: "resume",
		TeamID:    teamID,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		next, runErr := r.resumeTeam(ctx, teamID)
		if runErr != nil {
			return runErr
		}
		state = next
		envelope.Response = next
		return nil
	})
	return state, err
}

func (r *Runtime) startTeamPrepared(ctx context.Context, request StartTeamRequest, drain bool) (team.RunState, error) {
	pattern, err := r.lookupPattern(request.Pattern)
	if err != nil {
		return team.RunState{}, err
	}
	if request.TeamID == "" {
		request.TeamID = r.nextTeamID()
	}
	if len(request.Agent.OutputGuardrails) > 0 {
		r.storeInlineTeamOutputGuardrails(request.TeamID, request.Agent.OutputGuardrails)
	}
	if _, err := r.lookupProfile(request.SupervisorProfile); err != nil {
		return team.RunState{}, err
	}
	for _, name := range request.WorkerProfiles {
		if _, err := r.lookupProfile(name); err != nil {
			return team.RunState{}, err
		}
	}
	startRequest := team.StartRequest{
		TeamID:            request.TeamID,
		Pattern:           request.Pattern,
		Planner:           request.Planner,
		SupervisorProfile: request.SupervisorProfile,
		WorkerProfiles:    request.WorkerProfiles,
		Input:             request.Input,
		AgentOptions:      toTeamAgentOptions(request.Agent),
		Metadata:          request.Metadata,
	}
	var state team.RunState
	if request.Planner != "" {
		state, err = r.startPlannedTeam(ctx, pattern, startRequest)
		if err != nil {
			return team.RunState{}, err
		}
	} else {
		state, err = pattern.Start(ctx, startRequest)
		if err != nil {
			return team.RunState{}, err
		}
	}
	if state.ID == "" {
		state.ID = request.TeamID
	}
	if state.Pattern == "" {
		state.Pattern = request.Pattern
	}
	state.AgentOptions = mergeTeamAgentOptions(state.AgentOptions, startRequest.AgentOptions)
	state.Normalize()
	if err := r.validateTeamState(state); err != nil {
		return team.RunState{}, err
	}
	teamSession, err := r.ensureTeamSession(ctx, state)
	if err != nil {
		return team.RunState{}, err
	}
	state.SessionID = teamSession.ID
	if err := r.recordInitialEvents(ctx, state); err != nil {
		return team.RunState{}, err
	}
	if err := r.storage.Teams().Save(ctx, state); err != nil {
		return team.RunState{}, err
	}
	teamCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.activeTeams[state.ID] = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.activeTeams, state.ID)
		r.mu.Unlock()
	}()
	if !drain && r.queue != nil {
		if err := r.enqueueRunnableTasks(ctx, state); err != nil {
			return team.RunState{}, err
		}
		if err := r.saveTeam(ctx, &state); err != nil {
			return team.RunState{}, err
		}
		return state, nil
	}
	return r.driveTeam(teamCtx, pattern, state)
}

func (r *Runtime) AbortTeam(ctx context.Context, teamID string) error {
	return r.abortTeam(ctx, teamID)
}

func (r *Runtime) driveTeam(ctx context.Context, pattern team.Pattern, state team.RunState) (team.RunState, error) {
	current := state
	current.Normalize()
	for range r.maxDriveSteps() {
		next, terminal, err := r.driveTeamStep(ctx, pattern, current)
		if err != nil {
			return team.RunState{}, err
		}
		current = next
		if terminal {
			return current, nil
		}
	}
	current.Status = team.StatusFailed
	if current.Result == nil {
		current.Result = &team.Result{Error: "team exceeded execution steps"}
	}
	_ = r.saveTeam(ctx, &current)
	return current, nil
}

func (r *Runtime) maxDriveSteps() int {
	if r.maxTeamDriveSteps > 0 {
		return r.maxTeamDriveSteps
	}
	return defaultMaxTeamDriveSteps
}

func (r *Runtime) executeTasks(ctx context.Context, state team.RunState) (team.RunState, error) {
	current := state
	current.Normalize()
	runnable, runnableSet := runnableTaskSet(current)
	if len(runnable) == 0 {
		return current, nil
	}
	if guarded, blocked := r.blockGuardedSynthesis(current, runnableSet); blocked {
		guarded.UpdatedAt = time.Now().UTC()
		return guarded, nil
	}
	semByProfile, err := r.buildProfileSemaphores(current, runnableSet)
	if err != nil {
		return team.RunState{}, err
	}
	var outcomes <-chan taskOutcome
	if r.queue != nil {
		outcomes, err = r.executeViaQueue(ctx, current, runnableSet, semByProfile)
		if err != nil {
			return team.RunState{}, err
		}
	} else {
		outcomes = r.executeRunnableTasks(ctx, current, runnableSet, semByProfile)
	}
	for outcome := range outcomes {
		before := current.Tasks[outcome.index]
		updated, applied, blackboardPublished, messagePublished, applyErr := r.applyTaskOutcome(ctx, current, outcome.index, outcome.task)
		if applyErr != nil {
			return team.RunState{}, applyErr
		}
		if !applied {
			continue
		}
		current = updated
		task := current.Tasks[outcome.index]
		eventCtx := withTaskEventContext(ctx, taskEventContext{LeaseID: outcome.leaseID, WorkerID: outcome.workerID})
		if errors.Is(outcome.err, context.Canceled) {
			r.recordTaskCancelledEvent(ctx, current, task, "cancellation_propagated")
		}
		switch task.Status {
		case team.TaskStatusRunning:
			r.recordTaskLifecycleEvent(eventCtx, current, before, task, storage.EventTaskStarted)
		case team.TaskStatusCompleted:
			r.recordTaskLifecycleEvent(eventCtx, current, before, task, storage.EventTaskCompleted)
			if task.Kind == team.TaskKindVerify || task.Stage == team.TaskStageVerify {
				r.recordVerifierDecisionEvent(ctx, current, task)
			}
			if task.Kind == team.TaskKindSynthesize || task.Stage == team.TaskStageSynthesize {
				r.recordSynthesisCommittedEvent(ctx, current, task)
			}
		case team.TaskStatusFailed:
			r.recordTaskLifecycleEvent(eventCtx, current, before, task, storage.EventTaskFailed)
		}
		if messagePublished {
			if agentInstance, ok := current.Agent(task.EffectiveAssigneeAgentID()); ok {
				r.publishTaskOutputMessages(eventCtx, current, task, agentInstance)
			}
		}
		if blackboardPublished {
			r.recordTaskOutputsPublishedEvent(eventCtx, current, task)
		}
	}
	current.UpdatedAt = time.Now().UTC()
	return current, nil
}

func (r *Runtime) blockGuardedSynthesis(state team.RunState, runnableSet map[string]struct{}) (team.RunState, bool) {
	current := state
	blocked := false
	now := time.Now().UTC()
	for idx, task := range current.Tasks {
		if _, ok := runnableSet[task.ID]; !ok {
			continue
		}
		reason, shouldBlock := synthesisVerifierBlockReason(current, task)
		if !shouldBlock {
			continue
		}
		task.Status = team.TaskStatusFailed
		task.Error = reason
		task.Result = &team.Result{Error: reason}
		task.FinishedAt = now
		current.Tasks[idx] = task
		blocked = true
	}
	return current, blocked
}

func synthesisVerifierBlockReason(state team.RunState, task team.Task) (string, bool) {
	if !task.VerifierRequired {
		return "", false
	}
	if task.Kind != team.TaskKindSynthesize && task.Stage != team.TaskStageSynthesize {
		return "", false
	}
	verifiers := verifierDependenciesForTask(state, task)
	if len(verifiers) == 0 {
		return fmt.Sprintf("task %s blocked: missing verifier dependencies for guarded synthesis", task.ID), true
	}
	if state.Blackboard == nil {
		return fmt.Sprintf("task %s blocked: missing verifier evidence", task.ID), true
	}
	for _, verifier := range verifiers {
		decision, status, ok := verifierGateEvidence(state.Blackboard, verifier)
		if !ok {
			return fmt.Sprintf("task %s blocked: missing verifier evidence for %s", task.ID, verifier.ID), true
		}
		if decision != verifierGatePassDecision {
			return fmt.Sprintf("task %s blocked by verifier %s (%s)", task.ID, verifier.ID, status), true
		}
	}
	return "", false
}

func verifierDependenciesForTask(state team.RunState, task team.Task) []team.Task {
	if len(task.DependsOn) == 0 {
		return nil
	}
	index := make(map[string]team.Task, len(state.Tasks))
	for _, current := range state.Tasks {
		index[current.ID] = current
	}
	items := make([]team.Task, 0, len(task.DependsOn))
	seen := map[string]struct{}{}
	added := map[string]struct{}{}
	queue := append([]string{}, task.DependsOn...)
	for len(queue) > 0 {
		dependencyID := queue[0]
		queue = queue[1:]
		if _, ok := seen[dependencyID]; ok {
			continue
		}
		seen[dependencyID] = struct{}{}
		dependency, ok := index[dependencyID]
		if !ok {
			continue
		}
		if dependency.Kind == team.TaskKindVerify || dependency.Stage == team.TaskStageVerify {
			if _, ok := added[dependency.ID]; ok {
				continue
			}
			items = append(items, dependency)
			added[dependency.ID] = struct{}{}
		}
		queue = append(queue, dependency.DependsOn...)
	}
	return items
}

func verifierGateEvidence(board *blackboard.State, task team.Task) (string, string, bool) {
	if board == nil {
		return "", "", false
	}
	for _, exchange := range board.ExchangesForTask(task.ID) {
		decision, status, ok := verifierGateDecisionFromExchange(exchange)
		if ok {
			return decision, status, true
		}
	}
	return "", "", false
}

func verifierGateDecisionFromExchange(exchange blackboard.Exchange) (string, string, bool) {
	decision := strings.TrimSpace(exchange.Metadata[verifierGateDecisionField])
	status := strings.ToLower(strings.TrimSpace(exchange.Metadata[verifierGateStatusField]))
	if decision == "" && len(exchange.Structured) > 0 {
		if value, ok := exchange.Structured[verifierGateDecisionField].(string); ok {
			decision = strings.TrimSpace(value)
		}
		if value, ok := exchange.Structured[verifierGateStatusField].(string); ok {
			status = strings.TrimSpace(value)
		}
	}
	if status == "" {
		status = strings.ToLower(string(blackboard.InferVerificationStatus(exchange.Text)))
	}
	decision = normalizeVerificationDecision(decision)
	if decision == "" {
		decision = normalizeVerificationDecision(status)
	}
	if decision == "" {
		return "", status, false
	}
	return decision, status, true
}

func normalizeVerificationDecision(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case verifierGatePassDecision, "passed", "approved", "supported":
		return verifierGatePassDecision
	case verifierGateBlockDecision, "blocked", "fail", "failed", "rejected", "unsupported", "contradicted", "insufficient", "unknown":
		return verifierGateBlockDecision
	default:
		return ""
	}
}

func (r *Runtime) applyTaskOutcome(ctx context.Context, state team.RunState, index int, item team.Task) (team.RunState, bool, bool, bool, error) {
	current := state.Tasks[index]
	if current.Version != item.Version || current.IsTerminal() {
		return state, false, false, false, nil
	}
	if item.Status == team.TaskStatusCompleted {
		item = r.markTaskCompletion(item)
	}
	state.Tasks[index] = item
	if taskFailureCancelsDependents(item) {
		state = abortDependentTasks(state, item)
	}
	if item.Status != team.TaskStatusCompleted {
		return state, true, false, false, nil
	}
	options, err := r.resolvedAgentOptionsForTask(state, item)
	if err != nil {
		return state, false, false, false, err
	}
	messageResult := item.Result
	blackboardResult, allowBlackboard, err := r.applyTeamOutputGuardrails(ctx, state.ID, item.ID, TeamOutputBoundaryBlackboard, item.Result, options.TeamOutputGuardrails, map[string]string{
		"teamId": state.ID,
		"taskId": item.ID,
		"runId":  state.ID,
	})
	if err != nil {
		return state, false, false, false, err
	}
	if blackboardResult != nil {
		item.Result = blackboardResult
		messageResult = blackboardResult
	}
	var blackboardPublished bool
	if allowBlackboard {
		state.Tasks[index] = item
		state = r.applyBlackboardUpdate(state, item)
		blackboardPublished = true
	}
	taskOutputResult, allowTaskOutput, err := r.applyTeamOutputGuardrails(ctx, state.ID, item.ID, TeamOutputBoundaryTaskOutput, messageResult, options.TeamOutputGuardrails, map[string]string{
		"teamId": state.ID,
		"taskId": item.ID,
		"runId":  state.ID,
	})
	if err != nil {
		return state, false, false, false, err
	}
	if taskOutputResult != nil {
		item.Result = taskOutputResult
	}
	state.Tasks[index] = item
	return state, true, blackboardPublished, allowTaskOutput, nil
}

func taskFailureCancelsDependents(task team.Task) bool {
	if !task.BlocksTeamOnFailure() {
		return false
	}
	switch task.Status {
	case team.TaskStatusFailed, team.TaskStatusAborted:
		return true
	default:
		return false
	}
}

func abortDependentTasks(state team.RunState, task team.Task) team.RunState {
	now := time.Now().UTC()
	reason := fmt.Sprintf("cancelled because dependency %s ended with status %s", task.ID, task.Status)
	blocked := map[string]struct{}{task.ID: {}}
	changed := true
	for changed {
		changed = false
		for idx, current := range state.Tasks {
			if current.IsTerminal() || !dependsOnAny(current, blocked) {
				continue
			}
			current.Status = team.TaskStatusAborted
			current.Error = reason
			current.Result = &team.Result{Error: reason}
			current.FinishedAt = now
			state.Tasks[idx] = current
			blocked[current.ID] = struct{}{}
			changed = true
		}
	}
	return state
}

func dependsOnAny(task team.Task, ids map[string]struct{}) bool {
	for _, dep := range task.DependsOn {
		if _, ok := ids[dep]; ok {
			return true
		}
	}
	return false
}

func (r *Runtime) ensureCommittedTaskOutputs(state team.RunState) team.RunState {
	for _, task := range state.Tasks {
		if !task.HasAuthoritativeCompletion() || task.Result == nil {
			continue
		}
		if hasCommittedBlackboardOutputs(state, task) {
			continue
		}
		state = r.applyBlackboardUpdate(state, task)
	}
	return state
}

func hasCommittedBlackboardOutputs(state team.RunState, task team.Task) bool {
	if !needsBlackboard(task) {
		return true
	}
	if state.Blackboard == nil {
		return false
	}
	if task.Kind == team.TaskKindResearch || task.Kind == team.TaskKindVerify {
		if len(state.Blackboard.ClaimsForTask(task.ID)) == 0 && len(state.Blackboard.ExchangesForTask(task.ID)) == 0 {
			return false
		}
	}
	if task.PublishesTo(team.OutputVisibilityBlackboard) && len(state.Blackboard.ExchangesForTask(task.ID)) == 0 {
		return false
	}
	return true
}

func (r *Runtime) markTaskCompletion(task team.Task) team.Task {
	if task.Status != team.TaskStatusCompleted {
		return task
	}
	now := time.Now().UTC()
	if task.CompletedAt.IsZero() {
		task.CompletedAt = now
	}
	if task.FinishedAt.IsZero() {
		task.FinishedAt = task.CompletedAt
	}
	if task.CompletedBy == "" {
		task.CompletedBy = r.workerID
	}
	return task
}

type taskOutcome struct {
	index    int
	task     team.Task
	err      error
	leaseID  string
	workerID string
}

func (r *Runtime) driveTeamStep(ctx context.Context, pattern team.Pattern, current team.RunState) (team.RunState, bool, error) {
	if current.IsTerminal() {
		if err := r.saveTeam(ctx, &current); err != nil {
			return team.RunState{}, false, err
		}
		return current, true, nil
	}
	if next, progressed, terminal, err := r.resolveBlockedOrRunnable(ctx, current); progressed || err != nil || terminal {
		return next, terminal, err
	}
	if next, progressed, terminal, err := r.reviewPlannedTeam(ctx, current); progressed || err != nil || terminal {
		return next, terminal, err
	}
	next, err := r.advancePatternState(ctx, pattern, current)
	if err != nil {
		return team.RunState{}, false, err
	}
	terminal, err := r.persistTeamProgress(ctx, current, next)
	if err != nil {
		return team.RunState{}, false, err
	}
	return terminal, terminal.IsTerminal(), nil
}

func (r *Runtime) resolveBlockedOrRunnable(ctx context.Context, current team.RunState) (team.RunState, bool, bool, error) {
	if next, changed := current.ResolveBlockedTasks(); changed {
		terminal, err := r.persistTeamProgress(ctx, current, next)
		if err != nil {
			return team.RunState{}, false, false, err
		}
		return terminal, true, terminal.IsTerminal(), nil
	}
	if len(current.RunnableTasks()) == 0 {
		return current, false, false, nil
	}
	next, err := r.executeTasks(ctx, current)
	if err != nil {
		return team.RunState{}, false, false, err
	}
	terminal, err := r.persistTeamProgress(ctx, current, next)
	if err != nil {
		return team.RunState{}, false, false, err
	}
	return terminal, true, terminal.IsTerminal(), nil
}

func (r *Runtime) advancePatternState(ctx context.Context, pattern team.Pattern, current team.RunState) (team.RunState, error) {
	var next team.RunState
	err := r.runStage(ctx, &middleware.Envelope{
		Stage:     phaseStage(current.Phase),
		Operation: "advance",
		TeamID:    current.ID,
		Metadata: map[string]string{
			"phase": string(current.Phase),
		},
		Request: current,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		advanced, runErr := pattern.Advance(ctx, current)
		if runErr != nil {
			return runErr
		}
		next = advanced
		envelope.Response = advanced
		return nil
	})
	if err != nil {
		return team.RunState{}, err
	}
	if next.ID == "" {
		next.ID = current.ID
	}
	if next.SessionID == "" {
		next.SessionID = current.SessionID
	}
	next.Normalize()
	if err := r.validateTeamState(next); err != nil {
		return team.RunState{}, err
	}
	return next, nil
}

func (r *Runtime) persistTeamProgress(ctx context.Context, previous, current team.RunState) (team.RunState, error) {
	current.Normalize()
	if err := r.recordNewTaskScheduledEvents(ctx, previous, current); err != nil {
		return team.RunState{}, err
	}
	if failed := current.FirstBlockingFailure(); failed != nil {
		current = failTeam(current, *failed)
	}
	if err := r.saveTeam(ctx, &current); err != nil {
		return team.RunState{}, err
	}
	return current, nil
}

func (r *Runtime) buildProfileSemaphores(current team.RunState, runnableSet map[string]struct{}) (map[string]chan struct{}, error) {
	semByProfile := map[string]chan struct{}{}
	for _, task := range current.Tasks {
		if _, ok := runnableSet[task.ID]; !ok {
			continue
		}
		_, profile, err := r.resolveTaskExecution(current, task)
		if err != nil {
			return nil, err
		}
		if profile.MaxConcurrency > 0 {
			if _, ok := semByProfile[profile.Name]; !ok {
				semByProfile[profile.Name] = make(chan struct{}, profile.MaxConcurrency)
			}
		}
	}
	return semByProfile, nil
}

func (r *Runtime) executeRunnableTasks(ctx context.Context, current team.RunState, runnableSet map[string]struct{}, semByProfile map[string]chan struct{}) <-chan taskOutcome {
	runnableCount := len(runnableSet)
	results := make(chan taskOutcome, runnableCount)
	var wg sync.WaitGroup

	// siblingCtx isolates cancellation across the batch: when one task fails
	// terminally we cancel the context for its siblings so they abort quickly
	// without tearing down the parent runtime.
	siblingCtx, siblingCancel := context.WithCancel(ctx)
	defer siblingCancel()
	var cancelOnce sync.Once

	for idx, task := range current.Tasks {
		if _, ok := runnableSet[task.ID]; !ok {
			continue
		}
		wg.Add(1)
		go func(index int, original team.Task) {
			defer wg.Done()

			// Child context aborts when siblingCtx aborts, but not vice-versa.
			taskCtx, taskCancel := context.WithCancel(siblingCtx)
			defer taskCancel()

			agentInstance, profile, err := r.resolveTaskExecution(current, original)
			if err != nil {
				failed, _ := finalizeTaskFailure(original, err)
				results <- taskOutcome{index: index, task: failed, err: err, workerID: r.workerID}
				if shouldCancelSiblingBatch(failed, err) {
					cancelOnce.Do(siblingCancel)
				}
				return
			}
			if sem, ok := semByProfile[profile.Name]; ok {
				sem <- struct{}{}
				defer func() { <-sem }()
			}
			item, err := r.executeTask(taskCtx, current, original, agentInstance, profile)
			if shouldCancelSiblingBatch(item, err) {
				cancelOnce.Do(siblingCancel)
			}
			results <- taskOutcome{index: index, task: item, err: err, workerID: r.workerID}
		}(idx, task)
	}
	wg.Wait()
	close(results)
	return results
}

func shouldCancelSiblingBatch(task team.Task, err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	// Only cancel siblings when this task has reached a terminal failure
	// that blocks the team. Retryable failures leave the task in Pending,
	// and skip-optional failures end as Skipped — neither should tear the
	// batch down before the engine has a chance to retry or proceed.
	switch task.Status {
	case team.TaskStatusFailed, team.TaskStatusAborted:
		return task.BlocksTeamOnFailure()
	default:
		return false
	}
}

func (r *Runtime) executeTask(ctx context.Context, state team.RunState, task team.Task, agentInstance team.AgentInstance, profile team.Profile) (team.Task, error) {
	var output team.Task
	err := r.runStage(ctx, &middleware.Envelope{
		Stage:     middleware.StageTask,
		Operation: "execute",
		TeamID:    state.ID,
		TaskID:    task.ID,
		AgentID:   agentInstance.ID,
		Metadata: map[string]string{
			"profile": profile.Name,
		},
		Request: task,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		item, runErr := r.executeTaskCore(ctx, state, task, agentInstance, profile)
		output = item
		envelope.Response = item
		return runErr
	})
	return output, err
}

func (r *Runtime) executeTaskCore(ctx context.Context, state team.RunState, task team.Task, agentInstance team.AgentInstance, profile team.Profile) (team.Task, error) {
	task.Normalize()
	before := task
	task.Attempts++
	task.Status = team.TaskStatusRunning
	task.StartedAt = time.Now().UTC()
	r.recordTaskLifecycleEvent(ctx, state, before, task, storage.EventTaskStarted)
	var err error
	task, err = r.ensureTaskSession(ctx, state, task, agentInstance, profile)
	if err != nil {
		return finalizeTaskFailure(task, err)
	}
	initialMessages, err := r.loadInitialTaskMessages(ctx, state, task, agentInstance, profile)
	if err != nil {
		return finalizeTaskFailure(task, err)
	}
	generated, err := r.runTaskEngine(ctx, state, task, agentInstance, profile, initialMessages)
	if err != nil {
		return finalizeTaskFailure(task, err)
	}
	if err := r.persistTaskMessages(ctx, task, generated.Messages); err != nil {
		return finalizeTaskFailure(task, err)
	}
	task.Status = team.TaskStatusCompleted
	task.Result = r.buildTaskResult(task, generated.Messages)
	task.Result.Usage = generated.Usage
	task.Result.ToolCallCount = len(generated.ToolResults)
	task.FinishedAt = time.Now().UTC()
	r.recordTaskToolEvents(ctx, state, task, generated.ToolResults)
	return task, nil
}

func (r *Runtime) initialTaskMessages(state team.RunState, task team.Task, agentInstance team.AgentInstance, profile team.Profile) []message.Message {
	messages := make([]message.Message, 0, 2)
	if strings.TrimSpace(profile.Prompt) != "" {
		system := message.NewText(message.RoleSystem, profile.Prompt)
		system.TeamID = state.ID
		system.AgentID = agentInstance.ID
		system.Visibility = message.VisibilityPrivate
		messages = append(messages, system)
	}
	user := message.NewText(message.RoleUser, task.Input)
	user.TeamID = state.ID
	user.AgentID = agentInstance.ID
	user.Visibility = message.VisibilityPrivate
	messages = append(messages, user)
	return messages
}

func (r *Runtime) ensureTeamSession(ctx context.Context, state team.RunState) (session.Session, error) {
	if state.SessionID != "" {
		snapshot, err := r.loadSession(ctx, state.SessionID)
		if err == nil {
			return snapshot.Session, nil
		}
	}
	current, err := r.createSession(ctx, session.CreateParams{
		ID:     fmt.Sprintf("%s-session", state.ID),
		TeamID: state.ID,
		Scope:  message.VisibilityShared,
		Metadata: map[string]string{
			"pattern": state.Pattern,
		},
	})
	if err != nil {
		return session.Session{}, err
	}
	if query, ok := state.Input["query"].(string); ok && strings.TrimSpace(query) != "" {
		root := message.NewText(message.RoleUser, query)
		root.TeamID = state.ID
		root.Visibility = message.VisibilityShared
		_, _ = r.appendSessionMessages(ctx, current.ID, root)
	}
	return current, nil
}

func (r *Runtime) resolveTaskExecution(state team.RunState, task team.Task) (team.AgentInstance, team.Profile, error) {
	agentInstance, ok := state.Agent(task.EffectiveAssigneeAgentID())
	if !ok {
		return team.AgentInstance{}, team.Profile{}, fmt.Errorf("%w: task %s references unknown agent %s", ErrInvalidTeamState, task.ID, task.EffectiveAssigneeAgentID())
	}
	profile, err := r.lookupProfile(agentInstance.EffectiveProfileName())
	if err != nil {
		return team.AgentInstance{}, team.Profile{}, err
	}
	if task.RequiredRole != "" && agentInstance.Role != task.RequiredRole {
		return team.AgentInstance{}, team.Profile{}, fmt.Errorf("%w: task %s requires role %s but agent %s has role %s", ErrInvalidTeamState, task.ID, task.RequiredRole, agentInstance.ID, agentInstance.Role)
	}
	return agentInstance, profile, nil
}

func finalizeTaskFailure(task team.Task, err error) (team.Task, error) {
	task.Error = err.Error()
	if errors.Is(err, context.Canceled) {
		task.Status = team.TaskStatusAborted
		task.Result = &team.Result{Error: err.Error()}
		task.FinishedAt = time.Now().UTC()
		return task, err
	}
	if task.CanRetry() {
		task.Status = team.TaskStatusPending
		return task, nil
	}
	if task.FailurePolicy == team.FailurePolicySkipOptional {
		task.Status = team.TaskStatusSkipped
		task.Result = &team.Result{Error: err.Error()}
		task.FinishedAt = time.Now().UTC()
		return task, nil
	}
	task.Status = team.TaskStatusFailed
	task.Result = &team.Result{Error: err.Error()}
	task.FinishedAt = time.Now().UTC()
	if task.FailurePolicy == team.FailurePolicyDegrade {
		return task, nil
	}
	return task, err
}

func failTeam(state team.RunState, failed team.Task) team.RunState {
	state.Status = team.StatusFailed
	state.UpdatedAt = time.Now().UTC()
	if state.Result == nil {
		state.Result = &team.Result{}
	}
	state.Result.Error = fmt.Sprintf("task %s failed: %s", failed.ID, failed.Error)
	return state
}

func (r *Runtime) saveTeam(ctx context.Context, state *team.RunState) error {
	state.Normalize()
	if state.IsTerminal() && state.Result != nil {
		finalResult, allowed, err := r.applyTeamOutputGuardrails(ctx, state.ID, "", TeamOutputBoundaryFinal, state.Result, state.AgentOptions.TeamOutputGuardrails, map[string]string{
			"runId":  state.ID,
			"teamId": state.ID,
		})
		if err != nil {
			return err
		}
		if finalResult != nil {
			state.Result = finalResult
		}
		if !allowed {
			state.Status = team.StatusFailed
			state.Result = &team.Result{Error: "team final result blocked by output guardrail"}
		}
	}
	version, err := r.storage.Teams().SaveCAS(ctx, *state, state.Version)
	if err != nil {
		return err
	}
	state.Version = version
	r.recordTeamTerminalEvent(ctx, *state)
	return nil
}

func (r *Runtime) ensureTaskSession(ctx context.Context, state team.RunState, task team.Task, agentInstance team.AgentInstance, profile team.Profile) (team.Task, error) {
	if task.SessionID != "" {
		return task, nil
	}
	workerSession, err := r.createSession(ctx, session.CreateParams{
		TeamID:  state.ID,
		AgentID: agentInstance.ID,
		Scope:   message.VisibilityPrivate,
		Branch:  task.ID,
		Metadata: map[string]string{
			"agentId": agentInstance.ID,
			"kind":    string(task.Kind),
			"profile": profile.Name,
		},
	})
	if err != nil {
		return task, err
	}
	task.SessionID = workerSession.ID
	return task, nil
}

func (r *Runtime) loadInitialTaskMessages(ctx context.Context, state team.RunState, task team.Task, agentInstance team.AgentInstance, profile team.Profile) ([]message.Message, error) {
	snapshot, err := r.loadSession(ctx, task.SessionID)
	if err != nil {
		return nil, err
	}
	if len(snapshot.Messages) > 0 {
		return r.compactMessages(ctx, snapshot.Messages), nil
	}
	initialMessages := r.initialTaskMessages(state, task, agentInstance, profile)
	if materialized, text := r.materializeTaskInputs(state, task); len(materialized) > 0 && strings.TrimSpace(text) != "" {
		input := message.NewText(message.RoleUser, text)
		input.TeamID = state.ID
		input.AgentID = agentInstance.ID
		input.Visibility = message.VisibilityPrivate
		input.Metadata = map[string]string{
			"taskId":           task.ID,
			"materializedRead": strings.Join(task.Reads, ","),
		}
		initialMessages = append(initialMessages, input)
		r.recordTaskInputsMaterializedEvent(ctx, state, task, materialized)
	}
	if _, err := r.appendSessionMessages(ctx, task.SessionID, initialMessages...); err != nil {
		return nil, err
	}
	return initialMessages, nil
}

func (r *Runtime) runTaskEngine(ctx context.Context, state team.RunState, task team.Task, agentInstance team.AgentInstance, profile team.Profile, initialMessages []message.Message) (taskExecution, error) {
	initialMessages = r.compactMessages(ctx, initialMessages)
	currentProvider, err := r.lookupProvider(profile.Provider)
	if err != nil {
		return taskExecution{}, err
	}
	options, err := r.resolvedAgentOptionsForTask(state, task)
	if err != nil {
		return taskExecution{}, err
	}
	outputGuardrails, err := r.resolveOutputGuardrails(options)
	if err != nil {
		return taskExecution{}, err
	}
	engine := agent.Engine{
		Provider: currentProvider,
		Tools:    r.tools.Subset(profile.ToolNames),
		Hooks:    r.engineHooks(),
	}
	var result agent.Result
	err = r.runStage(ctx, &middleware.Envelope{
		Stage:     middleware.StageAgent,
		Operation: "run",
		TeamID:    state.ID,
		TaskID:    task.ID,
		AgentID:   agentInstance.ID,
		Metadata: map[string]string{
			"profile": profile.Name,
		},
		Request: initialMessages,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		metadata := map[string]string{
			"agentId": agentInstance.ID,
			"teamId":  state.ID,
			"taskId":  task.ID,
			"profile": profile.Name,
			"runId":   state.ID,
		}
		runResult, runErr := engine.Run(ctx, agent.Input{
			Model:            profile.Model,
			Messages:         initialMessages,
			ToolMode:         tool.ModeParallel,
			MaxIterations:    max(1, options.maxIterationsOrDefault(4)),
			StopSequences:    append([]string{}, options.StopSequences...),
			ThinkingBudget:   options.ThinkingBudget,
			OutputGuardrails: outputGuardrails,
			OutputRecorder:   r,
			Metadata:         metadata,
		})
		if runErr != nil {
			return runErr
		}
		result = runResult
		envelope.Response = runResult
		return nil
	})
	if err != nil {
		return taskExecution{}, err
	}
	generated := append([]message.Message{}, result.Messages[len(initialMessages):]...)
	return taskExecution{
		Messages:    generated,
		Usage:       result.Usage,
		ToolResults: toolResultsFromMessages(generated),
	}, nil
}

func (r *Runtime) compactMessages(ctx context.Context, messages []message.Message) []message.Message {
	if r.compactor == nil || r.compactThreshold <= 0 || len(messages) <= r.compactThreshold {
		return messages
	}
	compacted, err := r.compactor.Compact(ctx, messages)
	if err != nil {
		// Fail open: return original messages rather than breaking the session.
		return messages
	}
	return compacted
}

func (r *Runtime) persistTaskMessages(ctx context.Context, task team.Task, generated []message.Message) error {
	if len(generated) == 0 {
		return nil
	}
	_, err := r.appendSessionMessages(ctx, task.SessionID, generated...)
	return err
}
