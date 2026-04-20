package hydaelyn

import (
	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/team"
)

// New constructs a [Runtime] from the given [Config]. It is a thin alias
// for [host.New]; callers that need to customise middleware, plugins,
// or session storage should import [host] directly.
func New(cfg Config) *Runtime { return host.New(cfg) }

// Public façade types. Each is a Go type alias for the equivalent type
// in a subpackage, so values constructed via either name are
// interchangeable.
type (
	Runtime          = host.Runtime
	Config           = host.Config
	StartTeamRequest = host.StartTeamRequest

	Profile = team.Profile
	Role    = team.Role
)
