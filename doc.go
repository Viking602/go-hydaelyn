// Package hydaelyn is the root façade for the go-hydaelyn multi-agent
// runtime. It re-exports the primary Run/Task entry points so simple
// programs can write:
//
//	runtime := hydaelyn.New(hydaelyn.Config{})
//
// without importing any subpackage.
//
// Subpackages host the real API surface, grouped by concern:
//
//   - [host]       — deprecated Team + Pattern compatibility runtime
//   - [agent]      — agent engine and role definitions
//   - [team]       — legacy team orchestration, patterns, and run state
//   - [provider]   — LLM provider drivers (anthropic, openai, scripted)
//   - [tool]       — tool contract + kit/tooltest helpers
//   - [pattern]    — legacy presets being migrated to Flow
//   - [hook]       — pre/post-turn hook contracts
//   - [transport]  — MCP gateway and HTTP control plane
//   - [observe]    — tracing and metrics observer interface
//   - [capability] — capability/security policy and context plumbing
//   - [mailbox]    — agent-to-agent signal messaging (ask/answer/delegate)
//   - [orchestrator] — primary run/task runtime primitives, leases, handoff, response
//   - [planner]    — planner contract and template provider
//   - [scheduler]  — task queue and lease interfaces
//   - [storage]    — run/workflow/session persistence drivers
//   - [message]    — shared message/content data types
//   - [recipe]     — YAML/JSON runtime configuration loader
//   - [eval]       — evaluation suites and runners (eval/run, eval/cases)
//   - [cli]        — command-line entry point implementation
//
// Types under the internal/ tree are implementation details and are
// exposed only when they must appear in a public signature.
package hydaelyn
