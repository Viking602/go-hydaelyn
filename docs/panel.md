# Panel Task Board

`panel` is the collaborative task-board pattern. A user question becomes a
todo plan; expert agents claim matching todos, work in parallel, cross-review
the results, and synthesis returns the final answer with the evidence trail.

## Flow

```text
User question
  -> TodoPlan / TaskBoard
  -> capability-based todo claims
  -> parallel research tasks
  -> cross-review verification tasks
  -> verified synthesis
  -> TeamTimeline + final answer
```

## Input

```go
runner.RegisterPattern(panel.New())

state, err := runner.StartTeam(ctx, host.StartTeamRequest{
    Pattern:           "panel",
    SupervisorProfile: "supervisor",
    WorkerProfiles:    []string{"security", "frontend"},
    Input: map[string]any{
        "query": "launch auth feature",
        "requireVerification": true,
        "experts": []any{
            map[string]any{"profile": "security", "domains": []any{"security"}, "capabilities": []any{"threat_model"}},
            map[string]any{"profile": "frontend", "domains": []any{"frontend"}, "capabilities": []any{"browser"}},
        },
        "todos": []any{
            map[string]any{"id": "security-review", "title": "review auth threat model", "domain": "security", "requiredCapabilities": []any{"threat_model"}, "priority": "high"},
            map[string]any{"id": "ui-review", "title": "review login UI", "domain": "frontend", "requiredCapabilities": []any{"browser"}},
        },
    },
})
```

## Contracts

- `TaskBoard` stores todo state: `open`, `claimed`, `running`, `reviewing`,
  `verified`, `blocked`, or `completed`.
- Panel research and review tasks set `Task.ExpectedReportKind`, so they must
  submit typed reports through `submit_report` or strict JSON response format.
- Research reports must include at least one claim. Findings are optional and
  are verified through their claim links.
- Verification reports must include `perClaim`; an overall-only status is not
  enough for panel cross-review.
- Review tasks use `ReadSelectors`, not legacy `Reads`, to reference the exact
  research output they verify.
- Synthesis reads only `RequireVerified` findings.
- `TeamTimeline(ctx, teamID)` projects raw events into user-facing work,
  conversation, evidence, and control items.
- When `experts` is omitted, panel treats worker profile names as default
  domains, so a `security` profile can claim todos with `domain: "security"`.

## Final Data

The final `team.Result.Structured["panel"]` payload includes:

- `todos`
- `participants`
- `adoptedFindings`
- `excludedClaims`
- `evidence`

This lets callers show both the final answer and the collaboration audit trail.
