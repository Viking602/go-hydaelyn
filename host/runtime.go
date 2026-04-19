package host

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/auth"
	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/compact"
	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/mcp"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/middleware"
	"github.com/Viking602/go-hydaelyn/observe"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/workflow"
)

var ErrProviderNotFound = errors.New("provider not found")
var ErrProfileNotFound = errors.New("profile not found")
var ErrPatternNotFound = errors.New("pattern not found")
var ErrInvalidTeamState = errors.New("invalid team state")

type Config struct {
	Storage          storage.Driver
	Auth             auth.Driver
	WorkerID         string
	Defaults         map[string]string
	Plugins          []plugin.Spec
	Middlewares      []middleware.Handler
	Compactor        compact.Compactor
	CompactThreshold int
}

type Runtime struct {
	storage          storage.Driver
	eventSink        EventSink
	auth             auth.Driver
	tools            *tool.Bus
	workflows        *workflow.Registry
	hooks            hook.Chain
	middlewares      middleware.Chain
	capability       *capability.Invoker
	plugins          *plugin.Registry
	queue            scheduler.TaskQueue
	leaseReleaser    LeaseReleaser
	teamGuard        teamGuard
	providers        map[string]provider.Driver
	profiles         map[string]team.Profile
	patterns         map[string]team.Pattern
	defaults         map[string]string
	workerID         string
	compactor        compact.Compactor
	compactThreshold int
	mu               sync.RWMutex
	runSeq           uint64
	teamSeq          uint64
	activeRuns       map[string]context.CancelFunc
	activeTeams      map[string]context.CancelFunc
}

func New(config Config) *Runtime {
	driver := config.Storage
	if driver == nil {
		driver = storage.NewMemoryDriver()
	}
	runner := &Runtime{
		storage:          driver,
		eventSink:        &runtimeEventSink{store: driver.Events()},
		auth:             config.Auth,
		tools:            tool.NewBus(),
		workflows:        workflow.NewRegistry(),
		middlewares:      middleware.NewChain(config.Middlewares...),
		capability:       capability.NewInvoker(),
		plugins:          plugin.NewRegistry(),
		teamGuard:        &defaultTeamGuard{},
		providers:        map[string]provider.Driver{},
		profiles:         map[string]team.Profile{},
		patterns:         map[string]team.Pattern{},
		defaults:         cloneStringMap(config.Defaults),
		workerID:         config.WorkerID,
		compactor:        config.Compactor,
		compactThreshold: config.CompactThreshold,
		activeRuns:       map[string]context.CancelFunc{},
		activeTeams:      map[string]context.CancelFunc{},
	}
	runner.leaseReleaser = &defaultLeaseReleaser{queue: runner.queue}
	if runner.workerID == "" {
		runner.workerID = runner.nextWorkerID()
	}
	for _, spec := range config.Plugins {
		_ = runner.RegisterPlugin(spec)
	}
	return runner
}

func (r *Runtime) RegisterProvider(name string, driver provider.Driver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capability.Register(capability.TypeLLM, name, providerCapabilityHandler(driver))
	r.providers[name] = capabilityProviderDriver{
		name:     name,
		metadata: driver.Metadata(),
		invoker:  r.capability,
		recorder: r,
	}
	if _, exists := r.plugins.Lookup(plugin.TypeProvider, name); !exists {
		_ = r.plugins.Register(plugin.Spec{Type: plugin.TypeProvider, Name: name, Component: driver})
	}
}

func (r *Runtime) RegisterTool(driver tool.Driver) {
	name := driver.Definition().Name
	r.capability.Register(capability.TypeTool, name, toolCapabilityHandler(driver))
	r.tools.Register(capabilityToolDriver{
		definition: driver.Definition(),
		invoker:    r.capability,
		recorder:   r,
	})
	if _, exists := r.plugins.Lookup(plugin.TypeTool, name); !exists {
		_ = r.plugins.Register(plugin.Spec{Type: plugin.TypeTool, Name: name, Component: driver})
	}
}

func (r *Runtime) RegisterWorkflow(driver workflow.Driver) {
	r.workflows.Register(driver)
}

func (r *Runtime) RegisterHook(handler hook.Handler) {
	r.hooks = r.hooks.Append(handler)
}

func (r *Runtime) RegisterCompactor(compactor compact.Compactor) {
	r.compactor = compactor
}

func (r *Runtime) RegisterPlugin(spec plugin.Spec) error {
	if err := r.plugins.Register(spec); err != nil {
		return err
	}
	return r.applyPlugin(spec)
}

func (r *Runtime) Plugins() *plugin.Registry {
	return r.plugins
}

func (r *Runtime) UseMiddleware(handler middleware.Handler) {
	r.middlewares = r.middlewares.Append(handler)
}

func (r *Runtime) UseCapabilityMiddleware(handler capability.Middleware) {
	r.capability.Use(handler)
}

func (r *Runtime) RegisterCapability(callType capability.Type, name string, handler capability.Handler) {
	r.capability.Register(callType, name, handler)
}

func (r *Runtime) InvokeCapability(ctx context.Context, call capability.Call) (capability.Result, error) {
	return r.capability.Invoke(capability.WithPolicyOutcomeRecorder(ctx, r), call)
}

func (r *Runtime) UseObserver(observer observe.Observer) {
	if observer == nil {
		return
	}
	r.UseMiddleware(observe.RuntimeMiddleware(observer))
	r.UseCapabilityMiddleware(observe.CapabilityMiddleware(observer))
}

func (r *Runtime) RegisterProfile(profile team.Profile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles[profile.Name] = profile
}

func (r *Runtime) RegisterPattern(pattern team.Pattern) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.patterns[pattern.Name()] = pattern
}

func (r *Runtime) CreateSession(ctx context.Context, params session.CreateParams) (session.Session, error) {
	return r.createSession(ctx, params)
}

func (r *Runtime) GetSession(ctx context.Context, sessionID string) (session.Snapshot, error) {
	return r.loadSession(ctx, sessionID)
}

type PromptRequest struct {
	SessionID string
	Provider  string
	Model     string
	Messages  []message.Message
	ToolMode  tool.Mode
	Metadata  map[string]string
}

type PromptResponse struct {
	Run        storage.Run         `json:"run"`
	Session    session.Session     `json:"session"`
	NewEntries []session.Entry     `json:"newEntries"`
	Messages   []message.Message   `json:"messages"`
	Usage      provider.Usage      `json:"usage"`
	StopReason provider.StopReason `json:"stopReason"`
}

func (r *Runtime) Prompt(ctx context.Context, request PromptRequest) (PromptResponse, error) {
	return r.promptCore(ctx, request, nil)
}

func (r *Runtime) PromptStream(ctx context.Context, request PromptRequest, onEvent func(provider.Event) error) (PromptResponse, error) {
	return r.promptCore(ctx, request, onEvent)
}

func (r *Runtime) promptCore(ctx context.Context, request PromptRequest, onEvent func(provider.Event) error) (PromptResponse, error) {
	currentProvider, err := r.lookupProvider(request.Provider)
	if err != nil {
		return PromptResponse{}, err
	}
	snapshot, err := r.loadSession(ctx, request.SessionID)
	if err != nil {
		return PromptResponse{}, err
	}
	if len(request.Messages) > 0 {
		if _, err := r.appendSessionMessages(ctx, request.SessionID, request.Messages...); err != nil {
			return PromptResponse{}, err
		}
		snapshot.Messages = append(snapshot.Messages, request.Messages...)
	}
	run := storage.Run{
		ID:        r.nextRunID(),
		SessionID: request.SessionID,
		Status:    storage.RunStatusRunning,
		Provider:  request.Provider,
		Model:     request.Model,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := r.storage.Runs().Save(ctx, run); err != nil {
		return PromptResponse{}, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.activeRuns[run.ID] = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.activeRuns, run.ID)
		r.mu.Unlock()
	}()
	engine := agent.Engine{
		Provider: currentProvider,
		Tools:    r.tools,
		Hooks:    r.engineHooks(),
	}
	var result agent.Result
	compactedMessages := r.compactMessages(runCtx, snapshot.Messages)
	err = r.runStage(runCtx, &middleware.Envelope{
		Stage:     middleware.StageAgent,
		Operation: "prompt",
		Metadata:  cloneStringMap(request.Metadata),
		Request:   request,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		runResult, runErr := engine.Run(ctx, agent.Input{
			Model:         request.Model,
			Messages:      compactedMessages,
			Metadata:      request.Metadata,
			ToolMode:      request.ToolMode,
			MaxIterations: 6,
			OnEvent:       onEvent,
		})
		if runErr != nil {
			return runErr
		}
		result = runResult
		envelope.Response = runResult
		return nil
	})
	if err != nil {
		run.Status = storage.RunStatusFailed
		run.Error = err.Error()
		_ = r.storage.Runs().Save(ctx, run)
		return PromptResponse{}, err
	}
	generated := result.Messages[len(compactedMessages):]
	entries, err := r.appendSessionMessages(ctx, request.SessionID, generated...)
	if err != nil {
		return PromptResponse{}, err
	}
	run.Status = storage.RunStatusCompleted
	if err := r.storage.Runs().Save(ctx, run); err != nil {
		return PromptResponse{}, err
	}
	finalSnapshot, err := r.loadSession(ctx, request.SessionID)
	if err != nil {
		return PromptResponse{}, err
	}
	return PromptResponse{
		Run:        run,
		Session:    finalSnapshot.Session,
		NewEntries: entries,
		Messages:   generated,
		Usage:      result.Usage,
		StopReason: result.StopReason,
	}, nil
}

type ContinueRequest struct {
	SessionID string
	Provider  string
	Model     string
	ToolMode  tool.Mode
	Metadata  map[string]string
}

type DumpConfigRequest struct {
	Plugins     []plugin.Ref
	ProfileName string
	TeamConfig  map[string]string
	RunConfig   map[string]string
}

type DumpConfigResponse struct {
	Values map[string]string            `json:"values"`
	Layers map[string]map[string]string `json:"layers,omitempty"`
}

func (r *Runtime) Continue(ctx context.Context, request ContinueRequest) (PromptResponse, error) {
	return r.Prompt(ctx, PromptRequest{
		SessionID: request.SessionID,
		Provider:  request.Provider,
		Model:     request.Model,
		ToolMode:  request.ToolMode,
		Metadata:  request.Metadata,
	})
}

func (r *Runtime) DumpConfig(request DumpConfigRequest) (DumpConfigResponse, error) {
	values := map[string]string{}
	mergeStringMap(values, r.defaults)
	layers := map[string]map[string]string{
		"default": cloneStringMap(r.defaults),
	}
	pluginLayer := map[string]string{}
	for _, ref := range request.Plugins {
		spec, ok := r.plugins.Lookup(ref.Type, ref.Name)
		if !ok {
			return DumpConfigResponse{}, fmt.Errorf("plugin not found: %s/%s", ref.Type, ref.Name)
		}
		mergeStringMap(pluginLayer, spec.Config)
		mergeStringMap(values, spec.Config)
	}
	if len(pluginLayer) > 0 {
		layers["plugin"] = pluginLayer
	}
	if request.ProfileName != "" {
		profile, err := r.lookupProfile(request.ProfileName)
		if err != nil {
			return DumpConfigResponse{}, err
		}
		layers["profile"] = cloneStringMap(profile.Metadata)
		mergeStringMap(values, profile.Metadata)
	}
	layers["team"] = cloneStringMap(request.TeamConfig)
	mergeStringMap(values, request.TeamConfig)
	layers["run"] = cloneStringMap(request.RunConfig)
	mergeStringMap(values, request.RunConfig)
	return DumpConfigResponse{
		Values: values,
		Layers: layers,
	}, nil
}

func (r *Runtime) AbortRun(_ context.Context, runID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.activeRuns[runID]
	if !ok {
		return nil
	}
	cancel()
	delete(r.activeRuns, runID)
	return nil
}

type StartTeamRequest struct {
	TeamID            string
	Pattern           string
	Planner           string
	SupervisorProfile string
	WorkerProfiles    []string
	Input             map[string]any
	Metadata          map[string]string
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

func (r *Runtime) StartWorkflow(ctx context.Context, name string, input map[string]any) (workflow.State, error) {
	driver, ok := r.workflows.Driver(name)
	if !ok {
		return workflow.State{}, fmt.Errorf("workflow not found: %s", name)
	}
	state, err := driver.Start(ctx, input)
	if err != nil {
		return workflow.State{}, err
	}
	if err := r.storage.Workflows().Save(ctx, state); err != nil {
		return workflow.State{}, err
	}
	return state, nil
}

func (r *Runtime) ResumeWorkflow(ctx context.Context, workflowID string) (workflow.State, error) {
	current, err := r.storage.Workflows().Load(ctx, workflowID)
	if err != nil {
		return workflow.State{}, err
	}
	driver, ok := r.workflows.Driver(current.Name)
	if !ok {
		return workflow.State{}, fmt.Errorf("workflow not found: %s", current.Name)
	}
	next, err := driver.Resume(ctx, current)
	if err != nil {
		return workflow.State{}, err
	}
	if err := r.storage.Workflows().Save(ctx, next); err != nil {
		return workflow.State{}, err
	}
	return next, nil
}

func (r *Runtime) AbortWorkflow(ctx context.Context, workflowID string) (workflow.State, error) {
	current, err := r.storage.Workflows().Load(ctx, workflowID)
	if err != nil {
		return workflow.State{}, err
	}
	driver, ok := r.workflows.Driver(current.Name)
	if !ok {
		return workflow.State{}, fmt.Errorf("workflow not found: %s", current.Name)
	}
	next, err := driver.Abort(ctx, current)
	if err != nil {
		return workflow.State{}, err
	}
	if err := r.storage.Workflows().Save(ctx, next); err != nil {
		return workflow.State{}, err
	}
	return next, nil
}

func (r *Runtime) driveTeam(ctx context.Context, pattern team.Pattern, state team.RunState) (team.RunState, error) {
	current := state
	current.Normalize()
	for range 24 {
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
		updated, applied, published := r.applyTaskOutcome(current, outcome.index, outcome.task)
		if !applied {
			continue
		}
		current = updated
		task := current.Tasks[outcome.index]
		if errors.Is(outcome.err, context.Canceled) {
			r.recordTaskCancelledEvent(ctx, current, task, "cancellation_propagated")
		}
		switch task.Status {
		case team.TaskStatusRunning:
			r.recordTaskLifecycleEvent(ctx, current, task, storage.EventTaskStarted)
		case team.TaskStatusCompleted:
			r.recordTaskLifecycleEvent(ctx, current, task, storage.EventTaskCompleted)
			if task.Kind == team.TaskKindVerify || task.Stage == team.TaskStageVerify {
				r.recordVerifierDecisionEvent(ctx, current, task)
			}
			if task.Kind == team.TaskKindSynthesize || task.Stage == team.TaskStageSynthesize {
				r.recordSynthesisCommittedEvent(ctx, current, task)
			}
		case team.TaskStatusFailed:
			r.recordTaskLifecycleEvent(ctx, current, task, storage.EventTaskFailed)
		}
		if published {
			r.recordTaskOutputsPublishedEvent(ctx, current, task)
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
	for _, dependencyID := range task.DependsOn {
		dependency, ok := index[dependencyID]
		if !ok {
			continue
		}
		if dependency.Kind == team.TaskKindVerify || dependency.Stage == team.TaskStageVerify {
			items = append(items, dependency)
		}
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
	status := strings.TrimSpace(exchange.Metadata[verifierGateStatusField])
	if decision == "" && len(exchange.Structured) > 0 {
		if value, ok := exchange.Structured[verifierGateDecisionField].(string); ok {
			decision = strings.TrimSpace(value)
		}
		if value, ok := exchange.Structured[verifierGateStatusField].(string); ok {
			status = strings.TrimSpace(value)
		}
	}
	if decision == "" {
		return "", "", false
	}
	if status == "" {
		status = string(blackboard.InferVerificationStatus(exchange.Text))
	}
	return decision, status, true
}

func (r *Runtime) applyTaskOutcome(state team.RunState, index int, item team.Task) (team.RunState, bool, bool) {
	current := state.Tasks[index]
	if current.Version != item.Version || current.IsTerminal() {
		return state, false, false
	}
	if item.Status == team.TaskStatusCompleted {
		item = r.markTaskCompletion(item)
	}
	state.Tasks[index] = item
	if taskFailureCancelsDependents(item) {
		state = abortDependentTasks(state, item)
	}
	if item.Status != team.TaskStatusCompleted {
		return state, true, false
	}
	state = r.applyBlackboardUpdate(state, item)
	return state, true, true
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
	index int
	task  team.Task
	err   error
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
	terminal, err := r.persistTeamProgress(ctx, next)
	if err != nil {
		return team.RunState{}, false, err
	}
	return terminal, terminal.IsTerminal(), nil
}

func (r *Runtime) resolveBlockedOrRunnable(ctx context.Context, current team.RunState) (team.RunState, bool, bool, error) {
	if next, changed := current.ResolveBlockedTasks(); changed {
		terminal, err := r.persistTeamProgress(ctx, next)
		return terminal, true, terminal.IsTerminal(), err
	}
	if len(current.RunnableTasks()) == 0 {
		return current, false, false, nil
	}
	next, err := r.executeTasks(ctx, current)
	if err != nil {
		return team.RunState{}, false, false, err
	}
	terminal, err := r.persistTeamProgress(ctx, next)
	return terminal, true, terminal.IsTerminal(), err
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

func (r *Runtime) persistTeamProgress(ctx context.Context, current team.RunState) (team.RunState, error) {
	terminal, err := r.saveIfFailed(ctx, current)
	if err != nil {
		return team.RunState{}, err
	}
	if terminal.IsTerminal() {
		if err := r.saveTeam(ctx, &terminal); err != nil {
			return team.RunState{}, err
		}
		return terminal, nil
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
				results <- taskOutcome{index: index, task: failed, err: err}
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
			results <- taskOutcome{index: index, task: item, err: err}
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
	return task.BlocksTeamOnFailure()
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
	task.Attempts++
	task.Status = team.TaskStatusRunning
	task.StartedAt = time.Now().UTC()
	r.recordTaskLifecycleEvent(ctx, state, task, storage.EventTaskStarted)
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
	r.publishTaskOutputMessages(ctx, state, task, agentInstance)
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

func (r *Runtime) lookupProvider(name string) (provider.Driver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	driver, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	return driver, nil
}

func (r *Runtime) lookupProfile(name string) (team.Profile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	profile, ok := r.profiles[name]
	if !ok {
		return team.Profile{}, fmt.Errorf("%w: %s", ErrProfileNotFound, name)
	}
	return profile, nil
}

func (r *Runtime) lookupPattern(name string) (team.Pattern, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pattern, ok := r.patterns[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPatternNotFound, name)
	}
	return pattern, nil
}

func (r *Runtime) validateTeamState(state team.RunState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTeamState, err)
	}
	agents := append([]team.AgentInstance{state.Supervisor}, state.Workers...)
	for _, agentInstance := range agents {
		if _, err := r.lookupProfile(agentInstance.EffectiveProfileName()); err != nil {
			return err
		}
	}
	return nil
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

func (r *Runtime) saveIfFailed(ctx context.Context, state team.RunState) (team.RunState, error) {
	if failed := state.FirstBlockingFailure(); failed != nil {
		next := failTeam(state, *failed)
		if err := r.saveTeam(ctx, &next); err != nil {
			return team.RunState{}, err
		}
		return next, nil
	}
	return state, nil
}

func (r *Runtime) saveTeam(ctx context.Context, state *team.RunState) error {
	state.Normalize()
	r.recordTeamTerminalEvent(ctx, *state)
	version, err := r.storage.Teams().SaveCAS(ctx, *state, state.Version)
	if err == nil {
		state.Version = version
		return nil
	}
	if errors.Is(err, storage.ErrStaleState) {
		return err
	}
	return err
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
		runResult, runErr := engine.Run(ctx, agent.Input{
			Model:         profile.Model,
			Messages:      initialMessages,
			ToolMode:      tool.ModeParallel,
			MaxIterations: max(1, profile.MaxTurns),
			Metadata: map[string]string{
				"agentId": agentInstance.ID,
				"teamId":  state.ID,
				"taskId":  task.ID,
				"profile": profile.Name,
			},
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

func (r *Runtime) applyPlugin(spec plugin.Spec) error {
	switch spec.Type {
	case plugin.TypeProvider:
		driver, ok := spec.Component.(provider.Driver)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement provider.Driver", spec.Type, spec.Name)
		}
		r.RegisterProvider(spec.Name, driver)
	case plugin.TypeTool:
		driver, ok := spec.Component.(tool.Driver)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement tool.Driver", spec.Type, spec.Name)
		}
		r.RegisterTool(driver)
	case plugin.TypeStorage:
		driver, ok := spec.Component.(storage.Driver)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement storage.Driver", spec.Type, spec.Name)
		}
		r.storage = driver
	case plugin.TypeObserver:
		if observer, ok := spec.Component.(observe.Observer); ok {
			r.UseObserver(observer)
			return nil
		}
		handler, ok := spec.Component.(hook.Handler)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement observe.Observer or hook.Handler", spec.Type, spec.Name)
		}
		r.RegisterHook(handler)
	case plugin.TypeScheduler:
		queue, ok := spec.Component.(scheduler.TaskQueue)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement scheduler.TaskQueue", spec.Type, spec.Name)
		}
		r.queue = queue
		r.leaseReleaser = &defaultLeaseReleaser{queue: queue}
	case plugin.TypeMCPGateway:
		gateway, ok := spec.Component.(mcp.Gateway)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement mcp.Gateway", spec.Type, spec.Name)
		}
		drivers, err := gateway.ImportTools(context.Background())
		if err != nil {
			return err
		}
		for _, driver := range drivers {
			r.RegisterTool(driver)
		}
	case plugin.TypePlanner, plugin.TypeVerifier, plugin.TypeMemory:
		return nil
	default:
		return fmt.Errorf("unsupported plugin type: %s", spec.Type)
	}
	return nil
}

func (r *Runtime) engineHooks() hook.Chain {
	if r.middlewares.Len() == 0 {
		return r.hooks
	}
	return r.hooks.Append(r.middlewares.HookAdapter())
}

func mergeStringMap(target map[string]string, source map[string]string) {
	maps.Copy(target, source)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	maps.Copy(out, values)
	return out
}

func (r *Runtime) nextRunID() string {
	return fmt.Sprintf("run-%d", atomic.AddUint64(&r.runSeq, 1))
}

func (r *Runtime) nextTeamID() string {
	return fmt.Sprintf("team-%d", atomic.AddUint64(&r.teamSeq, 1))
}

func (r *Runtime) nextWorkerID() string {
	return fmt.Sprintf("runtime-worker-%d", atomic.AddUint64(&r.teamSeq, 1))
}
