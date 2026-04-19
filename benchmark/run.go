package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TraceBundle struct {
	EventsPath      string `json:"eventsPath,omitempty"`
	ReplayStatePath string `json:"replayStatePath,omitempty"`
	EvaluationPath  string `json:"evaluationPath,omitempty"`
}

type CostInfo struct {
	PromptTokens     int     `json:"promptTokens,omitempty"`
	CompletionTokens int     `json:"completionTokens,omitempty"`
	TotalTokens      int     `json:"totalTokens,omitempty"`
	InputCostUSD     float64 `json:"inputCostUsd,omitempty"`
	OutputCostUSD    float64 `json:"outputCostUsd,omitempty"`
	TotalCostUSD     float64 `json:"totalCostUsd,omitempty"`
	LatencyMs        int64   `json:"latencyMs,omitempty"`
}

type ScoreBundle struct {
	Scores            map[string]float64 `json:"scores,omitempty"`
	OfficialScoreFile string             `json:"officialScoreFile,omitempty"`
	OfficialMetadata  map[string]any     `json:"officialMetadata,omitempty"`
	Trace             TraceBundle        `json:"trace,omitempty"`
	Cost              CostInfo           `json:"cost,omitempty"`
}

type RunOptions struct {
	CatalogPath       string
	BenchmarkID       string
	LaneID            string
	Mode              string
	ModelOverride     string
	BaseURLOverride   string
	Workspace         string
	OutputDir         string
	DryRun            bool
	SkipSetup         bool
	ScoresFile        string
	Scores            map[string]float64
	BaselineLabel     string
	OfficialScoreFile string
	Trace             TraceBundle
	TrialCount        int
}

type CommandResult struct {
	Phase       string        `json:"phase"`
	Command     string        `json:"command"`
	StartedAt   time.Time     `json:"startedAt"`
	CompletedAt time.Time     `json:"completedAt"`
	Duration    time.Duration `json:"duration"`
	ExitCode    int           `json:"exitCode"`
	StdoutPath  string        `json:"stdoutPath,omitempty"`
	StderrPath  string        `json:"stderrPath,omitempty"`
}

type RunResult struct {
	Benchmark         BenchmarkSpec      `json:"benchmark"`
	Lane              LaneSpec           `json:"lane"`
	Mode              string             `json:"mode"`
	CatalogPath       string             `json:"catalogPath"`
	Workspace         string             `json:"workspace"`
	BenchmarkDir      string             `json:"benchmarkDir"`
	OutputDir         string             `json:"outputDir"`
	Timestamp         time.Time          `json:"timestamp"`
	DryRun            bool               `json:"dryRun"`
	TrialCount        int                `json:"trialCount,omitempty"`
	SetupCommands     []string           `json:"setupCommands,omitempty"`
	RunCommands       []string           `json:"runCommands,omitempty"`
	CommandResults    []CommandResult    `json:"commandResults,omitempty"`
	Scores            map[string]float64 `json:"scores,omitempty"`
	OfficialScoreFile string             `json:"officialScoreFile,omitempty"`
	Trace             TraceBundle        `json:"trace,omitempty"`
	Cost              CostInfo           `json:"cost,omitempty"`
	Comparison        ComparisonReport   `json:"comparison"`
}

func Run(ctx context.Context, options RunOptions) (RunResult, error) {
	if options.BenchmarkID == "" {
		return RunResult{}, errors.New("benchmark id is required")
	}
	if options.LaneID == "" {
		return RunResult{}, errors.New("lane id is required")
	}
	if options.Mode == "" {
		options.Mode = "smoke"
	}
	if options.Workspace == "" {
		options.Workspace = "."
	}
	workspace, err := filepath.Abs(options.Workspace)
	if err != nil {
		return RunResult{}, err
	}
	catalogPath := options.CatalogPath
	if catalogPath == "" {
		catalogPath = DefaultCatalogPath
	}
	catalogPath, err = filepath.Abs(catalogPath)
	if err != nil {
		return RunResult{}, err
	}
	catalog, err := LoadCatalog(catalogPath)
	if err != nil {
		return RunResult{}, err
	}
	bench, ok := catalog.Benchmark(options.BenchmarkID)
	if !ok {
		return RunResult{}, fmt.Errorf("unknown benchmark: %s", options.BenchmarkID)
	}
	lane, ok := catalog.Lane(options.LaneID)
	if !ok {
		return RunResult{}, fmt.Errorf("unknown lane: %s", options.LaneID)
	}
	if options.ModelOverride != "" {
		lane.Model = options.ModelOverride
	}
	if options.BaseURLOverride != "" {
		lane.BaseURL = options.BaseURLOverride
	}
	runCommands, err := bench.CommandsForMode(options.Mode)
	if err != nil {
		return RunResult{}, err
	}
	timestamp := time.Now().UTC()
	outputDir := options.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(workspace, catalog.DefaultOutputDir, bench.ID, lane.ID, timestamp.Format("20060102T150405Z"))
	} else if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(workspace, outputDir)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return RunResult{}, err
	}
	benchmarkDir := filepath.Join(workspace, "benchmarks", "_external", bench.ID)
	if err := os.MkdirAll(filepath.Dir(benchmarkDir), 0o755); err != nil {
		return RunResult{}, err
	}
	templateData := TemplateData{
		Catalog:      catalog,
		Benchmark:    bench,
		Lane:         lane,
		Mode:         options.Mode,
		Workspace:    workspace,
		BenchmarkDir: benchmarkDir,
		OutputDir:    outputDir,
		Timestamp:    timestamp.Format(time.RFC3339),
		TrialCount:   options.TrialCount,
	}
	setupCommands, err := ResolveCommands(bench.SetupCommands, templateData)
	if err != nil {
		return RunResult{}, err
	}
	resolvedRunCommands, err := ResolveCommands(runCommands, templateData)
	if err != nil {
		return RunResult{}, err
	}
	result := RunResult{
		Benchmark:     bench,
		Lane:          lane,
		Mode:          options.Mode,
		CatalogPath:   catalogPath,
		Workspace:     workspace,
		BenchmarkDir:  benchmarkDir,
		OutputDir:     outputDir,
		Timestamp:     timestamp,
		DryRun:        options.DryRun,
		TrialCount:    options.TrialCount,
		SetupCommands: setupCommands,
		RunCommands:   resolvedRunCommands,
		Scores:        map[string]float64{},
		Trace:         resolveTracePaths(workspace, options.Trace),
	}
	if bench.OfficialRepoURL != "" && !options.DryRun {
		commandResults, err := ensureOfficialRepo(ctx, workspace, benchmarkDir, bench.OfficialRepoURL, bench.OfficialRef)
		result.CommandResults = append(result.CommandResults, commandResults...)
		if err != nil {
			result.Comparison = BuildComparisonReport(bench, lane, options.Mode, timestamp, result.Scores, nil)
			_ = persistRunArtifacts(result)
			return result, err
		}
	}
	automaticBundle := ScoreBundle{Scores: map[string]float64{}}
	if !options.DryRun {
		preparedBundle, err := prepareBenchmarkArtifacts(ctx, bench, lane, benchmarkDir, outputDir, options.Mode)
		if err != nil {
			result.Comparison = BuildComparisonReport(bench, lane, options.Mode, timestamp, result.Scores, nil)
			_ = persistRunArtifacts(result)
			return result, err
		}
		automaticBundle = mergeScoreBundles(automaticBundle, preparedBundle)
	}
	if !options.DryRun {
		if !options.SkipSetup {
			commandResults, err := executeCommands(ctx, workspace, outputDir, "setup", setupCommands, lane)
			result.CommandResults = append(result.CommandResults, commandResults...)
			if err != nil {
				result.Comparison = BuildComparisonReport(bench, lane, options.Mode, timestamp, result.Scores, nil)
				_ = persistRunArtifacts(result)
				return result, err
			}
		}
		commandResults, err := executeCommands(ctx, workspace, outputDir, "run", resolvedRunCommands, lane)
		result.CommandResults = append(result.CommandResults, commandResults...)
		if err != nil {
			result.Comparison = BuildComparisonReport(bench, lane, options.Mode, timestamp, result.Scores, nil)
			_ = persistRunArtifacts(result)
			return result, err
		}
		collectedBundle, err := collectBenchmarkResults(bench, lane, outputDir)
		if err != nil {
			result.Comparison = BuildComparisonReport(bench, lane, options.Mode, timestamp, result.Scores, nil)
			_ = persistRunArtifacts(result)
			return result, err
		}
		automaticBundle = mergeScoreBundles(automaticBundle, collectedBundle)
	}
	bundle, err := loadScoreBundle(workspace, options.ScoresFile)
	if err != nil {
		return result, err
	}
	bundle = mergeScoreBundles(automaticBundle, bundle)
	for metric, score := range bundle.Scores {
		result.Scores[metric] = score
	}
	for metric, score := range options.Scores {
		result.Scores[metric] = score
	}
	if bundle.OfficialScoreFile != "" {
		result.OfficialScoreFile = resolveOptionalPath(workspace, bundle.OfficialScoreFile)
	}
	if options.OfficialScoreFile != "" {
		result.OfficialScoreFile = resolveOptionalPath(workspace, options.OfficialScoreFile)
	}
	if bundle.Trace != (TraceBundle{}) {
		if result.Trace == (TraceBundle{}) {
			result.Trace = resolveTracePaths(workspace, bundle.Trace)
		} else {
			result.Trace = mergeTraceBundles(result.Trace, resolveTracePaths(workspace, bundle.Trace))
		}
	}
	result.Cost = bundle.Cost
	var baseline *BaselineSnapshot
	if selected, ok := bench.Baseline(options.BaselineLabel); ok {
		baseline = &selected
	}
	result.Comparison = BuildComparisonReport(bench, lane, options.Mode, timestamp, result.Scores, baseline)
	if err := persistRunArtifacts(result); err != nil {
		return result, err
	}
	return result, nil
}

func executeCommands(ctx context.Context, workspace, outputDir, phase string, commands []string, lane LaneSpec) ([]CommandResult, error) {
	results := make([]CommandResult, 0, len(commands))
	for index, command := range commands {
		commandResult, err := executeCommand(ctx, workspace, outputDir, phase, index, command, lane)
		results = append(results, commandResult)
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

func ensureOfficialRepo(ctx context.Context, workspace, benchmarkDir, repoURL, ref string) ([]CommandResult, error) {
	results := []CommandResult{}
	gitDir := filepath.Join(benchmarkDir, ".git")
	if _, err := os.Stat(gitDir); errors.Is(err, os.ErrNotExist) {
		command := fmt.Sprintf("git clone %s %s", quoteShellPath(repoURL), quoteShellPath(benchmarkDir))
		result, runErr := executeCommand(ctx, workspace, filepath.Dir(benchmarkDir), "repo", 0, command, LaneSpec{})
		results = append(results, result)
		if runErr != nil {
			return results, runErr
		}
	}
	commands := []string{
		fmt.Sprintf("git -C %s fetch --tags --force origin", quoteShellPath(benchmarkDir)),
		fmt.Sprintf("git -C %s checkout --force %s", quoteShellPath(benchmarkDir), ref),
	}
	for index, command := range commands {
		result, runErr := executeCommand(ctx, workspace, filepath.Dir(benchmarkDir), "repo", index+1, command, LaneSpec{})
		results = append(results, result)
		if runErr != nil {
			return results, runErr
		}
	}
	return results, nil
}

func executeCommand(ctx context.Context, workspace, outputDir, phase string, index int, command string, lane LaneSpec) (CommandResult, error) {
	name := fmt.Sprintf("%s-%02d", phase, index+1)
	stdoutPath := filepath.Join(outputDir, name+".stdout.log")
	stderrPath := filepath.Join(outputDir, name+".stderr.log")
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return CommandResult{}, err
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		return CommandResult{}, err
	}
	defer stderrFile.Close()
	cmd := shellCommand(ctx, command)
	cmd.Dir = workspace
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = append(os.Environ(), laneEnvironment(lane)...)
	startedAt := time.Now().UTC()
	runErr := cmd.Run()
	completedAt := time.Now().UTC()
	result := CommandResult{
		Phase:       phase,
		Command:     command,
		StartedAt:   startedAt,
		CompletedAt: completedAt,
		Duration:    completedAt.Sub(startedAt),
		StdoutPath:  stdoutPath,
		StderrPath:  stderrPath,
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if runErr != nil {
		return result, fmt.Errorf("%s command failed: %w", phase, runErr)
	}
	return result, nil
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

func laneEnvironment(lane LaneSpec) []string {
	env := []string{
		"HYDAELYN_BENCH_PROVIDER=" + lane.Provider,
		"HYDAELYN_BENCH_MODEL=" + lane.Model,
		"HYDAELYN_BENCH_LANE=" + lane.ID,
		"PYTHONUTF8=1",
	}
	if lane.JudgeProvider != "" {
		env = append(env, "HYDAELYN_BENCH_JUDGE_PROVIDER="+lane.JudgeProvider)
	}
	if lane.JudgeModel != "" {
		env = append(env, "HYDAELYN_BENCH_JUDGE_MODEL="+lane.JudgeModel)
	}
	if lane.UserModelProvider != "" {
		env = append(env, "HYDAELYN_BENCH_USER_PROVIDER="+lane.UserModelProvider)
	}
	if lane.UserModel != "" {
		env = append(env, "HYDAELYN_BENCH_USER_MODEL="+lane.UserModel)
	}
	switch lane.Provider {
	case "openai", "openrouter":
		if value := os.Getenv(lane.APIKeyEnv); value != "" {
			env = append(env, "OPENAI_API_KEY="+value)
		}
		if baseURL := laneResolvedBaseURL(lane.BaseURL, lane.BaseURLEnv); baseURL != "" {
			env = append(env, "OPENAI_BASE_URL="+baseURL)
		}
	case "anthropic":
		if value := os.Getenv(lane.APIKeyEnv); value != "" {
			env = append(env, "ANTHROPIC_API_KEY="+value)
		}
	}
	switch lane.JudgeProvider {
	case "openai", "openrouter":
		if value := os.Getenv(lane.JudgeAPIKeyEnv); value != "" {
			env = append(env, "OPENAI_API_KEY="+value)
		}
		if baseURL := laneResolvedBaseURL(lane.JudgeBaseURL, lane.JudgeBaseURLEnv); baseURL != "" {
			env = append(env, "OPENAI_BASE_URL="+baseURL)
		}
	case "anthropic":
		if value := os.Getenv(lane.JudgeAPIKeyEnv); value != "" {
			env = append(env, "ANTHROPIC_API_KEY="+value)
		}
	}
	keys := make([]string, 0, len(lane.ExtraEnv))
	for key := range lane.ExtraEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+lane.ExtraEnv[key])
	}
	return env
}

func loadScoreBundle(workspace, path string) (ScoreBundle, error) {
	if path == "" {
		return ScoreBundle{Scores: map[string]float64{}}, nil
	}
	resolved := resolveOptionalPath(workspace, path)
	data, err := os.ReadFile(resolved)
	if err != nil {
		return ScoreBundle{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return ScoreBundle{}, fmt.Errorf("decode score bundle: %w", err)
	}
	if _, ok := raw["scores"]; ok {
		var bundle ScoreBundle
		if err := json.Unmarshal(data, &bundle); err != nil {
			return ScoreBundle{}, fmt.Errorf("decode score bundle: %w", err)
		}
		if bundle.Scores == nil {
			bundle.Scores = map[string]float64{}
		}
		return bundle, nil
	}
	scores := map[string]float64{}
	for key, value := range raw {
		var score float64
		if err := json.Unmarshal(value, &score); err != nil {
			return ScoreBundle{}, fmt.Errorf("score %s must be numeric", key)
		}
		scores[key] = score
	}
	return ScoreBundle{Scores: scores}, nil
}

func persistRunArtifacts(result RunResult) error {
	runPath := filepath.Join(result.OutputDir, "run.json")
	if err := writeJSON(runPath, result); err != nil {
		return err
	}
	comparisonPath := filepath.Join(result.OutputDir, "comparison.json")
	if err := writeJSON(comparisonPath, result.Comparison); err != nil {
		return err
	}
	markdownPath := filepath.Join(result.OutputDir, "comparison.md")
	if err := os.WriteFile(markdownPath, []byte(RenderComparisonMarkdown(result.Comparison)), 0o644); err != nil {
		return err
	}
	return persistArtifactManifest(result)
}

func writeJSON(path string, value any) error {
	data, err := MarshalIndented(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func resolveOptionalPath(workspace, path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspace, path)
}

func quoteShellPath(path string) string {
	return strconv.Quote(path)
}

func resolveTracePaths(workspace string, trace TraceBundle) TraceBundle {
	return TraceBundle{
		EventsPath:      resolveOptionalPath(workspace, trace.EventsPath),
		ReplayStatePath: resolveOptionalPath(workspace, trace.ReplayStatePath),
		EvaluationPath:  resolveOptionalPath(workspace, trace.EvaluationPath),
	}
}

func mergeTraceBundles(left, right TraceBundle) TraceBundle {
	if left.EventsPath == "" {
		left.EventsPath = right.EventsPath
	}
	if left.ReplayStatePath == "" {
		left.ReplayStatePath = right.ReplayStatePath
	}
	if left.EvaluationPath == "" {
		left.EvaluationPath = right.EvaluationPath
	}
	return left
}

func mergeScoreBundles(left, right ScoreBundle) ScoreBundle {
	if left.Scores == nil {
		left.Scores = map[string]float64{}
	}
	for metric, score := range right.Scores {
		left.Scores[metric] = score
	}
	if left.OfficialScoreFile == "" {
		left.OfficialScoreFile = right.OfficialScoreFile
	}
	if left.Trace == (TraceBundle{}) {
		left.Trace = right.Trace
	} else {
		left.Trace = mergeTraceBundles(left.Trace, right.Trace)
	}
	if left.Cost == (CostInfo{}) {
		left.Cost = right.Cost
	} else {
		left.Cost = mergeCostInfo(left.Cost, right.Cost)
	}
	if left.OfficialMetadata == nil && right.OfficialMetadata != nil {
		left.OfficialMetadata = right.OfficialMetadata
	}
	return left
}

func mergeCostInfo(left, right CostInfo) CostInfo {
	if right.PromptTokens != 0 {
		left.PromptTokens = right.PromptTokens
	}
	if right.CompletionTokens != 0 {
		left.CompletionTokens = right.CompletionTokens
	}
	if right.TotalTokens != 0 {
		left.TotalTokens = right.TotalTokens
	}
	if right.InputCostUSD != 0 {
		left.InputCostUSD = right.InputCostUSD
	}
	if right.OutputCostUSD != 0 {
		left.OutputCostUSD = right.OutputCostUSD
	}
	if right.TotalCostUSD != 0 {
		left.TotalCostUSD = right.TotalCostUSD
	}
	if right.LatencyMs != 0 {
		left.LatencyMs = right.LatencyMs
	}
	return left
}

func laneResolvedBaseURL(baseURL, baseURLEnv string) string {
	if baseURL != "" {
		return baseURL
	}
	if baseURLEnv == "" {
		return ""
	}
	return os.Getenv(baseURLEnv)
}

func ParseScoreValues(values []string) (map[string]float64, error) {
	scores := map[string]float64{}
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("score %q must use metric=value", value)
		}
		score, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return nil, fmt.Errorf("score %q has invalid value: %w", value, err)
		}
		scores[parts[0]] = score
	}
	return scores, nil
}
