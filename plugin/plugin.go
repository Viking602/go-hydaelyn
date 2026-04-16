package plugin

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

type Type string

const (
	TypeProvider   Type = "provider"
	TypeTool       Type = "tool"
	TypePlanner    Type = "planner"
	TypeVerifier   Type = "verifier"
	TypeStorage    Type = "storage"
	TypeMemory     Type = "memory"
	TypeObserver   Type = "observer"
	TypeScheduler  Type = "scheduler"
	TypeMCPGateway Type = "mcp_gateway"
)

var ErrInvalidSpec = errors.New("invalid plugin spec")
var ErrDuplicate = errors.New("duplicate plugin registration")

type Ref struct {
	Type Type   `json:"type"`
	Name string `json:"name"`
}

type Spec struct {
	Type      Type              `json:"type"`
	Name      string            `json:"name"`
	Component any               `json:"-"`
	Config    map[string]string `json:"config,omitempty"`
}

type Registry struct {
	mu    sync.RWMutex
	items map[Type]map[string]Spec
}

func NewRegistry(specs ...Spec) *Registry {
	registry := &Registry{
		items: map[Type]map[string]Spec{},
	}
	for _, spec := range specs {
		_ = registry.Register(spec)
	}
	return registry
}

func (r *Registry) Register(spec Spec) error {
	if err := validateSpec(spec); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[spec.Type]; !ok {
		r.items[spec.Type] = map[string]Spec{}
	}
	if _, exists := r.items[spec.Type][spec.Name]; exists {
		return ErrDuplicate
	}
	spec.Config = clone(spec.Config)
	r.items[spec.Type][spec.Name] = spec
	return nil
}

func (r *Registry) Lookup(pluginType Type, name string) (Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	typed := r.items[pluginType]
	if typed == nil {
		return Spec{}, false
	}
	spec, ok := typed[name]
	if !ok {
		return Spec{}, false
	}
	spec.Config = clone(spec.Config)
	return spec, true
}

func (r *Registry) List(pluginType Type) []Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	typed := r.items[pluginType]
	items := make([]Spec, 0, len(typed))
	for _, spec := range typed {
		spec.Config = clone(spec.Config)
		items = append(items, spec)
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].Name < items[right].Name
	})
	return items
}

func validateSpec(spec Spec) error {
	if strings.TrimSpace(string(spec.Type)) == "" {
		return ErrInvalidSpec
	}
	if strings.TrimSpace(spec.Name) == "" {
		return ErrInvalidSpec
	}
	if spec.Component == nil {
		return ErrInvalidSpec
	}
	return nil
}

func clone(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
