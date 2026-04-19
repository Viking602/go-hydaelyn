package evalrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/evalcase"
	"github.com/Viking602/go-hydaelyn/evaluation"
)

const SuiteRunSchemaVersion = "1.0"

type SuiteRun struct {
	SchemaVersion    string                       `json:"schemaVersion"`
	ID               string                       `json:"id"`
	Name             string                       `json:"name"`
	Root             string                       `json:"root,omitempty"`
	OutputDir        string                       `json:"outputDir,omitempty"`
	StartedAt        time.Time                    `json:"startedAt"`
	CompletedAt      time.Time                    `json:"completedAt"`
	TotalCases       int                          `json:"totalCases"`
	Passed           int                          `json:"passed"`
	Failed           int                          `json:"failed"`
	Pass             bool                         `json:"pass"`
	AggregateScore   *evaluation.ScorePayload     `json:"aggregateScore,omitempty"`
	CapabilityReport *evaluation.CapabilityReport `json:"capabilityReport,omitempty"`
	ReleaseDecision  evaluation.ReleaseDecision   `json:"releaseDecision,omitempty"`
	Cases            []SuiteCaseResult            `json:"cases,omitempty"`
	Artifacts        map[string]string            `json:"artifacts,omitempty"`
}

type SuiteCaseResult struct {
	CaseID       string                   `json:"caseId,omitempty"`
	Suite        string                   `json:"suite,omitempty"`
	CasePath     string                   `json:"casePath,omitempty"`
	RunID        string                   `json:"runId,omitempty"`
	Status       evaluation.EvalRunStatus `json:"status,omitempty"`
	Pass         bool                     `json:"pass,omitempty"`
	OverallScore float64                  `json:"overallScore,omitempty"`
	Level        evaluation.ScoreLevel    `json:"level,omitempty"`
	OutputDir    string                   `json:"outputDir,omitempty"`
	ScorePath    string                   `json:"scorePath,omitempty"`
	Error        string                   `json:"error,omitempty"`
}

func (r *Runner) RunCaseDirectory(ctx context.Context, root string) (*SuiteRun, error) {
	casePaths, err := evalcase.DiscoverCasePaths(root)
	if err != nil {
		return nil, err
	}
	return r.RunSuite(ctx, filepath.Base(root), root, casePaths)
}

func (r *Runner) RunSuite(ctx context.Context, name, root string, casePaths []string) (*SuiteRun, error) {
	if len(casePaths) == 0 {
		return nil, fmt.Errorf("run suite: at least one case path is required")
	}
	startedAt := r.options.now()
	suiteName := normalizeSuiteName(name)
	outputRoot := r.options.OutputRoot
	if strings.TrimSpace(outputRoot) == "" {
		workspace := r.options.Workspace
		if strings.TrimSpace(workspace) == "" {
			workspace = root
		}
		outputRoot = workspace
	}
	outputRoot, err := filepath.Abs(outputRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve suite output root: %w", err)
	}
	outputDir := filepath.Join(outputRoot, "suites", suiteName, startedAt.Format(timestampLayout))
	stagingDir := outputDir + ".tmp"
	_ = os.RemoveAll(stagingDir)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, fmt.Errorf("create suite staging dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(stagingDir)
	}()

	results := make([]SuiteCaseResult, 0, len(casePaths))
	scores := make([]evaluation.ScorePayload, 0, len(casePaths))
	passed := 0
	for _, casePath := range casePaths {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, score := r.runSuiteCase(ctx, casePath)
		results = append(results, result)
		scores = append(scores, score)
		if score.Pass {
			passed++
		}
	}
	sort.Slice(results, func(i, j int) bool {
		left := firstNonEmptyString(results[i].CaseID, results[i].CasePath)
		right := firstNonEmptyString(results[j].CaseID, results[j].CasePath)
		return left < right
	})

	suiteID := fmt.Sprintf("%s-%s", suiteName, startedAt.Format(timestampLayout))
	aggregateScore := evaluation.AggregateScores(suiteID, suiteName, scores)
	capabilityReport := evaluation.GenerateCapabilityReport(aggregateScore)
	releaseDecision := evaluation.ReleaseDecisionNoGo
	pass := false
	if aggregateScore != nil {
		pass = aggregateScore.Pass
		if pass {
			releaseDecision = evaluation.EvaluateReleaseGate(aggregateScore)
		}
		if capabilityReport != nil {
			capabilityReport.ReleaseDecision = releaseDecision
		}
	}

	suiteRun := &SuiteRun{
		SchemaVersion:    SuiteRunSchemaVersion,
		ID:               suiteID,
		Name:             suiteName,
		Root:             root,
		StartedAt:        startedAt,
		CompletedAt:      r.options.now(),
		TotalCases:       len(results),
		Passed:           passed,
		Failed:           len(results) - passed,
		Pass:             pass,
		AggregateScore:   aggregateScore,
		CapabilityReport: capabilityReport,
		ReleaseDecision:  releaseDecision,
		Cases:            results,
	}
	if err := persistSuiteArtifacts(stagingDir, suiteRun); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(outputDir), 0o755); err != nil {
		return nil, fmt.Errorf("create suite output parent: %w", err)
	}
	if err := os.Rename(stagingDir, outputDir); err != nil {
		return nil, fmt.Errorf("promote suite output: %w", err)
	}
	suiteRun.OutputDir = outputDir
	suiteRun.Artifacts = map[string]string{
		"suite":   filepath.Join(outputDir, "suite.json"),
		"cases":   filepath.Join(outputDir, "cases.json"),
		"score":   filepath.Join(outputDir, "score.json"),
		"report":  filepath.Join(outputDir, "capability.report.json"),
		"summary": filepath.Join(outputDir, "summary.md"),
	}
	if err := writeJSON(filepath.Join(outputDir, "suite.json"), suiteRun); err != nil {
		return nil, err
	}
	return suiteRun, nil
}

func (r *Runner) runSuiteCase(ctx context.Context, casePath string) (SuiteCaseResult, evaluation.ScorePayload) {
	meta := probeSuiteCase(casePath)
	run, err := r.Run(ctx, casePath)
	if err != nil {
		score := failedSuiteScore(meta, err)
		return SuiteCaseResult{
			CaseID:   meta.CaseID,
			Suite:    meta.Suite,
			CasePath: casePath,
			Status:   evaluation.EvalRunStatusFailed,
			Pass:     false,
			Level:    score.Level,
			Error:    err.Error(),
		}, score
	}
	score, scoreErr := loadSuiteScore(run)
	if scoreErr != nil {
		fallback := failedSuiteScore(meta, scoreErr)
		return SuiteCaseResult{
			CaseID:    firstNonEmptyString(run.CaseID, meta.CaseID),
			Suite:     firstNonEmptyString(meta.Suite, fallback.Suite),
			CasePath:  casePath,
			RunID:     run.ID,
			Status:    evaluation.EvalRunStatusFailed,
			Pass:      false,
			OutputDir: scoreDirectory(run),
			Error:     scoreErr.Error(),
			Level:     fallback.Level,
		}, fallback
	}
	return SuiteCaseResult{
		CaseID:       score.CaseID,
		Suite:        score.Suite,
		CasePath:     casePath,
		RunID:        run.ID,
		Status:       run.Status,
		Pass:         score.Pass,
		OverallScore: score.OverallScore,
		Level:        score.Level,
		OutputDir:    scoreDirectory(run),
		ScorePath:    refPath(run.ScoreRef),
	}, score
}

type suiteCaseMeta struct {
	CaseID string
	Suite  string
}

func probeSuiteCase(casePath string) suiteCaseMeta {
	var meta suiteCaseMeta
	data, err := os.ReadFile(casePath)
	if err != nil {
		meta.CaseID = strings.TrimSuffix(filepath.Base(casePath), filepath.Ext(casePath))
		return meta
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		meta.CaseID = strings.TrimSuffix(filepath.Base(casePath), filepath.Ext(casePath))
		return meta
	}
	_ = json.Unmarshal(envelope["id"], &meta.CaseID)
	_ = json.Unmarshal(envelope["suite"], &meta.Suite)
	if strings.TrimSpace(meta.CaseID) == "" {
		meta.CaseID = strings.TrimSuffix(filepath.Base(casePath), filepath.Ext(casePath))
	}
	return meta
}

func failedSuiteScore(meta suiteCaseMeta, err error) evaluation.ScorePayload {
	score := evaluation.ScorePayload{
		SchemaVersion:    evaluation.ScorePayloadSchemaVersion,
		RunID:            firstNonEmptyString(meta.CaseID, "failed-case"),
		CaseID:           meta.CaseID,
		Suite:            meta.Suite,
		Pass:             false,
		ReplayConsistent: false,
		RuntimeMetrics: &evaluation.ScoreRuntimeMetrics{
			TaskCompletionRate:  0,
			BlockingFailureRate: 1,
			RetrySuccessRate:    0,
		},
		Failures: []evaluation.ScoreFailure{{
			Code:     "evalrun.execution_failed",
			Message:  err.Error(),
			Metric:   "taskCompletionRate",
			Layer:    "runner",
			Severity: "error",
			Blocking: true,
		}},
	}
	score.OverallScore = 0
	score.Level = evaluation.ApplyHardDowngradeRules(&score)
	return score
}

func loadSuiteScore(run *evaluation.EvalRun) (evaluation.ScorePayload, error) {
	if run == nil || run.ScoreRef == nil || strings.TrimSpace(run.ScoreRef.Path) == "" {
		return evaluation.ScorePayload{}, fmt.Errorf("eval run score ref is missing")
	}
	data, err := os.ReadFile(run.ScoreRef.Path)
	if err != nil {
		return evaluation.ScorePayload{}, fmt.Errorf("read score artifact: %w", err)
	}
	var score evaluation.ScorePayload
	if err := json.Unmarshal(data, &score); err != nil {
		return evaluation.ScorePayload{}, fmt.Errorf("decode score artifact: %w", err)
	}
	return score, nil
}

func persistSuiteArtifacts(outputDir string, suiteRun *SuiteRun) error {
	if suiteRun == nil {
		return fmt.Errorf("persist suite artifacts: suite run is required")
	}
	if err := writeJSON(filepath.Join(outputDir, "cases.json"), suiteRun.Cases); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "score.json"), suiteRun.AggregateScore); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "capability.report.json"), suiteRun.CapabilityReport); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "summary.md"), []byte(renderSuiteSummary(suiteRun)), 0o644); err != nil {
		return fmt.Errorf("write suite summary: %w", err)
	}
	return writeJSON(filepath.Join(outputDir, "suite.json"), suiteRun)
}

func renderSuiteSummary(suiteRun *SuiteRun) string {
	var builder strings.Builder
	builder.WriteString("# Deterministic Eval Suite\n\n")
	builder.WriteString(fmt.Sprintf("- Suite: %s\n", suiteRun.Name))
	builder.WriteString(fmt.Sprintf("- Total cases: %d\n", suiteRun.TotalCases))
	builder.WriteString(fmt.Sprintf("- Passed: %d\n", suiteRun.Passed))
	builder.WriteString(fmt.Sprintf("- Failed: %d\n", suiteRun.Failed))
	builder.WriteString(fmt.Sprintf("- Release decision: %s\n", suiteRun.ReleaseDecision))
	if suiteRun.AggregateScore != nil {
		builder.WriteString(fmt.Sprintf("- Overall score: %.3f\n", suiteRun.AggregateScore.OverallScore))
		builder.WriteString(fmt.Sprintf("- Level: %s\n", suiteRun.AggregateScore.Level))
	}
	builder.WriteString("\n## Cases\n\n")
	for _, result := range suiteRun.Cases {
		caseID := firstNonEmptyString(result.CaseID, filepath.Base(result.CasePath))
		line := fmt.Sprintf("- `%s`: pass=%t level=%s", caseID, result.Pass, result.Level)
		if result.Error != "" {
			line += fmt.Sprintf(" error=%s", result.Error)
		}
		builder.WriteString(line + "\n")
	}
	return builder.String()
}

func refPath(ref *evaluation.EvalRunRef) string {
	if ref == nil {
		return ""
	}
	return ref.Path
}

func scoreDirectory(run *evaluation.EvalRun) string {
	if run == nil || run.ScoreRef == nil {
		return ""
	}
	return filepath.Dir(run.ScoreRef.Path)
}

func normalizeSuiteName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "deterministic"
	}
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	normalized := strings.ToLower(replacer.Replace(trimmed))
	normalized = strings.Trim(normalized, "-")
	if normalized == "" {
		return "deterministic"
	}
	return normalized
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
