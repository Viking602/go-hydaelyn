# 02 — Capability Layer

## Goal

Give every "thing an Agent can call" a declarative, schema-bearing public type. That type is the single source of truth for export to MCP, OpenAPI, CLI command synthesis, and LLM tool definitions.

## Relationship to existing concepts

- **ADR-005** (`internal/capability` package, `CapabilityInvoker`) is the **execution-time governance layer** that wraps LLM calls and Tool calls with timeout/retry/permission/approval/rate-limit. It stays internal.
- **This document** defines `api.Capability` as the **declarative schema** that anything-callable exposes to the outside world.
- `api.Tool` (the existing public type) continues to be the **executable binding** — code that runs. A Tool produces a Capability via `Tool.AsCapability()`.

Three layers, three responsibilities:

```
api.Capability         (declaration:  what)
api.Tool               (execution:    how)
internal/capability    (enforcement:  timeout/retry/permission)
```

## Type definitions

New file: `api/capability.go`

```go
package api

import "encoding/json"

// Capability is the declarative description of something an Agent can invoke.
// One Tool produces one Capability; one external system can produce many
// Capabilities. The Capability is what gets serialized to MCP tools,
// OpenAPI operations, CLI subcommands, and LLM tool definitions.
type Capability struct {
    // Name uniquely identifies the capability within the manifest. Recommended
    // convention: "provider.action", e.g. "github.create_issue". Names are
    // case-sensitive and SHOULD NOT contain spaces.
    Name string `json:"name"`

    // Version is the capability schema version. Optional. Format unspecified
    // (semver, date, integer all allowed). Consumers MAY use it for
    // compatibility checks.
    Version string `json:"version,omitempty"`

    // Description is the human-readable and LLM-readable explanation.
    Description string `json:"description,omitempty"`

    // InputSchema and OutputSchema are JSON Schema documents (typically draft
    // 2020-12, but the framework does not enforce the dialect). Storing as
    // raw JSON allows adapters to choose their preferred schema standard.
    InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
    OutputSchema json.RawMessage `json:"outputSchema,omitempty"`

    // EffectType classifies the side-effect surface for policy and risk
    // routing. Reuses the existing ToolEffectType enum.
    EffectType ToolEffectType `json:"effectType"`

    // Idempotent declares whether invoking the capability twice with identical
    // input produces identical effect. The framework does NOT enforce this;
    // it is metadata for policy, retry, and replay reasoning.
    Idempotent bool `json:"idempotent,omitempty"`

    // RequiresLease declares that invocation must run inside a TaskExecutionLease.
    // The runtime rejects invocations that do not hold a lease when this is true.
    RequiresLease bool `json:"requiresLease,omitempty"`

    // RequiresPolicy declares that invocation must be authorized by the
    // PolicyEngine before execution. The runtime rejects bypass attempts.
    RequiresPolicy bool `json:"requiresPolicy,omitempty"`

    // Tags are free-form labels for discovery and grouping.
    Tags []string `json:"tags,omitempty"`

    // Metadata carries adapter- or pack-specific fields. Framework code
    // ignores Metadata except for serialization.
    Metadata map[string]string `json:"metadata,omitempty"`
}

// CapabilityManifest is a versioned bundle of Capabilities, typically
// exported from one external system or one pack.
type CapabilityManifest struct {
    Name         string       `json:"name"`
    Version      string       `json:"version"`
    Description  string       `json:"description,omitempty"`
    Capabilities []Capability `json:"capabilities"`
}
```

## Tool → Capability adapter

Add method to existing `api.Tool` (in `api/types.go` or new `api/tool.go`):

```go
// AsCapability returns the declarative Capability view of this Tool.
// Tools always require Policy authorization; Tools marked RequiresActionTask
// always require a lease. Other fields default conservatively.
func (t Tool) AsCapability() Capability {
    return Capability{
        Name:           t.Name,
        Description:    t.Description,
        InputSchema:    t.InputSchema,
        OutputSchema:   t.OutputSchema,
        EffectType:     t.EffectType,
        RequiresPolicy: true,
        RequiresLease:  t.RequiresActionTask,
        Idempotent:     false, // conservative default; tool authors can publish a richer Capability separately
    }
}
```

If `api.Tool` does not currently expose `InputSchema` / `OutputSchema` as `json.RawMessage`, they are added in this release (additive change, no break).

## Exports

Four export paths. All read a `CapabilityManifest` and emit a target-specific format.

### Export 1 — MCP Tool

Location: extend `transport/mcp/`

```go
// transport/mcp/export.go
package mcp

import "github.com/Viking602/go-hydaelyn/api"

// ToolsFromCapabilities renders a CapabilityManifest as MCP tool descriptors.
func ToolsFromCapabilities(m api.CapabilityManifest) []ToolDescriptor

type ToolDescriptor struct {
    Name        string
    Description string
    InputSchema json.RawMessage
    // ... MCP-specific fields
}
```

### Export 2 — OpenAPI Operation

New package: `transport/openapi/`

```go
// transport/openapi/export.go
package openapi

func DocumentFromManifest(m api.CapabilityManifest) Document
```

Generates a minimal OpenAPI 3.1 document where each Capability becomes one POST operation under `/capabilities/{name}`. `InputSchema` populates `requestBody`; `OutputSchema` populates `responses.200`.

### Export 3 — CLI Command

New package or extension of existing `cmd/`:

```go
// cmd/capabilitycli/generator.go
func CommandsFromManifest(m api.CapabilityManifest) []*cobra.Command
```

Each Capability becomes a CLI subcommand. Flags are generated from `InputSchema` properties.

### Export 4 — LLM Tool Definition

Location: extend `provider/`

```go
// provider/tooldef.go
package provider

func ToolDefinitionFromCapability(c api.Capability) ToolDefinition
```

`ToolDefinition` shape matches the major LLM tool-call APIs (OpenAI, Anthropic, Gemini). Adapters in `provider/openai/`, `provider/anthropic/` etc. consume this.

## Reserved namespace: `hydaelyn.self.*`

The `hydaelyn.self.*` Capability name prefix is reserved by the framework for built-in self-knowledge capabilities. v0.8.0 reserves four names but ships no implementations; user code MUST NOT register Capabilities whose names collide with this prefix.

Reserved names:

| Name | Intended purpose (v0.9.0+) | EffectType | Idempotent |
|------|----------------------------|------------|------------|
| `hydaelyn.self.profile` | Return the calling Agent's `AgentProfile` (sanitized — no secrets) | `ToolEffectRead` | yes |
| `hydaelyn.self.memory.read` | Read from a registered `api.Memory[T]` where `Identified.Scope()=Agent, Identified.SubjectID()=<calling agent ID>`. Binding-activated (ADR-013): the capability appears only when the application has registered a `Memory[T]`. | `ToolEffectRead` | yes |
| `hydaelyn.self.history` | List runs via `RunSelector{AgentID: <self>, AgentVersion: <self> | ""}` | `ToolEffectRead` | yes |
| `hydaelyn.self.summarize_history` | Produce a structured summary of past runs for the calling Agent | `ToolEffectRead` | no (depends on time window) |

Why reserve in v0.8.0 without shipping:

- Once Agents start populating their Capability allowlists in production, claiming the namespace later would either collide with user-chosen names or force a renaming migration.
- Reservation costs nothing: a `const` block plus a registration guard that rejects user attempts to define names under the prefix.
- The naming alone communicates the framework's stance: self-knowledge is **read-only, query-shaped, and provided by the framework** — not synthesized by the Agent at runtime.

`api/capability.go` adds:

```go
// HydaelynSelfNamespace is the reserved Capability name prefix for built-in
// self-knowledge capabilities. Names under this prefix are reserved by the
// framework; user registrations MUST fail.
const HydaelynSelfNamespace = "hydaelyn.self."

// Reserved capability names. v0.8.0 declares them; v0.9.0+ may ship default
// implementations bound to the calling Agent's identity.
const (
    CapabilityNameSelfProfile          = "hydaelyn.self.profile"
    CapabilityNameSelfMemoryRead       = "hydaelyn.self.memory.read"
    CapabilityNameSelfHistory          = "hydaelyn.self.history"
    CapabilityNameSelfSummarizeHistory = "hydaelyn.self.summarize_history"
)
```

`Registry.RegisterCapability` MUST reject names that start with `HydaelynSelfNamespace` unless the registration comes from a designated internal package. The rejection produces `ErrCapabilityNameReserved` (new error in `api/errors.go`).

What this is **not**:

- Not a claim that Agents have introspection in the consciousness sense — these are SQL-shaped queries against AgentProfile / Memory / Run records.
- Not a hook for an Agent to mutate its own profile or memory. All four reserved names are read-only.
- Not a substitute for ADR-014 (which records the structural-ontology stance these four names instantiate).

## Contract tests

New file: `api/capability_test.go`

- `TestCapability_JSONRoundTrip`: marshal → unmarshal → DeepEqual
- `TestCapabilityManifest_JSONRoundTrip`
- `TestTool_AsCapability_PreservesIdentity`: a Tool round-tripped through AsCapability and back yields equivalent Name/EffectType/RequiresLease
- `TestCapability_MCPExportShape`: an Exported MCP descriptor matches expected JSON shape
- `TestCapability_OpenAPIExportValidates`: Exported OpenAPI document passes spec validation (use `getkin/kin-openapi` or equivalent)
- `TestCapability_LLMToolDefShape`: Exported LLM tool definition contains required fields
- `TestRegistry_RejectsReservedSelfNamespace`: registering a Capability whose Name starts with `hydaelyn.self.` returns `ErrCapabilityNameReserved`
- `TestSelfNamespaceConstants_StableStrings`: the four reserved name constants serialize to the exact strings documented (guards against accidental renames)

## Verification

- `go build ./...` succeeds with new package
- Capability JSON round-trip succeeds for empty, populated, and edge-case Capabilities (empty schemas, unicode names, large Metadata)
- All four exports produce valid output for the same input manifest
- `_examples/` gain one example: declaring a Capability, exporting it as MCP + LLM tool def
