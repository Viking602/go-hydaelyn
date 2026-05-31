package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/provider"
)

// fakeDriver is a minimal Driver that records the models it advertises. Stream
// is never exercised by the resolver tests.
type fakeDriver struct {
	name   string
	models []string
}

func (d fakeDriver) Metadata() provider.Metadata {
	return provider.Metadata{Name: d.name, Models: d.models}
}

func (d fakeDriver) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return provider.NewSliceStream(nil), nil
}

func TestSingleResolver_IgnoresModelName(t *testing.T) {
	driver := fakeDriver{name: "only", models: []string{"a"}}
	resolver := provider.Single(driver)

	for _, model := range []string{"a", "b", ""} {
		got, err := resolver.Driver(model)
		if err != nil {
			t.Fatalf("Driver(%q) returned error: %v", model, err)
		}
		if got.Metadata().Name != "only" {
			t.Fatalf("Driver(%q) = %q, want %q", model, got.Metadata().Name, "only")
		}
	}
}

func TestSingleResolver_NilDriver(t *testing.T) {
	resolver := provider.Single(nil)
	if _, err := resolver.Driver("a"); !errors.Is(err, provider.ErrNoDriverForModel) {
		t.Fatalf("Driver on nil single resolver err = %v, want ErrNoDriverForModel", err)
	}
}

func TestRegistry_RoutesByModel(t *testing.T) {
	anthropic := fakeDriver{name: "anthropic", models: []string{"opus", "haiku"}}
	openai := fakeDriver{name: "openai", models: []string{"gpt"}}
	registry := provider.NewRegistry(anthropic, openai)

	cases := map[string]string{
		"opus":  "anthropic",
		"haiku": "anthropic",
		"gpt":   "openai",
	}
	for model, wantProvider := range cases {
		got, err := registry.Driver(model)
		if err != nil {
			t.Fatalf("Driver(%q) returned error: %v", model, err)
		}
		if got.Metadata().Name != wantProvider {
			t.Fatalf("Driver(%q) provider = %q, want %q", model, got.Metadata().Name, wantProvider)
		}
	}
}

func TestRegistry_UnknownModel(t *testing.T) {
	registry := provider.NewRegistry(fakeDriver{name: "anthropic", models: []string{"opus"}})
	if _, err := registry.Driver("mystery"); !errors.Is(err, provider.ErrNoDriverForModel) {
		t.Fatalf("Driver(unknown) err = %v, want ErrNoDriverForModel", err)
	}
}

func TestRegistry_LastRegistrationWinsForDuplicateModel(t *testing.T) {
	first := fakeDriver{name: "first", models: []string{"shared"}}
	second := fakeDriver{name: "second", models: []string{"shared"}}
	registry := provider.NewRegistry(first)
	registry.Register(second)

	got, err := registry.Driver("shared")
	if err != nil {
		t.Fatalf("Driver(shared) returned error: %v", err)
	}
	if got.Metadata().Name != "second" {
		t.Fatalf("Driver(shared) = %q, want last-registered %q", got.Metadata().Name, "second")
	}
}

func TestRegistry_NilDriverIgnored(t *testing.T) {
	registry := provider.NewRegistry(nil)
	if _, err := registry.Driver("anything"); !errors.Is(err, provider.ErrNoDriverForModel) {
		t.Fatalf("Driver after registering nil err = %v, want ErrNoDriverForModel", err)
	}
}
