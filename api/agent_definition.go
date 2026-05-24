package api

import "time"

// TriggerType discriminates how a Trigger is dispatched at runtime.
// Each value corresponds to a transport driver under transport/*.
type TriggerType string

const (
	// TriggerManual fires from an explicit command/API call. Used by
	// CLI/UI invocations and ad-hoc tests. No transport driver attached.
	TriggerManual TriggerType = "manual"

	// TriggerSchedule fires on a cron expression. Dispatched by
	// transport/scheduler.
	TriggerSchedule TriggerType = "schedule"

	// TriggerWebhook fires on an inbound HTTP request. Dispatched by
	// transport/webhook.
	TriggerWebhook TriggerType = "webhook"

	// TriggerEvent fires on a system event matched against Filter.
	// Dispatched by transport/event.
	TriggerEvent TriggerType = "event"
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

// ModelPolicy declares which model an AgentDefinition prefers and what
// constraints surround that choice. The fields are intentionally narrow:
// model selection in v0.8.0 is delegated to the agent.Engine, with the
// declarative spec carried here so the registry can show humans what an
// agent will actually call.
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

// AgentDefinition is the declarative, version-controlled description of
// an agent: what it does, what model it prefers, what capabilities it
// may call, how it is triggered, and the governance envelope around it.
//
// Unlike AgentProfile (runtime identity used to attribute writes during
// a run), AgentDefinition is the *config* an operator publishes ahead of
// time. The registry stores AgentDefinitions; transport drivers and the
// runner consume them.
//
// Spec anchor: docs/product-spec/v0.8.0/03-agent-ontology.md §"AgentDefinition".
type AgentDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`

	Instructions string      `json:"instructions,omitempty"`
	Model        ModelPolicy `json:"model,omitempty"`

	Capabilities []string         `json:"capabilities,omitempty"`
	Context      []ContextSource  `json:"context,omitempty"`
	Triggers     []Trigger        `json:"triggers,omitempty"`
	Governance   GovernancePolicy `json:"governance,omitempty"`

	Status            string `json:"status,omitempty"`
	PreviousVersionID string `json:"previousVersionId,omitempty"`

	Metadata map[string]string `json:"metadata,omitempty"`
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
