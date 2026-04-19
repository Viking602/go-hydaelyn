package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/evaluation"
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/providers/anthropic"
	"github.com/Viking602/go-hydaelyn/providers/openai"
	"github.com/Viking602/go-hydaelyn/recipe"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing command")
	}
	handler, ok := commandHandlers(ctx, stdout)[args[0]]
	if !ok {
		return fmt.Errorf("unknown command: %s", args[0])
	}
	return handler(args[1:])
}

func commandHandlers(ctx context.Context, stdout io.Writer) map[string]func([]string) error {
	return map[string]func([]string) error{
		"init":     func(args []string) error { return runInit(args, stdout) },
		"new":      func(args []string) error { return runNew(args, stdout) },
		"run":      func(args []string) error { return runRun(ctx, args, stdout) },
		"validate": func(args []string) error { return runValidate(args, stdout) },
		"compile":  func(args []string) error { return runCompile(args, stdout) },
		"inspect":  func(args []string) error { return runInspect(args, stdout) },
		"evaluate": func(args []string) error { return runEvaluate(args, stdout) },
		"replay":   func(args []string) error { return runReplay(args, stdout) },
	}
}

func runInit(args []string, _ io.Writer) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	target := filepath.Join(dir, ".hydaelyn")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	config := map[string]any{
		"pattern":    "deepsearch",
		"supervisor": "supervisor",
		"workers":    []string{"researcher"},
	}
	return writeJSONFile(filepath.Join(target, "config.json"), config)
}

func runNew(args []string, _ io.Writer) error {
	if len(args) == 0 {
		return errors.New("new requires output path")
	}
	request := host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query":      "example query",
			"subqueries": []string{"branch-a", "branch-b"},
		},
	}
	return writeJSONFile(args[0], request)
}

func runRun(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	requestPath := flags.String("request", "", "path to team request json")
	eventsPath := flags.String("events", "", "path to write event log json")
	providerName := flags.String("provider", "fake", "provider: fake, openai, anthropic")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *requestPath == "" {
		return errors.New("run requires --request")
	}
	request := host.StartTeamRequest{}
	if err := readJSONFile(*requestPath, &request); err != nil {
		return err
	}
	runner, err := newCLIRuntime(*providerName)
	if err != nil {
		return err
	}
	state, err := runner.StartTeam(ctx, request)
	if err != nil {
		return err
	}
	if *eventsPath != "" {
		events, err := runner.TeamEvents(ctx, state.ID)
		if err != nil {
			return err
		}
		if err := writeJSONFile(*eventsPath, events); err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(state)
}

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
		state := storage.ReplayTeam(events)
		return encodeJSON(stdout, map[string]any{
			"kind":       "events",
			"ok":         true,
			"teamId":     state.ID,
			"eventCount": len(events),
			"taskCount":  len(state.Tasks),
			"status":     state.Status,
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
	state := storage.ReplayTeam(events)
	return encodeJSON(stdout, state)
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
	report := evaluation.Evaluate(events)
	return encodeJSON(stdout, evaluation.AdaptReportToScorePayload(report, report.TeamID))
}

func newCLIRuntime(providerName string) (*host.Runtime, error) {
	runner := host.New(host.Config{})
	runner.RegisterPattern(deepsearch.New())
	switch providerName {
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, errors.New("OPENAI_API_KEY environment variable required")
		}
		drv := openai.New(openai.Config{APIKey: apiKey})
		runner.RegisterProvider("openai", drv)
		runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "openai", Model: "gpt-5.4-mini"})
		runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "openai", Model: "gpt-5.4-mini"})
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, errors.New("ANTHROPIC_API_KEY environment variable required")
		}
		drv := anthropic.New(anthropic.Config{APIKey: apiKey})
		runner.RegisterProvider("anthropic", drv)
		runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "anthropic", Model: "claude-sonnet-4"})
		runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "anthropic", Model: "claude-sonnet-4"})
	default:
		runner.RegisterProvider("fake", cliProvider{})
		runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "fake", Model: "test"})
		runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "fake", Model: "test"})
	}
	return runner, nil
}

type cliProvider struct{}

func (cliProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "fake"}
}

func (cliProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1]
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last.Text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func readJSONFile(path string, target any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}

func firstTeamID(events []storage.Event) string {
	if len(events) == 0 {
		return ""
	}
	return events[0].TeamID
}

func encodeJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
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
