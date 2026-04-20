package cli

import (
	"errors"
	"flag"
	"io"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/eval"
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/recipe"
	"github.com/Viking602/go-hydaelyn/storage"
)

func runInspect(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "team":
			return runInspectTeam(args[1:], stdout)
		case "events":
			return runInspectEvents(args[1:], stdout)
		}
	}
	return runInspectEvents(args, stdout)
}

func runValidate(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	recipePath := flags.String("recipe", "", "path to recipe yaml/json")
	requestPath := flags.String("request", "", "path to team request json")
	eventsPath := flags.String("events", "", "path to event log json")
	strictDataflow := flags.Bool("strict-dataflow", false, "enable strict recipe dataflow checks")
	if err := flags.Parse(args); err != nil {
		return err
	}
	switch {
	case *recipePath != "":
		spec, err := recipe.DecodeFile(*recipePath)
		if err != nil {
			return err
		}
		compiled, err := recipe.Compile(spec)
		if err != nil {
			return err
		}
		if *strictDataflow {
			report := recipe.ValidateStrictDataflow(compiled.Plan)
			return encodeJSON(stdout, map[string]any{
				"kind":      "recipe",
				"ok":        report.OK,
				"pattern":   compiled.Request.Pattern,
				"taskCount": len(compiled.Plan.Tasks),
				"issues":    report.Issues,
			})
		}
		return encodeJSON(stdout, map[string]any{
			"kind":      "recipe",
			"ok":        true,
			"pattern":   compiled.Request.Pattern,
			"taskCount": len(compiled.Plan.Tasks),
		})
	case *requestPath != "":
		request := host.StartTeamRequest{}
		if err := readJSONFile(*requestPath, &request); err != nil {
			return err
		}
		if request.Pattern == "" {
			return errors.New("request pattern is required")
		}
		if request.SupervisorProfile == "" {
			return errors.New("request supervisorProfile is required")
		}
		if len(request.WorkerProfiles) == 0 {
			return errors.New("request workerProfiles must not be empty")
		}
		return encodeJSON(stdout, map[string]any{
			"kind":        "request",
			"ok":          true,
			"pattern":     request.Pattern,
			"planner":     request.Planner,
			"workerCount": len(request.WorkerProfiles),
		})
	case *eventsPath != "":
		var events []storage.Event
		if err := readJSONFile(*eventsPath, &events); err != nil {
			return err
		}
		if len(events) == 0 {
			return errors.New("event log is empty")
		}
		validation := storage.ValidateReplay(events, storage.ReplayTeam(events))
		return encodeJSON(stdout, map[string]any{
			"kind":             "events",
			"ok":               validation.Valid,
			"valid":            validation.Valid,
			"replayConsistent": validation.ReplayConsistent,
			"teamId":           validation.ReplayedState.ID,
			"eventCount":       len(events),
			"taskCount":        len(validation.ReplayedState.Tasks),
			"status":           validation.ReplayedState.Status,
			"mismatchCount":    validation.MismatchCount,
			"mismatches":       validation.Mismatches,
		})
	default:
		return errors.New("validate requires --recipe, --request, or --events")
	}
}

func runCompile(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("compile", flag.ContinueOnError)
	recipePath := flags.String("recipe", "", "path to recipe yaml/json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *recipePath == "" {
		return errors.New("compile requires --recipe")
	}
	spec, err := recipe.DecodeFile(*recipePath)
	if err != nil {
		return err
	}
	compiled, err := recipe.Compile(spec)
	if err != nil {
		return err
	}
	return encodeJSON(stdout, compiled)
}

func runInspectTeam(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	eventsPath := flags.String("events", "", "path to event log json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *eventsPath == "" {
		return errors.New("inspect requires --events")
	}
	var events []storage.Event
	if err := readJSONFile(*eventsPath, &events); err != nil {
		return err
	}
	state := storage.ReplayTeam(events)
	tasks := make([]map[string]any, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		taskSummary := map[string]any{
			"id":      task.ID,
			"kind":    task.Kind,
			"status":  task.Status,
			"reads":   task.Reads,
			"writes":  task.Writes,
			"publish": task.Publish,
		}
		if task.Result != nil {
			taskSummary["summary"] = task.Result.Summary
			taskSummary["artifactIds"] = task.Result.ArtifactIDs
		}
		tasks = append(tasks, taskSummary)
	}
	return encodeJSON(stdout, map[string]any{
		"teamId":            state.ID,
		"status":            state.Status,
		"phase":             state.Phase,
		"eventCount":        len(events),
		"taskCount":         len(state.Tasks),
		"verificationCount": verificationCount(state.Blackboard),
		"tasks":             tasks,
	})
}

func runInspectEvents(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("inspect-events", flag.ContinueOnError)
	eventsPath := flags.String("events", "", "path to event log json")
	taskID := flags.String("task", "", "filter events by task id")
	after := flags.String("after", "", "filter events recorded after RFC3339 time")
	before := flags.String("before", "", "filter events recorded before RFC3339 time")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *eventsPath == "" {
		return errors.New("inspect requires --events")
	}
	var events []storage.Event
	if err := readJSONFile(*eventsPath, &events); err != nil {
		return err
	}
	afterTime, err := parseOptionalTime(*after)
	if err != nil {
		return err
	}
	beforeTime, err := parseOptionalTime(*before)
	if err != nil {
		return err
	}
	filtered := make([]storage.Event, 0, len(events))
	for _, event := range events {
		if *taskID != "" && event.TaskID != *taskID {
			continue
		}
		if !afterTime.IsZero() && event.RecordedAt.Before(afterTime) {
			continue
		}
		if !beforeTime.IsZero() && event.RecordedAt.After(beforeTime) {
			continue
		}
		filtered = append(filtered, event)
	}
	return encodeJSON(stdout, map[string]any{
		"teamId":     firstTeamID(filtered),
		"eventCount": len(filtered),
		"events":     filtered,
	})
}

func runReplay(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	eventsPath := flags.String("events", "", "path to event log json")
	statePath := flags.String("state", "", "path to authoritative team state json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *eventsPath == "" {
		return errors.New("replay requires --events")
	}
	var events []storage.Event
	if err := readJSONFile(*eventsPath, &events); err != nil {
		return err
	}
	authoritativeState := storage.ReplayTeam(events)
	if *statePath != "" {
		if err := readJSONFile(*statePath, &authoritativeState); err != nil {
			return err
		}
	}
	result := storage.ValidateReplay(events, authoritativeState)
	if err := encodeJSON(stdout, result); err != nil {
		return err
	}
	if !result.Valid {
		return errors.New("replay validation failed")
	}
	return nil
}

func runEvaluate(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	eventsPath := flags.String("events", "", "path to event log json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *eventsPath == "" {
		return errors.New("evaluate requires --events")
	}
	var events []storage.Event
	if err := readJSONFile(*eventsPath, &events); err != nil {
		return err
	}
	report := eval.Evaluate(events)
	validation := storage.ValidateReplay(events, storage.ReplayTeam(events))
	payload := eval.AdaptReportToScorePayloadWithReplayConsistency(report, report.TeamID, validation.Valid)
	return encodeJSON(stdout, payload)
}

func firstTeamID(events []storage.Event) string {
	if len(events) == 0 {
		return ""
	}
	return events[0].TeamID
}

func parseOptionalTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}

func verificationCount(board *blackboard.State) int {
	if board == nil {
		return 0
	}
	return len(board.Verifications)
}
