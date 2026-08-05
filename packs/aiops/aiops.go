// Package aiops is a v0.8.0 skeleton pack for production-monitoring
// style agents: alert triage, log search, runbook execution. As with
// the other shipped packs, this only declares shapes — backend wiring
// (PagerDuty/Datadog/Grafana/your-own) is the host's job.
package aiops

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/packs"
)

// PackName is the registry identifier for this pack.
const PackName = "aiops"

// Pack is the top-level exported value mounted into a packs.Registry.
var Pack = packs.Pack{
	Name:        PackName,
	Version:     "0.8.0",
	Description: "Skeleton: an alert-triage agent that fetches signals and proposes a runbook step.",
	Agents:      []api.AgentDefinition{alertTriage},
	Capabilities: []api.CapabilityManifest{
		{
			Name:        "aiops.core",
			Version:     "0.8.0",
			Description: "Alert/metric/log reads plus runbook execution.",
			Capabilities: []api.Capability{
				{
					Name:        "fetch_alert",
					Description: "Fetch the alert payload by ID from the host monitoring system.",
					EffectType:  api.ToolEffectReadOnly,
					Idempotent:  true,
					Tags:        []string{"aiops", "alert"},
				},
				{
					Name:        "query_metrics",
					Description: "Run a metrics query against the host TSDB.",
					EffectType:  api.ToolEffectReadOnly,
					Idempotent:  true,
					Tags:        []string{"aiops", "metrics"},
				},
				{
					Name:        "search_logs",
					Description: "Search logs over a time window.",
					EffectType:  api.ToolEffectReadOnly,
					Idempotent:  true,
					Tags:        []string{"aiops", "logs"},
				},
				{
					Name:             "execute_runbook_step",
					Description:      "Execute one step from a named runbook against a target resource.",
					EffectType:       api.ToolEffectExternalSideEffect,
					RequiresApproval: true,
					Idempotent:       false,
					Tags:             []string{"aiops", "runbook"},
				},
			},
		},
	},
	Recipes: []packs.Recipe{
		{
			Name:        "alert-triage",
			Description: "Fetch the alert, correlate metrics + logs, and propose the next runbook step for approval.",
			DocumentURL: "docs/recipes/aiops-alert-triage.md",
		},
	},
}

var alertTriage = api.AgentDefinition{
	ID:           "aiops.alert-triage",
	Name:         "Alert Triage",
	Version:      "0.8.0",
	Description:  "Triages a production alert and recommends a runbook step.",
	Instructions: "Read the alert, correlate metrics and logs, propose a runbook step, and request approval.",
	Model:        api.ModelPolicy{Model: "claude-sonnet-4-6", Temperature: 0.2},
	Tools:        []string{"fetch_alert", "query_metrics", "search_logs", "execute_runbook_step"},
	Capabilities: []string{"fetch_alert", "query_metrics", "search_logs", "execute_runbook_step"},
	Triggers: []api.Trigger{
		{ID: "manual", Type: api.TriggerManual, Enabled: true},
		{ID: "alert-fired", Type: api.TriggerEvent, Config: map[string]string{"topic": "alert.fired"}, Enabled: true},
	},
	Governance: api.GovernancePolicy{
		Budget:                api.Budget{MaxToolCalls: 20, MaxModelCalls: 5},
		ApprovalRequiredFor:   []api.ToolEffectType{api.ToolEffectExternalSideEffect},
		PauseOnExcessFailures: 3,
	},
}
