package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"strings"

	"github.com/Viking602/go-hydaelyn/evalcase"
	"github.com/Viking602/go-hydaelyn/evalrun"
)

func runDeterministic(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("run-deterministic", flag.ContinueOnError)
	caseDir := flags.String("case-dir", "", "directory containing deterministic eval case json files")
	casePath := flags.String("case", "", "path to a single deterministic eval case json file")
	workspace := flags.String("workspace", ".", "workspace used to resolve relative scripts and fixtures")
	outputDir := flags.String("output-dir", "", "directory to write deterministic suite artifacts")
	name := flags.String("name", "", "optional suite name override")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*caseDir) == "" && strings.TrimSpace(*casePath) == "" {
		return errors.New("run-deterministic requires --case-dir or --case")
	}
	runner := evalrun.NewRunner(evalrun.RunnerOptions{
		Workspace:  *workspace,
		OutputRoot: *outputDir,
	})
	var (
		suite *evalrun.SuiteRun
		err   error
	)
	if strings.TrimSpace(*casePath) != "" {
		runName := *name
		if strings.TrimSpace(runName) == "" {
			runName = filepath.Base(filepath.Dir(*casePath))
		}
		suite, err = runner.RunSuite(ctx, runName, filepath.Dir(*casePath), []string{*casePath})
	} else {
		runName := *name
		if strings.TrimSpace(runName) == "" {
			runName = filepath.Base(*caseDir)
		}
		casePaths, discoverErr := evalcase.DiscoverCasePaths(*caseDir)
		if discoverErr != nil {
			return discoverErr
		}
		suite, err = runner.RunSuite(ctx, runName, *caseDir, casePaths)
	}
	if err != nil {
		return err
	}
	return encodeJSON(stdout, suite)
}
