# Mailbox — task envelopes between agents

Venat has two orthogonal planes for agent collaboration:

| Plane       | Purpose                                                | Public surface                                                          |
|-------------|--------------------------------------------------------|-------------------------------------------------------------------------|
| Blackboard  | **Data** — findings, artifacts, shared evidence        | `api.BlackboardItem` + `Runner.WriteItem` / `SelectItems` / `Subscribe` |
| **Mailbox** | **Dispatch** — hand a task to a specific agent, ack it | `api.TaskEnvelope` + the `Runner` envelope methods                      |

Use the blackboard when one task's *output* feeds another's *input*. Use the
mailbox when a task must be **delivered to a particular agent** (or fanned out
to a role/group) with explicit acknowledgement and dead-letter handling.

There is no separate mailbox object: envelopes are created, acked, and
dead-lettered through `Runner` commands, and every state change is persisted
through the same durable stores as the rest of the run.

## Quick tour

```go
runner := venat.NewDevelopment()
if err := runner.RegisterAgent(api.AgentProfile{ID: "alice"}); err != nil {
    panic(err)
}
if err := runner.RegisterAgent(api.AgentProfile{ID: "bob"}); err != nil {
    panic(err)
}

run, _, _ := runner.StartRun(ctx, api.StartRunCommand{Request: "ping pong"})

// Alice dispatches a question task to Bob.
ask, _ := runner.CreateTask(ctx, api.CreateTaskCommand{
    RunID: run.ID, TaskID: "ask", OwnerAgentID: "bob",
    Goal: "verify alpha-cohort effect",
})
env, _ := runner.DispatchTask(ctx, api.DispatchTaskCommand{
    RunID: run.ID, TaskID: ask.ID, TargetAgentID: "bob",
    Payload: map[string]any{"from": "alice", "subject": "verify claim"},
})

// Bob acquires the execution lease, acks the envelope, then reports.
lease, _, _ := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
    RunID: run.ID, TaskID: ask.ID, EnvelopeID: env.ID,
    HolderType: api.HolderAgent, HolderID: "bob", TTL: time.Minute,
})
_ = runner.AckEnvelope(ctx, api.AckEnvelopeCommand{EnvelopeID: env.ID, HolderID: "bob"})
_ = runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
    RunID: run.ID, TaskID: ask.ID, LeaseID: lease.ID,
    HolderType: api.HolderAgent, HolderID: "bob",
    TaskVersion: ask.Version,
    Report:      api.TypedReport{Status: api.ReportStatusSuccess, Summary: "p=0.012"},
})
```

Run `go run ./_examples/mailbox_pingpong` for the full ask → ack → reply demo.

## Addressing and fan-out

`DispatchTask` targets one agent (`TargetAgentID`) or one host component
(`TargetComponent`). `DispatchTaskFanOut` expands an `api.Address` into one
envelope per matching registered agent:

```go
// One envelope per agent whose AgentProfile.Role == "verifier".
envs, _ := runner.DispatchTaskFanOut(ctx, api.FanOutDispatchTaskCommand{
    RunID: run.ID, TaskID: task.ID,
    To: api.Address{Kind: api.AddressKindRole, Role: "verifier"},
})
```

`api.AddressKind` selects the matching rule: `agent` (exact `AgentID`),
`role` (`AgentProfile.Role`), or `group` (membership in
`AgentProfile.Groups`). Exactly one of `AgentID`/`Role`/`Group` must be set,
matching `Kind`.

## Envelope lifecycle

```
DispatchTask ──▶ pending ──AckEnvelope──▶ acked
                   │
                   └─DeadLetter──▶ TaskMonitor.DecideDeadLetter
                            ├─ retry: status back to pending,
                            │         Attempts+1, NextRetryAt set
                            └─ dead:  status dead, task → blocked
```

The status strings are exported as `api.EnvelopeStatusPending` /
`api.EnvelopeStatusAcked` / `api.EnvelopeStatusDead`; the `Status` field
itself stays a plain string so hosts can add their own states.

- **Dispatch** transitions the task to `dispatched` and persists a `pending`
  envelope carrying the task's payload, read selectors, write targets, and
  retry policy.
- **Ack** (`api.AckEnvelopeCommand{EnvelopeID, HolderID}`) marks delivery;
  the holder must own the active execution lease.
- **DeadLetter** (`api.DeadLetterCommand{EnvelopeID, Reason}`) consults the
  configured `api.TaskMonitor.DecideDeadLetter`. A *retry* decision re-queues
  the envelope (`Attempts` incremented, `NextRetryAt` from the task's
  `RetryPolicy`) and re-dispatches the task; a *dead* decision parks the
  envelope and blocks the task.

Inspection helpers: `runner.LoadEnvelope(ctx, id)`,
`runner.ListEnvelopes(ctx, runID)`, plus `QueueEnvelope`/`UpdateEnvelope` for
host-managed queues.

## Observability

Envelope state changes append `api.Event`s to the run's event store —
`EnvelopeAcked` and `EnvelopeDeadLettered` carry the envelope ID and reason —
so replay and audit see the full dispatch history alongside task transitions.
