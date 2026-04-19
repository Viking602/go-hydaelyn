package evalrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/evalcase"
	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
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

func (r *Runner) Run(ctx context.Context, casePath string) (_ *evaluation.EvalRun, err error) {
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

	evalCase, err := evalcase.LoadCase(casePath)
	if err != nil {
		return nil, err
	}
	if err := evalcase.ValidateCase(evalCase); err != nil {
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
	providerDriver, err := resolveProvider(workspace, evalCase)
	if err != nil {
		return nil, err
	}
	runtimeRunner.RegisterProvider(defaultProviderName, providerDriver)

	supervisorProfile := defaultSupervisorProfile
	workerProfile := defaultWorkerProfile
	if evalCase.Profiles != nil {
		supervisorProfile = evalCase.Profiles.Supervisor
		workerProfile = evalCase.Profiles.Worker
	}
	runtimeRunner.RegisterProfile(team.Profile{Name: supervisorProfile, Role: team.RoleSupervisor, Provider: defaultProviderName, Model: defaultProviderName, ToolNames: append([]string{}, evalCase.Tools...)})
	runtimeRunner.RegisterProfile(team.Profile{Name: workerProfile, Role: team.RoleResearcher, Provider: defaultProviderName, Model: defaultProviderName, ToolNames: append([]string{}, evalCase.Tools...)})

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
		WorkerProfiles:    []string{workerProfile},
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
	canonicalReplay := canonicalizeReplayState(replay, startedAt)
	answer := strings.TrimSpace(canonicalReplay.Result.Summary)
	report := evaluation.Evaluate(canonicalEvents)
	score := evaluation.AdaptReportToScorePayload(report, runID)

	artifacts, err := persistArtifacts(stagingDir, evalCase.ID, runID, startedAt, canonicalEvents, canonicalReplay, answer, score)
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

	completedAt := startedAt
	eventsRef := manifestRef(outputDir, "events", "events.json")
	replayRef := manifestRef(outputDir, "replay", "replay.json")
	answerRef := manifestRef(outputDir, "answer", "answer.txt")
	scoreRef := manifestRef(outputDir, "score", "score.json")
	result := &evaluation.EvalRun{
		SchemaVersion:     evaluation.EvalRunSchemaVersion,
		ID:                runID,
		CaseID:            evalCase.ID,
		Mode:              evaluation.EvalRunModeDeterministic,
		RuntimeConfigHash: runtimeConfigHash,
		StartedAt:         startedAt,
		CompletedAt:       completedAt,
		TraceRefs:         &evaluation.EvalRunTraceRefs{Events: eventsRef},
		ArtifactRefs:      &evaluation.EvalRunArtifactRefs{Events: eventsRef, Replay: replayRef, Answer: answerRef},
		ScoreRef:          scoreRef,
		Status:            evaluation.EvalRunStatusCompleted,
	}
	if err := writeJSON(filepath.Join(outputDir, "run.json"), result); err != nil {
		return nil, err
	}
	return result, nil
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

func resolveProvider(workspace string, evalCase evaluation.EvalCase) (provider.Driver, error) {
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

func buildRunMetadata(evalCase evaluation.EvalCase) (string, string, error) {
	payload, err := json.Marshal(struct {
		Pattern  string                       `json:"pattern"`
		Provider *evaluation.EvalCaseProvider `json:"provider,omitempty"`
		Tools    []string                     `json:"tools,omitempty"`
		Profiles *evaluation.EvalCaseProfiles `json:"profiles,omitempty"`
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

func loadFixtureDocuments(workspace string, evalCase evaluation.EvalCase) (map[string]evalcase.CorpusDocument, error) {
	docs := map[string]evalcase.CorpusDocument{}
	if evalCase.Fixtures == nil {
		if hasTool(evalCase.Tools, "fixture_search") {
			defaultCorpus := filepath.Join(workspace, "fixtures", "corpus")
			if _, err := os.Stat(defaultCorpus); err == nil {
				loaded, err := evalcase.LoadCorpus(defaultCorpus)
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
		var loaded map[string]evalcase.CorpusDocument
		if info.IsDir() {
			loaded, err = evalcase.LoadCorpus(resolved)
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

func stageCorpus(stagingDir string, docs map[string]evalcase.CorpusDocument, evalCase evaluation.EvalCase) (string, error) {
	if len(docs) == 0 {
		return "", nil
	}
	selected := make([]evalcase.CorpusDocument, 0, len(docs))
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

func loadCorpusFile(path string) (map[string]evalcase.CorpusDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus file %s: %w", path, err)
	}
	var list []evalcase.CorpusDocument
	if err := json.Unmarshal(data, &list); err == nil {
		items := make(map[string]evalcase.CorpusDocument, len(list))
		for _, doc := range list {
			items[doc.ID] = doc
		}
		return items, nil
	}
	var doc evalcase.CorpusDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode corpus file %s: %w", path, err)
	}
	return map[string]evalcase.CorpusDocument{doc.ID: doc}, nil
}

func mergeDocuments(dst, src map[string]evalcase.CorpusDocument) error {
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

func canonicalizeEvents(events []storage.Event, startedAt time.Time) []storage.Event {
	cloned := make([]storage.Event, 0, len(events))
	for _, event := range events {
		current := event
		current.RecordedAt = startedAt.Add(time.Duration(event.Sequence-1) * time.Millisecond)
		cloned = append(cloned, current)
	}
	return cloned
}

func canonicalizeReplayState(state team.RunState, startedAt time.Time) team.RunState {
	state.CreatedAt = startedAt
	state.UpdatedAt = startedAt
	for i := range state.Tasks {
		state.Tasks[i].StartedAt = time.Time{}
		state.Tasks[i].CompletedAt = time.Time{}
		state.Tasks[i].FinishedAt = time.Time{}
	}
	return state
}

type artifactRecord struct {
	ID          string
	Kind        evaluation.ArtifactManifestKind
	FileName    string
	ContentType string
	Content     []byte
	Redacted    bool
}

func persistArtifacts(stagingDir, caseID, runID string, startedAt time.Time, events []storage.Event, replay team.RunState, answer string, score evaluation.ScorePayload) ([]artifactRecord, error) {
	entries := []struct {
		id          string
		kind        evaluation.ArtifactManifestKind
		fileName    string
		contentType string
		value       any
	}{
		{id: "events", kind: evaluation.ArtifactManifestKindEvents, fileName: "events.json", contentType: "application/json", value: events},
		{id: "replay", kind: evaluation.ArtifactManifestKindReplayState, fileName: "replay.json", contentType: "application/json", value: replay},
		{id: "score", kind: evaluation.ArtifactManifestKindScore, fileName: "score.json", contentType: "application/json", value: score},
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
	artifacts = append(artifacts, artifactRecord{ID: "answer", Kind: evaluation.ArtifactManifestKindAnswer, FileName: "answer.txt", ContentType: "text/plain", Content: answerBytes})
	summary := []byte("# " + caseID + "\n\n" + answer + "\n")
	if err := os.WriteFile(filepath.Join(stagingDir, "summary.md"), summary, 0o644); err != nil {
		return nil, fmt.Errorf("write summary artifact: %w", err)
	}
	artifacts = append(artifacts, artifactRecord{ID: "summary", Kind: evaluation.ArtifactManifestKindSummary, FileName: "summary.md", ContentType: "text/markdown", Content: summary})
	_ = startedAt
	_ = runID
	return artifacts, nil
}

func buildManifest(caseID, runID string, createdAt time.Time, artifacts []artifactRecord) (*evaluation.ArtifactManifest, error) {
	timestamp := createdAt.Format(timestampLayout)
	entries := make([]evaluation.ArtifactManifestEntry, 0, len(artifacts))
	for _, artifact := range artifacts {
		sum := sha256.Sum256(artifact.Content)
		stablePath := filepath.ToSlash(filepath.Join("runs", caseID, runID, timestamp, artifact.FileName))
		entries = append(entries, evaluation.ArtifactManifestEntry{
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
	return &evaluation.ArtifactManifest{SchemaVersion: evaluation.ArtifactManifestSchemaVersion, RunID: runID, CreatedAt: createdAt, Entries: entries}, nil
}

func retentionClassForKind(kind evaluation.ArtifactManifestKind) evaluation.RetentionClass {
	switch kind {
	case evaluation.ArtifactManifestKindScore, evaluation.ArtifactManifestKindSummary:
		return evaluation.RetentionClassPermanent
	case evaluation.ArtifactManifestKindAnswer:
		return evaluation.RetentionClassLongTerm
	default:
		return evaluation.RetentionClassLongTerm
	}
}

func manifestRef(outputDir, id, fileName string) *evaluation.EvalRunRef {
	path := filepath.Join(outputDir, fileName)
	return &evaluation.EvalRunRef{ID: id, Path: path, URI: "file://" + filepath.ToSlash(path)}
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
