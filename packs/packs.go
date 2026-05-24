// Package packs is the v0.8.0 "vertical pack" registry root. A pack is a
// curated bundle of AgentDefinitions, Capabilities, eval Suites, and
// recipes for one application domain. Packs MUST NOT touch the Hydaelyn
// kernel surface — they only consume the public api/* and runtime/* types
// and re-export configuration the host application can mount into its
// own Runner.
//
// The framework ships skeleton packs under packs/research,
// packs/customer-support, packs/devops, and packs/aiops. Each pack
// defines a Pack value at its top level so a host can do:
//
//	packs.Register(myRegistry, research.Pack)
//	packs.Register(myRegistry, devops.Pack)
//
// Adding a new pack means creating a sibling directory and exposing a
// Pack value of the same shape — no kernel changes required.
package packs

import (
	"sort"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/eval"
)

// Pack is the declarative bundle a vertical pack exports.
type Pack struct {
	// Name is a short identifier ("research", "devops"). Must be unique
	// within a single Registry.
	Name string

	// Description is a one-paragraph human-readable summary shown to
	// operators browsing the pack catalog.
	Description string

	// Version follows semantic versioning. Backwards-compatible additions
	// bump minor; AgentDefinition / Capability shape changes bump major.
	Version string

	// Agents are the AgentDefinitions this pack contributes. Hosts that
	// want to install only a subset can pick from this list.
	Agents []api.AgentDefinition

	// Capabilities are the capability manifests the host should publish
	// when this pack is installed. The host is responsible for binding
	// each Capability to an actual implementation (tool, action handler,
	// MCP server).
	Capabilities []api.CapabilityManifest

	// Recipes are descriptive run templates documented in the pack's
	// README. They are not currently consumed by the runtime — the
	// field exists so future versions can wire them into a "preset"
	// catalog without breaking pack authors.
	Recipes []Recipe

	// EvalSuites grade agents in this pack. Hosts MAY run them in CI.
	EvalSuites []eval.Suite
}

// Recipe is a minimal descriptor for a documented "how to use this pack"
// template. Fields beyond Name/Description are intentionally absent —
// the document URL points at the canonical write-up.
type Recipe struct {
	Name        string
	Description string
	DocumentURL string
}

// Registry holds the set of installed packs at runtime. It is safe for
// concurrent reads; mutating it during runtime is the host's
// responsibility (the framework does not synchronize it).
type Registry struct {
	packs map[string]Pack
}

// NewRegistry returns an empty Registry. Pre-populate via Register.
func NewRegistry() *Registry { return &Registry{packs: map[string]Pack{}} }

// Register installs p into r. Returns an error when a pack with the
// same Name is already installed — Hosts that want to override packs
// should Deregister first.
func Register(r *Registry, p Pack) error {
	if _, dup := r.packs[p.Name]; dup {
		return &DuplicateError{Name: p.Name}
	}
	r.packs[p.Name] = p
	return nil
}

// Deregister removes a pack by name. Returns false when no such pack
// is installed.
func (r *Registry) Deregister(name string) bool {
	if _, ok := r.packs[name]; !ok {
		return false
	}
	delete(r.packs, name)
	return true
}

// Get returns a pack by name. The bool is false when no pack with that
// name is installed.
func (r *Registry) Get(name string) (Pack, bool) {
	p, ok := r.packs[name]
	return p, ok
}

// List returns every installed pack in deterministic name order.
func (r *Registry) List() []Pack {
	out := make([]Pack, 0, len(r.packs))
	for _, p := range r.packs {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DuplicateError is returned by Register when a pack with the same name
// is already installed.
type DuplicateError struct {
	Name string
}

// Error satisfies the error interface.
func (e *DuplicateError) Error() string { return "packs: pack already registered: " + e.Name }
