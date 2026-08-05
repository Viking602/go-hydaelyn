package provider

import (
	"errors"
	"fmt"
	"sync"
)

// Resolver maps a model name to the Driver that serves it. agent.Build calls
// Resolver.Driver(spec.Model) once per materialized agent, so each agent can
// run on a different model — and, when drivers from different vendors are
// registered, a different provider — while sharing a single Build path.
//
// A Driver already takes the model name per request (Request.Model), so a
// single Driver serves many model names on its own. The Resolver only adds the
// cross-vendor dimension: picking which Driver a given model name belongs to.
//
// Spec anchor: docs/adr/ADR-018-self-sufficient-agent-layer.md
// §"Per-agent model and provider selection".
type Resolver interface {
	Driver(model string) (Driver, error)
}

// NamedResolver resolves an explicit provider/model pair. Resolver
// implementations may expose it when model names are not globally unique.
type NamedResolver interface {
	Resolver
	DriverFor(providerName, model string) (Driver, error)
}

// Resolve chooses a driver by model and, when providerName is non-empty,
// verifies or explicitly resolves the requested provider.
func Resolve(resolver Resolver, providerName, model string) (Driver, error) {
	if resolver == nil {
		return nil, fmt.Errorf("%w: nil resolver", ErrNoDriverForModel)
	}
	if providerName == "" {
		return resolver.Driver(model)
	}
	if named, ok := resolver.(NamedResolver); ok {
		return named.DriverFor(providerName, model)
	}
	driver, err := resolver.Driver(model)
	if err != nil {
		return nil, err
	}
	if driver.Metadata().Name != providerName {
		return nil, fmt.Errorf("%w: provider %q model %q", ErrNoDriverForModel, providerName, model)
	}
	return driver, nil
}

// ErrNoDriverForModel is returned by a Resolver that has no Driver registered
// for the requested model name. agent.Build wraps it into a build error so an
// unservable model fails at construction rather than at the first model call.
var ErrNoDriverForModel = errors.New("provider: no driver registered for model")

// Single returns a Resolver that always yields d, ignoring the model name. It
// is the trivial single-provider case: every agent shares one Driver and only
// the model name (Request.Model) varies per call. A deployment that never needs
// cross-vendor routing passes Single(driver) wherever a Resolver is required.
func Single(d Driver) Resolver {
	return singleResolver{driver: d}
}

type singleResolver struct {
	driver Driver
}

func (s singleResolver) Driver(string) (Driver, error) {
	if s.driver == nil {
		return nil, fmt.Errorf("%w: single resolver holds a nil driver", ErrNoDriverForModel)
	}
	return s.driver, nil
}

// Registry resolves a model name to a registered Driver by indexing each
// Driver's Metadata().Models. Register every Driver a deployment can route to,
// then hand the Registry to agent.Build as the Resolver; the Build for an agent
// whose Model is served by driver X selects X, and an agent on a model served
// by driver Y selects Y. When two drivers declare the same model name, the
// last registration wins.
//
// Registry is safe for concurrent Driver lookups; concurrent Register calls are
// serialized. The expected pattern is to register all drivers at startup and
// then only resolve.
type Registry struct {
	mu              sync.RWMutex
	byModel         map[string]Driver
	byProviderModel map[string]Driver
}

// NewRegistry builds a Registry pre-populated with the given drivers, each
// indexed by the model names it declares in Metadata().Models.
func NewRegistry(drivers ...Driver) *Registry {
	r := &Registry{
		byModel:         make(map[string]Driver),
		byProviderModel: make(map[string]Driver),
	}
	for _, d := range drivers {
		r.Register(d)
	}
	return r
}

// Register indexes d under every model name in d.Metadata().Models. A nil
// driver is ignored, and a driver that declares no models is indexed under no
// key, so it matches no lookup. The byModel map is allocated lazily, so a
// zero-value Registry (&Registry{} or var r Registry) is safe to Register into
// without NewRegistry.
func (r *Registry) Register(d Driver) {
	if d == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byModel == nil {
		r.byModel = make(map[string]Driver)
	}
	if r.byProviderModel == nil {
		r.byProviderModel = make(map[string]Driver)
	}
	name := d.Metadata().Name
	for _, model := range d.Metadata().Models {
		r.byModel[model] = d
		if name != "" {
			r.byProviderModel[providerModelKey(name, model)] = d
		}
	}
}

// Driver returns the Driver registered for model, or ErrNoDriverForModel when
// no registered Driver declares it.
func (r *Registry) Driver(model string) (Driver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if d, ok := r.byModel[model]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrNoDriverForModel, model)
}

// DriverFor returns the driver registered for the exact provider/model pair.
func (r *Registry) DriverFor(providerName, model string) (Driver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if d, ok := r.byProviderModel[providerModelKey(providerName, model)]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("%w: provider %q model %q", ErrNoDriverForModel, providerName, model)
}

func providerModelKey(providerName, model string) string {
	return providerName + "\x00" + model
}
