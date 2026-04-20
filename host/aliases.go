package host

import (
	"github.com/Viking602/go-hydaelyn/internal/auth"
	"github.com/Viking602/go-hydaelyn/internal/compact"
	"github.com/Viking602/go-hydaelyn/internal/middleware"
)

// Type aliases re-exporting internal implementation details that appear in
// the public [Config] surface. These allow external callers to name the
// types they need to provide without importing internal/.

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
)
