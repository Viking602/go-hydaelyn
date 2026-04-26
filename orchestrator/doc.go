// Package orchestrator provides the run/task runtime primitives that sit
// below reusable multi-agent patterns.
//
// The package keeps orchestration state in Run and Task records, treats the
// event log as append-only audit/replay input, and requires a live
// TaskExecutionLease before agents or runtime components can submit typed
// results. Mailbox envelopes notify agents, but they do not grant execution
// permission and mailbox acknowledgements do not complete tasks.
package orchestrator
