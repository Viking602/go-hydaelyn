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

func runReport(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	runPath := flags.String("run-file", "", "path to a run.json file")
	format := flags.String("format", "markdown", "report format: markdown or json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *runPath == "" {
		return errors.New("report requires --run-file")
	}
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
		return fmt.Errorf("unsupported format: %s", *format)
	}
}

func encodeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
