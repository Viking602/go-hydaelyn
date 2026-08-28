package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/hook"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/transport/trigger"
)

var (
	// ErrDefinitionInvalid reports a malformed executable definition.
	ErrDefinitionInvalid = errors.New("worker: invalid agent definition")
	// ErrDefinitionUnsupported reports configured behavior this deployment
	// cannot execute rather than silently ignoring it.
	ErrDefinitionUnsupported = errors.New("worker: unsupported agent definition field")
)

// DefinitionDeployment materializes and installs immutable AgentDefinitions.
// Runner records the definition snapshot and runtime identity; BuildDeps binds
// live model, tool, skill, hook, and context implementations.
type DefinitionDeployment struct {
	Runner    *venat.Runner
	BuildDeps agent.BuildDeps
	// HookRegistry resolves the persisted definition Hooks names. The registry
	// is host-owned; only the ordered names are persisted in the definition.
	HookRegistry   map[string]hook.Handler
	Registrars     trigger.Registrars
	TriggerHandler trigger.Handler
	Admission      AdmissionController
	ToolMode       tool.Mode
	MaxIterations  int
	TTL            time.Duration
	UsagePricer    UsagePricer
	Now            func() time.Time
}

// DeployedDefinition is one live materialization. Close only tears down its
// transient trigger registrations; the immutable snapshot remains durable.
type DeployedDefinition struct {
	Definition api.AgentDefinition
	Snapshot   api.AgentDefinitionSnapshot
	Spec       agent.Spec
	Worker     AgentWorker
	Admission  AdmissionController

	restoreWorker func(context.Context, string, string) (AgentWorker, error)
	triggers      *trigger.Lifecycle
}

// Deploy validates every configured execution field, builds the agent engine,
// persists the immutable revision, publishes its runtime identity, and finally
// installs enabled transport triggers.
func (d DefinitionDeployment) Deploy(ctx context.Context, definition api.AgentDefinition) (*DeployedDefinition, error) {
	if d.Runner == nil {
		return nil, ErrRunnerMissing
	}
	effective, err := d.effectiveDefinition(definition)
	if err != nil {
		return nil, err
	}
	if err := validateDefinition(effective); err != nil {
		return nil, err
	}
	profile := effective.AsProfile()
	if err := profile.Validate(); err != nil {
		return nil, fmt.Errorf("worker: validate definition %q agent profile: %w", effective.ID, err)
	}
	capabilities, err := d.Runner.StoreCapabilities(ctx)
	if err != nil {
		return nil, fmt.Errorf("worker: inspect definition storage capability: %w", err)
	}
	if !capabilities.SupportsDefinitionSnapshots {
		return nil, fmt.Errorf("%w: definition snapshot storage", ErrDefinitionUnsupported)
	}
	if RequiresAdmission(effective.Governance) {
		if d.Admission == nil {
			return nil, ErrAdmissionControllerMissing
		}
		if !capabilities.SupportsAdmissionReservations {
			return nil, fmt.Errorf("%w: admission reservation storage", ErrDefinitionUnsupported)
		}
	}

	spec, worker, err := d.materialize(effective)
	if err != nil {
		return nil, err
	}
	snapshot, err := snapshotDefinition(effective, d.now())
	if err != nil {
		return nil, err
	}
	if err := d.Runner.SaveAgentDefinitionSnapshot(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("worker: save definition %q version %q: %w", effective.ID, effective.Version, err)
	}
	if err := d.Runner.RegisterAgent(profile); err != nil {
		return nil, fmt.Errorf("worker: register definition %q agent profile: %w", effective.ID, err)
	}

	registrations, err := d.Registrars.Register(effective, d.TriggerHandler)
	if err != nil {
		return nil, fmt.Errorf("worker: install definition %q triggers: %w", effective.ID, err)
	}
	return &DeployedDefinition{
		Definition:    effective,
		Snapshot:      snapshot,
		Spec:          spec,
		Worker:        worker,
		Admission:     d.Admission,
		restoreWorker: d.restoreWorker,
		triggers:      registrations,
	}, nil
}

// TriggerRegistrations returns the currently installed trigger snapshot.
func (d *DeployedDefinition) TriggerRegistrations() []trigger.Registration {
	if d == nil || d.triggers == nil {
		return nil
	}
	return d.triggers.Registrations()
}

// Close removes this deployment's transient trigger registrations.
func (d *DeployedDefinition) Close() error {
	if d == nil || d.triggers == nil {
		return nil
	}
	return d.triggers.Close()
}

func (d DefinitionDeployment) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

func (d DefinitionDeployment) effectiveDefinition(definition api.AgentDefinition) (api.AgentDefinition, error) {
	effective, err := cloneDefinition(definition)
	if err != nil {
		return api.AgentDefinition{}, err
	}
	if effective.ToolMode == "" {
		mode := d.ToolMode
		if mode == "" {
			mode = tool.ModeSequential
		}
		effective.ToolMode = api.ToolMode(mode)
	}
	if effective.MaxIterations == 0 {
		effective.MaxIterations = d.MaxIterations
	}
	if effective.TTL == 0 {
		effective.TTL = d.TTL
	}
	if _, err := d.buildDepsFor(effective); err != nil {
		return api.AgentDefinition{}, err
	}
	return effective, nil
}

func (d DefinitionDeployment) buildDepsFor(definition api.AgentDefinition) (agent.BuildDeps, error) {
	deps := d.BuildDeps
	if deps.Hooks.Len() > 0 {
		return agent.BuildDeps{}, fmt.Errorf("%w: unversioned build deps hooks", ErrDefinitionUnsupported)
	}
	if len(definition.Hooks) == 0 {
		return deps, nil
	}
	handlers := make([]hook.Handler, 0, len(definition.Hooks))
	for _, name := range definition.Hooks {
		handler, ok := d.HookRegistry[name]
		if !ok || handler == nil {
			return agent.BuildDeps{}, fmt.Errorf("%w: hook %q is not registered", ErrDefinitionInvalid, name)
		}
		handlers = append(handlers, handler)
	}
	deps.Hooks = hook.NewChain(handlers...)
	return deps, nil
}

func (d DefinitionDeployment) materialize(definition api.AgentDefinition) (agent.Spec, AgentWorker, error) {
	if err := validateDefinition(definition); err != nil {
		return agent.Spec{}, AgentWorker{}, err
	}
	deps, err := d.buildDepsFor(definition)
	if err != nil {
		return agent.Spec{}, AgentWorker{}, err
	}
	spec := specFromDefinition(definition)
	engine, err := agent.Build(spec, deps)
	if err != nil {
		return agent.Spec{}, AgentWorker{}, fmt.Errorf("worker: build definition %q: %w", definition.ID, err)
	}
	engine.ToolMode = tool.Mode(definition.ToolMode)
	engine.LoopPolicy.MaxIterations = definition.MaxIterations
	return spec, AgentWorker{
		Runner:        d.Runner,
		Engine:        engine,
		AgentID:       definition.ID,
		Model:         definition.Model.Model,
		ToolMode:      tool.Mode(definition.ToolMode),
		MaxIterations: definition.MaxIterations,
		TTL:           definition.TTL,
		UsagePricer:   d.UsagePricer,
	}, nil
}

func (d DefinitionDeployment) restoreWorker(ctx context.Context, definitionID, version string) (AgentWorker, error) {
	snapshot, err := d.Runner.LoadAgentDefinitionSnapshot(ctx, definitionID, version)
	if err != nil {
		return AgentWorker{}, fmt.Errorf("worker: load definition %q version %q: %w", definitionID, version, err)
	}
	expected, err := snapshotDefinition(snapshot.Definition, snapshot.CreatedAt)
	if err != nil {
		return AgentWorker{}, err
	}
	if expected.Digest != snapshot.Digest {
		return AgentWorker{}, fmt.Errorf("%w: definition %q version %q digest mismatch", ErrDefinitionInvalid, definitionID, version)
	}
	_, worker, err := d.materialize(snapshot.Definition)
	if err != nil {
		return AgentWorker{}, fmt.Errorf("worker: restore definition %q version %q: %w", definitionID, version, err)
	}
	return worker, nil
}

func specFromDefinition(definition api.AgentDefinition) agent.Spec {
	return agent.Spec{
		Instructions:    definition.Instructions,
		Skills:          append([]string(nil), definition.Skills...),
		AvailableSkills: append([]string(nil), definition.AvailableSkills...),
		Provider:        definition.Model.Provider,
		Model:           definition.Model.Model,
		FallbackModel:   definition.Model.FallbackModel,
		Temperature:     definition.Model.Temperature,
		TopP:            definition.Model.TopP,
		MaxTokens:       definition.Model.MaxTokens,
		Tools:           append([]string(nil), definition.Tools...),
		InputSchema:     append(json.RawMessage(nil), definition.InputSchema...),
		OutputSchema:    append(json.RawMessage(nil), definition.OutputSchema...),
		LoopPolicy:      agent.LoopPolicy{MaxIterations: definition.MaxIterations},
	}
}

func cloneDefinition(definition api.AgentDefinition) (api.AgentDefinition, error) {
	encoded, err := json.Marshal(definition)
	if err != nil {
		return api.AgentDefinition{}, fmt.Errorf("%w: encode definition %q: %w", ErrDefinitionInvalid, definition.ID, err)
	}
	var cloned api.AgentDefinition
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return api.AgentDefinition{}, fmt.Errorf("%w: clone definition %q: %w", ErrDefinitionInvalid, definition.ID, err)
	}
	return cloned, nil
}

func snapshotDefinition(definition api.AgentDefinition, createdAt time.Time) (api.AgentDefinitionSnapshot, error) {
	cloned, err := cloneDefinition(definition)
	if err != nil {
		return api.AgentDefinitionSnapshot{}, err
	}
	encoded, err := json.Marshal(cloned)
	if err != nil {
		return api.AgentDefinitionSnapshot{}, fmt.Errorf("%w: encode definition %q: %w", ErrDefinitionInvalid, cloned.ID, err)
	}
	digest := sha256.Sum256(encoded)
	return api.AgentDefinitionSnapshot{
		Definition: cloned,
		Digest:     hex.EncodeToString(digest[:]),
		CreatedAt:  createdAt,
	}, nil
}

func validateDefinition(definition api.AgentDefinition) error {
	if definition.ID == "" {
		return fmt.Errorf("%w: id is required", ErrDefinitionInvalid)
	}
	if definition.Name == "" {
		return fmt.Errorf("%w: name is required", ErrDefinitionInvalid)
	}
	if definition.Version == "" {
		return fmt.Errorf("%w: version is required", ErrDefinitionInvalid)
	}
	if definition.Model.Model == "" {
		return fmt.Errorf("%w: model.model is required", ErrDefinitionInvalid)
	}
	if definition.Model.MaxTokens < 0 {
		return fmt.Errorf("%w: model.maxTokens cannot be negative", ErrDefinitionInvalid)
	}
	if definition.Status != "" && definition.Status != "active" {
		return fmt.Errorf("%w: status %q is not deployable", ErrDefinitionInvalid, definition.Status)
	}
	if len(definition.Context) > 0 {
		return fmt.Errorf("%w: context", ErrDefinitionUnsupported)
	}
	if err := validateDefinitionGovernance(definition.Governance); err != nil {
		return err
	}
	if err := validateNames("skill", definition.Skills); err != nil {
		return err
	}
	if err := validateNames("available skill", definition.AvailableSkills); err != nil {
		return err
	}
	if err := validateNames("capability", definition.Capabilities); err != nil {
		return err
	}
	if err := validateDefinitionTriggers(definition.Triggers); err != nil {
		return err
	}
	if definition.ToolMode != "" && definition.ToolMode != api.ToolModeSequential && definition.ToolMode != api.ToolModeParallel {
		return fmt.Errorf("%w: toolMode %q", ErrDefinitionUnsupported, definition.ToolMode)
	}
	if definition.MaxIterations < 0 {
		return fmt.Errorf("%w: maxIterations cannot be negative", ErrDefinitionInvalid)
	}
	if definition.TTL < 0 {
		return fmt.Errorf("%w: ttl cannot be negative", ErrDefinitionInvalid)
	}
	if err := validateNames("tool", definition.Tools); err != nil {
		return err
	}
	return validateNames("hook", definition.Hooks)
}

func validateDefinitionTriggers(triggers []api.Trigger) error {
	seen := make(map[string]struct{}, len(triggers))
	for _, configured := range triggers {
		if configured.ID == "" {
			return fmt.Errorf("%w: trigger id is required", ErrDefinitionInvalid)
		}
		if _, exists := seen[configured.ID]; exists {
			return fmt.Errorf("%w: duplicate trigger %q", ErrDefinitionInvalid, configured.ID)
		}
		seen[configured.ID] = struct{}{}
		switch configured.Type {
		case api.TriggerManual, api.TriggerSchedule, api.TriggerWebhook, api.TriggerEvent:
		default:
			return fmt.Errorf("%w: trigger type %q", ErrDefinitionUnsupported, configured.Type)
		}
	}
	return nil
}

func validateNames(kind string, names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			return fmt.Errorf("%w: %s name is required", ErrDefinitionInvalid, kind)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: duplicate %s %q", ErrDefinitionInvalid, kind, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validateDefinitionGovernance(policy api.GovernancePolicy) error {
	if len(policy.AllowedCapabilities) > 0 {
		return fmt.Errorf("%w: governance.allowedCapabilities", ErrDefinitionUnsupported)
	}
	if len(policy.DeniedCapabilities) > 0 {
		return fmt.Errorf("%w: governance.deniedCapabilities", ErrDefinitionUnsupported)
	}
	if len(policy.ApprovalRequiredFor) > 0 {
		return fmt.Errorf("%w: governance.approvalRequiredFor", ErrDefinitionUnsupported)
	}
	if policy.Budget.MaxCredits > 0 {
		return fmt.Errorf("%w: governance.budget.maxCredits", ErrDefinitionUnsupported)
	}
	if policy.Budget.MaxActionCalls > 0 {
		return fmt.Errorf("%w: governance.budget.maxActionCalls", ErrDefinitionUnsupported)
	}
	if policy.Quota.Window < 0 || policy.Quota.MaxRunsPerWindow < 0 || policy.Quota.MaxCredits < 0 ||
		policy.MaxConcurrentRuns < 0 || policy.PauseOnExcessFailures < 0 {
		return fmt.Errorf("%w: governance limits cannot be negative", ErrDefinitionInvalid)
	}
	usesWindow := policy.Quota.MaxRunsPerWindow > 0 || policy.Quota.MaxCredits > 0 || policy.PauseOnExcessFailures > 0
	if usesWindow && policy.Quota.Window <= 0 {
		return fmt.Errorf("%w: governance quota window is required", ErrDefinitionInvalid)
	}
	if policy.Quota.MaxCredits > 0 && policy.Budget.MaxCredits <= 0 {
		return fmt.Errorf("%w: governance quota.maxCredits requires budget.maxCredits", ErrDefinitionInvalid)
	}
	return nil
}
