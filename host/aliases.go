package host

import (
	"github.com/Viking602/go-hydaelyn/internal/compact"
)

// Type aliases re-exporting internal implementation details that appear in
// the public [Config] surface. These allow external callers to name the
// types they need to provide (Compactor, etc.) without importing internal/.

type (
	Compactor       = compact.Compactor
	SimpleCompactor = compact.SimpleCompactor
	LLMCompactor    = compact.LLMCompactor
)
