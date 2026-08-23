// Package venat is the public façade for the Venat runner.
//
// Import github.com/Viking602/venat/api for public contracts such as
// Config, commands, interfaces, and value types. The root package owns only
// construction, Runner methods, and sentinel error re-exports.
package venat

import (
	"fmt"
	"reflect"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
	"github.com/Viking602/venat/internal/core/adapter"
)

// New constructs an in-memory development runner.
//
// The default store is process-local memory: it is not crash-durable and
// does not survive process restart. Do not use New as a production
// persistence backend.
//
// Deprecated: use NewDevelopment for local/test use or NewProduction for a
// runner that rejects missing durable storage and policy dependencies.
func New(configs ...api.Config) *Runner {
	return NewDevelopment(configs...)
}

// NewDevelopment constructs a runner with in-memory storage and allow-all
// policy defaults. It is intended for tests, examples, and local development.
// Like New, the default store is process-local memory and is not
// crash-durable.
func NewDevelopment(configs ...api.Config) *Runner {
	cfg := resolveConfig(configs...)
	return newRunner(cfg, api.RuntimeModeDevelopment)
}

// NewProduction constructs a runner only when the host supplies both durable
// storage and an explicit policy. The framework cannot verify whether a
// StoreProvider is durable, so production ownership remains with the host.
func NewProduction(cfg api.Config) (*Runner, error) {
	if isNilDependency(cfg.StoreProvider) {
		return nil, fmt.Errorf("%w: store provider is required", api.ErrInvalidConfiguration)
	}
	if isNilDependency(cfg.PolicyEngine) {
		return nil, fmt.Errorf("%w: policy engine is required", api.ErrInvalidConfiguration)
	}
	return newRunner(cfg, api.RuntimeModeProduction), nil
}

func isNilDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func newRunner(cfg api.Config, mode api.RuntimeMode) *Runner {
	return &Runner{rt: core.NewRuntime(adapter.ConfigToCore(cfg)), mode: mode}
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
