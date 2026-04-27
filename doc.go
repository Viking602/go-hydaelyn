// Package hydaelyn is the root facade for the go-hydaelyn Orchestrator
// runtime. It re-exports the primary Run/Task entry points so simple
// programs can write:
//
//	runtime := hydaelyn.New(hydaelyn.Config{})
//
// without importing any subpackage.
//
// Public packages are grouped by stable extension concern:
//
//   - [orchestrator] — primary run/task runtime primitives, leases, handoff, response
//   - [agent]        — agent engine and profile contracts
//   - [tool]         — tool contract, effect types, and tooltest helpers
//   - [flow]         — flow preset contracts
//   - [policy]       — unified authorization contract
//   - [blackboard]   — blackboard item and selector contracts
//   - [provider]     — LLM provider drivers (anthropic, openai, scripted)
//   - [message]      — shared message/content data types
//   - [hook]         — pre/post-turn hook contracts
//   - [transport]    — integration transports such as MCP
//   - [legacy]       — deprecated host/team/pattern compatibility only
//
// Types under internal/ are implementation details. Runtime storage,
// mailbox, scheduler, observe, transition, command-handler, and replay
// internals are not public extension points.
package hydaelyn
