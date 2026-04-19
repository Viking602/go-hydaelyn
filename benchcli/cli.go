package benchcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Viking602/go-hydaelyn/benchmark"
	"github.com/Viking602/go-hydaelyn/evaluation"
)

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing command")
	}
	switch args[0] {
	case "catalog":
		return runCatalog(args[1:], stdout)
	case "validate":
		return runValidate(args[1:], stdout)
	case "run-live":
		return runLive(ctx, args[1:], stdout)
	case "run":
		return runRun(ctx, args[1:], stdout)
	case "report":
		return runReport(args[1:], stdout)
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runCatalog(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
	catalogPath := flags.String("catalog", benchmark.DefaultCatalogPath, "path to benchmark catalog json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	catalog, err := benchmark.LoadCatalog(*catalogPath)
	if err != nil {
		return err
	}
	return encodeJSON(stdout, catalog.Summary())
}

func runValidate(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	catalogPath := flags.String("catalog", benchmark.DefaultCatalogPath, "path to benchmark catalog json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	catalog, err := benchmark.LoadCatalog(*catalogPath)
	if err != nil {
		return err
	}
	return encodeJSON(stdout, map[string]any{
		"ok":             true,
		"version":        catalog.Version,
		"benchmarkCount": len(catalog.Benchmarks),
		"laneCount":      len(catalog.Lanes),
	})
}

type scoreValues []string

func (values *scoreValues) String() string {
	return fmt.Sprintf("%v", []string(*values))
}

func (values *scoreValues) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func runRun(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	catalogPath := flags.String("catalog", benchmark.DefaultCatalogPath, "path to benchmark catalog json")
	benchmarkID := flags.String("benchmark", "", "benchmark id from the catalog")
	laneID := flags.String("lane", "", "model lane id from the catalog")
	mode := flags.String("mode", "smoke", "benchmark mode: smoke or nightly")
	modelOverride := flags.String("model", "", "override the selected lane model for this run")
	baseURLOverride := flags.String("base-url", "", "override the selected lane base URL for this run")
	workspace := flags.String("workspace", ".", "workspace used for external benchmark clones and runs")
	outputDir := flags.String("output-dir", "", "directory to write run artifacts")
	dryRun := flags.Bool("dry-run", false, "resolve commands without executing them")
	skipSetup := flags.Bool("skip-setup", false, "skip benchmark setup commands")
	scoresFile := flags.String("scores-file", "", "json file containing metric scores or an extended score bundle")
	baselineLabel := flags.String("baseline-label", "", "baseline label to compare against")
	officialScoreFile := flags.String("official-score-file", "", "path to the official benchmark score artifact")
	eventsPath := flags.String("events-path", "", "path to a Hydaelyn TeamEvents artifact")
	replayPath := flags.String("replay-path", "", "path to a replay state artifact")
	evaluationPath := flags.String("evaluation-path", "", "path to an evaluation.Report artifact")
	trials := flags.Int("trials", 0, "number of benchmark trials represented by this run")
	var scores scoreValues
	flags.Var(&scores, "score", "metric=value score to merge into the result")
	if err := flags.Parse(args); err != nil {
		return err
	}
	parsedScores, err := benchmark.ParseScoreValues(scores)
	if err != nil {
		return err
	}
	result, err := benchmark.Run(ctx, benchmark.RunOptions{
		CatalogPath:       *catalogPath,
		BenchmarkID:       *benchmarkID,
		LaneID:            *laneID,
		Mode:              *mode,
		ModelOverride:     *modelOverride,
		BaseURLOverride:   *baseURLOverride,
		Workspace:         *workspace,
		OutputDir:         *outputDir,
		DryRun:            *dryRun,
		SkipSetup:         *skipSetup,
		ScoresFile:        *scoresFile,
		Scores:            parsedScores,
		BaselineLabel:     *baselineLabel,
		OfficialScoreFile: *officialScoreFile,
		Trace: benchmark.TraceBundle{
			EventsPath:      *eventsPath,
			ReplayStatePath: *replayPath,
			EvaluationPath:  *evaluationPath,
		},
		TrialCount: *trials,
	})
	if err != nil {
		return err
	}
	runID := filepath.Base(result.OutputDir)
	if runID == "." || runID == string(filepath.Separator) || runID == "" {
		runID = result.Benchmark.ID + "-" + result.Lane.ID
	}
	bundle := benchmark.ScoreBundle{
		Scores:            result.Scores,
		OfficialScoreFile: result.OfficialScoreFile,
		Trace:             result.Trace,
		Cost:              result.Cost,
	}
	return encodeJSON(stdout, benchmark.AdaptScoreBundleToScorePayload(bundle, runID))
}

func runLive(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("run-live", flag.ContinueOnError)
	catalogPath := flags.String("catalog", benchmark.DefaultCatalogPath, "path to benchmark catalog json")
	casePath := flags.String("case", "", "path to an eval case json file")
	laneID := flags.String("lane", "", "model lane id from the catalog")
	modelOverride := flags.String("model", "", "override the selected lane model for this run")
	baseURLOverride := flags.String("base-url", "", "override the selected lane base URL for this run")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *casePath == "" {
		return errors.New("run-live requires --case")
	}
	if *laneID == "" {
		return errors.New("run-live requires --lane")
	}
	catalog, err := benchmark.LoadCatalog(*catalogPath)
	if err != nil {
		return err
	}
	lane, ok := catalog.Lane(*laneID)
	if !ok {
		return fmt.Errorf("unknown lane: %s", *laneID)
	}
	if *modelOverride != "" {
		lane.Model = *modelOverride
	}
	if *baseURLOverride != "" {
		lane.BaseURL = *baseURLOverride
	}
	run, err := benchmark.RunLiveLane(ctx, *casePath, lane)
	if run != nil {
		if encodeErr := encodeJSON(stdout, run); encodeErr != nil {
			return encodeErr
		}
	}
	return err
}

func runReport(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	runPath := flags.String("run-file", "", "path to a run.json file")
	scorePath := flags.String("score-file", "", "path to a score.json file")
	format := flags.String("format", "markdown", "report format: markdown, json, radar, or summary")
	if err := flags.Parse(args); err != nil {
		return err
	}

	// Support both run-file and score-file inputs
	if *runPath == "" && *scorePath == "" {
		return errors.New("report requires --run-file or --score-file")
	}

	// If score file provided, generate capability report
	if *scorePath != "" {
		return generateScoreReport(*scorePath, *format, stdout)
	}

	// Legacy run-file based reporting
	data, err := os.ReadFile(*runPath)
	if err != nil {
		return err
	}
	var result benchmark.RunResult
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	switch *format {
	case "json":
		return encodeJSON(stdout, result.Comparison)
	case "markdown":
		_, err := io.WriteString(stdout, benchmark.RenderComparisonMarkdown(result.Comparison))
		return err
	default:
		return fmt.Errorf("unsupported format for run-file: %s", *format)
	}
}

func generateScoreReport(scorePath string, format string, stdout io.Writer) error {
	data, err := os.ReadFile(scorePath)
	if err != nil {
		return fmt.Errorf("read score file: %w", err)
	}

	var score evaluation.ScorePayload
	if err := json.Unmarshal(data, &score); err != nil {
		return fmt.Errorf("decode score: %w", err)
	}

	switch format {
	case "radar":
		// Output capability radar JSON with all dimensions
		report := evaluation.GenerateCapabilityReport(&score)
		return encodeJSON(stdout, report)
	case "json":
		// Output full score payload with release decision
		output := struct {
			*evaluation.ScorePayload
			ReleaseDecision evaluation.ReleaseDecision `json:"releaseDecision"`
		}{
			ScorePayload:    &score,
			ReleaseDecision: evaluation.EvaluateReleaseGate(&score),
		}
		return encodeJSON(stdout, output)
	case "summary":
		// Output human-readable summary
		summary := evaluation.GenerateSummaryReportFromPayload(&score)
		_, err := io.WriteString(stdout, summary)
		return err
	case "gate":
		// Output release gate decision only
		gateOutput := evaluation.EvaluateReleaseGateWithOutput(&score)
		return encodeJSON(stdout, gateOutput)
	default:
		return fmt.Errorf("unsupported format for score-file: %s", format)
	}
}

func encodeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
