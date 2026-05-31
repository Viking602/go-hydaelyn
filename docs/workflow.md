# Workflow

`workflow/` is the user-facing modeling layer for multi-step agent work. A
workflow definition compiles to `multiagent.Graph`, then executes through
`multiagent.Scheduler` decisions and `multiagent.Dispatch` values.

It is not a second durable runtime. Runner still owns durable `Run`, `Task`,
`Event`, `Lease`, policy, approval, outbox, and audit behavior.

Naming boundaries:

- `transport/cron`: time-based trigger transport; decides when a run starts.
- `workflow`: workflow definitions; compiles to `multiagent.Graph`.
- `multiagent.Scheduler`: dispatch decision primitive; decides which agent/task runs next.
- `flow` / `api.Flow`: preset adapter metadata; configures runtime adapters and never bypasses Runner invariants.

Branch conditions must be pure functions of `api.TypedReport`. The compiled
graph may call predicates during replay or recovery, so predicates must not
read clocks, call providers, mutate external systems, or depend on process
local counters.
