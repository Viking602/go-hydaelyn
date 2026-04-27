package cli

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

func runInspectEvents(_ context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("inspect-events", flag.ContinueOnError)
	eventsPath := flags.String("events", "", "path to JSON-encoded []orchestrator.Event")
	taskFilter := flags.String("task", "", "only show events for this task id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *eventsPath == "" {
		return errors.New("inspect-events requires --events")
	}
	var events []orchestrator.Event
	if err := readJSONFile(*eventsPath, &events); err != nil {
		return err
	}
	out := events
	if *taskFilter != "" {
		filtered := make([]orchestrator.Event, 0, len(events))
		for _, ev := range events {
			if ev.TaskID == *taskFilter {
				filtered = append(filtered, ev)
			}
		}
		out = filtered
	}
	return encodeJSON(stdout, map[string]any{
		"runId":      firstRunID(out),
		"eventCount": len(out),
		"events":     out,
	})
}

func firstRunID(events []orchestrator.Event) string {
	if len(events) == 0 {
		return ""
	}
	return events[0].RunID
}
