# 02 — Capability Layer

## Goal

Give every "thing an Agent can call" a declarative, schema-bearing public type. That type is the single source of truth for export to MCP, OpenAPI, CLI command synthesis, and LLM tool definitions.

## Relationship to existing concepts

- **ADR-005** (`internal/capability` package, `CapabilityInvoker`) is the **execution-time governance layer** that wraps LLM calls and Tool calls with timeout/retry/permission/approval/rate-limit. It stays internal.
- **This document** defines `api.Capability` as the **declarative schema** that anything-callable exposes to the outside world.
- `api.Tool` (existing public type) is the **executable binding** — code that runs. A Tool produces a Capability via `Tool.AsCapability()`.

Three layers, three responsibilities:

```
api.Capability         (declaration:  what)
api.Tool               (execution:    how)
internal/capability    (enforcement:  timeout/retry/permission)
```

A fourth, distinct layer added by v0.8.0:

```
agent.ToolSafety       (loop policy: retry / approval routing)
```

`agent.ToolSafety` (`03-agent-loop.md`) decides whether the Agent Loop
may auto-retry a tool — orthogonal to `api.Capability`, which only
declares what the tool *is*.

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
    Name string `json:"name"`
    Version string `json:"version,omitempty"`
    Description string `json:"description,omitempty"`

    InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
    OutputSchema json.RawMessage `json:"outputSchema,omitempty"`

    EffectType ToolEffectType `json:"effectType"`
    Idempotent bool `json:"idempotent,omitempty"`
    RequiresLease  bool `json:"requiresLease,omitempty"`
    RequiresPolicy bool `json:"requiresPolicy,omitempty"`

    Tags []string `json:"tags,omitempty"`
    Metadata map[string]string `json:"metadata,omitempty"`
}

type CapabilityManifest struct {
    Name         string       `json:"name"`
    Version      string       `json:"version"`
    Description  string       `json:"description,omitempty"`
    Capabilities []Capability `json:"capabilities"`
}
```

Field semantics (unchanged from prior draft): see source code comments
for `Name` convention, `Version` policy, `InputSchema/OutputSchema`
dialect choice, `EffectType` enum, `Idempotent` metadata-only contract,
`RequiresLease` and `RequiresPolicy` enforcement.

## Tool → Capability adapter

```go
func (t Tool) AsCapability() Capability {
    return Capability{
        Name:           t.Name,
        Description:    t.Description,
        InputSchema:    t.InputSchema,
        OutputSchema:   t.OutputSchema,
        EffectType:     t.EffectType,
        RequiresPolicy: true,
        RequiresLease:  t.RequiresActionTask,
        Idempotent:     false,
    }
}
```

If `api.Tool` does not currently expose `InputSchema` / `OutputSchema` as `json.RawMessage`, they are added in this release (additive change, no break).

## Exports

Four export paths. All read a `CapabilityManifest` and emit a target-specific format.

### Export 1 — MCP Tool

Location: extend `transport/mcp/`

```go
package mcp

func ToolsFromCapabilities(m api.CapabilityManifest) []ToolDescriptor
```

### Export 2 — OpenAPI Operation

New package: `transport/openapi/`

```go
package openapi

func DocumentFromManifest(m api.CapabilityManifest) Document
```

Each Capability becomes one POST operation under `/capabilities/{name}`.

### Export 3 — CLI Command

```go
func CommandsFromManifest(m api.CapabilityManifest) []*cobra.Command
```

Each Capability becomes a CLI subcommand. Flags from `InputSchema`.

### Export 4 — LLM Tool Definition

```go
package provider

func ToolDefinitionFromCapability(c api.Capability) ToolDefinition
```

Shape matches OpenAI / Anthropic / Gemini tool-call APIs.

## Reserved namespace: `hydaelyn.self.*`

The `hydaelyn.self.*` Capability name prefix is reserved by the framework. v0.8.0 reserves four names but ships no implementations.

| Name | Intended purpose (v0.9.0+) | EffectType | Idempotent |
|------|----------------------------|------------|------------|
| `hydaelyn.self.profile` | Return calling Agent's `AgentProfile` (sanitized) | `ToolEffectRead` | yes |
| `hydaelyn.self.memory.read` | Read from registered `api.Memory[T]` scoped to calling AgentID — binding-activated (ADR-013) | `ToolEffectRead` | yes |
| `hydaelyn.self.history` | List runs via `RunSelector{AgentID: <self>}` | `ToolEffectRead` | yes |
| `hydaelyn.self.summarize_history` | Structured summary of past runs for calling Agent | `ToolEffectRead` | no |

`api/capability.go` adds:

```go
const HydaelynSelfNamespace = "hydaelyn.self."

const (
    CapabilityNameSelfProfile          = "hydaelyn.self.profile"
    CapabilityNameSelfMemoryRead       = "hydaelyn.self.memory.read"
    CapabilityNameSelfHistory          = "hydaelyn.self.history"
    CapabilityNameSelfSummarizeHistory = "hydaelyn.self.summarize_history"
)
```

`Registry.RegisterCapability` MUST reject names starting with
`HydaelynSelfNamespace` unless registration comes from a designated
internal package. Rejection produces `ErrCapabilityNameReserved` (new
error in `api/errors.go`).

## Multi-agent vocabulary disclaimer

Per the ADR-008 revision (`11-boundaries.md` Principle 1), the
following names are framework primitives, not business vocabulary,
and are exempt from the business-word ban — they may appear in
`api/`, `agent/`, `multiagent/`:

```
Scheduler, Supervisor, Voting, Debate, Handoff, Dispatch, Team,
AgentClass, AgentInstance, TypedReport, TeamState
```

Capability authors building multi-agent primitives may freely name
Capabilities using these terms (e.g. `multiagent.dispatch.emit`).
Business-domain vocabulary (incident, change, ticket, customer,
deploy, …) remains banned in `api/`, `agent/`, `multiagent/`.

## Contract tests

`api/capability_test.go`:

- `TestCapability_JSONRoundTrip`
- `TestCapabilityManifest_JSONRoundTrip`
- `TestTool_AsCapability_PreservesIdentity`
- `TestCapability_MCPExportShape`
- `TestCapability_OpenAPIExportValidates`
- `TestCapability_LLMToolDefShape`
- `TestRegistry_RejectsReservedSelfNamespace`
- `TestSelfNamespaceConstants_StableStrings`

## Verification

- `go build ./...` succeeds with new package
- Capability JSON round-trip succeeds for empty, populated, and edge-case Capabilities
- All four exports produce valid output for the same input manifest
- `_examples/` gain one example: declaring a Capability, exporting it as MCP + LLM tool def
