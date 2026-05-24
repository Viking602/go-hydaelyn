// Package memory is the optional-plugin memory surface for v0.8.0+.
// The framework ships the interface only; backends (vector DB / KV /
// file / in-memory) live in user code per ADR-013.
//
// Spec anchor: docs/product-spec/v0.8.0/15-memory-optional-plugin.md.
//
// The pre-existing api.Memory[T] interface (api/memory.go) remains
// untouched for backward compatibility — it is a Write/Read/Forget
// shape bound to api.ContextScope identity. The memory package's
// Memory[T] is the v0.8.0+ canonical surface targeted by the design
// docs and the Phase 4 recipes (recipe/memory-pyramid,
// recipe/memory-retrieval); applications choose either surface.
package memory

import "context"

// Identified is the constraint Memory entities must satisfy: each item
// must expose a stable ID for Get / Delete.
type Identified interface {
	ID() string
}

// Memory[T] is the verb surface for storing and retrieving items of
// type T. The framework owns the interface; backends live in user
// code per ADR-013 (Memory as optional plugin).
type Memory[T Identified] interface {
	Put(ctx context.Context, item T) error
	Get(ctx context.Context, id string) (T, error)
	Query(ctx context.Context, q Query) ([]T, error)
	Delete(ctx context.Context, id string) error
}

// Query parameters supported by all backends. Backends MAY ignore
// fields they cannot implement (e.g. EmbeddingMatch on a pure KV
// backend) but MUST document the omission.
type Query struct {
	TextSearch     string          `json:"textSearch,omitempty"`
	Filter         map[string]any  `json:"filter,omitempty"`
	EmbeddingMatch *EmbeddingMatch `json:"embeddingMatch,omitempty"`
	Limit          int             `json:"limit,omitempty"`
	Offset         int             `json:"offset,omitempty"`
}

// EmbeddingMatch parameterizes vector-similarity backends. Threshold is
// in the [0, 1] cosine-similarity range; backends that use a different
// metric MUST document their interpretation.
type EmbeddingMatch struct {
	Vector    []float32 `json:"vector,omitempty"`
	Threshold float32   `json:"threshold,omitempty"`
}
