// Package hydaelyn is the public façade for the Hydaelyn runner.
//
// Import github.com/Viking602/go-hydaelyn/api for public contracts such as
// Config, commands, interfaces, and value types. The root package owns only
// construction, Runner methods, and sentinel error re-exports.
package hydaelyn

import (
	"github.com/Viking602/go-hydaelyn/api"
	core "github.com/Viking602/go-hydaelyn/internal/core"
	"github.com/Viking602/go-hydaelyn/internal/core/adapter"
)

// New constructs the primary Run/Task runner. With no arguments it uses the
// default in-memory configuration; pass api.Config values to override defaults.
func New(configs ...api.Config) *Runner {
	cfg := resolveConfig(configs...)
	return &Runner{rt: core.NewRuntime(adapter.ConfigToCore(cfg))}
}

// DefaultConfig returns an empty api.Config; useful as a baseline before
// overriding individual fields.
func DefaultConfig() api.Config { return api.DefaultConfig() }

func resolveConfig(configs ...api.Config) api.Config {
	out := DefaultConfig()
	for _, override := range configs {
		if override.StoreProvider != nil {
			out.StoreProvider = override.StoreProvider
		}
		if override.PolicyEngine != nil {
			out.PolicyEngine = override.PolicyEngine
		}
		if override.OutputGateway != nil {
			out.OutputGateway = override.OutputGateway
		}
		out.Pipeline = mergePipelineConfig(out.Pipeline, override.Pipeline)
	}
	return out
}

func mergePipelineConfig(base, override api.PipelineComponents) api.PipelineComponents {
	if override.IntentAnalyzer != nil {
		base.IntentAnalyzer = override.IntentAnalyzer
	}
	if override.Planner != nil {
		base.Planner = override.Planner
	}
	if override.Validator != nil {
		base.Validator = override.Validator
	}
	if override.Router != nil {
		base.Router = override.Router
	}
	if override.Dispatcher != nil {
		base.Dispatcher = override.Dispatcher
	}
	if override.TaskMonitor != nil {
		base.TaskMonitor = override.TaskMonitor
	}
	return base
}
