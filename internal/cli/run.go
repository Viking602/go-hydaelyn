package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/pattern/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/provider/anthropic"
	"github.com/Viking602/go-hydaelyn/provider/openai"
	"github.com/Viking602/go-hydaelyn/team"
)

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
	if strings.Contains(request.Metadata["taskId"], "synth") {
		payload, err := json.Marshal(map[string]any{
			"report": map[string]any{
				"kind":   string(team.ReportKindSynthesis),
				"answer": last.Text,
			},
		})
		if err != nil {
			return nil, err
		}
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventTextDelta, Text: string(payload)},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last.Text},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}
