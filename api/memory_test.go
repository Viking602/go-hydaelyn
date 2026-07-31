package api_test

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
)

// fakeEntry verifies api.Identified is satisfiable by an application-defined
// value type whose other fields the framework knows nothing about.
type fakeEntry struct {
	id        string
	scope     api.ContextScope
	subjectID string
	// Application-defined fields below — framework does not see them.
	content string
	tags    []string
}

func (e fakeEntry) ID() string              { return e.id }
func (e fakeEntry) Scope() api.ContextScope { return e.scope }
func (e fakeEntry) SubjectID() string       { return e.subjectID }

// fakeStore verifies api.Memory[T] can be implemented against an arbitrary
// application-owned T. The store itself is trivial; the point is the type
// satisfaction at compile time.
type fakeStore struct {
	entries []fakeEntry
}

func (s *fakeStore) Write(_ context.Context, e fakeEntry) error {
	s.entries = append(s.entries, e)
	return nil
}
func (s *fakeStore) Read(_ context.Context, _ api.MemorySelector) ([]fakeEntry, error) {
	return s.entries, nil
}
func (s *fakeStore) Forget(_ context.Context, _ api.MemorySelector) (int, error) {
	n := len(s.entries)
	s.entries = nil
	return n, nil
}

// Compile-time guarantee.
var _ api.Memory[fakeEntry] = (*fakeStore)(nil)

// TestMemorySelector_ZeroValueIsValid pins the documented zero-value semantics:
// empty IDs = no ID restriction, Limit == 0 = unbounded. Guards against
// silent drift if someone repurposes those fields.
func TestMemorySelector_ZeroValueIsValid(t *testing.T) {
	var sel api.MemorySelector
	if sel.Limit != 0 {
		t.Fatalf("zero-value Limit should be 0 (unbounded sentinel), got %d", sel.Limit)
	}
	if len(sel.IDs) != 0 {
		t.Fatalf("zero-value IDs should be empty (no ID restriction), got %v", sel.IDs)
	}
}

// TestMemory_ApplicationFieldsRoundTrip exercises the design intent that T may
// carry arbitrary application-owned fields (here: content, tags) which the
// framework never inspects but which round-trip through Write/Read intact.
func TestMemory_ApplicationFieldsRoundTrip(t *testing.T) {
	ctx := context.Background()
	var mem api.Memory[fakeEntry] = &fakeStore{}
	in := fakeEntry{
		id:        "e1",
		scope:     api.ContextAgent,
		subjectID: "agent-1",
		content:   "hello world",
		tags:      []string{"greeting", "demo"},
	}
	if err := mem.Write(ctx, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := mem.Read(ctx, api.MemorySelector{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 || got[0].content != in.content || len(got[0].tags) != len(in.tags) {
		t.Fatalf("application fields did not round-trip: %+v", got)
	}
}
