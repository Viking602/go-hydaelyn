// Package memory is a deprecated compatibility surface.
//
// The canonical optional-plugin contract is api.Memory[T api.Identified]
// (Write / Read / Forget, isolation by Scope + SubjectID). See ADR-013
// and ADR-021. This package keeps the historical Put / Get / Query /
// Delete verbs so existing importers compile. It will be removed in a
// later minor.
//
// The framework still ships no Memory backend (ADR-012 Position D).
package memory

import "context"

// Identified is the historical ID-only constraint.
//
// Deprecated: implement api.Identified (ID, Scope, SubjectID) and use
// api.Memory.
type Identified interface {
	ID() string
}

// Memory is the historical verb surface.
//
// Deprecated: use api.Memory.
type Memory[T Identified] interface {
	Put(ctx context.Context, item T) error
	Get(ctx context.Context, id string) (T, error)
	Query(ctx context.Context, q Query) ([]T, error)
	Delete(ctx context.Context, id string) error
}

// Query is the historical retrieval parameter bag. Retrieval stays an
// application or recipe concern under ADR-013; this type exists only
// for the deprecated Memory[T] surface.
//
// Deprecated: use api.MemorySelector or an application-specific query
// type.
type Query struct {
	TextSearch     string          `json:"textSearch,omitempty"`
	Filter         map[string]any  `json:"filter,omitempty"`
	EmbeddingMatch *EmbeddingMatch `json:"embeddingMatch,omitempty"`
	Limit          int             `json:"limit,omitempty"`
	Offset         int             `json:"offset,omitempty"`
}

// EmbeddingMatch parameterizes historical vector-similarity backends.
//
// Deprecated: keep embedding search in application or recipe code.
type EmbeddingMatch struct {
	Vector    []float32 `json:"vector,omitempty"`
	Threshold float32   `json:"threshold,omitempty"`
}
