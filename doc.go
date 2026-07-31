// Package venat is the root facade for the Venat runner. It owns
// construction and the Runner wrapper:
//
//	runner := venat.NewDevelopment()
//
// Import [api] for public contracts such as Config, commands, store
// interfaces, policy requests, and Run/Task value types.
//
// Public packages are grouped by stable extension concern:
//
//   - venat          — Runner construction, methods, and error re-exports
//   - [api]          — Config, commands, interfaces, Run/Task data contracts
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
// points and cannot be imported by consumers.
package venat
