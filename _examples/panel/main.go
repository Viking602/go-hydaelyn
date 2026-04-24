package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/pattern/panel"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type panelProvider struct{}

func (panelProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "panel-demo"}
}

func (panelProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	taskID := request.Metadata["taskId"]
	switch request.Metadata["taskKind"] {
	case string(team.TaskKindResearch):
		return provider.NewSliceStream(reportEvents(map[string]any{
			"report": map[string]any{
				"kind": string(team.ReportKindResearch),
				"claims": []map[string]any{{
					"id":          "claim-" + taskID,
					"summary":     taskID + " is ready for cross-review",
					"evidenceIds": []string{"evidence-" + taskID},
					"confidence":  0.82,
				}},
				"evidence": []map[string]any{{
					"id":      "evidence-" + taskID,
					"source":  "demo",
					"snippet": taskID + " evidence",
					"score":   0.82,
				}},
				"findings": []map[string]any{{
					"id":         "finding-" + taskID,
					"summary":    taskID + " finding",
					"claimIds":   []string{"claim-" + taskID},
					"confidence": 0.82,
				}},
			},
		})), nil
	case string(team.TaskKindVerify):
		reviewedTaskID := strings.TrimSuffix(taskID, "-review")
		if reviewedTaskID == taskID {
			reviewedTaskID = strings.TrimSuffix(taskID, "-review-2")
		}
		claimID := fmt.Sprintf("claim-%s-claim-%s", reviewedTaskID, reviewedTaskID)
		return provider.NewSliceStream(reportEvents(map[string]any{
			"report": map[string]any{
				"kind":       string(team.ReportKindVerification),
				"status":     string(team.VerificationStatusSupported),
				"confidence": 0.9,
				"reason":     "demo verifier supports the reviewed claims",
				"perClaim": []map[string]any{{
					"claimId":     claimID,
					"status":      string(team.VerificationStatusSupported),
					"confidence":  0.9,
					"evidenceIds": []string{fmt.Sprintf("evidence-%s-evidence-%s", reviewedTaskID, reviewedTaskID)},
					"reason":      "demo verifier supports the reviewed claim",
				}},
			},
		})), nil
	default:
		return provider.NewSliceStream(reportEvents(map[string]any{
			"report": map[string]any{
				"kind":   string(team.ReportKindSynthesis),
				"answer": "Panel completed: experts claimed todos, cross-reviewed claims, and synthesized verified findings.",
			},
		})), nil
	}
}

func reportEvents(payload map[string]any) []provider.Event {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return []provider.Event{
		{Kind: provider.EventTextDelta, Text: string(raw)},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}
}

func main() {
	ctx := context.Background()
	runner := host.New(host.Config{})
	runner.RegisterProvider("panel-demo", panelProvider{})
	runner.RegisterPattern(panel.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "panel-demo", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "security", Role: team.RoleResearcher, Provider: "panel-demo", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "frontend", Role: team.RoleResearcher, Provider: "panel-demo", Model: "test"})

	state, err := runner.StartTeam(ctx, host.StartTeamRequest{
		Pattern:           "panel",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"security", "frontend"},
		Input: map[string]any{
			"query":               "launch auth feature",
			"requireVerification": true,
			"experts": []any{
				map[string]any{"profile": "security", "domains": []any{"security"}, "capabilities": []any{"threat_model"}},
				map[string]any{"profile": "frontend", "domains": []any{"frontend"}, "capabilities": []any{"browser"}},
			},
			"todos": []any{
				map[string]any{"id": "security-review", "title": "review auth threat model", "domain": "security", "requiredCapabilities": []any{"threat_model"}, "priority": "high"},
				map[string]any{"id": "ui-review", "title": "review login UI", "domain": "frontend", "requiredCapabilities": []any{"browser"}},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(state.Result.Summary)

	timeline, err := runner.TeamTimeline(ctx, state.ID)
	if err != nil {
		panic(err)
	}
	for _, item := range timeline {
		fmt.Printf("[%s] %s\n", item.Kind, strings.TrimSpace(item.Text))
	}
}
