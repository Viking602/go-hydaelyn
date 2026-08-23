package memory

import "testing"

type item struct{ id string }

func (i item) ID() string { return i.id }

func TestDeprecatedMemorySurfaceStillCompiles(t *testing.T) {
	var _ Memory[item]
	_ = Query{Limit: 1}
	_ = EmbeddingMatch{Threshold: 0.5}
}
