package hydaelyn

import (
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/orchestrator"
	"github.com/Viking602/go-hydaelyn/team"
)

// New constructs a [Runtime] from the given [Config]. It is a thin alias
// for [host.New]; callers that need to customise middleware, plugins,
// or session storage should import [host] directly.
func New(cfg Config) *Runtime { return host.New(cfg) }

// NewOrchestrator constructs the additive Run/Task orchestrator runtime.
// The current implementation is in-memory and is intended for embedding,
// tests, and adapter development while durable drivers converge.
func NewOrchestrator() *OrchestratorRuntime { return orchestrator.NewMemoryRuntime() }

// Public façade types. Each is a Go type alias for the equivalent type
// in a subpackage, so values constructed via either name are
// interchangeable.
type (
	Runtime          = host.Runtime
	Config           = host.Config
	StartTeamRequest = host.StartTeamRequest

	OrchestratorRuntime = orchestrator.Runtime
	Run                 = orchestrator.Run
	Task                = orchestrator.Task
	StartRunCommand     = orchestrator.StartRunCommand
	AdvanceRunCommand   = orchestrator.AdvanceRunCommand
	RunTimelineItem     = orchestrator.RunTimelineItem

	Profile = team.Profile
	Role    = team.Role
)
