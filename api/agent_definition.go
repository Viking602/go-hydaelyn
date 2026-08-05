package api

import (
	"encoding/json"
	"time"
)

// TriggerType discriminates how a Trigger is dispatched at runtime.
// Each value corresponds to a transport driver under transport/*.
type TriggerType string

const (
	// TriggerManual fires from an explicit command/API call. Used by
	// CLI/UI invocations and ad-hoc tests. No transport driver attached.
	TriggerManual TriggerType = "manual"

	// TriggerSchedule fires on a cron expression. Dispatched by
	// transport/cron.
	TriggerSchedule TriggerType = "schedule"

	// TriggerWebhook fires on an inbound HTTP request. Dispatched by
	// transport/webhook.
	TriggerWebhook TriggerType = "webhook"

	// TriggerEvent fires on a system event matched against Filter.
	// Dispatched by transport/event.
	TriggerEvent TriggerType = "event"
)

// ToolMode controls how an agent dispatches a batch of tool calls.
// It is defined in api so persisted definitions do not depend on worker or
// agent implementation packages.
type ToolMode string

const (
	ToolModeSequential ToolMode = "sequential"
	ToolModeParallel   ToolMode = "parallel"
)

// Trigger is the declarative description of how an AgentDefinition is
// invoked. Each trigger type carries a small, type-specific config
// payload in Config — the transport driver that owns the type is
// responsible for parsing it.
//
//   - TriggerSchedule: Config["cron"] = "0 */15 * * * *" (robfig/cron v3 syntax)
//   - TriggerWebhook:  Config["path"] = "/hooks/my-agent", Config["method"]="POST"
//   - TriggerEvent:    Config["topic"] = "incident.created"
//
// Triggers are stored alongside the AgentDefinition; transport drivers
// scan the registry at start to install handlers.
type Trigger struct {
	ID      string            `json:"id"`
	Type    TriggerType       `json:"type"`
	Source  string            `json:"source,omitempty"`
	Config  map[string]string `json:"config,omitempty"`
	Filter  map[string]string `json:"filter,omitempty"`
	Enabled bool              `json:"enabled"`
}

// ModelPolicy declares the provider/model request that an AgentDefinition
// executes. worker.DefinitionDeployment carries these values into agent.Spec
// and provider.Request. A non-zero field that the selected driver cannot honor
// is a deployment error rather than inert registry metadata.
type ModelPolicy struct {
	Provider      string  `json:"provider,omitempty"`
	Model         string  `json:"model"`
	Temperature   float64 `json:"temperature,omitempty"`
	TopP          float64 `json:"topP,omitempty"`
	MaxTokens     int     `json:"maxTokens,omitempty"`
	FallbackModel string  `json:"fallbackModel,omitempty"`
}

// Budget bounds the total spend a single Run is allowed to incur. The
// runtime enforces these against UsageRecords appended during execution.
// Zero or negative values mean "unbounded for that dimension."
type Budget struct {
	MaxCredits     int64         `json:"maxCredits,omitempty"`
	MaxTokens      int64         `json:"maxTokens,omitempty"`
	MaxToolCalls   int           `json:"maxToolCalls,omitempty"`
	MaxRuntime     time.Duration `json:"maxRuntime,omitempty"`
	MaxModelCalls  int           `json:"maxModelCalls,omitempty"`
	MaxActionCalls int           `json:"maxActionCalls,omitempty"`
}

// Quota bounds aggregate spend across many runs in a time window. Used
// by transport-level governance to throttle a noisy AgentDefinition
// before it bankrupts the deployment.
type Quota struct {
	Window           time.Duration `json:"window"`
	MaxRunsPerWindow int           `json:"maxRunsPerWindow,omitempty"`
	MaxCredits       int64         `json:"maxCredits,omitempty"`
}

// GovernancePolicy is the per-AgentDefinition rulebook a deployment
// applies before a Run is dispatched, before a tool is called, and
// before an action with external effects is taken. The runtime resolves
// these against the active api.PolicyEngine — the policy itself is just
// a declarative shape stored on disk and in the registry.
type GovernancePolicy struct {
	Budget Budget `json:"budget,omitempty"`
	Quota  Quota  `json:"quota,omitempty"`

	// AllowedCapabilities lists capability names this agent may invoke.
	// Empty means "no restriction" — pair with DeniedCapabilities for an
	// allow-list-first or deny-list-first deployment as appropriate.
	AllowedCapabilities []string `json:"allowedCapabilities,omitempty"`
	DeniedCapabilities  []string `json:"deniedCapabilities,omitempty"`

	// ApprovalRequiredFor names ToolEffectType values that always
	// require human approval before execution, regardless of the
	// capability's own RequiresApproval flag.
	ApprovalRequiredFor []ToolEffectType `json:"approvalRequiredFor,omitempty"`

	// MaxConcurrentRuns is a transport-level cap on simultaneous runs
	// triggered for this agent. Zero means "no cap."
	MaxConcurrentRuns int `json:"maxConcurrentRuns,omitempty"`

	// PauseOnExcessFailures triggers a circuit-breaker style pause when
	// the trailing failure count exceeds this threshold inside Quota.Window.
	// Zero disables the breaker.
	PauseOnExcessFailures int `json:"pauseOnExcessFailures,omitempty"`
}

// ContextScope identifies the lifetime over which a context source
// remains valid.
type ContextScope string

const (
	// ContextRun: context resets at run boundary.
	ContextRun ContextScope = "run"
	// ContextTask: context resets at task boundary.
	ContextTask ContextScope = "task"
	// ContextAgent: context persists across runs for the same agent.
	ContextAgent ContextScope = "agent"
	// ContextUser: context persists across runs for the same end-user.
	ContextUser ContextScope = "user"
	// ContextGlobal: context shared across the entire deployment.
	ContextGlobal ContextScope = "global"
)

// ContextSource names an external system whose data should be made
// available to the agent at run-start. The interpretation of URI is
// driver-specific: a vector store, a document corpus, a SQL view, a
// memory namespace.
type ContextSource struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	URI      string            `json:"uri,omitempty"`
	Scope    ContextScope      `json:"scope,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// AgentDefinition is the executable, version-controlled deployment contract for
// one agent: what it does, which model and capabilities it may use, how context
// is resolved, how it is triggered, and the governance envelope around it.
//
// AgentProfile remains the runtime write-attribution identity.
// AgentDefinition is published ahead of time and materialized by
// worker.DefinitionDeployment. A deployment MUST reject any configured field it
// cannot execute; it must not silently treat executable fields as display-only
// metadata.
type AgentDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`

	Instructions    string      `json:"instructions,omitempty"`
	Skills          []string    `json:"skills,omitempty"`
	AvailableSkills []string    `json:"availableSkills,omitempty"`
	Model           ModelPolicy `json:"model,omitempty"`

	// Tools is the explicit executable tool allow-list. Capabilities remains
	// capability metadata and is never interpreted as tool selection.
	Tools        []string        `json:"tools,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	Hooks        []string        `json:"hooks,omitempty"`

	// These values are resolved from DefinitionDeployment defaults before this
	// definition is validated, built, and persisted.
	ToolMode      ToolMode      `json:"toolMode,omitempty"`
	MaxIterations int           `json:"maxIterations,omitempty"`
	TTL           time.Duration `json:"ttl,omitempty"`

	Capabilities []string         `json:"capabilities,omitempty"`
	Context      []ContextSource  `json:"context,omitempty"`
	Triggers     []Trigger        `json:"triggers,omitempty"`
	Governance   GovernancePolicy `json:"governance,omitempty"`

	Status            string            `json:"status,omitempty"`
	PreviousVersionID string            `json:"previousVersionId,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

// AgentDefinitionSnapshot is the immutable stored form of one published
// AgentDefinition version. Digest is the lowercase SHA-256 digest of the
// canonical JSON representation of Definition.
type AgentDefinitionSnapshot struct {
	Definition AgentDefinition `json:"definition"`
	Digest     string          `json:"digest"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// AgentDefinitionSnapshotSelector filters AgentDefinitionStore listings.
// All populated fields AND-combine.
type AgentDefinitionSnapshotSelector struct {
	DefinitionIDs []string  `json:"definitionIds,omitempty"`
	Versions      []string  `json:"versions,omitempty"`
	Since         time.Time `json:"since,omitempty"`
	Limit         int       `json:"limit,omitempty"`
}

// AsProfile derives the runtime AgentProfile from a declarative
// AgentDefinition. The ID is preserved; Role defaults to the
// AgentDefinition's Name when none is set so attribution displays
// human-readable names by default.
func (d AgentDefinition) AsProfile() AgentProfile {
	role := d.Metadata["role"]
	if role == "" {
		role = d.Name
	}
	return AgentProfile{
		ID:       d.ID,
		Role:     role,
		Metadata: cloneStringMap(d.Metadata),
	}
}
