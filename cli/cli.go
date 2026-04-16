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

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/providers/anthropic"
	"github.com/Viking602/go-hydaelyn/providers/openai"
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
		"init":    func(args []string) error { return runInit(args, stdout) },
		"new":     func(args []string) error { return runNew(args, stdout) },
		"run":     func(args []string) error { return runRun(ctx, args, stdout) },
		"inspect": func(args []string) error { return runInspect(args, stdout) },
		"replay":  func(args []string) error { return runReplay(args, stdout) },
	}
}

func runInit(args []string, stdout io.Writer) error {
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

func runNew(args []string, stdout io.Writer) error {
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
	runtime, err := newCLIRuntime(*providerName)
	if err != nil {
		return err
	}
	state, err := runtime.StartTeam(ctx, request)
	if err != nil {
		return err
	}
	if *eventsPath != "" {
		events, err := runtime.TeamEvents(ctx, state.ID)
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
	summary := map[string]any{
		"teamId":     firstTeamID(events),
		"eventCount": len(events),
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(summary)
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
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(state)
}

func newCLIRuntime(providerName string) (*host.Runtime, error) {
	runtime := host.New(host.Config{})
	runtime.RegisterPattern(deepsearch.New())
	switch providerName {
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, errors.New("OPENAI_API_KEY environment variable required")
		}
		drv := openai.New(openai.Config{APIKey: apiKey})
		runtime.RegisterProvider("openai", drv)
		runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "openai", Model: "gpt-5.4-mini"})
		runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "openai", Model: "gpt-5.4-mini"})
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, errors.New("ANTHROPIC_API_KEY environment variable required")
		}
		drv := anthropic.New(anthropic.Config{APIKey: apiKey})
		runtime.RegisterProvider("anthropic", drv)
		runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "anthropic", Model: "claude-sonnet-4"})
		runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "anthropic", Model: "claude-sonnet-4"})
	default:
		runtime.RegisterProvider("fake", cliProvider{})
		runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "fake", Model: "test"})
		runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "fake", Model: "test"})
	}
	return runtime, nil
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
