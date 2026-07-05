package skill

import (
	"errors"
	"slices"
	"testing"
)

func TestRegistry_RegisterListGetDeregister(t *testing.T) {
	r := NewRegistry()
	for _, s := range []Skill{
		registryTestSkill("delta"),
		registryTestSkill("alpha"),
		registryTestSkill("charlie"),
	} {
		if err := Register(r, s); err != nil {
			t.Fatalf("Register(%s): %v", s.Name, err)
		}
	}

	list := r.List()
	if got, want := registryTestSkillNames(list), []string{"alpha", "charlie", "delta"}; !slices.Equal(got, want) {
		t.Fatalf("List names = %v, want %v", got, want)
	}

	got, ok := r.Get("charlie")
	if !ok {
		t.Fatal("Get(charlie) ok = false after Register")
	}
	if got.Description != "Skill charlie" {
		t.Fatalf("Get(charlie).Description = %q, want %q", got.Description, "Skill charlie")
	}

	if !r.Deregister("charlie") {
		t.Fatal("Deregister(charlie) = false, want true")
	}
	if _, ok := r.Get("charlie"); ok {
		t.Fatal("Get(charlie) ok = true after Deregister")
	}
	if r.Deregister("charlie") {
		t.Fatal("second Deregister(charlie) = true, want false")
	}
}

func TestRegistry_RegisterNilReturnsErrRegistryNil(t *testing.T) {
	err := Register(nil, registryTestSkill("orphan"))
	if !errors.Is(err, ErrRegistryNil) {
		t.Fatalf("Register(nil, skill) error = %v, want ErrRegistryNil", err)
	}
}

func TestRegistry_RegisterDuplicateReturnsError(t *testing.T) {
	r := NewRegistry()
	s := registryTestSkill("review")
	if err := Register(r, s); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	err := Register(r, s)
	if err == nil {
		t.Fatal("second Register error = nil, want duplicate error")
	}
	var dup *DuplicateError
	if !errors.As(err, &dup) {
		t.Fatalf("second Register error = %T, want *DuplicateError", err)
	}
	if dup.Name != "review" {
		t.Fatalf("DuplicateError.Name = %q, want %q", dup.Name, "review")
	}
}

func TestRegistry_ResolveMissingReturnsNotFoundError(t *testing.T) {
	r := NewRegistry()
	if err := Register(r, registryTestSkill("present")); err != nil {
		t.Fatalf("Register(present): %v", err)
	}

	_, err := r.Resolve("present", "missing")
	if err == nil {
		t.Fatal("Resolve missing skill error = nil, want not found error")
	}
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Resolve missing skill error = %T, want *NotFoundError", err)
	}
	if notFound.Name != "missing" {
		t.Fatalf("NotFoundError.Name = %q, want %q", notFound.Name, "missing")
	}
}

func TestRegistry_ResolvePreservesOrderAndDeduplicates(t *testing.T) {
	r := NewRegistry()
	for _, s := range []Skill{registryTestSkill("a"), registryTestSkill("b")} {
		if err := Register(r, s); err != nil {
			t.Fatalf("Register(%s): %v", s.Name, err)
		}
	}

	resolved, err := r.Resolve("b", "a", "b")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := registryTestSkillNames(resolved), []string{"b", "a"}; !slices.Equal(got, want) {
		t.Fatalf("Resolve names = %v, want %v", got, want)
	}
}

func TestRegistry_ReturnedSkillsAreDefensiveCopies(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(*testing.T, *Registry) Skill
	}{
		{
			name: "Get",
			read: func(t *testing.T, r *Registry) Skill {
				s, ok := r.Get("copyable")
				if !ok {
					t.Fatal("Get(copyable) ok = false")
				}
				return s
			},
		},
		{
			name: "List",
			read: func(t *testing.T, r *Registry) Skill {
				list := r.List()
				if len(list) != 1 {
					t.Fatalf("List length = %d, want 1", len(list))
				}
				return list[0]
			},
		},
		{
			name: "Resolve",
			read: func(t *testing.T, r *Registry) Skill {
				resolved, err := r.Resolve("copyable")
				if err != nil {
					t.Fatalf("Resolve(copyable): %v", err)
				}
				if len(resolved) != 1 {
					t.Fatalf("Resolve length = %d, want 1", len(resolved))
				}
				return resolved[0]
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry()
			if err := Register(r, registryTestSkill("copyable")); err != nil {
				t.Fatalf("Register(copyable): %v", err)
			}

			returned := tc.read(t, r)
			if returned.Metadata == nil {
				t.Fatal("returned Metadata is nil, want registered metadata")
			}
			if len(returned.AllowedTools) == 0 {
				t.Fatal("returned AllowedTools is empty, want registered tools")
			}

			returned.Metadata["topic"] = "mutated"
			returned.AllowedTools[0] = "mutated-tool"

			stored, ok := r.Get("copyable")
			if !ok {
				t.Fatal("Get(copyable) ok = false after mutating returned skill")
			}
			if got, want := stored.Metadata["topic"], "copyable"; got != want {
				t.Fatalf("stored Metadata[topic] after mutating %s result = %q, want %q", tc.name, got, want)
			}
			if got, want := stored.AllowedTools[0], "read"; got != want {
				t.Fatalf("stored AllowedTools[0] after mutating %s result = %q, want %q", tc.name, got, want)
			}
		})
	}
}

func registryTestSkill(name string) Skill {
	return Skill{
		Name:         name,
		Description:  "Skill " + name,
		Metadata:     map[string]string{"topic": name, "stable": "true"},
		AllowedTools: []string{"read", "write"},
		Body:         "Use the " + name + " procedure.",
	}
}

func registryTestSkillNames(skills []Skill) []string {
	names := make([]string, len(skills))
	for i, s := range skills {
		names[i] = s.Name
	}
	return names
}
