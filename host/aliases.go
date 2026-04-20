package host

import (
	"github.com/Viking602/go-hydaelyn/internal/auth"
	"github.com/Viking602/go-hydaelyn/internal/compact"
	"github.com/Viking602/go-hydaelyn/internal/middleware"
	"github.com/Viking602/go-hydaelyn/internal/plugin"
	"github.com/Viking602/go-hydaelyn/internal/session"
	"github.com/Viking602/go-hydaelyn/internal/workflow"
)

// Type aliases re-exporting internal implementation details that appear in
// the public [Config] surface or method signatures. These allow external
// callers to name the types they need to provide without importing internal/.

type (
	// Compactor family (from internal/compact).
	Compactor       = compact.Compactor
	SimpleCompactor = compact.SimpleCompactor
	LLMCompactor    = compact.LLMCompactor

	// Auth family (from internal/auth).
	AuthIdentity    = auth.Identity
	AuthCredentials = auth.Credentials
	AuthDriver      = auth.Driver
	StaticAuth      = auth.StaticDriver

	// Middleware family (from internal/middleware).
	MiddlewareHandler  = middleware.Handler
	MiddlewareEnvelope = middleware.Envelope
	MiddlewareNext     = middleware.Next
	MiddlewareFunc     = middleware.Func
	MiddlewareStage    = middleware.Stage

	// Plugin family (from internal/plugin).
	PluginType     = plugin.Type
	PluginSpec     = plugin.Spec
	PluginRef      = plugin.Ref
	PluginRegistry = plugin.Registry

	// Session family (from internal/session).
	Session              = session.Session
	SessionEntry         = session.Entry
	SessionSnapshot      = session.Snapshot
	SessionCreateParams  = session.CreateParams
	SessionStore         = session.Store
	MemorySessionStore   = session.MemoryStore

	// Workflow family (from internal/workflow).
	WorkflowStatus      = workflow.Status
	WorkflowState       = workflow.State
	WorkflowTaskState   = workflow.TaskState
	WorkflowChildRun    = workflow.ChildRunState
	WorkflowRetryPolicy = workflow.RetryPolicy
	WorkflowAbortState  = workflow.AbortState
	WorkflowDriver      = workflow.Driver
	WorkflowRegistry    = workflow.Registry
)

// Plugin type constants re-exported so external callers can match on
// plugin kind without importing internal/.
var (
	PluginTypeProvider   = plugin.TypeProvider
	PluginTypeTool       = plugin.TypeTool
	PluginTypePlanner    = plugin.TypePlanner
	PluginTypeVerifier   = plugin.TypeVerifier
	PluginTypeStorage    = plugin.TypeStorage
	PluginTypeMemory     = plugin.TypeMemory
	PluginTypeObserver   = plugin.TypeObserver
	PluginTypeScheduler  = plugin.TypeScheduler
	PluginTypeMCPGateway = plugin.TypeMCPGateway

	ErrInvalidPluginSpec   = plugin.ErrInvalidSpec
	ErrDuplicatePlugin     = plugin.ErrDuplicate
	ErrSessionNotFound     = session.ErrSessionNotFound
)

// Re-exported constructors for internal types that have no useful external
// alias mechanism (function exports).
func NewSessionStore() *MemorySessionStore { return session.NewMemoryStore() }
func NewPluginRegistry(specs ...PluginSpec) *PluginRegistry {
	return plugin.NewRegistry(specs...)
}
func NewWorkflowRegistry(drivers ...WorkflowDriver) *WorkflowRegistry {
	return workflow.NewRegistry(drivers...)
}
