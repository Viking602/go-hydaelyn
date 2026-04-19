package host

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/auth"
	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/compact"
	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/mcp"
	"github.com/Viking602/go-hydaelyn/middleware"
	"github.com/Viking602/go-hydaelyn/observe"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/workflow"
)

var ErrProviderNotFound = errors.New("provider not found")
var ErrProfileNotFound = errors.New("profile not found")
var ErrPatternNotFound = errors.New("pattern not found")
var ErrInvalidTeamState = errors.New("invalid team state")

const defaultMaxTeamDriveSteps = 24

type Config struct {
	Storage           storage.Driver
	Auth              auth.Driver
	WorkerID          string
	Defaults          map[string]string
	Plugins           []plugin.Spec
	Middlewares       []middleware.Handler
	Compactor         compact.Compactor
	CompactThreshold  int
	MaxTeamDriveSteps int
}

type Runtime struct {
	storage                    storage.Driver
	eventSink                  EventSink
	auth                       auth.Driver
	tools                      *tool.Bus
	workflows                  *workflow.Registry
	hooks                      hook.Chain
	middlewares                middleware.Chain
	capability                 *capability.Invoker
	plugins                    *plugin.Registry
	queue                      scheduler.TaskQueue
	leaseReleaser              LeaseReleaser
	teamGuard                  teamGuard
	providers                  map[string]provider.Driver
	profiles                   map[string]team.Profile
	patterns                   map[string]team.Pattern
	outputGuardrails           map[string]agent.OutputGuardrail
	defaultOutputGuardrails    []agent.OutputGuardrail
	teamGuardrails             map[string]TeamOutputGuardrail
	defaults                   map[string]string
	workerID                   string
	compactor                  compact.Compactor
	compactThreshold           int
	maxTeamDriveSteps          int
	mu                         sync.RWMutex
	runSeq                     uint64
	teamSeq                    uint64
	activeRuns                 map[string]context.CancelFunc
	activeTeams                map[string]context.CancelFunc
	inlineTeamOutputGuardrails map[string][]agent.OutputGuardrail
}

func New(config Config) *Runtime {
	runner, err := NewWithError(config)
	if err != nil {
		panic(err)
	}
	return runner
}

func NewWithError(config Config) (*Runtime, error) {
	driver := config.Storage
	if driver == nil {
		driver = storage.NewMemoryDriver()
	}
	runner := &Runtime{
		storage:                    driver,
		eventSink:                  &runtimeEventSink{store: driver.Events()},
		auth:                       config.Auth,
		tools:                      tool.NewBus(),
		workflows:                  workflow.NewRegistry(),
		middlewares:                middleware.NewChain(config.Middlewares...),
		capability:                 capability.NewInvoker(),
		plugins:                    plugin.NewRegistry(),
		teamGuard:                  &defaultTeamGuard{},
		providers:                  map[string]provider.Driver{},
		profiles:                   map[string]team.Profile{},
		patterns:                   map[string]team.Pattern{},
		outputGuardrails:           map[string]agent.OutputGuardrail{},
		defaultOutputGuardrails:    []agent.OutputGuardrail{},
		teamGuardrails:             map[string]TeamOutputGuardrail{},
		defaults:                   cloneStringMap(config.Defaults),
		workerID:                   config.WorkerID,
		compactor:                  config.Compactor,
		compactThreshold:           config.CompactThreshold,
		maxTeamDriveSteps:          config.MaxTeamDriveSteps,
		activeRuns:                 map[string]context.CancelFunc{},
		activeTeams:                map[string]context.CancelFunc{},
		inlineTeamOutputGuardrails: map[string][]agent.OutputGuardrail{},
	}
	runner.leaseReleaser = &defaultLeaseReleaser{queue: runner.queue}
	if runner.workerID == "" {
		runner.workerID = runner.nextWorkerID()
	}
	for _, spec := range config.Plugins {
		if err := runner.RegisterPlugin(spec); err != nil {
			return nil, err
		}
	}
	return runner, nil
}

func (r *Runtime) RegisterProvider(name string, driver provider.Driver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capability.Register(capability.TypeLLM, name, providerCapabilityHandler(driver))
	r.capability.RegisterStream(capability.TypeLLM, name, providerCapabilityStreamHandler(driver))
	r.providers[name] = capabilityProviderDriver{
		name:     name,
		metadata: driver.Metadata(),
		invoker:  r.capability,
		recorder: r,
	}
	if _, exists := r.plugins.Lookup(plugin.TypeProvider, name); !exists {
		_ = r.plugins.Register(plugin.Spec{Type: plugin.TypeProvider, Name: name, Component: driver})
	}
}

func (r *Runtime) RegisterTool(driver tool.Driver) {
	name := driver.Definition().Name
	r.capability.Register(capability.TypeTool, name, toolCapabilityHandler(driver))
	r.tools.Register(capabilityToolDriver{
		definition: driver.Definition(),
		invoker:    r.capability,
		recorder:   r,
	})
	if _, exists := r.plugins.Lookup(plugin.TypeTool, name); !exists {
		_ = r.plugins.Register(plugin.Spec{Type: plugin.TypeTool, Name: name, Component: driver})
	}
}

func (r *Runtime) RegisterWorkflow(driver workflow.Driver) {
	r.workflows.Register(driver)
}

func (r *Runtime) RegisterHook(handler hook.Handler) {
	r.hooks = r.hooks.Append(handler)
}

func (r *Runtime) RegisterCompactor(compactor compact.Compactor) {
	r.compactor = compactor
}

func (r *Runtime) RegisterPlugin(spec plugin.Spec) error {
	if err := r.plugins.Register(spec); err != nil {
		return err
	}
	return r.applyPlugin(spec)
}

func (r *Runtime) Plugins() *plugin.Registry {
	return r.plugins
}

// Deprecated: use UseStageMiddleware instead.
func (r *Runtime) UseMiddleware(handler middleware.Handler) {
	r.middlewares = r.middlewares.Append(handler)
}

func (r *Runtime) UseStageMiddleware(handler middleware.Handler) {
	r.UseMiddleware(handler)
}

// Deprecated: use UseCapabilityPolicy instead.
func (r *Runtime) UseCapabilityMiddleware(handler capability.Middleware) {
	r.capability.Use(handler)
}

func (r *Runtime) UseCapabilityPolicy(policy capability.Policy) {
	r.capability.UsePolicy(policy)
}

func (r *Runtime) RegisterCapability(callType capability.Type, name string, handler capability.Handler) {
	r.capability.Register(callType, name, handler)
}

func (r *Runtime) UseOutputGuardrail(guardrail agent.OutputGuardrail) {
	if guardrail == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultOutputGuardrails = append(r.defaultOutputGuardrails, guardrail)
}

func (r *Runtime) RegisterOutputGuardrail(name string, guardrail agent.OutputGuardrail) {
	if guardrail == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputGuardrails[name] = guardrail
}

func (r *Runtime) RegisterTeamOutputGuardrail(name string, guardrail TeamOutputGuardrail) {
	if guardrail == nil || name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.teamGuardrails[name] = guardrail
}

func (r *Runtime) InvokeCapability(ctx context.Context, call capability.Call) (capability.Result, error) {
	return r.capability.Invoke(capability.WithPolicyOutcomeRecorder(ctx, r), call)
}

func (r *Runtime) UseObserver(observer observe.Observer) {
	if observer == nil {
		return
	}
	r.UseStageMiddleware(observe.RuntimeMiddleware(observer))
	r.UseCapabilityPolicy(observe.CapabilityMiddleware(observer))
}

func (r *Runtime) RegisterProfile(profile team.Profile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles[profile.Name] = profile
}

func (r *Runtime) RegisterPattern(pattern team.Pattern) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.patterns[pattern.Name()] = pattern
}

func (r *Runtime) CreateSession(ctx context.Context, params session.CreateParams) (session.Session, error) {
	return r.createSession(ctx, params)
}

func (r *Runtime) GetSession(ctx context.Context, sessionID string) (session.Snapshot, error) {
	return r.loadSession(ctx, sessionID)
}

func (r *Runtime) lookupProvider(name string) (provider.Driver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	driver, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	return driver, nil
}

func (r *Runtime) lookupProfile(name string) (team.Profile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	profile, ok := r.profiles[name]
	if !ok {
		return team.Profile{}, fmt.Errorf("%w: %s", ErrProfileNotFound, name)
	}
	return profile, nil
}

func (r *Runtime) lookupPattern(name string) (team.Pattern, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pattern, ok := r.patterns[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPatternNotFound, name)
	}
	return pattern, nil
}

func (r *Runtime) validateTeamState(state team.RunState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTeamState, err)
	}
	agents := append([]team.AgentInstance{state.Supervisor}, state.Workers...)
	for _, agentInstance := range agents {
		if _, err := r.lookupProfile(agentInstance.EffectiveProfileName()); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) applyPlugin(spec plugin.Spec) error {
	switch spec.Type {
	case plugin.TypeProvider:
		driver, ok := spec.Component.(provider.Driver)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement provider.Driver", spec.Type, spec.Name)
		}
		r.RegisterProvider(spec.Name, driver)
	case plugin.TypeTool:
		driver, ok := spec.Component.(tool.Driver)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement tool.Driver", spec.Type, spec.Name)
		}
		r.RegisterTool(driver)
	case plugin.TypeStorage:
		driver, ok := spec.Component.(storage.Driver)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement storage.Driver", spec.Type, spec.Name)
		}
		r.storage = driver
	case plugin.TypeObserver:
		if observer, ok := spec.Component.(observe.Observer); ok {
			r.UseObserver(observer)
			return nil
		}
		handler, ok := spec.Component.(hook.Handler)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement observe.Observer or hook.Handler", spec.Type, spec.Name)
		}
		r.RegisterHook(handler)
	case plugin.TypeScheduler:
		queue, ok := spec.Component.(scheduler.TaskQueue)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement scheduler.TaskQueue", spec.Type, spec.Name)
		}
		r.queue = queue
		r.leaseReleaser = &defaultLeaseReleaser{queue: queue}
	case plugin.TypeMCPGateway:
		gateway, ok := spec.Component.(mcp.Gateway)
		if !ok {
			return fmt.Errorf("plugin %s/%s does not implement mcp.Gateway", spec.Type, spec.Name)
		}
		drivers, err := gateway.ImportTools(context.Background())
		if err != nil {
			return err
		}
		for _, driver := range drivers {
			r.RegisterTool(driver)
		}
	case plugin.TypePlanner, plugin.TypeVerifier, plugin.TypeMemory:
		return nil
	default:
		return fmt.Errorf("unsupported plugin type: %s", spec.Type)
	}
	return nil
}

func (r *Runtime) engineHooks() hook.Chain {
	if r.middlewares.Len() == 0 {
		return r.hooks
	}
	return r.hooks.Prepend(r.middlewares.HookAdapter())
}

func mergeStringMap(target map[string]string, source map[string]string) {
	maps.Copy(target, source)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	maps.Copy(out, values)
	return out
}

func (r *Runtime) nextRunID() string {
	return fmt.Sprintf("run-%d", atomic.AddUint64(&r.runSeq, 1))
}

func (r *Runtime) nextTeamID() string {
	return fmt.Sprintf("team-%d", atomic.AddUint64(&r.teamSeq, 1))
}

func (r *Runtime) nextWorkerID() string {
	return fmt.Sprintf("runtime-worker-%d", atomic.AddUint64(&r.teamSeq, 1))
}
