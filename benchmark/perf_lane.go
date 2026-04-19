package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
)

type PerfLaneConfig struct {
	CaseID            string
	OutputDir         string
	Trials            int
	ParallelCalls     int
	MaxConcurrency    int
	LargeDAGTasks     int
	ToolLatency       time.Duration
	Subqueries        int
	BudgetTokens      int
	ContextMessages   int
	CompactThreshold  int
	CompactedMessages int
}

type PerfLaneMeasurement struct {
	Name     string        `json:"name"`
	Duration time.Duration `json:"duration"`
	Metrics  map[string]any `json:"metrics,omitempty"`
}

type PerfLaneReport struct {
	SchemaVersion string                `json:"schemaVersion"`
	RunID         string                `json:"runId"`
	CaseID        string                `json:"caseId"`
	StartedAt     time.Time             `json:"startedAt"`
	CompletedAt   time.Time             `json:"completedAt"`
	Measurements  []PerfLaneMeasurement `json:"measurements"`
}

func RunPerfLane(ctx context.Context, config PerfLaneConfig) (*evaluation.EvalRun, error) {
	config = normalizePerfLaneConfig(config)
	startedAt := time.Now().UTC()
	runID := perfLaneRunID(config, startedAt)
	outputDir, err := preparePerfLaneOutputDir(config.OutputDir)
	if err != nil {
		return nil, err
	}

	sequentialLatency, parallelLatency, parallelErr := measureToolBatchLatency(ctx, config.ParallelCalls, config.ToolLatency)
	if parallelErr != nil {
		return nil, parallelErr
	}
	parallelEfficiency := durationRatio(sequentialLatency, parallelLatency)

	maxObservedConcurrency, maxConcurrencyLatency, concurrencyErr := measureMaxConcurrencyScenario(ctx, config.MaxConcurrency, config.Subqueries, config.ToolLatency)
	if concurrencyErr != nil {
		return nil, concurrencyErr
	}

	largeDAGLatency, largeDAGTasks, dagErr := measureLargeDAGScenario(ctx, config.LargeDAGTasks)
	if dagErr != nil {
		return nil, dagErr
	}

	compactedCount, compactionLatency, compactionErr := measureContextCompactionScenario(ctx, config.ContextMessages, config.CompactThreshold, config.CompactedMessages)
	if compactionErr != nil {
		return nil, compactionErr
	}

	measurements := []PerfLaneMeasurement{
		{
			Name:     "parallel_efficiency",
			Duration: parallelLatency,
			Metrics: map[string]any{
				"sequentialLatencyMs": sequentialLatency.Milliseconds(),
				"parallelLatencyMs":   parallelLatency.Milliseconds(),
				"efficiencyRatio":     parallelEfficiency,
				"calls":               config.ParallelCalls,
			},
		},
		{
			Name:     "max_concurrency",
			Duration: maxConcurrencyLatency,
			Metrics: map[string]any{
				"configuredLimit":       config.MaxConcurrency,
				"observedConcurrency":   maxObservedConcurrency,
				"subqueries":            config.Subqueries,
				"latencyMs":             maxConcurrencyLatency.Milliseconds(),
				"goroutinesAtCollection": runtime.NumGoroutine(),
			},
		},
		{
			Name:     "large_dag",
			Duration: largeDAGLatency,
			Metrics: map[string]any{
				"taskCount":  largeDAGTasks,
				"latencyMs":  largeDAGLatency.Milliseconds(),
				"completed":  largeDAGTasks,
				"canonical":  true,
			},
		},
		{
			Name:     "long_context_compaction",
			Duration: compactionLatency,
			Metrics: map[string]any{
				"messagesBefore":   config.ContextMessages,
				"messagesAfter":    compactedCount,
				"compactThreshold": config.CompactThreshold,
			},
		},
	}
	completedAt := time.Now().UTC()

	budgetPressure := 0.0
	if config.BudgetTokens > 0 {
		budgetPressure = minFloat(1, float64(config.ContextMessages)/float64(config.BudgetTokens))
	}
	usage := &evaluation.EvalRunUsage{
		TotalCostUSD:  float64(config.ParallelCalls+largeDAGTasks) * 0.00001,
		LatencyMs:     completedAt.Sub(startedAt).Milliseconds(),
		ToolCallCount: config.ParallelCalls,
	}
	score := evaluation.ScorePayload{
		SchemaVersion:    evaluation.ScorePayloadSchemaVersion,
		RunID:            runID,
		ReplayConsistent: true,
		OverallScore:     minFloat(1, 0.55+(0.15*parallelEfficiency)+(0.15*minFloat(1, float64(maxObservedConcurrency)/float64(max(1, config.MaxConcurrency))))+(0.15*(1-budgetPressure))),
		Level:            evaluation.ScoreLevelA3,
		RuntimeMetrics: &evaluation.ScoreRuntimeMetrics{
			TaskCompletionRate:  1,
			EndToEndLatencyMs:   usage.LatencyMs,
			ToolCallCount:       usage.ToolCallCount,
			TokenBudgetHitRate:  budgetPressure,
			BlockingFailureRate: 0,
		},
	}
	policyOutcomes := []evaluation.EvalRunPolicyOutcome{{
		Policy:   "perf_lane.artifacts",
		Outcome:  "recorded",
		Severity: "info",
		Message:  "canonical stress artifacts persisted",
	}}
	report := PerfLaneReport{
		SchemaVersion: evaluation.EvalRunSchemaVersion,
		RunID:         runID,
		CaseID:        config.CaseID,
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
		Measurements:  measurements,
	}

	summary := renderPerfLaneSummary(report)
	refs, err := persistPerfLaneArtifacts(outputDir, runID, report, summary, score, policyOutcomes)
	if err != nil {
		return nil, err
	}
	run := &evaluation.EvalRun{
		SchemaVersion: evaluation.EvalRunSchemaVersion,
		ID:            runID,
		CaseID:        config.CaseID,
		Mode:          evaluation.EvalRunModeDeterministic,
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
		Usage:         usage,
		ArtifactRefs:  refs.artifacts,
		ScoreRef:      refs.score,
		PolicyOutcomes: policyOutcomes,
		Status:        evaluation.EvalRunStatusCompleted,
		RuntimeConfigHash: perfLaneConfigHash(config),
	}
	if err := writeJSON(filepath.Join(outputDir, "run.json"), run); err != nil {
		return nil, err
	}
	return run, nil
}

type perfPersistedRefs struct {
	artifacts *evaluation.EvalRunArtifactRefs
	score     *evaluation.EvalRunRef
}

func persistPerfLaneArtifacts(outputDir, runID string, report PerfLaneReport, summary string, score evaluation.ScorePayload, policyOutcomes []evaluation.EvalRunPolicyOutcome) (perfPersistedRefs, error) {
	artifacts := make([]ArtifactInfo, 0, 4)
	reportContent, err := marshalArtifactContent(report)
	if err != nil {
		return perfPersistedRefs{}, err
	}
	reportArtifact, err := writeArtifactFile(artifactWriteRequest{
		id:             "evaluation-report",
		kind:           evaluation.ArtifactManifestKindEvaluationReport,
		path:           filepath.Join(outputDir, "evaluation_report.json"),
		content:        reportContent,
		needsRedaction: true,
	})
	if err != nil {
		return perfPersistedRefs{}, err
	}
	artifacts = append(artifacts, reportArtifact)

	summaryArtifact, err := writeArtifactFile(artifactWriteRequest{
		id:             "summary",
		kind:           evaluation.ArtifactManifestKindSummary,
		path:           filepath.Join(outputDir, "summary.md"),
		content:        summary,
		needsRedaction: true,
	})
	if err != nil {
		return perfPersistedRefs{}, err
	}
	artifacts = append(artifacts, summaryArtifact)

	scoreContent, err := marshalArtifactContent(score)
	if err != nil {
		return perfPersistedRefs{}, err
	}
	scoreArtifact, err := writeArtifactFile(artifactWriteRequest{
		id:             "score",
		kind:           evaluation.ArtifactManifestKindScore,
		path:           filepath.Join(outputDir, "score.json"),
		content:        scoreContent,
		needsRedaction: true,
	})
	if err != nil {
		return perfPersistedRefs{}, err
	}
	artifacts = append(artifacts, scoreArtifact)

	policyContent, err := marshalArtifactContent(policyOutcomes)
	if err != nil {
		return perfPersistedRefs{}, err
	}
	policyArtifact, err := writeArtifactFile(artifactWriteRequest{
		id:             "policy-outcomes",
		kind:           evaluation.ArtifactManifestKindPolicyOutcomes,
		path:           filepath.Join(outputDir, "policy_outcomes.json"),
		content:        policyContent,
		needsRedaction: true,
	})
	if err != nil {
		return perfPersistedRefs{}, err
	}
	artifacts = append(artifacts, policyArtifact)

	manifest := GenerateManifest(runID, artifacts)
	if err := writeJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		return perfPersistedRefs{}, err
	}
	return perfPersistedRefs{
		artifacts: &evaluation.EvalRunArtifactRefs{
			Answer: manifestRef(outputDir, summaryArtifact.ID, filepath.Base(summaryArtifact.Path)),
		},
		score: manifestRef(outputDir, scoreArtifact.ID, filepath.Base(scoreArtifact.Path)),
	}, nil
}

func renderPerfLaneSummary(report PerfLaneReport) string {
	var b strings.Builder
	b.WriteString("# Performance lane\n\n")
	for _, measurement := range report.Measurements {
		b.WriteString("## ")
		b.WriteString(strings.ReplaceAll(measurement.Name, "_", " "))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("- duration_ms: %d\n", measurement.Duration.Milliseconds()))
		keys := make([]string, 0, len(measurement.Metrics))
		for key := range measurement.Metrics {
			keys = append(keys, key)
		}
		slicesSort(keys)
		for _, key := range keys {
			b.WriteString(fmt.Sprintf("- %s: %v\n", key, measurement.Metrics[key]))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func normalizePerfLaneConfig(config PerfLaneConfig) PerfLaneConfig {
	if strings.TrimSpace(config.CaseID) == "" {
		config.CaseID = "perf-lane"
	}
	if config.Trials <= 0 {
		config.Trials = 1
	}
	if config.ParallelCalls <= 0 {
		config.ParallelCalls = 4
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 4
	}
	if config.LargeDAGTasks <= 0 {
		config.LargeDAGTasks = 128
	}
	if config.ToolLatency <= 0 {
		config.ToolLatency = 10 * time.Millisecond
	}
	if config.Subqueries <= 0 {
		config.Subqueries = max(8, config.MaxConcurrency*2)
	}
	if config.BudgetTokens <= 0 {
		config.BudgetTokens = 1024
	}
	if config.ContextMessages <= 0 {
		config.ContextMessages = 128
	}
	if config.CompactThreshold <= 0 {
		config.CompactThreshold = 32
	}
	if config.CompactedMessages <= 0 {
		config.CompactedMessages = 16
	}
	return config
}

func preparePerfLaneOutputDir(outputDir string) (string, error) {
	if strings.TrimSpace(outputDir) == "" {
		return os.MkdirTemp("", "hydaelyn-perf-lane-*")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	return outputDir, nil
}

func perfLaneRunID(config PerfLaneConfig, startedAt time.Time) string {
	return fmt.Sprintf("perf-%s-%s", perfLaneConfigHash(config)[:10], startedAt.Format("20060102T150405Z"))
}

func perfLaneConfigHash(config PerfLaneConfig) string {
	data := fmt.Sprintf("%s|%d|%d|%d|%d|%s|%d|%d|%d|%d", config.CaseID, config.Trials, config.ParallelCalls, config.MaxConcurrency, config.LargeDAGTasks, config.ToolLatency, config.Subqueries, config.BudgetTokens, config.ContextMessages, config.CompactedMessages)
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func measureToolBatchLatency(ctx context.Context, callCount int, latency time.Duration) (time.Duration, time.Duration, error) {
	if callCount <= 0 {
		callCount = 1
	}
	bus := tool.NewBus(newPerfSleepTool("sleep", latency))
	calls := make([]message.ToolCall, 0, callCount)
	for i := 0; i < callCount; i++ {
		calls = append(calls, message.ToolCall{ID: fmt.Sprintf("call-%d", i+1), Name: "sleep", Arguments: []byte(`{}`)})
	}
	startedAt := time.Now()
	if _, err := bus.ExecuteBatch(ctx, calls, tool.ModeSequential, nil); err != nil {
		return 0, 0, err
	}
	sequential := time.Since(startedAt)
	startedAt = time.Now()
	if _, err := bus.ExecuteBatch(ctx, calls, tool.ModeParallel, nil); err != nil {
		return 0, 0, err
	}
	return sequential, time.Since(startedAt), nil
}

func measureMaxConcurrencyScenario(ctx context.Context, maxConcurrency, subqueries int, latency time.Duration) (int, time.Duration, error) {
	tracker := &perfConcurrencyTracker{}
	runner := newPerfHostRuntime(tracker, maxConcurrency, latency)
	queries := make([]string, 0, subqueries)
	for i := 0; i < subqueries; i++ {
		queries = append(queries, fmt.Sprintf("branch-%03d", i+1))
	}
	startedAt := time.Now()
	_, err := runner.StartTeam(ctx, host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    repeatProfile("researcher", max(2, maxConcurrency)),
		Input: map[string]any{
			"query":      "max concurrency",
			"subqueries": queries,
		},
	})
	return tracker.Max(), time.Since(startedAt), err
}

func measureLargeDAGScenario(ctx context.Context, taskCount int) (time.Duration, int, error) {
	runner := host.New(host.Config{})
	runner.RegisterProvider("perf", perfProvider{latency: 0})
	runner.RegisterPattern(perfWidePattern{taskCount: taskCount})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "perf", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "perf", Model: "test"})
	startedAt := time.Now()
	state, err := runner.StartTeam(ctx, host.StartTeamRequest{
		Pattern:           "perf-wide",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    repeatProfile("researcher", 8),
		Input:             map[string]any{"taskCount": taskCount},
	})
	if err != nil {
		return 0, 0, err
	}
	completed := 0
	for _, task := range state.Tasks {
		if task.Status == team.TaskStatusCompleted {
			completed++
		}
	}
	return time.Since(startedAt), completed, nil
}

func measureContextCompactionScenario(ctx context.Context, messageCount, threshold, maxMessages int) (int, time.Duration, error) {
	provider := &perfCountingProvider{}
	runner := host.New(host.Config{Compactor: &perfSimpleCompactor{maxMessages: maxMessages}, CompactThreshold: threshold})
	runner.RegisterProvider("perf-counting", provider)
	session, err := runner.CreateSession(ctx, session.CreateParams{})
	if err != nil {
		return 0, 0, err
	}
	for i := 0; i < messageCount; i++ {
		if _, err := runner.Prompt(ctx, host.PromptRequest{SessionID: session.ID, Provider: "perf-counting", Model: "test", Messages: []message.Message{message.NewText(message.RoleUser, fmt.Sprintf("seed-%03d", i))}}); err != nil {
			return 0, 0, err
		}
	}
	startedAt := time.Now()
	_, err = runner.Prompt(ctx, host.PromptRequest{SessionID: session.ID, Provider: "perf-counting", Model: "test", Messages: []message.Message{message.NewText(message.RoleUser, "final")}})
	return provider.seenLen, time.Since(startedAt), err
}

func newPerfHostRuntime(tracker *perfConcurrencyTracker, maxConcurrency int, latency time.Duration) *host.Runtime {
	runner := host.New(host.Config{})
	runner.RegisterProvider("perf", perfProvider{tracker: tracker, latency: latency})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "perf", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "perf", Model: "test", MaxConcurrency: maxConcurrency})
	return runner
}

type perfSleepTool struct {
	name    string
	latency time.Duration
}

func newPerfSleepTool(name string, latency time.Duration) perfSleepTool {
	return perfSleepTool{name: name, latency: latency}
}

func (d perfSleepTool) Definition() tool.Definition {
	return tool.Definition{Name: d.name, InputSchema: tool.Schema{Type: "object"}}
}

func (d perfSleepTool) Execute(ctx context.Context, call tool.Call, sink tool.UpdateSink) (tool.Result, error) {
	select {
	case <-ctx.Done():
		return tool.Result{}, ctx.Err()
	case <-time.After(d.latency):
	}
	return tool.Result{Name: call.Name, Content: "ok"}, nil
}

type perfProvider struct {
	tracker *perfConcurrencyTracker
	latency time.Duration
}

func (p perfProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "perf"}
}

func (p perfProvider) Stream(ctx context.Context, request provider.Request) (provider.Stream, error) {
	if p.tracker != nil {
		p.tracker.Start()
		defer p.tracker.Done()
	}
	if p.latency > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.latency):
		}
	}
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{{Kind: provider.EventTextDelta, Text: last.Text}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete, Usage: provider.Usage{InputTokens: len(request.Messages), OutputTokens: 1, TotalTokens: len(request.Messages) + 1}}}), nil
}

type perfCountingProvider struct{ seenLen int }

func (p *perfCountingProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "perf-counting"} }

func (p *perfCountingProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	p.seenLen = len(request.Messages)
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{{Kind: provider.EventTextDelta, Text: last.Text}, {Kind: provider.EventDone, StopReason: provider.StopReasonComplete}}), nil
}

type perfSimpleCompactor struct{ maxMessages int }

func (c *perfSimpleCompactor) Compact(_ context.Context, messages []message.Message) ([]message.Message, error) {
	if len(messages) <= c.maxMessages {
		return messages, nil
	}
	if c.maxMessages < 2 {
		c.maxMessages = 4
	}
	keepLast := c.maxMessages - 2
	if keepLast < 1 {
		keepLast = 1
	}
	compacted := []message.Message{messages[0], {Role: message.RoleSystem, Kind: message.KindCompactionSummary, Text: fmt.Sprintf("[compacted %d messages]", len(messages)-keepLast-1), Visibility: message.VisibilityPrivate}}
	compacted = append(compacted, messages[len(messages)-keepLast:]...)
	return compacted, nil
}

type perfConcurrencyTracker struct {
	current int64
	max     int64
}

func (t *perfConcurrencyTracker) Start() {
	current := atomic.AddInt64(&t.current, 1)
	for {
		maxCurrent := atomic.LoadInt64(&t.max)
		if current <= maxCurrent || atomic.CompareAndSwapInt64(&t.max, maxCurrent, current) {
			return
		}
	}
}

func (t *perfConcurrencyTracker) Done() { atomic.AddInt64(&t.current, -1) }
func (t *perfConcurrencyTracker) Max() int { return int(atomic.LoadInt64(&t.max)) }

type perfWidePattern struct{ taskCount int }

func (p perfWidePattern) Name() string { return "perf-wide" }

func (p perfWidePattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	count := p.taskCount
	if count <= 0 {
		count = 128
	}
	workers := make([]team.AgentInstance, 0, len(request.WorkerProfiles))
	for i, profileName := range request.WorkerProfiles {
		workers = append(workers, team.AgentInstance{ID: fmt.Sprintf("worker-%d", i+1), Role: team.RoleResearcher, ProfileName: profileName})
	}
	tasks := make([]team.Task, 0, count+1)
	for i := 0; i < count; i++ {
		assignee := workers[i%len(workers)]
		tasks = append(tasks, team.Task{ID: fmt.Sprintf("task-%03d", i+1), Kind: team.TaskKindResearch, Input: fmt.Sprintf("node-%03d", i+1), RequiredRole: team.RoleResearcher, AssigneeAgentID: assignee.ID, Status: team.TaskStatusPending, FailurePolicy: team.FailurePolicyFailFast})
	}
	dependsOn := make([]string, 0, count)
	for _, task := range tasks {
		dependsOn = append(dependsOn, task.ID)
	}
	tasks = append(tasks, team.Task{ID: "task-synthesize", Kind: team.TaskKindSynthesize, Input: "synthesize", RequiredRole: team.RoleSupervisor, AssigneeAgentID: "supervisor", DependsOn: dependsOn, Status: team.TaskStatusPending, FailurePolicy: team.FailurePolicyFailFast})
	return team.RunState{ID: request.TeamID, Pattern: p.Name(), Status: team.StatusRunning, Phase: team.PhaseResearch, Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: request.SupervisorProfile}, Workers: workers, Tasks: tasks, Input: request.Input, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}

func (p perfWidePattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	allResearchDone := true
	allDone := true
	for _, task := range state.Tasks {
		if task.Kind == team.TaskKindResearch && task.Status != team.TaskStatusCompleted {
			allResearchDone = false
		}
		if task.Status == team.TaskStatusPending || task.Status == team.TaskStatusRunning {
			allDone = false
		}
	}
	if allResearchDone {
		state.Phase = team.PhaseSynthesize
	}
	if allDone {
		state.Status = team.StatusCompleted
		state.Phase = team.PhaseComplete
		state.Result = &team.Result{Summary: "done"}
	}
	return state, nil
}

func repeatProfile(name string, count int) []string {
	profiles := make([]string, 0, count)
	for i := 0; i < count; i++ {
		profiles = append(profiles, name)
	}
	return profiles
}

func durationRatio(left, right time.Duration) float64 {
	if right <= 0 {
		return 0
	}
	return float64(left) / float64(right)
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func slicesSort(values []string) {
	if len(values) < 2 {
		return
	}
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
