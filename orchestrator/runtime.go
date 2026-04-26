package orchestrator

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
)

const maxHandoffDepth = 8

type Runtime struct {
	mu                sync.Mutex
	runs              map[string]Run
	tasks             map[string]map[string]Task
	events            map[string][]Event
	blackboard        map[string][]BlackboardItem
	envelopes         map[string]TaskEnvelope
	envelopesByRun    map[string][]string
	leases            map[string]TaskExecutionLease
	activeLeaseByTask map[string]string
	tools             map[string]Tool
	messages          map[string]UserMessage
	messagesByRun     map[string][]string
	flows             map[string]Flow
	messagePolicy     MessagePolicyChecker
	seq               map[string]int
	nextID            int
}

func NewMemoryRuntime() *Runtime {
	return &Runtime{
		runs:              map[string]Run{},
		tasks:             map[string]map[string]Task{},
		events:            map[string][]Event{},
		blackboard:        map[string][]BlackboardItem{},
		envelopes:         map[string]TaskEnvelope{},
		envelopesByRun:    map[string][]string{},
		leases:            map[string]TaskExecutionLease{},
		activeLeaseByTask: map[string]string{},
		tools:             map[string]Tool{},
		messages:          map[string]UserMessage{},
		messagesByRun:     map[string][]string{},
		flows:             map[string]Flow{},
		seq:               map[string]int{},
	}
}

type StartRunCommand struct {
	RunID      string
	RootTaskID string
	Request    string
	Metadata   map[string]string
}

type CreateTaskCommand struct {
	RunID              string
	TaskID             string
	ParentTaskID       string
	Type               TaskType
	Goal               string
	AssignedAgentID    string
	OwnerAgentID       string
	OwnerComponent     string
	CompletionCriteria []string
	DependsOn          []string
	ReadSelectors      []BlackboardSelector
	WriteTargets       []string
	RetryPolicy        RetryPolicy
	PolicyDecisions    []PolicyDecision
}

func (r *Runtime) StartRun(_ context.Context, cmd StartRunCommand) (Run, Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	runID := cmd.RunID
	if runID == "" {
		runID = r.newID("run")
	}
	rootID := cmd.RootTaskID
	if rootID == "" {
		rootID = r.newID("task")
	}
	run := Run{
		ID:         runID,
		Status:     RunStatusCreated,
		Request:    cmd.Request,
		RootTaskID: rootID,
		Metadata:   maps.Clone(cmd.Metadata),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	root := Task{
		ID:             rootID,
		RunID:          runID,
		Type:           TaskTypeWorker,
		OwnerComponent: "orchestrator",
		Status:         TaskStatusCreated,
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	r.runs[runID] = run
	r.tasks[runID] = map[string]Task{rootID: root}
	r.appendEventLocked(runID, rootID, EventRunStarted, map[string]any{
		"request": cmd.Request,
		"run":     runPayload(run),
	})
	r.appendEventLocked(runID, rootID, EventTaskCreated, taskEventPayload(root))
	return run, root, nil
}

func (r *Runtime) CreateTask(_ context.Context, cmd CreateTaskCommand) (Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[cmd.RunID]
	if !ok {
		return Task{}, ErrNotFound
	}
	if isTerminalRun(run.Status) {
		return Task{}, ErrTerminalState
	}
	taskID := cmd.TaskID
	if taskID == "" {
		taskID = r.newID("task")
	}
	now := time.Now().UTC()
	status := TaskStatusCreated
	if len(cmd.DependsOn) > 0 {
		status = TaskStatusWaitingDependency
	}
	task := Task{
		ID:                 taskID,
		RunID:              cmd.RunID,
		ParentTaskID:       cmd.ParentTaskID,
		Type:               cmd.Type,
		Goal:               cmd.Goal,
		AssignedAgentID:    cmd.AssignedAgentID,
		OwnerAgentID:       cmd.OwnerAgentID,
		OwnerComponent:     cmd.OwnerComponent,
		Status:             status,
		Version:            1,
		CompletionCriteria: slices.Clone(cmd.CompletionCriteria),
		DependsOn:          slices.Clone(cmd.DependsOn),
		ReadSelectors:      slices.Clone(cmd.ReadSelectors),
		WriteTargets:       slices.Clone(cmd.WriteTargets),
		RetryPolicy:        cmd.RetryPolicy,
		PolicyDecisions:    slices.Clone(cmd.PolicyDecisions),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if task.Type == "" {
		task.Type = TaskTypeWorker
	}
	if task.AssignedAgentID == "" && task.OwnerAgentID != "" {
		task.AssignedAgentID = task.OwnerAgentID
	}
	if task.OwnerAgentID != "" {
		task.OwnerHistory = []string{task.OwnerAgentID}
	}
	if r.tasks[cmd.RunID] == nil {
		r.tasks[cmd.RunID] = map[string]Task{}
	}
	r.tasks[cmd.RunID][task.ID] = task
	r.appendEventLocked(cmd.RunID, task.ID, EventTaskCreated, taskEventPayload(task))
	return task, nil
}

func (r *Runtime) Run(_ context.Context, runID string) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return Run{}, ErrNotFound
	}
	return run, nil
}

func (r *Runtime) Task(_ context.Context, runID, taskID string) (Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[runID][taskID]
	if !ok {
		return Task{}, ErrNotFound
	}
	return task, nil
}

func (r *Runtime) ReadyTasks(runID string) []Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	tasks := r.tasks[runID]
	out := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		if !taskCanBecomeReady(task.Status) || !r.dependenciesCompletedLocked(runID, task.DependsOn) {
			continue
		}
		out = append(out, task)
	}
	slices.SortFunc(out, func(a, b Task) int {
		return stringsCompare(a.ID, b.ID)
	})
	return out
}

func (r *Runtime) Events(runID string) []Event {
	events, _ := r.RunEvents(context.Background(), runID)
	return events
}

func (r *Runtime) RunEvents(_ context.Context, runID string) ([]Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[runID]; !ok && len(r.events[runID]) == 0 {
		return nil, ErrNotFound
	}
	return slices.Clone(r.events[runID]), nil
}

func (r *Runtime) ActiveLeaseCount(runID, taskID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, lease := range r.leases {
		if lease.RunID == runID && lease.TaskID == taskID && lease.Status == LeaseStatusActive {
			count++
		}
	}
	return count
}

func (r *Runtime) RegisterTool(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tool.Name == "" {
		return
	}
	if tool.EffectType == "" {
		tool.EffectType = ToolEffectReadOnly
	}
	r.tools[tool.Name] = tool
}

func (r *Runtime) SetMessagePolicy(policy MessagePolicyChecker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messagePolicy = policy
}

func (r *Runtime) appendEventLocked(runID, taskID string, typ EventType, payload map[string]any) {
	r.seq[runID]++
	if r.seq[runID] == 1 && len(r.events[runID]) > 0 {
		r.seq[runID] = len(r.events[runID]) + 1
	}
	event := Event{
		RunID:      runID,
		TaskID:     taskID,
		Sequence:   r.seq[runID],
		Type:       typ,
		Payload:    payload,
		RecordedAt: time.Now().UTC(),
	}
	r.events[runID] = append(r.events[runID], event)
}

func (r *Runtime) writeBlackboardLocked(item BlackboardItem) BlackboardItem {
	if item.ID == "" {
		item.ID = r.newID("bb")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	r.blackboard[item.RunID] = append(r.blackboard[item.RunID], item)
	r.appendEventLocked(item.RunID, item.TaskID, EventBlackboardItemWritten, map[string]any{
		"itemId":     item.ID,
		"sourceType": string(item.Source.Type),
		"sourceId":   item.Source.ID,
		"visibility": string(item.Visibility),
		"key":        item.Key,
	})
	return item
}

func (r *Runtime) writeEnvelopeLocked(env TaskEnvelope) TaskEnvelope {
	if env.ID == "" {
		env.ID = r.newID("env")
	}
	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now().UTC()
	}
	if env.Status == "" {
		env.Status = "pending"
	}
	if env.Type == "" {
		env.Type = "TaskEnvelope"
	}
	if env.TaskVersion == 0 {
		if task, ok := r.tasks[env.RunID][env.TaskID]; ok {
			env.TaskVersion = task.Version
		}
	}
	r.envelopes[env.ID] = env
	r.envelopesByRun[env.RunID] = append(r.envelopesByRun[env.RunID], env.ID)
	r.appendEventLocked(env.RunID, env.TaskID, EventTaskDispatched, map[string]any{
		"envelope": envPayload(env),
	})
	return env
}

func (r *Runtime) updateRunLocked(run Run, status RunStatus) Run {
	run.Status = status
	run.UpdatedAt = time.Now().UTC()
	r.runs[run.ID] = run
	r.appendEventLocked(run.ID, run.RootTaskID, EventRunStatusChanged, map[string]any{
		"to":  string(status),
		"run": runPayload(run),
	})
	return run
}

func (r *Runtime) saveTaskLocked(task Task) Task {
	task.UpdatedAt = time.Now().UTC()
	r.tasks[task.RunID][task.ID] = task
	return task
}

func (r *Runtime) newID(prefix string) string {
	r.nextID++
	return fmt.Sprintf("%s-%d", prefix, r.nextID)
}

func taskEventPayload(task Task) map[string]any {
	return map[string]any{
		"taskId":             task.ID,
		"runId":              task.RunID,
		"parentTaskId":       task.ParentTaskID,
		"type":               string(task.Type),
		"goal":               task.Goal,
		"status":             string(task.Status),
		"version":            task.Version,
		"attempts":           task.Attempts,
		"handoffCount":       task.HandoffCount,
		"assignedAgentId":    task.AssignedAgentID,
		"ownerAgentId":       task.OwnerAgentID,
		"ownerComponent":     task.OwnerComponent,
		"completionCriteria": slices.Clone(task.CompletionCriteria),
		"dependsOn":          slices.Clone(task.DependsOn),
		"retryPolicy":        retryPolicyPayload(task.RetryPolicy),
	}
}

func runPayload(run Run) map[string]any {
	return map[string]any{
		"id":         run.ID,
		"status":     string(run.Status),
		"request":    run.Request,
		"rootTaskId": run.RootTaskID,
		"metadata":   maps.Clone(run.Metadata),
		"createdAt":  run.CreatedAt,
		"updatedAt":  run.UpdatedAt,
	}
}

func envPayload(env TaskEnvelope) map[string]any {
	return map[string]any{
		"envelopeId":      env.ID,
		"runId":           env.RunID,
		"taskId":          env.TaskID,
		"targetAgentId":   env.TargetAgentID,
		"targetComponent": env.TargetComponent,
		"type":            env.Type,
		"status":          env.Status,
		"taskVersion":     env.TaskVersion,
		"attempts":        env.Attempts,
		"createdAt":       env.CreatedAt,
		"deliveredAt":     env.DeliveredAt,
	}
}

func retryPolicyPayload(policy RetryPolicy) map[string]any {
	if policy.MaxAttempts == 0 && policy.Backoff == 0 {
		return nil
	}
	return map[string]any{
		"maxAttempts": policy.MaxAttempts,
		"backoff":     policy.Backoff,
	}
}

func (r *Runtime) dependenciesCompletedLocked(runID string, deps []string) bool {
	for _, dep := range deps {
		task, ok := r.tasks[runID][dep]
		if !ok || task.Status != TaskStatusCompleted {
			return false
		}
	}
	return true
}

func taskCanBecomeReady(status TaskStatus) bool {
	switch status {
	case TaskStatusCreated, TaskStatusPlanned, TaskStatusValidated, TaskStatusRouted, TaskStatusWaitingDependency:
		return true
	default:
		return false
	}
}

func isTerminalTask(status TaskStatus) bool {
	return status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusCancelled
}

func isTerminalRun(status RunStatus) bool {
	return status == RunStatusCompleted || status == RunStatusFailed || status == RunStatusCancelled
}

func stringsCompare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
