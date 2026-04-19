package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/evalcase"
	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/observe"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/providers/anthropic"
	"github.com/Viking602/go-hydaelyn/providers/openai"
)

var liveLaneProviderFactories = map[string]func(LaneSpec) (provider.Driver, error){
	"openai": func(lane LaneSpec) (provider.Driver, error) {
		return openai.New(openai.Config{
			APIKey:  os.Getenv(strings.TrimSpace(lane.APIKeyEnv)),
			BaseURL: laneResolvedBaseURL(lane.BaseURL, lane.BaseURLEnv),
			Models:  []string{lane.Model},
		}), nil
	},
	"openrouter": func(lane LaneSpec) (provider.Driver, error) {
		return openai.New(openai.Config{
			APIKey:  os.Getenv(strings.TrimSpace(lane.APIKeyEnv)),
			BaseURL: laneResolvedBaseURL(lane.BaseURL, lane.BaseURLEnv),
			Models:  []string{lane.Model},
		}), nil
	},
	"anthropic": func(lane LaneSpec) (provider.Driver, error) {
		return anthropic.New(anthropic.Config{
			APIKey:  os.Getenv(strings.TrimSpace(lane.APIKeyEnv)),
			BaseURL: laneResolvedBaseURL(lane.BaseURL, lane.BaseURLEnv),
			Models:  []string{lane.Model},
		}), nil
	},
}

func LiveLaneProviderFactory(name string) (func(LaneSpec) (provider.Driver, error), bool) {
	factory, ok := liveLaneProviderFactories[name]
	return factory, ok
}

func SetLiveLaneProviderFactory(name string, factory func(LaneSpec) (provider.Driver, error)) {
	if strings.TrimSpace(name) == "" || factory == nil {
		return
	}
	liveLaneProviderFactories[name] = factory
}

func DeleteLiveLaneProviderFactory(name string) {
	delete(liveLaneProviderFactories, name)
}

func RunLiveLane(ctx context.Context, casePath string, laneConfig LaneSpec) (*evaluation.EvalRun, error) {
	if strings.TrimSpace(casePath) == "" {
		return nil, errors.New("eval case path is required")
	}
	if strings.TrimSpace(laneConfig.ID) == "" {
		return nil, errors.New("lane id is required")
	}
	if err := validateLiveLaneSecrets(laneConfig); err != nil {
		return nil, err
	}

	casePath, err := filepath.Abs(casePath)
	if err != nil {
		return nil, fmt.Errorf("resolve eval case path: %w", err)
	}
	caseDef, err := evalcase.LoadCase(casePath)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	runID, runtimeConfigHash, err := buildLiveLaneRunID(caseDef, laneConfig)
	if err != nil {
		return nil, err
	}
	laneRoot := filepath.Join(filepath.Dir(casePath), "runs", caseDef.ID, laneConfig.ID)
	outputDir := filepath.Join(laneRoot, startedAt.Format("20060102T150405.000000000Z"))
	stagingDir := outputDir + ".tmp"
	_ = os.RemoveAll(stagingDir)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, fmt.Errorf("create live lane staging dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(stagingDir)
	}()

	driver, err := resolveLiveLaneProvider(laneConfig)
	if err != nil {
		return nil, err
	}
	request, err := buildLiveLaneRequest(caseDef, laneConfig)
	if err != nil {
		return nil, err
	}
	stream, err := driver.Stream(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("start live lane stream: %w", err)
	}
	defer stream.Close()

	type toolCallCounter struct {
		seen map[string]struct{}
		next int
	}
	countToolCall := func(counter *toolCallCounter, event provider.Event) int {
		key := ""
		if event.ToolCall != nil {
			key = event.ToolCall.ID
			if key == "" {
				key = event.ToolCall.Name
			}
		}
		if event.ToolCallDelta != nil {
			key = event.ToolCallDelta.ID
			if key == "" {
				key = event.ToolCallDelta.Name
			}
		}
		if key == "" {
			counter.next++
			key = fmt.Sprintf("tool-call-%d", counter.next)
		}
		if _, ok := counter.seen[key]; ok {
			return 0
		}
		counter.seen[key] = struct{}{}
		return 1
	}

	var (
		answerBuilder  strings.Builder
		providerEvents []provider.Event
		usage          provider.Usage
		toolCalls      int
		stopReason     provider.StopReason
		policyOutcomes []evaluation.EvalRunPolicyOutcome
		executionErr   error
	)
	toolCounter := &toolCallCounter{seen: map[string]struct{}{}}
	for {
		event, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				break
			}
			executionErr = fmt.Errorf("receive live lane event: %w", recvErr)
			break
		}
		providerEvents = append(providerEvents, redactProviderEvent(event))
		if event.Kind == provider.EventTextDelta {
			text, _ := observe.RedactSecrets(event.Text)
			answerBuilder.WriteString(text)
		}
		if event.Kind == provider.EventToolCall || event.Kind == provider.EventToolCallDelta {
			toolCalls += countToolCall(toolCounter, event)
		}
		if event.Usage != (provider.Usage{}) {
			usage = event.Usage
		}
		if event.Kind == provider.EventDone {
			stopReason = event.StopReason
		}
		if budgetOutcome, budgetErr := liveLaneBudgetOutcome(laneConfig, usage, toolCalls); budgetErr != nil {
			policyOutcomes = append(policyOutcomes, budgetOutcome)
			executionErr = budgetErr
			break
		}
	}
	completedAt := time.Now().UTC()
	usageInfo := buildLiveLaneUsage(laneConfig, usage, toolCalls, startedAt, completedAt)
	answer, _ := observe.RedactSecrets(strings.TrimSpace(answerBuilder.String()))
	score := buildLiveLaneScore(runID, caseDef, usageInfo, stopReason, answer, executionErr == nil)
	variance, varianceOutcomes := evaluateLiveLaneVariance(laneRoot, score, usageInfo, laneConfig)
	policyOutcomes = append(policyOutcomes, varianceOutcomes...)
	policyOutcomes = append(policyOutcomes, evaluateLiveLaneAssertions(caseDef, laneConfig, score, usageInfo)...)
	if executionErr != nil {
		policyOutcomes = append(policyOutcomes, evaluation.EvalRunPolicyOutcome{
			Policy:   "live_lane.execution",
			Outcome:  "failed",
			Severity: "error",
			Message:  redactError(executionErr),
			Blocking: true,
		})
	}
	status := evaluation.EvalRunStatusCompleted
	if executionErr != nil {
		status = evaluation.EvalRunStatusFailed
	}
	manifest, refs, persistErr := persistLiveLaneArtifacts(stagingDir, runID, providerEvents, answer, score, policyOutcomes)
	if persistErr != nil {
		return nil, persistErr
	}
	run := &evaluation.EvalRun{
		SchemaVersion:     evaluation.EvalRunSchemaVersion,
		ID:                runID,
		CaseID:            caseDef.ID,
		Mode:              evaluation.EvalRunModeLive,
		RuntimeConfigHash: runtimeConfigHash,
		Provenance:        buildLiveLaneProvenance(laneConfig, driver.Metadata()),
		Usage:             usageInfo,
		Variance:          variance,
		StartedAt:         startedAt,
		CompletedAt:       completedAt,
		TraceRefs:         &evaluation.EvalRunTraceRefs{ModelEvents: refs.modelEvents},
		ArtifactRefs:      &evaluation.EvalRunArtifactRefs{Answer: refs.answer},
		ScoreRef:          refs.score,
		PolicyOutcomes:    policyOutcomes,
		Status:            status,
		Error:             redactError(executionErr),
	}
	if len(manifest.Entries) == 0 {
		run.TraceRefs = nil
		run.ArtifactRefs = nil
		run.ScoreRef = nil
	}
	if err := writeJSON(filepath.Join(stagingDir, "run.json"), run); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(outputDir), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(stagingDir, outputDir); err != nil {
		return nil, fmt.Errorf("promote live lane output: %w", err)
	}
	if run.TraceRefs != nil && run.TraceRefs.ModelEvents != nil {
		run.TraceRefs.ModelEvents = manifestRef(outputDir, "model-events", "model_events.json")
	}
	if run.ArtifactRefs != nil && run.ArtifactRefs.Answer != nil {
		run.ArtifactRefs.Answer = manifestRef(outputDir, "answer", "answer.txt")
	}
	if run.ScoreRef != nil {
		run.ScoreRef = manifestRef(outputDir, "score", "score.json")
	}
	if err := writeJSON(filepath.Join(outputDir, "run.json"), run); err != nil {
		return run, err
	}
	return run, executionErr
}

type persistedLiveRefs struct {
	modelEvents *evaluation.EvalRunRef
	answer      *evaluation.EvalRunRef
	score       *evaluation.EvalRunRef
}

func persistLiveLaneArtifacts(outputDir, runID string, providerEvents []provider.Event, answer string, score evaluation.ScorePayload, policyOutcomes []evaluation.EvalRunPolicyOutcome) (*evaluation.ArtifactManifest, persistedLiveRefs, error) {
	artifacts := make([]ArtifactInfo, 0, 4)
	modelEventsContent, err := marshalArtifactContent(providerEvents)
	if err != nil {
		return nil, persistedLiveRefs{}, err
	}
	artifact, err := writeArtifactFile(artifactWriteRequest{
		id:             "model-events",
		kind:           evaluation.ArtifactManifestKindModelEvents,
		path:           filepath.Join(outputDir, "model_events.json"),
		content:        modelEventsContent,
		needsRedaction: true,
	})
	if err != nil {
		return nil, persistedLiveRefs{}, err
	}
	artifacts = append(artifacts, artifact)
	artifact, err = writeArtifactFile(artifactWriteRequest{
		id:             "answer",
		kind:           evaluation.ArtifactManifestKindAnswer,
		path:           filepath.Join(outputDir, "answer.txt"),
		content:        answer,
		needsRedaction: true,
	})
	if err != nil {
		return nil, persistedLiveRefs{}, err
	}
	artifacts = append(artifacts, artifact)
	scoreContent, err := marshalArtifactContent(score)
	if err != nil {
		return nil, persistedLiveRefs{}, err
	}
	artifact, err = writeArtifactFile(artifactWriteRequest{
		id:             "score",
		kind:           evaluation.ArtifactManifestKindScore,
		path:           filepath.Join(outputDir, "score.json"),
		content:        scoreContent,
		needsRedaction: true,
	})
	if err != nil {
		return nil, persistedLiveRefs{}, err
	}
	artifacts = append(artifacts, artifact)
	policyContent, err := marshalArtifactContent(policyOutcomes)
	if err != nil {
		return nil, persistedLiveRefs{}, err
	}
	artifact, err = writeArtifactFile(artifactWriteRequest{
		id:             "policy-outcomes",
		kind:           evaluation.ArtifactManifestKindPolicyOutcomes,
		path:           filepath.Join(outputDir, "policy_outcomes.json"),
		content:        policyContent,
		needsRedaction: true,
	})
	if err != nil {
		return nil, persistedLiveRefs{}, err
	}
	artifacts = append(artifacts, artifact)
	manifest := GenerateManifest(runID, artifacts)
	if err := writeJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		return nil, persistedLiveRefs{}, err
	}
	return manifest, persistedLiveRefs{
		modelEvents: manifestRef(outputDir, "model-events", "model_events.json"),
		answer:      manifestRef(outputDir, "answer", "answer.txt"),
		score:       manifestRef(outputDir, "score", "score.json"),
	}, nil
}

func resolveLiveLaneProvider(lane LaneSpec) (provider.Driver, error) {
	factory, ok := liveLaneProviderFactories[strings.ToLower(strings.TrimSpace(lane.Provider))]
	if !ok {
		return nil, fmt.Errorf("unsupported live lane provider %q", lane.Provider)
	}
	return factory(lane)
}

func buildLiveLaneRequest(caseDef evaluation.EvalCase, lane LaneSpec) (provider.Request, error) {
	messages, err := liveLaneMessages(caseDef)
	if err != nil {
		return provider.Request{}, err
	}
	return provider.Request{Model: lane.Model, Messages: messages}, nil
}

func liveLaneMessages(caseDef evaluation.EvalCase) ([]message.Message, error) {
	items := make([]message.Message, 0, 2)
	if system, _ := caseDef.Input["system"].(string); strings.TrimSpace(system) != "" {
		items = append(items, message.NewText(message.RoleSystem, system))
	}
	if rawMessages, ok := caseDef.Input["messages"]; ok {
		decoded, err := decodeLiveLaneMessages(rawMessages)
		if err != nil {
			return nil, err
		}
		items = append(items, decoded...)
	}
	if len(items) > 0 {
		return items, nil
	}
	for _, key := range []string{"prompt", "query", "input"} {
		if text, _ := caseDef.Input[key].(string); strings.TrimSpace(text) != "" {
			items = append(items, message.NewText(message.RoleUser, text))
			return items, nil
		}
	}
	data, err := json.Marshal(caseDef.Input)
	if err != nil {
		return nil, fmt.Errorf("marshal eval case input: %w", err)
	}
	items = append(items, message.NewText(message.RoleUser, string(data)))
	return items, nil
}

func decodeLiveLaneMessages(raw any) ([]message.Message, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal eval case messages: %w", err)
	}
	var decoded []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, fmt.Errorf("decode eval case messages: %w", err)
	}
	items := make([]message.Message, 0, len(decoded))
	for _, current := range decoded {
		role := message.Role(strings.TrimSpace(current.Role))
		if role == "" {
			role = message.RoleUser
		}
		items = append(items, message.NewText(role, current.Text))
	}
	return items, nil
}

func validateLiveLaneSecrets(lane LaneSpec) error {
	required := map[string]struct{}{}
	for _, key := range lane.RequiredSecrets {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			required[trimmed] = struct{}{}
		}
	}
	if trimmed := strings.TrimSpace(lane.APIKeyEnv); trimmed != "" {
		required[trimmed] = struct{}{}
	}
	missing := make([]string, 0)
	for key := range required {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("missing required live lane secrets: %s", strings.Join(missing, ", "))
	}
	return nil
}

func buildLiveLaneRunID(caseDef evaluation.EvalCase, lane LaneSpec) (string, string, error) {
	payload, err := json.Marshal(struct {
		CaseID                 string             `json:"caseId"`
		Provider               string             `json:"provider"`
		Model                  string             `json:"model"`
		ProviderProvenance     string             `json:"providerProvenance,omitempty"`
		ModelProvenance        string             `json:"modelProvenance,omitempty"`
		MetricTolerances       map[string]float64 `json:"metricTolerances,omitempty"`
		LatencyToleranceMs     int64              `json:"latencyToleranceMs,omitempty"`
		CostToleranceUSD       float64            `json:"costToleranceUsd,omitempty"`
		MaxTokens              int                `json:"maxTokens,omitempty"`
		MaxToolCalls           int                `json:"maxToolCalls,omitempty"`
		MaxCostUSD             float64            `json:"maxCostUsd,omitempty"`
		PromptCostPer1KUSD     float64            `json:"promptCostPer1kUsd,omitempty"`
		CompletionCostPer1KUSD float64            `json:"completionCostPer1kUsd,omitempty"`
	}{
		CaseID:                 caseDef.ID,
		Provider:               lane.Provider,
		Model:                  lane.Model,
		ProviderProvenance:     lane.ProviderProvenance,
		ModelProvenance:        lane.ModelProvenance,
		MetricTolerances:       lane.MetricTolerances,
		LatencyToleranceMs:     lane.LatencyToleranceMs,
		CostToleranceUSD:       lane.CostToleranceUSD,
		MaxTokens:              lane.MaxTokens,
		MaxToolCalls:           lane.MaxToolCalls,
		MaxCostUSD:             lane.MaxCostUSD,
		PromptCostPer1KUSD:     lane.PromptCostPer1KUSD,
		CompletionCostPer1KUSD: lane.CompletionCostPer1KUSD,
	})
	if err != nil {
		return "", "", fmt.Errorf("marshal live lane runtime config: %w", err)
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-%s", caseDef.ID, lane.ID, hash[:8]), hash, nil
}

func buildLiveLaneUsage(lane LaneSpec, usage provider.Usage, toolCalls int, startedAt, completedAt time.Time) *evaluation.EvalRunUsage {
	if completedAt.Before(startedAt) {
		completedAt = startedAt
	}
	promptCost := (float64(usage.InputTokens) / 1000.0) * lane.PromptCostPer1KUSD
	completionCost := (float64(usage.OutputTokens) / 1000.0) * lane.CompletionCostPer1KUSD
	return &evaluation.EvalRunUsage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.TotalTokens,
		TotalCostUSD:     promptCost + completionCost,
		LatencyMs:        completedAt.Sub(startedAt).Milliseconds(),
		ToolCallCount:    toolCalls,
	}
}

func buildLiveLaneScore(runID string, caseDef evaluation.EvalCase, usage *evaluation.EvalRunUsage, stopReason provider.StopReason, answer string, succeeded bool) evaluation.ScorePayload {
	quality := &evaluation.ScoreQualityMetrics{}
	qualitySignals := []float64{}
	if score, ok := liveLaneAnswerScore(caseDef, answer); ok {
		quality.AnswerCorrectness = score
		quality.Groundedness = score
		qualitySignals = append(qualitySignals, score, score)
	}
	taskCompletion := 0.0
	blockingFailure := 1.0
	if succeeded && strings.TrimSpace(answer) != "" && stopReason != provider.StopReasonError {
		taskCompletion = 1
		blockingFailure = 0
	}
	payload := evaluation.ScorePayload{
		SchemaVersion:    evaluation.ScorePayloadSchemaVersion,
		RunID:            runID,
		ReplayConsistent: true,
		RuntimeMetrics: &evaluation.ScoreRuntimeMetrics{
			TaskCompletionRate:  taskCompletion,
			BlockingFailureRate: blockingFailure,
			RetrySuccessRate:    boolToUnit(succeeded),
			EndToEndLatencyMs:   usage.LatencyMs,
			ToolCallCount:       usage.ToolCallCount,
		},
		QualityMetrics: quality,
	}
	components := []float64{taskCompletion, 1 - blockingFailure, boolToUnit(succeeded)}
	if caseDef.Limits != nil && caseDef.Limits.MaxToolCalls > 0 {
		components = append(components, boundedUnitLimit(float64(usage.ToolCallCount), float64(caseDef.Limits.MaxToolCalls)))
		if usage.TotalTokens > 0 {
			payload.RuntimeMetrics.TokenBudgetHitRate = clampLiveMetric(float64(maxInt(usage.TotalTokens-caseDef.Limits.MaxTokens, 0)) / float64(maxInt(caseDef.Limits.MaxTokens, 1)))
		}
	}
	if caseDef.Limits != nil && caseDef.Limits.MaxLatencyMs > 0 {
		components = append(components, boundedUnitLimit(float64(usage.LatencyMs), float64(caseDef.Limits.MaxLatencyMs)))
	}
	components = append(components, qualitySignals...)
	payload.OverallScore = liveMetricAverage(components)
	payload.Level = evaluation.ScoreLevelForOverallScore(payload.OverallScore)
	if len(qualitySignals) == 0 {
		payload.QualityMetrics = nil
	}
	return payload
}

func liveLaneAnswerScore(caseDef evaluation.EvalCase, answer string) (float64, bool) {
	if caseDef.Expected == nil {
		return 0, false
	}
	checks := 0
	passed := 0
	answerLower := strings.ToLower(answer)
	for _, want := range caseDef.Expected.MustInclude {
		trimmed := strings.ToLower(strings.TrimSpace(want))
		if trimmed == "" {
			continue
		}
		checks++
		if strings.Contains(answerLower, trimmed) {
			passed++
		}
	}
	for _, forbidden := range caseDef.Expected.MustNotInclude {
		trimmed := strings.ToLower(strings.TrimSpace(forbidden))
		if trimmed == "" {
			continue
		}
		checks++
		if !strings.Contains(answerLower, trimmed) {
			passed++
		}
	}
	if checks == 0 {
		if strings.TrimSpace(answer) == "" {
			return 0, true
		}
		return 1, true
	}
	return float64(passed) / float64(checks), true
}

func liveLaneBudgetOutcome(lane LaneSpec, usage provider.Usage, toolCalls int) (evaluation.EvalRunPolicyOutcome, error) {
	if lane.MaxTokens > 0 && usage.TotalTokens > lane.MaxTokens {
		return evaluation.EvalRunPolicyOutcome{Policy: "live_lane.budget.tokens", Outcome: "rate_limited", Severity: "warning", Blocking: true, Message: fmt.Sprintf("token budget exceeded: %d > %d", usage.TotalTokens, lane.MaxTokens)}, fmt.Errorf("token budget exceeded")
	}
	if lane.MaxToolCalls > 0 && toolCalls > lane.MaxToolCalls {
		return evaluation.EvalRunPolicyOutcome{Policy: "live_lane.budget.tool_calls", Outcome: "rate_limited", Severity: "warning", Blocking: true, Message: fmt.Sprintf("tool call budget exceeded: %d > %d", toolCalls, lane.MaxToolCalls)}, fmt.Errorf("tool call budget exceeded")
	}
	totalCost := (float64(usage.InputTokens)/1000.0)*lane.PromptCostPer1KUSD + (float64(usage.OutputTokens)/1000.0)*lane.CompletionCostPer1KUSD
	if lane.MaxCostUSD > 0 && totalCost > lane.MaxCostUSD {
		return evaluation.EvalRunPolicyOutcome{Policy: "live_lane.budget.cost", Outcome: "rate_limited", Severity: "warning", Blocking: true, Message: fmt.Sprintf("cost budget exceeded: %.6f > %.6f", totalCost, lane.MaxCostUSD)}, fmt.Errorf("cost budget exceeded")
	}
	return evaluation.EvalRunPolicyOutcome{}, nil
}

func evaluateLiveLaneAssertions(caseDef evaluation.EvalCase, lane LaneSpec, score evaluation.ScorePayload, usage *evaluation.EvalRunUsage) []evaluation.EvalRunPolicyOutcome {
	items := make([]evaluation.EvalRunPolicyOutcome, 0, 4)
	if caseDef.Thresholds != nil {
		thresholdChecks := []struct {
			policy    string
			metric    string
			want      float64
			got       float64
			available bool
		}{
			{policy: "live_lane.threshold.task_completion", metric: "taskCompletionRate", want: caseDef.Thresholds.TaskCompletionRate, got: score.RuntimeMetrics.TaskCompletionRate, available: score.RuntimeMetrics != nil},
			{policy: "live_lane.threshold.groundedness", metric: "groundedness", want: caseDef.Thresholds.Groundedness, got: metricOrZero(score.QualityMetrics, func(metrics *evaluation.ScoreQualityMetrics) float64 { return metrics.Groundedness }), available: score.QualityMetrics != nil},
			{policy: "live_lane.threshold.supported_claim_ratio", metric: "supportedClaimRatio", want: caseDef.Thresholds.SupportedClaimRatio, got: metricOrZero(score.QualityMetrics, func(metrics *evaluation.ScoreQualityMetrics) float64 { return metrics.Groundedness }), available: score.QualityMetrics != nil},
			{policy: "live_lane.threshold.retry_success", metric: "retrySuccessRate", want: caseDef.Thresholds.RetrySuccessRate, got: score.RuntimeMetrics.RetrySuccessRate, available: score.RuntimeMetrics != nil},
		}
		for _, check := range thresholdChecks {
			if check.want == 0 || !check.available {
				continue
			}
			tolerance := laneMetricTolerance(lane, check.metric)
			if check.got+1e-9 < check.want-tolerance {
				items = append(items, evaluation.EvalRunPolicyOutcome{Policy: check.policy, Outcome: "degraded", Severity: "warning", Message: fmt.Sprintf("%s below tolerated threshold: got %.4f want %.4f tolerance %.4f", check.metric, check.got, check.want, tolerance)})
			}
		}
	}
	if caseDef.Limits != nil {
		if caseDef.Limits.MaxLatencyMs > 0 && usage.LatencyMs > int64(caseDef.Limits.MaxLatencyMs)+lane.LatencyToleranceMs {
			items = append(items, evaluation.EvalRunPolicyOutcome{Policy: "live_lane.limit.latency", Outcome: "degraded", Severity: "warning", Message: fmt.Sprintf("latency exceeded tolerated limit: %d > %d", usage.LatencyMs, int64(caseDef.Limits.MaxLatencyMs)+lane.LatencyToleranceMs)})
		}
	}
	return items
}

func evaluateLiveLaneVariance(laneRoot string, score evaluation.ScorePayload, usage *evaluation.EvalRunUsage, lane LaneSpec) (*evaluation.EvalRunVariance, []evaluation.EvalRunPolicyOutcome) {
	previousRunID, previousScore, previousUsage, ok := loadPreviousLiveLaneScore(laneRoot)
	if !ok {
		return nil, nil
	}
	currentMetrics := liveLaneVarianceMetrics(score, usage)
	previousMetrics := liveLaneVarianceMetrics(previousScore, previousUsage)
	metricDeltas := map[string]float64{}
	exceeded := make([]string, 0)
	for metric, current := range currentMetrics {
		previous, exists := previousMetrics[metric]
		if !exists {
			continue
		}
		delta := current - previous
		metricDeltas[metric] = delta
		if math.Abs(delta) > liveLaneVarianceTolerance(lane, metric) {
			exceeded = append(exceeded, metric)
		}
	}
	sort.Strings(exceeded)
	variance := &evaluation.EvalRunVariance{
		ComparedRunID:   previousRunID,
		Window:          1,
		MetricDeltas:    metricDeltas,
		ExceededMetrics: exceeded,
		WithinTolerance: len(exceeded) == 0,
	}
	if len(metricDeltas) == 0 {
		variance.WithinTolerance = true
	}
	if len(exceeded) == 0 {
		return variance, nil
	}
	items := make([]evaluation.EvalRunPolicyOutcome, 0, len(exceeded))
	for _, metric := range exceeded {
		items = append(items, evaluation.EvalRunPolicyOutcome{Policy: "live_lane.variance." + metric, Outcome: "degraded", Severity: "warning", Message: fmt.Sprintf("variance exceeded for %s: delta %.4f tolerance %.4f", metric, metricDeltas[metric], liveLaneVarianceTolerance(lane, metric))})
	}
	return variance, items
}

func loadPreviousLiveLaneScore(laneRoot string) (string, evaluation.ScorePayload, *evaluation.EvalRunUsage, bool) {
	matches, err := filepath.Glob(filepath.Join(laneRoot, "*", "score.json"))
	if err != nil || len(matches) == 0 {
		return "", evaluation.ScorePayload{}, nil, false
	}
	sort.Strings(matches)
	latest := matches[len(matches)-1]
	scoreData, err := os.ReadFile(latest)
	if err != nil {
		return "", evaluation.ScorePayload{}, nil, false
	}
	var score evaluation.ScorePayload
	if err := json.Unmarshal(scoreData, &score); err != nil {
		return "", evaluation.ScorePayload{}, nil, false
	}
	runData, err := os.ReadFile(filepath.Join(filepath.Dir(latest), "run.json"))
	if err != nil {
		return "", evaluation.ScorePayload{}, nil, false
	}
	var run evaluation.EvalRun
	if err := json.Unmarshal(runData, &run); err != nil {
		return "", evaluation.ScorePayload{}, nil, false
	}
	return run.ID, score, run.Usage, true
}

func liveLaneVarianceMetrics(score evaluation.ScorePayload, usage *evaluation.EvalRunUsage) map[string]float64 {
	metrics := map[string]float64{"overallScore": score.OverallScore}
	if score.QualityMetrics != nil {
		metrics["answerCorrectness"] = score.QualityMetrics.AnswerCorrectness
		metrics["groundedness"] = score.QualityMetrics.Groundedness
	}
	if score.RuntimeMetrics != nil {
		metrics["latencyMs"] = float64(score.RuntimeMetrics.EndToEndLatencyMs)
		metrics["toolCallCount"] = float64(score.RuntimeMetrics.ToolCallCount)
	}
	if usage != nil {
		metrics["tokens"] = float64(usage.TotalTokens)
		metrics["costUsd"] = usage.TotalCostUSD
	}
	return metrics
}

func buildLiveLaneProvenance(lane LaneSpec, metadata provider.Metadata) *evaluation.EvalRunProvenance {
	return &evaluation.EvalRunProvenance{
		LaneID:             lane.ID,
		Provider:           firstNonEmpty(metadata.Name, lane.Provider),
		ProviderVersion:    metadata.Version,
		ProviderProvenance: lane.ProviderProvenance,
		Model:              lane.Model,
		ModelProvenance:    lane.ModelProvenance,
		ResolvedBaseURL:    laneResolvedBaseURL(lane.BaseURL, lane.BaseURLEnv),
	}
}

func liveLaneVarianceTolerance(lane LaneSpec, metric string) float64 {
	switch metric {
	case "latencyMs":
		return float64(lane.LatencyToleranceMs)
	case "costUsd":
		return lane.CostToleranceUSD
	default:
		return laneMetricTolerance(lane, metric)
	}
}

func laneMetricTolerance(lane LaneSpec, metric string) float64 {
	if lane.MetricTolerances == nil {
		return 0
	}
	return lane.MetricTolerances[metric]
}

func manifestRef(outputDir, id, filename string) *evaluation.EvalRunRef {
	path := filepath.Join(outputDir, filename)
	return &evaluation.EvalRunRef{ID: id, Path: path, URI: "file://" + path}
}

func redactProviderEvent(event provider.Event) provider.Event {
	current := event
	current.Text, _ = observe.RedactSecrets(current.Text)
	current.Thinking, _ = observe.RedactSecrets(current.Thinking)
	if current.ToolCall != nil {
		toolCall := *current.ToolCall
		toolCall.Arguments = redactRawJSON(toolCall.Arguments)
		current.ToolCall = &toolCall
	}
	if current.ToolCallDelta != nil {
		delta := *current.ToolCallDelta
		delta.ArgumentsDelta, _ = observe.RedactSecrets(delta.ArgumentsDelta)
		current.ToolCallDelta = &delta
	}
	return current
}

func redactRawJSON(input json.RawMessage) json.RawMessage {
	if len(input) == 0 {
		return nil
	}
	text, _ := observe.RedactSecrets(string(input))
	return json.RawMessage(text)
}

func redactError(err error) string {
	if err == nil {
		return ""
	}
	text, _ := observe.RedactSecrets(err.Error())
	return text
}

func metricOrZero[T any](value *T, project func(*T) float64) float64 {
	if value == nil {
		return 0
	}
	return project(value)
}

func boolToUnit(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func boundedUnitLimit(actual, limit float64) float64 {
	if limit <= 0 || actual <= limit {
		return 1
	}
	return clampLiveMetric(limit / actual)
}

func liveMetricAverage(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	count := 0
	for _, value := range values {
		total += clampLiveMetric(value)
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func clampLiveMetric(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
