// Package workflow provides a user-facing workflow modeling layer.
//
// A workflow Definition is compiled to multiagent.Graph, which means workflow
// execution still flows through multiagent.Scheduler decisions and
// multiagent.Dispatch values. The workflow package does not create a second
// durable runtime and does not bypass Runner-owned Run, Task, Event, Lease,
// Policy, or Outbox behavior.
//
// Conditions used by Branch must be pure functions of api.TypedReport. The
// compiled graph may call them during replay or recovery, so conditions must
// not read clocks, mutate external state, call providers, or depend on
// process-local counters.
//
// Engine is an in-process convenience wrapper over multiagent.Drive. For
// durable execution, hosts should supply a multiagent.Executor that persists
// each dispatch through the root Runner before executing agent work.
package workflow
