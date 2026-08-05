package core

// DefaultConfig returns an empty Config with all fields zero. Callers can
// supply their own implementations for StoreProvider, PolicyEngine,
// OutputGateway, or Pipeline; any unset field falls back to the in-memory
// defaults wired by NewRuntime.
func DefaultConfig() Config { return Config{} }

// New constructs a Runtime. With no arguments it uses the default in-memory
// configuration; pass a Config to override individual fields.
func New(configs ...Config) *Runtime {
	cfg := resolveConfig(configs...)
	return NewRuntime(cfg)
}

func resolveConfig(configs ...Config) Config {
	out := DefaultConfig()
	for _, override := range configs {
		if override.StoreProvider != nil {
			out.StoreProvider = override.StoreProvider
		}
		if override.PolicyEngine != nil {
			out.PolicyEngine = override.PolicyEngine
		}
		if override.PolicyEnforcer != nil {
			out.PolicyEnforcer = override.PolicyEnforcer
		}
		if override.OutputGateway != nil {
			out.OutputGateway = override.OutputGateway
		}
		out.Pipeline = mergePipelineConfig(out.Pipeline, override.Pipeline)
	}
	return out
}

func mergePipelineConfig(base, override PipelineComponents) PipelineComponents {
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
