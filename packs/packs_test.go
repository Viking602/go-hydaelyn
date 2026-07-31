package packs_test

import (
	"errors"
	"testing"

	"github.com/Viking602/venat/packs"
	"github.com/Viking602/venat/packs/aiops"
	"github.com/Viking602/venat/packs/customersupport"
	"github.com/Viking602/venat/packs/devops"
	"github.com/Viking602/venat/packs/research"
)

func TestRegistry_RegisterListDeregister(t *testing.T) {
	r := packs.NewRegistry()
	all := []packs.Pack{research.Pack, customersupport.Pack, devops.Pack, aiops.Pack}
	for _, p := range all {
		if err := packs.Register(r, p); err != nil {
			t.Fatalf("Register(%s): %v", p.Name, err)
		}
	}
	list := r.List()
	if got, want := len(list), len(all); got != want {
		t.Fatalf("List length = %d, want %d", got, want)
	}
	// List is sorted by name.
	for i := 1; i < len(list); i++ {
		if list[i-1].Name >= list[i].Name {
			t.Fatalf("List not sorted: %q >= %q", list[i-1].Name, list[i].Name)
		}
	}
	if _, ok := r.Get(research.PackName); !ok {
		t.Fatalf("research pack not found after Register")
	}
	if !r.Deregister(research.PackName) {
		t.Fatal("Deregister returned false for installed pack")
	}
	if _, ok := r.Get(research.PackName); ok {
		t.Fatal("Get returned ok after Deregister")
	}
}

func TestRegistry_RegisterDuplicateReturnsError(t *testing.T) {
	r := packs.NewRegistry()
	if err := packs.Register(r, research.Pack); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := packs.Register(r, research.Pack)
	if err == nil {
		t.Fatal("expected duplicate error, got nil")
	}
	var dup *packs.DuplicateError
	if !errors.As(err, &dup) {
		t.Fatalf("expected *DuplicateError, got %T", err)
	}
	if dup.Name != research.PackName {
		t.Fatalf("DuplicateError.Name = %q, want %q", dup.Name, research.PackName)
	}
}

func TestPacks_ShapeInvariants(t *testing.T) {
	for _, p := range []packs.Pack{research.Pack, customersupport.Pack, devops.Pack, aiops.Pack} {
		if p.Name == "" {
			t.Fatalf("pack with empty name: %+v", p)
		}
		if p.Version == "" {
			t.Fatalf("pack %s missing version", p.Name)
		}
		for _, a := range p.Agents {
			if a.ID == "" || a.Name == "" {
				t.Fatalf("pack %s agent missing id/name: %+v", p.Name, a)
			}
		}
		for _, m := range p.Capabilities {
			if m.Name == "" {
				t.Fatalf("pack %s capability manifest missing name", p.Name)
			}
			for _, c := range m.Capabilities {
				if c.Name == "" {
					t.Fatalf("pack %s manifest %s has nameless capability", p.Name, m.Name)
				}
			}
		}
	}
}
