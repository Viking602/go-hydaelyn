# Mailbox — agent-to-agent signals

`go-hydaelyn` has two orthogonal data planes for agent collaboration:

| Plane       | Purpose                                                     | Where it lives                    |
|-------------|-------------------------------------------------------------|-----------------------------------|
| Blackboard  | **Data** — findings, artifacts, CAS-protected exchanges     | `internal/blackboard.Exchange`    |
| **Mailbox** | **Signals** — ask / answer / delegate / cancel / handoff    | `mailbox.Mailbox`                 |

Use the blackboard when one task's *output* feeds another's *input*. Use the
mailbox when one agent wants to **talk to another agent directly**, out-of-band
from the static plan.

## Quick tour

```go
driver := storage.NewMemoryDriver()
runner := host.New(host.Config{Storage: driver})

mbox := runner.Mailbox()

// Ask verifier-1 a question.
ids, _ := mbox.Send(ctx, mailbox.SendInput{
    TeamRunID: state.ID,
    From: mailbox.Address{
        Kind: mailbox.AddressKindAgent, TeamRunID: state.ID, AgentID: "researcher-1",
    },
    To: mailbox.Address{
        Kind: mailbox.AddressKindAgent, TeamRunID: state.ID, AgentID: "verifier-1",
    },
    Letter: mailbox.Letter{
        Subject:  "verify claim",
        Body:     "is the p-value significant?",
        Intent:   mailbox.IntentAsk,
        Priority: mailbox.PriorityHigh,
    },
    CorrelationID: "thread-7",
})
```

When verifier-1 is next scheduled, the runtime drains its inbox and injects
the letter into its prompt as an `[Incoming messages]` block. On task success
the envelope is acked; on retryable failure it's nacked. Best-effort events
flow to `storage.EventStore` so patterns can observe the lifecycle.

Run `go run ./_examples/mailbox_pingpong` for a full ask → answer demo.

## Addressing

```go
// Direct agent
mailbox.Address{Kind: AddressKindAgent, AgentID: "worker-1"}

// All agents with a role (fan-out is server-side, one envelope per recipient)
mailbox.Address{Kind: AddressKindRole, Role: team.RoleVerifier}

// All agents whose metadata["group"] matches
mailbox.Address{Kind: AddressKindGroup, Group: "qa-squad"}
```

## Intents & priorities

| Intent       | Typical use                                     |
|--------------|--------------------------------------------------|
| `ask`        | Default. Needs an answer.                        |
| `answer`     | Reply to an `ask`; set `InReplyTo`.              |
| `delegate`   | "You own this now."                              |
| `cancel`     | Ask a peer to stop a sub-task.                   |
| `broadcast`  | FYI, no answer expected.                         |
| `handoff`    | Transfer ownership; typically paired with state. |

Priorities `low | normal | high | urgent` govern inbox ordering. Within the
same priority, per-recipient FIFO is guaranteed.

## Delivery semantics

- **At-least-once.** Every envelope requires an explicit `Ack`. Retryable
  failures `Nack`, which increments the attempt counter and promotes to DLQ
  after `MaxAttempts`.
- **Lease-based.** `Fetch` claims a lease (default 60s). If the worker dies
  before `Ack`, `RecoverExpiredLeases` returns the envelope to `pending`.
- **Per-recipient FIFO + priority.** Higher priority first; ties broken by
  sequence.
- **TTL.** Expired envelopes are swept during `Fetch` and marked `expired`.

## Guardrails (defaults)

| Knob                 | Default   | Override via              |
|----------------------|-----------|---------------------------|
| `MaxBodySize`        | 64 KiB    | `mailbox.Limits`          |
| `MaxInlineBodySize`  | 4 KiB     | `mailbox.Limits`          |
| `MaxPerRecipient`    | 1024      | `mailbox.Limits`          |
| `MaxAttempts`        | 3         | `mailbox.Limits`          |
| `MaxHops`            | 8         | `mailbox.Limits`          |
| `DefaultTTL`         | 24h       | `mailbox.Limits`          |
| `SendRatePerMinute`  | 60        | `mailbox.Limits`          |

Body and subject are automatically scrubbed for common PII (API keys, emails,
phone numbers, card-like digits). Size/rate/hop overflow returns a typed
error (`ErrOverSize`, `ErrRateLimited`, `ErrHopLimit`) which the
`send_message` tool surfaces back to the LLM.

Pass custom limits through `host.Config`:

```go
host.New(host.Config{
    MailboxLimits: mailbox.Limits{
        MaxBodySize:       8 * 1024,
        SendRatePerMinute: 10,
    },
})
```

## Using it from an agent

Register the `send_message` tool so LLMs can post letters:

```go
runner.RegisterTool(kit.NewSendMessageTool(runner))
```

The tool auto-discovers the caller's `TeamRunID` and `AgentID` from the
task context — the LLM only supplies recipient, body, and intent.

## Disabling the mailbox

It's wired automatically when `storage.Driver.Mailboxes()` returns a store.
To opt out (e.g. for a minimal embedded build):

```go
disabled := false
host.New(host.Config{MailboxEnabled: &disabled})
```

## Observability

Every state change is persisted as a `storage.Event`:

- `MailboxSent`, `MailboxDelivered`, `MailboxAcked`, `MailboxNacked`,
  `MailboxExpired`, `MailboxDead`.

Pair with `observe.Observer` to ship these to your tracer of choice.

## Phase 2 roadmap

Things deliberately left out of the first cut:

- `reply_message` tool wrapper.
- `await_message` capability (blocking receive).
- `Subscribe` push channel (Redis / NATS / long-poll).
- `Task.Receives` pattern-level expectations.
- Cross-team-run addressing.
- DLQ browser + visualization.
