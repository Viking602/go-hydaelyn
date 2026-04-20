package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/eval/cases"
	"github.com/Viking602/go-hydaelyn/eval"
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/pattern/collab"
	"github.com/Viking602/go-hydaelyn/pattern/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	errorprovider "github.com/Viking602/go-hydaelyn/provider/error"
	"github.com/Viking602/go-hydaelyn/provider/scripted"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
	fixturetool "github.com/Viking602/go-hydaelyn/tool/fixture"
)

const (
	defaultSupervisorProfile = "supervisor"
	defaultWorkerProfile     = "researcher"
	defaultProviderName      = "deterministic"
	timestampLayout          = "20060102T150405Z"
)

type Runner struct {
	options RunnerOptions
}

func NewRunner(options RunnerOptions) *Runner {
	return &Runner{options: options}
}

func (r *Runner) Run(ctx context.Context, casePath string) (_ *eval.EvalRun, err error) {
	casePath, err = filepath.Abs(casePath)
	if err != nil {
		return nil, fmt.Errorf("resolve eval case path: %w", err)
	}
	workspace := r.options.Workspace
	if strings.TrimSpace(workspace) == "" {
		workspace = filepath.Dir(casePath)
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}

	evalCase, err := cases.LoadCase(casePath)
	if err != nil {
		return nil, err
	}
	if err := cases.ValidateCase(evalCase); err != nil {
		return nil, err
	}

	startedAt := r.options.now()
	runID, runtimeConfigHash, err := buildRunMetadata(evalCase)
	if err != nil {
		return nil, err
	}
	outputRoot := r.options.OutputRoot
	if strings.TrimSpace(outputRoot) == "" {
		outputRoot = workspace
	}
	outputRoot, err = filepath.Abs(outputRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve output root: %w", err)
	}
	outputDir := filepath.Join(outputRoot, "runs", evalCase.ID, runID, startedAt.Format(timestampLayout))
	stagingDir := outputDir + ".tmp"
	_ = os.RemoveAll(stagingDir)
	defer func() {
		if err != nil {
			_ = os.RemoveAll(stagingDir)
		}
	}()
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, fmt.Errorf("create staging output directory: %w", err)
	}

	documents, err := loadFixtureDocuments(workspace, evalCase)
	if err != nil {
		return nil, err
	}
	corpusPath, err := stageCorpus(stagingDir, documents, evalCase)
	if err != nil {
		return nil, err
	}

	runtimeRunner := host.New(host.Config{Storage: storage.NewMemoryDriver()})
	runtimeRunner.RegisterPattern(deepsearch.New())
	runtimeRunner.RegisterPattern(collab.New())
	providerDriver, err := resolveProvider(workspace, evalCase)
	if err != nil {
		return nil, err
	}
	runtimeRunner.RegisterProvider(defaultProviderName, providerDriver)

	supervisorProfile := defaultSupervisorProfile
	workerProfiles := []string{defaultWorkerProfile}
	if evalCase.Profiles != nil {
		supervisorProfile = evalCase.Profiles.Supervisor
		workerProfiles = resolveWorkerProfiles(*evalCase.Profiles)
	}
	runtimeRunner.RegisterProfile(team.Profile{Name: supervisorProfile, Role: team.RoleSupervisor, Provider: defaultProviderName, Model: defaultProviderName, ToolNames: append([]string{}, evalCase.Tools...)})
	for _, workerProfile := range workerProfiles {
		runtimeRunner.RegisterProfile(team.Profile{Name: workerProfile, Role: team.RoleResearcher, Provider: defaultProviderName, Model: defaultProviderName, ToolNames: append([]string{}, evalCase.Tools...)})
	}

	for _, name := range evalCase.Tools {
		driver, toolErr := buildTool(name, corpusPath)
		if toolErr != nil {
			return nil, toolErr
		}
		runtimeRunner.RegisterTool(driver)
	}

	state, err := runtimeRunner.StartTeam(ctx, host.StartTeamRequest{
		TeamID:            runID,
		Pattern:           evalCase.Pattern,
		SupervisorProfile: supervisorProfile,
		WorkerProfiles:    append([]string{}, workerProfiles...),
		Input:             cloneInput(evalCase.Input),
		Metadata:          map[string]string{"evalCaseId": evalCase.ID, "evalSuite": evalCase.Suite},
	})
	if err != nil {
		return nil, err
	}

	events, err := runtimeRunner.TeamEvents(ctx, state.ID)
	if err != nil {
		return nil, err
	}
	replay, err := runtimeRunner.ReplayTeamState(ctx, state.ID)
	if err != nil {
		return nil, err
	}
	canonicalEvents := canonicalizeEvents(events, startedAt)
	canonicalState := canonicalizeRunState(state, startedAt)
	canonicalReplay := canonicalizeRunState(replay, startedAt)
	replayConsistent := deterministicReplayEqual(canonicalState, canonicalReplay)
	answer := strings.TrimSpace(canonicalState.Result.Summary)
	report := eval.Evaluate(canonicalEvents)
	corpus := buildEvaluationCorpus(documents, evalCase)
	qualityMetrics := deterministicQualityMetrics(answer, corpus, evalCase)
	evalRun := &eval.EvalRun{
		SchemaVersion:     eval.EvalRunSchemaVersion,
		ID:                runID,
		CaseID:            evalCase.ID,
		Mode:              eval.EvalRunModeDeterministic,
		RuntimeConfigHash: runtimeConfigHash,
		StartedAt:         startedAt,
		CompletedAt:       startedAt.Add(report.EndToEndLatency),
		ReplayConsistent:  boolRef(replayConsistent),
		Usage:             aggregateUsage(canonicalState),
		QualityMetrics:    qualityMetrics,
		Status:            eval.EvalRunStatusCompleted,
	}
	score, err := eval.ScoreCase(evalRun, canonicalEvents, evalCase)
	if err != nil {
		return nil, err
	}
	modelEvents, toolCalls, err := collectSessionArtifacts(ctx, runtimeRunner, state, events)
	if err != nil {
		return nil, err
	}
	evaluationArtifact := buildEvaluationArtifact(evalCase, report, qualityMetrics, score)
	artifacts, err := persistArtifacts(stagingDir, evalCase.ID, runID, startedAt, canonicalEvents, canonicalState, canonicalReplay, answer, report, score, modelEvents, toolCalls, evaluationArtifact)
	if err != nil {
		return nil, err
	}
	manifest, err := buildManifest(evalCase.ID, runID, startedAt, artifacts)
	if err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(stagingDir, "manifest.json"), manifest); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(outputDir), 0o755); err != nil {
		return nil, fmt.Errorf("create output parent directory: %w", err)
	}
	if _, statErr := os.Stat(outputDir); statErr == nil {
		return nil, fmt.Errorf("output directory already exists: %s", outputDir)
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if err := os.Rename(stagingDir, outputDir); err != nil {
		return nil, fmt.Errorf("promote output directory: %w", err)
	}

	completedAt := startedAt.Add(report.EndToEndLatency)
	eventsRef := manifestRef(outputDir, "events", "events.json")
	replayRef := manifestRef(outputDir, "replay", "replay.json")
	finalStateRef := manifestRef(outputDir, "state-final", "state.final.json")
	replayedStateRef := manifestRef(outputDir, "state-replayed", "state.replayed.json")
	answerRef := manifestRef(outputDir, "answer", "answer.txt")
	modelEventsRef := manifestRef(outputDir, "model-events", "model_events.jsonl")
	toolCallsRef := manifestRef(outputDir, "tool-calls", "tool_calls.jsonl")
	evaluationReportRef := manifestRef(outputDir, "evaluation-report", "eval.report.json")
	qualityScoreRef := manifestRef(outputDir, "quality-score", "quality.score.json")
	summaryRef := manifestRef(outputDir, "summary", "summary.md")
	scoreRef := manifestRef(outputDir, "score", "score.json")
	evalRun.CompletedAt = completedAt
	evalRun.TraceRefs = &eval.EvalRunTraceRefs{Events: eventsRef, ModelEvents: modelEventsRef}
	evalRun.ArtifactRefs = &eval.EvalRunArtifactRefs{
		Events:           eventsRef,
		Replay:           replayRef,
		Answer:           answerRef,
		FinalState:       finalStateRef,
		ReplayedState:    replayedStateRef,
		ToolCalls:        toolCallsRef,
		ModelEvents:      modelEventsRef,
		EvaluationReport: evaluationReportRef,
		QualityScore:     qualityScoreRef,
		Summary:          summaryRef,
	}
	evalRun.ScoreRef = scoreRef
	if err := writeJSON(filepath.Join(outputDir, "run.json"), evalRun); err != nil {
		return nil, err
	}
	return evalRun, nil
}

type echoProvider struct{}

func (echoProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: defaultProviderName, Models: []string{defaultProviderName}}
}

func (echoProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last.Text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func resolveProvider(workspace string, evalCase eval.EvalCase) (provider.Driver, error) {
	if evalCase.Provider == nil {
		return echoProvider{}, nil
	}
	if evalCase.Provider.ScriptPath != "" {
		scriptPath := resolvePath(workspace, evalCase.Provider.ScriptPath)
		events, err := scripted.LoadScript(scriptPath)
		if err != nil {
			return nil, err
		}
		return scripted.New(events), nil
	}
	if evalCase.Provider.ErrorKind != "" {
		return errorprovider.New(errorprovider.Kind(evalCase.Provider.ErrorKind)), nil
	}
	return echoProvider{}, nil
}

func buildTool(name, corpusPath string) (tool.Driver, error) {
	switch name {
	case "fixture_search":
		if strings.TrimSpace(corpusPath) == "" {
			return nil, fmt.Errorf("fixture_search requires fixture corpus")
		}
		return fixturetool.NewSearchTool(corpusPath)
	case "calculator":
		return fixturetool.NewCalculatorTool(), nil
	case "flaky":
		return fixturetool.NewFlakyTool(1), nil
	case "slow":
		return fixturetool.NewSlowTool(10 * time.Millisecond), nil
	case "permission":
		return fixturetool.NewPermissionTool(), nil
	case "approval":
		return fixturetool.NewApprovalTool(), nil
	case "write_mock":
		return fixturetool.NewWriteMockTool(), nil
	case "email_mock":
		return fixturetool.NewEmailMockTool(), nil
	default:
		return nil, fmt.Errorf("unsupported fixture tool %q", name)
	}
}

func buildRunMetadata(evalCase eval.EvalCase) (string, string, error) {
	payload, err := json.Marshal(struct {
		Pattern  string                       `json:"pattern"`
		Provider *eval.EvalCaseProvider `json:"provider,omitempty"`
		Tools    []string                     `json:"tools,omitempty"`
		Profiles *eval.EvalCaseProfiles `json:"profiles,omitempty"`
	}{
		Pattern:  evalCase.Pattern,
		Provider: evalCase.Provider,
		Tools:    append([]string{}, evalCase.Tools...),
		Profiles: evalCase.Profiles,
	})
	if err != nil {
		return "", "", fmt.Errorf("marshal runtime config: %w", err)
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s", evalCase.ID, hash[:12]), hash, nil
}

func loadFixtureDocuments(workspace string, evalCase eval.EvalCase) (map[string]cases.CorpusDocument, error) {
	docs := map[string]cases.CorpusDocument{}
	if evalCase.Fixtures == nil {
		if hasTool(evalCase.Tools, "fixture_search") {
			defaultCorpus := filepath.Join(workspace, "fixtures", "corpus")
			if _, err := os.Stat(defaultCorpus); err == nil {
				loaded, err := cases.LoadCorpus(defaultCorpus)
				if err != nil {
					return nil, err
				}
				mergeDocuments(docs, loaded)
			}
		}
		return docs, nil
	}
	paths := append([]string{}, evalCase.Fixtures.Paths...)
	if len(paths) == 0 && len(evalCase.Fixtures.CorpusIDs) > 0 {
		paths = append(paths, filepath.Join("fixtures", "corpus"))
	}
	for _, item := range paths {
		resolved := resolvePath(workspace, item)
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("stat fixture path %s: %w", resolved, err)
		}
		var loaded map[string]cases.CorpusDocument
		if info.IsDir() {
			loaded, err = cases.LoadCorpus(resolved)
		} else {
			loaded, err = loadCorpusFile(resolved)
		}
		if err != nil {
			return nil, err
		}
		if err := mergeDocuments(docs, loaded); err != nil {
			return nil, err
		}
	}
	for _, id := range evalCase.Fixtures.CorpusIDs {
		if _, ok := docs[id]; !ok {
			return nil, fmt.Errorf("fixture corpus id %q not found", id)
		}
	}
	return docs, nil
}

func stageCorpus(stagingDir string, docs map[string]cases.CorpusDocument, evalCase eval.EvalCase) (string, error) {
	if len(docs) == 0 {
		return "", nil
	}
	selected := make([]cases.CorpusDocument, 0, len(docs))
	if evalCase.Fixtures != nil && len(evalCase.Fixtures.CorpusIDs) > 0 {
		for _, id := range evalCase.Fixtures.CorpusIDs {
			selected = append(selected, docs[id])
		}
	} else {
		ids := make([]string, 0, len(docs))
		for id := range docs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			selected = append(selected, docs[id])
		}
	}
	corpusDir := filepath.Join(stagingDir, "runtime", "corpus")
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		return "", fmt.Errorf("create corpus staging directory: %w", err)
	}
	if err := writeJSON(filepath.Join(corpusDir, "documents.json"), selected); err != nil {
		return "", err
	}
	return corpusDir, nil
}

func loadCorpusFile(path string) (map[string]cases.CorpusDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus file %s: %w", path, err)
	}
	var list []cases.CorpusDocument
	if err := json.Unmarshal(data, &list); err == nil {
		items := make(map[string]cases.CorpusDocument, len(list))
		for _, doc := range list {
			items[doc.ID] = doc
		}
		return items, nil
	}
	var doc cases.CorpusDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode corpus file %s: %w", path, err)
	}
	return map[string]cases.CorpusDocument{doc.ID: doc}, nil
}

func mergeDocuments(dst, src map[string]cases.CorpusDocument) error {
	for id, doc := range src {
		if _, exists := dst[id]; exists {
			return fmt.Errorf("duplicate corpus document id %q", id)
		}
		dst[id] = doc
	}
	return nil
}

func hasTool(names []string, target string) bool {
	return slices.Contains(names, target)
}

func resolvePath(workspace, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspace, path)
}

func cloneInput(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(src))
	maps.Copy(cloned, src)
	return cloned
}

func resolveWorkerProfiles(profiles eval.EvalCaseProfiles) []string {
	names := make([]string, 0, 1+len(profiles.Workers))
	seen := map[string]struct{}{}
	if trimmed := strings.TrimSpace(profiles.Worker); trimmed != "" {
		seen[trimmed] = struct{}{}
		names = append(names, trimmed)
	}
	for _, name := range profiles.Workers {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			names = append(names, trimmed)
		}
	}
	if len(names) == 0 {
		return []string{defaultWorkerProfile}
	}
	return names
}

func canonicalizeEvents(events []storage.Event, startedAt time.Time) []storage.Event {
	cloned := make([]storage.Event, 0, len(events))
	for _, event := range events {
		current := event
		current.RecordedAt = startedAt.Add(time.Duration(event.Sequence-1) * time.Millisecond)
		cloned = append(cloned, current)
	}
	return cloned
}

func canonicalizeRunState(state team.RunState, startedAt time.Time) team.RunState {
	state.SessionID = ""
	state.CreatedAt = startedAt
	state.UpdatedAt = startedAt
	state.Version = 0
	state.Input = nil
	state.Metadata = nil
	state.RequireVerification = false
	if state.Planning != nil && deterministicEmptyPlanningState(*state.Planning) {
		state.Planning = nil
	}
	state.Supervisor.Profile = ""
	state.Supervisor.SessionID = ""
	state.Supervisor.Budget = team.Budget{}
	state.Supervisor.Metadata = nil
	for i := range state.Workers {
		state.Workers[i].Profile = ""
		state.Workers[i].SessionID = ""
		state.Workers[i].Budget = team.Budget{}
		state.Workers[i].Metadata = nil
	}
	if state.Result != nil {
		normalizeComparableResult(state.Result)
	}
	for i := range state.Tasks {
		state.Tasks[i].Stage = ""
		state.Tasks[i].RequiredCapabilities = nil
		state.Tasks[i].Assignee = ""
		state.Tasks[i].Namespace = ""
		state.Tasks[i].VerifierRequired = false
		state.Tasks[i].IdempotencyKey = ""
		state.Tasks[i].StartedAt = time.Time{}
		state.Tasks[i].CompletedAt = time.Time{}
		state.Tasks[i].FinishedAt = time.Time{}
		state.Tasks[i].CompletedBy = ""
		state.Tasks[i].Error = ""
		state.Tasks[i].SessionID = ""
		state.Tasks[i].MaxAttempts = 0
		state.Tasks[i].Version = 0
		if state.Tasks[i].Result != nil {
			normalizeComparableResult(state.Tasks[i].Result)
		}
		if len(state.Tasks[i].DependsOn) == 0 {
			state.Tasks[i].DependsOn = nil
		}
		if len(state.Tasks[i].Reads) == 0 {
			state.Tasks[i].Reads = nil
		}
		if len(state.Tasks[i].Writes) == 0 {
			state.Tasks[i].Writes = nil
		}
		if len(state.Tasks[i].Publish) == 0 {
			state.Tasks[i].Publish = nil
		}
	}
	if state.Blackboard != nil {
		if len(state.Blackboard.Sources) == 0 {
			state.Blackboard.Sources = nil
		}
		if len(state.Blackboard.Artifacts) == 0 {
			state.Blackboard.Artifacts = nil
		}
		if len(state.Blackboard.Evidence) == 0 {
			state.Blackboard.Evidence = nil
		}
		if len(state.Blackboard.Claims) == 0 {
			state.Blackboard.Claims = nil
		}
		if len(state.Blackboard.Findings) == 0 {
			state.Blackboard.Findings = nil
		}
		if len(state.Blackboard.Verifications) == 0 {
			state.Blackboard.Verifications = nil
		}
		if len(state.Blackboard.Exchanges) == 0 {
			state.Blackboard.Exchanges = nil
		}
	}
	if len(state.Workers) == 0 {
		state.Workers = nil
	}
	if len(state.Tasks) == 0 {
		state.Tasks = nil
	}
	return state
}

func normalizeComparableResult(result *team.Result) {
	if result == nil {
		return
	}
	result.Findings = nil
	result.Evidence = nil
	result.Confidence = 0
	if len(result.Structured) == 0 {
		result.Structured = nil
	}
	if len(result.ArtifactIDs) == 0 {
		result.ArtifactIDs = nil
	}
	if deterministicEmptyResult(*result) {
		*result = team.Result{}
	}
}

func deterministicEmptyPlanningState(state team.PlanningState) bool {
	return state.PlannerName == "" &&
		state.Goal == "" &&
		len(state.SuccessCriteria) == 0 &&
		state.ReviewCount == 0 &&
		state.LastAction == "" &&
		state.LastActionReason == "" &&
		state.PlanVersion == 0
}

func deterministicEmptyResult(result team.Result) bool {
	return result.Summary == "" &&
		len(result.Structured) == 0 &&
		len(result.ArtifactIDs) == 0 &&
		len(result.Findings) == 0 &&
		len(result.Evidence) == 0 &&
		result.Confidence == 0 &&
		result.Usage == (team.Result{}).Usage &&
		result.ToolCallCount == 0 &&
		result.Error == ""
}

func buildEvaluationCorpus(docs map[string]cases.CorpusDocument, evalCase eval.EvalCase) eval.Corpus {
	if len(docs) == 0 {
		return eval.Corpus{}
	}
	items := make([]eval.CorpusDocument, 0, len(docs))
	if evalCase.Fixtures != nil && len(evalCase.Fixtures.CorpusIDs) > 0 {
		for _, id := range evalCase.Fixtures.CorpusIDs {
			doc, ok := docs[id]
			if !ok {
				continue
			}
			items = append(items, eval.CorpusDocument{ID: doc.ID, Date: doc.Date, Text: doc.Text})
		}
	} else {
		ids := make([]string, 0, len(docs))
		for id := range docs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			doc := docs[id]
			items = append(items, eval.CorpusDocument{ID: doc.ID, Date: doc.Date, Text: doc.Text})
		}
	}
	return eval.Corpus{Documents: items}
}

func deterministicQualityMetrics(answer string, corpus eval.Corpus, evalCase eval.EvalCase) *eval.ScoreQualityMetrics {
	expected := evalCase.Expected
	if strings.TrimSpace(answer) == "" && expected == nil && len(corpus.Documents) == 0 {
		return nil
	}
	metrics := &eval.ScoreQualityMetrics{}
	hasSignal := false
	if expected != nil && (len(expected.MustInclude) > 0 || len(expected.MustNotInclude) > 0) {
		metrics.AnswerCorrectness = expectationScore(answer, *expected)
		hasSignal = true
	}
	refusal := eval.DetectRefusal(answer)
	if len(corpus.Documents) == 0 {
		if refusal.RefusalDetected {
			metrics.Groundedness = 1
			hasSignal = true
		}
	} else {
		groundedness := eval.CheckGroundedness(answer, corpus)
		if groundedness.GroundednessRatio > 0 || groundedness.SupportedClaims > 0 || groundedness.UnsupportedClaims > 0 || groundedness.BlockedClaims > 0 {
			metrics.Groundedness = groundedness.GroundednessRatio
			if refusal.RefusalDetected && groundedness.UnsupportedClaims == 0 {
				metrics.Groundedness = 1
			}
			hasSignal = true
		}
		citations := eval.ValidateCitations(answer, corpus)
		if len(citations.Citations) > 0 || len(citations.RelevantCitations) > 0 {
			metrics.CitationPrecision = citations.Precision
			metrics.CitationRecall = citations.Recall
			hasSignal = true
		}
	}
	if expected != nil && len(expected.RequiredCitations) > 0 {
		metrics.CitationRecall = requiredCitationRecall(answer, expected.RequiredCitations)
		hasSignal = true
	}
	if !hasSignal {
		return nil
	}
	return metrics
}

func expectationScore(answer string, expected eval.EvalCaseExpected) float64 {
	totalChecks := len(expected.MustInclude) + len(expected.MustNotInclude)
	if totalChecks == 0 {
		return 0
	}
	normalized := strings.ToLower(normalizeWhitespace(answer))
	passed := 0
	for _, fragment := range expected.MustInclude {
		if strings.Contains(normalized, strings.ToLower(normalizeWhitespace(fragment))) {
			passed++
		}
	}
	for _, fragment := range expected.MustNotInclude {
		if !strings.Contains(normalized, strings.ToLower(normalizeWhitespace(fragment))) {
			passed++
		}
	}
	return float64(passed) / float64(totalChecks)
}

func requiredCitationRecall(answer string, required []string) float64 {
	requiredSet := map[string]struct{}{}
	for _, id := range required {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			requiredSet[trimmed] = struct{}{}
		}
	}
	if len(requiredSet) == 0 {
		return 0
	}
	citations := citationIDsFromAnswer(answer)
	matched := 0
	for _, citation := range citations {
		if _, ok := requiredSet[citation]; ok {
			matched++
		}
	}
	return float64(matched) / float64(len(requiredSet))
}

func citationIDsFromAnswer(answer string) []string {
	corpus := eval.Corpus{Documents: []eval.CorpusDocument{}}
	return eval.ValidateCitations(answer, corpus).Citations
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	maps.Copy(cloned, values)
	return cloned
}

func boolRef(value bool) *bool {
	return &value
}

func deterministicReplayEqual(left, right team.RunState) bool {
	if reflect.DeepEqual(left, right) {
		return true
	}
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftJSON) == string(rightJSON)
}

func aggregateUsage(state team.RunState) *eval.EvalRunUsage {
	usage := &eval.EvalRunUsage{}
	hasUsage := false
	for _, task := range state.Tasks {
		if task.Result == nil {
			continue
		}
		if task.Result.Usage.TotalTokens > 0 || task.Result.Usage.InputTokens > 0 || task.Result.Usage.OutputTokens > 0 {
			hasUsage = true
		}
		usage.PromptTokens += task.Result.Usage.InputTokens
		usage.CompletionTokens += task.Result.Usage.OutputTokens
		usage.TotalTokens += task.Result.Usage.TotalTokens
		usage.ToolCallCount += task.Result.ToolCallCount
	}
	if !hasUsage && usage.ToolCallCount == 0 {
		return nil
	}
	return usage
}

type sessionModelEvent struct {
	TaskID     string              `json:"taskId,omitempty"`
	SessionID  string              `json:"sessionId,omitempty"`
	AgentID    string              `json:"agentId,omitempty"`
	Index      int                 `json:"index"`
	Role       message.Role        `json:"role"`
	Kind       message.Kind        `json:"kind,omitempty"`
	Text       string              `json:"text,omitempty"`
	Thinking   string              `json:"thinking,omitempty"`
	ToolCalls  []message.ToolCall  `json:"toolCalls,omitempty"`
	ToolResult *message.ToolResult `json:"toolResult,omitempty"`
	Metadata   map[string]string   `json:"metadata,omitempty"`
}

type toolCallLogRecord struct {
	TaskID     string          `json:"taskId,omitempty"`
	SessionID  string          `json:"sessionId,omitempty"`
	AgentID    string          `json:"agentId,omitempty"`
	Index      int             `json:"index"`
	Kind       string          `json:"kind"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	Name       string          `json:"name,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Content    string          `json:"content,omitempty"`
	Structured json.RawMessage `json:"structured,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
}

func collectSessionArtifacts(ctx context.Context, runtimeRunner *host.Runtime, state team.RunState, events []storage.Event) ([]sessionModelEvent, []toolCallLogRecord, error) {
	modelEvents := make([]sessionModelEvent, 0, len(state.Tasks)*4)
	toolCalls := make([]toolCallLogRecord, 0, len(state.Tasks)*2)
	for _, task := range state.Tasks {
		if strings.TrimSpace(task.SessionID) == "" {
			continue
		}
		snapshot, err := runtimeRunner.GetSession(ctx, task.SessionID)
		if err != nil {
			return nil, nil, err
		}
		for idx, msg := range snapshot.Messages {
			modelEvents = append(modelEvents, sessionModelEvent{
				TaskID:     task.ID,
				SessionID:  task.SessionID,
				AgentID:    task.EffectiveAssigneeAgentID(),
				Index:      idx,
				Role:       msg.Role,
				Kind:       msg.Kind,
				Text:       msg.Text,
				Thinking:   msg.Thinking,
				ToolCalls:  append([]message.ToolCall{}, msg.ToolCalls...),
				ToolResult: msg.ToolResult,
				Metadata:   cloneStringMap(msg.Metadata),
			})
			for _, call := range msg.ToolCalls {
				toolCalls = append(toolCalls, toolCallLogRecord{
					TaskID:     task.ID,
					SessionID:  task.SessionID,
					AgentID:    task.EffectiveAssigneeAgentID(),
					Index:      idx,
					Kind:       "call",
					ToolCallID: call.ID,
					Name:       call.Name,
					Arguments:  call.Arguments,
				})
			}
			if msg.ToolResult != nil {
				toolCalls = append(toolCalls, toolCallLogRecord{
					TaskID:     task.ID,
					SessionID:  task.SessionID,
					AgentID:    task.EffectiveAssigneeAgentID(),
					Index:      idx,
					Kind:       "result",
					ToolCallID: msg.ToolResult.ToolCallID,
					Name:       msg.ToolResult.Name,
					Content:    msg.ToolResult.Content,
					Structured: msg.ToolResult.Structured,
					IsError:    msg.ToolResult.IsError,
				})
			}
		}
	}
	if len(modelEvents) == 0 && strings.TrimSpace(state.SessionID) != "" {
		snapshot, err := runtimeRunner.GetSession(ctx, state.SessionID)
		if err != nil {
			return nil, nil, err
		}
		for idx, msg := range snapshot.Messages {
			taskID := ""
			if msg.Metadata != nil {
				taskID = msg.Metadata["taskId"]
			}
			modelEvents = append(modelEvents, sessionModelEvent{
				TaskID:     taskID,
				SessionID:  state.SessionID,
				AgentID:    msg.AgentID,
				Index:      idx,
				Role:       msg.Role,
				Kind:       msg.Kind,
				Text:       msg.Text,
				Thinking:   msg.Thinking,
				ToolCalls:  append([]message.ToolCall{}, msg.ToolCalls...),
				ToolResult: msg.ToolResult,
				Metadata:   cloneStringMap(msg.Metadata),
			})
		}
	}
	if len(toolCalls) == 0 {
		for _, event := range events {
			if event.Type != storage.EventToolCalled {
				continue
			}
			toolCalls = append(toolCalls, toolCallLogRecord{
				TaskID:     event.TaskID,
				Kind:       "event",
				ToolCallID: payloadString(event.Payload["toolCallId"]),
				Name:       payloadString(event.Payload["name"]),
			})
		}
	}
	return modelEvents, toolCalls, nil
}

func buildEvaluationArtifact(evalCase eval.EvalCase, report eval.Report, qualityMetrics *eval.ScoreQualityMetrics, score *eval.ScorePayload) map[string]any {
	payload := map[string]any{
		"caseId":  evalCase.ID,
		"suite":   evalCase.Suite,
		"runtime": report,
	}
	if qualityMetrics != nil {
		payload["quality"] = qualityMetrics
	}
	if score != nil {
		payload["score"] = score
	}
	return payload
}

func payloadString(value any) string {
	text, _ := value.(string)
	return text
}

type artifactRecord struct {
	ID          string
	Kind        eval.ArtifactManifestKind
	FileName    string
	ContentType string
	Content     []byte
	Redacted    bool
}

func persistArtifacts(
	stagingDir,
	caseID,
	runID string,
	startedAt time.Time,
	events []storage.Event,
	finalState team.RunState,
	replayedState team.RunState,
	answer string,
	report eval.Report,
	score *eval.ScorePayload,
	modelEvents []sessionModelEvent,
	toolCalls []toolCallLogRecord,
	evaluationArtifact map[string]any,
) ([]artifactRecord, error) {
	entries := []struct {
		id          string
		kind        eval.ArtifactManifestKind
		fileName    string
		contentType string
		value       any
	}{
		{id: "events", kind: eval.ArtifactManifestKindEvents, fileName: "events.json", contentType: "application/json", value: events},
		{id: "replay", kind: eval.ArtifactManifestKindReplayState, fileName: "replay.json", contentType: "application/json", value: replayedState},
		{id: "state-final", kind: eval.ArtifactManifestKindReplayState, fileName: "state.final.json", contentType: "application/json", value: finalState},
		{id: "state-replayed", kind: eval.ArtifactManifestKindReplayState, fileName: "state.replayed.json", contentType: "application/json", value: replayedState},
		{id: "evaluation-report", kind: eval.ArtifactManifestKindEvaluationReport, fileName: "eval.report.json", contentType: "application/json", value: evaluationArtifact},
		{id: "score", kind: eval.ArtifactManifestKindScore, fileName: "score.json", contentType: "application/json", value: score},
		{id: "quality-score", kind: eval.ArtifactManifestKindScore, fileName: "quality.score.json", contentType: "application/json", value: score},
	}
	artifacts := make([]artifactRecord, 0, 5)
	for _, entry := range entries {
		data, err := json.MarshalIndent(entry.value, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal %s artifact: %w", entry.id, err)
		}
		path := filepath.Join(stagingDir, entry.fileName)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, fmt.Errorf("write %s artifact: %w", entry.id, err)
		}
		artifacts = append(artifacts, artifactRecord{ID: entry.id, Kind: entry.kind, FileName: entry.fileName, ContentType: entry.contentType, Content: data})
	}
	answerBytes := []byte(answer)
	if err := os.WriteFile(filepath.Join(stagingDir, "answer.txt"), answerBytes, 0o644); err != nil {
		return nil, fmt.Errorf("write answer artifact: %w", err)
	}
	artifacts = append(artifacts, artifactRecord{ID: "answer", Kind: eval.ArtifactManifestKindAnswer, FileName: "answer.txt", ContentType: "text/plain", Content: answerBytes})
	modelEventBytes, err := marshalJSONLines(modelEvents)
	if err != nil {
		return nil, fmt.Errorf("marshal model event log: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "model_events.jsonl"), modelEventBytes, 0o644); err != nil {
		return nil, fmt.Errorf("write model event artifact: %w", err)
	}
	artifacts = append(artifacts, artifactRecord{ID: "model-events", Kind: eval.ArtifactManifestKindModelEvents, FileName: "model_events.jsonl", ContentType: "application/x-ndjson", Content: modelEventBytes})
	toolCallBytes, err := marshalJSONLines(toolCalls)
	if err != nil {
		return nil, fmt.Errorf("marshal tool call log: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "tool_calls.jsonl"), toolCallBytes, 0o644); err != nil {
		return nil, fmt.Errorf("write tool call artifact: %w", err)
	}
	artifacts = append(artifacts, artifactRecord{ID: "tool-calls", Kind: eval.ArtifactManifestKindToolCalls, FileName: "tool_calls.jsonl", ContentType: "application/x-ndjson", Content: toolCallBytes})
	summary := []byte("# " + caseID + "\n\n" + answer + "\n")
	if err := os.WriteFile(filepath.Join(stagingDir, "summary.md"), summary, 0o644); err != nil {
		return nil, fmt.Errorf("write summary artifact: %w", err)
	}
	artifacts = append(artifacts, artifactRecord{ID: "summary", Kind: eval.ArtifactManifestKindSummary, FileName: "summary.md", ContentType: "text/markdown", Content: summary})
	_ = report
	_ = startedAt
	_ = runID
	return artifacts, nil
}

func marshalJSONLines[T any](items []T) ([]byte, error) {
	if len(items) == 0 {
		return []byte{}, nil
	}
	lines := make([][]byte, 0, len(items))
	total := 0
	for _, item := range items {
		line, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
		total += len(line) + 1
	}
	buffer := make([]byte, 0, total)
	for _, line := range lines {
		buffer = append(buffer, line...)
		buffer = append(buffer, '\n')
	}
	return buffer, nil
}

func buildManifest(caseID, runID string, createdAt time.Time, artifacts []artifactRecord) (*eval.ArtifactManifest, error) {
	timestamp := createdAt.Format(timestampLayout)
	entries := make([]eval.ArtifactManifestEntry, 0, len(artifacts))
	for _, artifact := range artifacts {
		sum := sha256.Sum256(artifact.Content)
		stablePath := filepath.ToSlash(filepath.Join("runs", caseID, runID, timestamp, artifact.FileName))
		entries = append(entries, eval.ArtifactManifestEntry{
			ID:             artifact.ID,
			Kind:           artifact.Kind,
			Path:           stablePath,
			URI:            "file://" + stablePath,
			Checksum:       "sha256:" + hex.EncodeToString(sum[:]),
			Size:           int64(len(artifact.Content)),
			ContentType:    artifact.ContentType,
			RetentionClass: retentionClassForKind(artifact.Kind),
			Redacted:       artifact.Redacted,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind == entries[j].Kind {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].Kind < entries[j].Kind
	})
	return &eval.ArtifactManifest{SchemaVersion: eval.ArtifactManifestSchemaVersion, RunID: runID, CreatedAt: createdAt, Entries: entries}, nil
}

func retentionClassForKind(kind eval.ArtifactManifestKind) eval.RetentionClass {
	switch kind {
	case eval.ArtifactManifestKindScore, eval.ArtifactManifestKindSummary:
		return eval.RetentionClassPermanent
	case eval.ArtifactManifestKindAnswer:
		return eval.RetentionClassLongTerm
	default:
		return eval.RetentionClassLongTerm
	}
}

func manifestRef(outputDir, id, fileName string) *eval.EvalRunRef {
	path := filepath.Join(outputDir, fileName)
	return &eval.EvalRunRef{ID: id, Path: path, URI: "file://" + filepath.ToSlash(path)}
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
