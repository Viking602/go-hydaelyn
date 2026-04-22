package host

import (
	"encoding/json"

	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

func synthesisReportEvents(answer string) []provider.Event {
	payload, err := json.Marshal(map[string]any{
		"report": map[string]any{
			"kind":   string(team.ReportKindSynthesis),
			"answer": answer,
		},
	})
	if err != nil {
		panic(err)
	}
	return []provider.Event{
		{Kind: provider.EventTextDelta, Text: string(payload)},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}
}
