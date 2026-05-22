package api

import "context"

// Identified is the minimum identity contract any memory entity must satisfy.
// The framework uses Scope+SubjectID to enforce isolation and ID for selective
// deletion. Every other field on the entity belongs to the application schema.
type Identified interface {
	ID() string
	Scope() ContextScope
	SubjectID() string
}

// Memory is the optional plugin contract for application memory.
//
// T is the application-owned entity (ChatMessage, UserPreference,
// LearnedFact, vector chunk, knowledge-graph node, ...). Storage is entirely
// the application's responsibility — the framework ships no backend, neither
// in-process nor durable.
//
// Applications may also choose not to implement this interface at all and
// instead use their own memory mechanism. The framework's runtime does not
// require a Memory to be configured.
type Memory[T Identified] interface {
	// Write persists a single entry. Implementations MUST honor the entry's
	// Scope() and SubjectID() as the identity boundary; an entry written with
	// (scopeA, subjA) MUST NOT be returned to a read at (scopeB, subjA) or
	// (scopeA, subjB).
	Write(ctx context.Context, entry T) error

	// Read returns entries matching the selector's identity filter. Filtering
	// by application-specific fields (tags, content, time range) is not part
	// of this contract; either over-fetch and filter in caller code, or expose
	// typed query methods on the concrete implementation.
	Read(ctx context.Context, sel MemorySelector) ([]T, error)

	// Forget deletes entries matching the selector and returns the number
	// deleted. Implementations SHOULD be idempotent: forgetting an
	// already-forgotten set returns 0, not an error.
	Forget(ctx context.Context, sel MemorySelector) (int, error)
}

// MemorySelector filters by identity only.
//
// Filtering by application-specific fields (tags, content, time range, layer)
// is intentionally not part of the kernel contract — those are application
// schema. Applications either over-fetch and filter in code, or expose typed
// query methods on the concrete Memory implementation.
type MemorySelector struct {
	// Scope is required; an empty Scope matches no entries.
	Scope ContextScope

	// SubjectID is required; an empty SubjectID matches no entries.
	SubjectID string

	// IDs, if non-empty, restricts the result to entries with one of the
	// given IDs. An empty IDs slice imposes no ID restriction.
	IDs []string

	// Limit caps the number of entries returned by Read. 0 means unbounded.
	// Forget MUST treat Limit as advisory and SHOULD delete all matches.
	Limit int
}
