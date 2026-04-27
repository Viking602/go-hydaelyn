// Package flow exposes orchestrator flow presets.
package flow

import "github.com/Viking602/go-hydaelyn/orchestrator"

// Flow is a preset. It must not bypass TaskStore, PolicyEngine,
// TaskExecutionLease, Handoff, ResponseLayer, or OutputGateway.
type Flow = orchestrator.Flow
