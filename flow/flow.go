// Package flow exposes orchestrator flow presets.
//
// Deprecated: import github.com/Viking602/venat/api and use api.Flow.
// This alias package will be removed in a later minor (ADR-027).
package flow

import "github.com/Viking602/venat/api"

// Flow is a preset. It must not bypass TaskStore, PolicyEngine,
// TaskExecutionLease, Handoff, ResponseLayer, or OutputGateway.
//
// Deprecated: use api.Flow.
type Flow = api.Flow
