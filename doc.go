// Package hydaelyn is the root facade for the go-hydaelyn runner. It
// re-exports the primary Run/Task entry points so simple programs can write:
//
//	runner := hydaelyn.New()
//
// without importing any subpackage. Pass Config only when overriding the
// default in-memory configuration.
//
// Public packages are grouped by stable extension concern:
//
//   - [orchestrator] — advanced run/task primitives, leases, handoff, response
//   - [agent]        — agent engine and profile contracts
//   - [tool]         — tool contract, effect types, and tooltest helpers
//   - [flow]         — flow preset contracts
//   - [policy]       — unified authorization contract
//   - [blackboard]   — blackboard item and selector contracts
//   - [provider]     — LLM provider drivers (anthropic, openai, scripted)
//   - [message]      — shared message/content data types
//   - [hook]         — pre/post-turn hook contracts
//   - [transport]    — integration transports such as MCP
//   - [worker]       — optional Runner-to-agent.Engine execution glue
//
// Types under internal/ are implementation details. Core storage, mailbox,
// transition, command-handler, and replay internals are not public extension
// points.
package hydaelyn
