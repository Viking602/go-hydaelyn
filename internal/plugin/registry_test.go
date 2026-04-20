package plugin

import "testing"

func TestRegistryRegistersByTypeAndName(t *testing.T) {
	registry := NewRegistry()
	spec := Spec{
		Type:      TypeProvider,
		Name:      "openai",
		Component: "provider-driver",
		Config: map[string]string{
			"timeout": "plugin",
		},
	}
	if err := registry.Register(spec); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	current, ok := registry.Lookup(TypeProvider, "openai")
	if !ok {
		t.Fatalf("expected provider plugin to be registered")
	}
	if current.Type != TypeProvider || current.Name != "openai" {
		t.Fatalf("unexpected plugin: %#v", current)
	}
	if current.Config["timeout"] != "plugin" {
		t.Fatalf("expected plugin config to round-trip, got %#v", current.Config)
	}
}

func TestRegistryRejectsDuplicateTypeAndName(t *testing.T) {
	registry := NewRegistry()
	spec := Spec{Type: TypeTool, Name: "search", Component: "tool-driver"}
	if err := registry.Register(spec); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(spec); err == nil {
		t.Fatalf("expected duplicate registration to fail")
	}
}

func TestRegistryAllowsSameNameAcrossDifferentTypes(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Spec{Type: TypeProvider, Name: "shared", Component: "provider"}); err != nil {
		t.Fatalf("provider register error = %v", err)
	}
	if err := registry.Register(Spec{Type: TypeObserver, Name: "shared", Component: "observer"}); err != nil {
		t.Fatalf("observer register error = %v", err)
	}
	if _, ok := registry.Lookup(TypeProvider, "shared"); !ok {
		t.Fatalf("expected provider plugin to exist")
	}
	if _, ok := registry.Lookup(TypeObserver, "shared"); !ok {
		t.Fatalf("expected observer plugin to exist")
	}
}
