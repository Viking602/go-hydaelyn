# 16 — Multi-Agent Demo: Security Incident Triage

> Worked example that exercises every primitive in the
> v0.8.0 four-layer architecture. Anchored to the master spec §13.

## Goal

Show the smallest non-trivial multi-agent workflow that uses **every**
v0.8.0 multi-agent primitive at least once: AgentClass, AgentInstance,
Team, Scheduler, Dispatch, typed Handoff, multi-agent Blackboard,
TaskBudget, OutputPolicy + schema repair, ToolSafety, AgentFailure,
Approval, Resume.

The chosen scenario is **security incident triage** — a 5-role
workflow that mirrors real on-call patterns and naturally produces
auditable artifacts (forensic evidence, containment actions,
remediation records, communications).

## Role inventory

| Role | AgentClass.Name | Owns | Output |
|------|-----------------|------|--------|
| 1 | `intake` | Initial classification | IncidentReport (severity, scope) |
| 2 | `forensics` | Evidence gathering | EvidenceBundle (logs, indicators) |
| 3 | `containment` | Blast-radius reduction | ContainmentPlan + executed actions |
| 4 | `remediation` | Long-term fix | RemediationPlan + completion records |
| 5 | `communications` | Stakeholder updates | CommsBundle (statuses, drafts) |

This sits in `packs/aiops/incident-triage/` (under `packs/`, where
domain vocabulary is permitted per `11-boundaries.md` Principle 1).

## Architecture mapping

```
Pack: packs/aiops/incident-triage/
  ├── classes.go        (5 AgentClasses)
  ├── team.go           (multiagent.NewTeam wiring)
  ├── scheduler.go      (SupervisorScheduler with intake as supervisor)
  ├── tools/            (siem_query, log_pull, isolate_host, …)
  ├── schemas/          (incident_report.json, evidence_bundle.json, …)
  └── demo_test.go      (eval.EvalCase with multi-agent assertions)
```

## AgentClass definitions

`packs/aiops/incident-triage/classes.go`:

```go
package incidenttriage

import (
    "github.com/Viking602/go-hydaelyn/agent"
    "github.com/Viking602/go-hydaelyn/api"
    "github.com/Viking602/go-hydaelyn/multiagent"
)

var IntakeClass = multiagent.AgentClass{
    Name: "intake",
    Description: "Initial incident classification and supervisor routing.",
    InputSchema:  schemas.IncidentInputSchema,
    OutputSchema: schemas.SupervisorDecisionSchema,
    Tools:        []multiagent.ToolRef{{Name: "siem_query"}},
    Capabilities: []api.Capability{capSIEMQuery},
    LoopPolicy: agent.LoopPolicy{
        MaxSteps:    8,
        StepTimeout: 30 * time.Second,
    },
}

var ForensicsClass = multiagent.AgentClass{
    Name: "forensics",
    InputSchema:  schemas.ForensicsInputSchema,
    OutputSchema: schemas.EvidenceBundleSchema,
    Tools: []multiagent.ToolRef{
        {Name: "siem_query"}, {Name: "log_pull"}, {Name: "process_tree"},
    },
    Capabilities: []api.Capability{capSIEMQuery, capLogPull, capProcessTree},
    LoopPolicy: agent.LoopPolicy{MaxSteps: 20, StepTimeout: 2 * time.Minute},
}

var ContainmentClass = multiagent.AgentClass{
    Name: "containment",
    InputSchema:  schemas.ContainmentInputSchema,
    OutputSchema: schemas.ContainmentPlanSchema,
    Tools: []multiagent.ToolRef{
        {Name: "isolate_host"}, {Name: "rotate_credential"}, {Name: "block_ip"},
    },
    Capabilities: []api.Capability{
        // every containment tool is NonIdempotent — see ToolSafety §
        capIsolateHost, capRotateCredential, capBlockIP,
    },
    LoopPolicy: agent.LoopPolicy{MaxSteps: 12, StepTimeout: 1 * time.Minute},
}

var RemediationClass = multiagent.AgentClass{
    Name: "remediation",
    InputSchema:  schemas.RemediationInputSchema,
    OutputSchema: schemas.RemediationPlanSchema,
    Tools:        []multiagent.ToolRef{{Name: "patch_deploy"}, {Name: "config_diff"}},
    LoopPolicy:   agent.LoopPolicy{MaxSteps: 16, StepTimeout: 2 * time.Minute},
}

var CommunicationsClass = multiagent.AgentClass{
    Name: "communications",
    InputSchema:  schemas.CommsInputSchema,
    OutputSchema: schemas.CommsBundleSchema,
    Tools:        []multiagent.ToolRef{{Name: "draft_statement"}, {Name: "post_status"}},
    LoopPolicy:   agent.LoopPolicy{MaxSteps: 8, StepTimeout: 1 * time.Minute},
}
```

## Team wiring

`packs/aiops/incident-triage/team.go`:

```go
func NewIncidentTeam(r *hydaelyn.Runner) *multiagent.Team {
    team := multiagent.NewTeam("incident-triage", r).
        AddRole(IntakeClass).
        AddRole(ForensicsClass).
        AddRole(ContainmentClass).
        AddRole(RemediationClass).
        AddRole(CommunicationsClass).
        UseScheduler(&multiagent.SupervisorScheduler{
            SupervisorClass: "intake",
        })
    return team
}
```

`intake` plays double duty: it produces the initial classification AND
serves as the supervisor that dispatches subsequent roles. This is the
canonical Supervisor pattern from `05-multi-agent-layer.md` §Reference
implementations.

## OutputPolicy + schema repair in action

The `intake` Class's `OutputSchema` is `SupervisorDecisionSchema`:

```json
{
  "type": "object",
  "required": ["severity", "dispatches"],
  "properties": {
    "severity": {"enum": ["P0","P1","P2","P3"]},
    "dispatches": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["to","input"],
        "properties": {
          "to": {"enum": ["forensics","containment","communications"]},
          "input": {"type": "object"}
        }
      }
    }
  }
}
```

`agent.Engine` invokes `intake` with
`OutputPolicy{Schema: SupervisorDecisionSchema, Validate: true, Repair: true, MaxRepairAttempts: 2}`.
If the model returns `dispatches: [{"to": "intake", ...}]` (forbidden
self-route), the validator rejects, the error is fed back to the
model, and the loop repairs to a valid dispatch list. If repair
fails twice, `Result.Failure.Kind == FailureRepairFailed` and the
SupervisorScheduler terminates the Team with
`EventSchedulerFailure`.

## ToolSafety boundary

Every containment tool is `ToolNonIdempotentSideEffect`:

```go
var capIsolateHost = api.Capability{
    Name:           "isolate_host",
    EffectType:     api.ToolEffectExternalSideEffect,
    Idempotent:     false,  // re-isolating a re-joined host has different effect
    RequiresPolicy: true,
    RequiresLease:  true,
}
```

`agent.Engine` MUST NOT auto-retry these. Invocation routes through
Runner's `ApprovalStore` + `ActionAttemptStore` (per
`03-agent-loop.md` ToolSafety table). The demo's
`PolicyEngine` is configured to require human approval for any
P0/P1 containment action; the workflow pauses on `RequestApproval`
and resumes via `Team.Resume(ctx, runID)` when the approval lands.

## Multi-agent Blackboard usage

`forensics` writes its `EvidenceBundle` to the Blackboard with
required metadata:

```go
multiagent.Write(ctx, runner.Blackboard(), runID, multiagent.BlackboardEntry{
    Key:        "evidence/initial",
    Value:      evidenceBundleJSON,
    WrittenBy:  forensicsInstance.ID,
    StepID:     fmt.Sprintf("step-%d", step.Index),
    EvidenceID: "ev-" + ulid.Make().String(),
})
```

Downstream `containment` reads via `BlackboardSelector{ItemTypes:
[]BlackboardItemType{"evidence"}}` and includes the EvidenceBundle's
indicators in its containment plan.

## Typed Handoff (v0.9.0 target)

The intended v0.9.0 behavior: when `forensics` decides its evidence is
insufficient (e.g. the SIEM returned ambiguous results), it produces
`Result.Failure = &AgentFailure{Kind: FailureInsufficientEvidence}`. A
future Scheduler can read the typed failure and dispatch a second
`forensics` instance with a Handoff:

```go
multiagent.Handoff{
    From: forensicsInstanceA.ID,
    To:   forensicsInstanceB.ID,
    Reason: "Initial evidence ambiguous around lateral movement; widen scope.",
    Payload: scopedInputJSON,            // v0.9: validate against ForensicsInputSchema
    EvidenceIDs: []string{"ev-abc123"},  // partial evidence to carry forward
    RequiredOutputSchema: schemas.EvidenceBundleSchema,
}
```

This v0.9 target exercises:

- Typed Handoff with `Payload` validation against
  `ForensicsClass.InputSchema`
- `EvidenceIDs` reference to prior Blackboard entries
- Multiple `AgentInstance`s of the same `AgentClass` in one Run
  (`forensicsInstanceA` and `forensicsInstanceB`, with deterministic
  IDs from `ComputeInstanceID`)

## Resume after kill

The demo includes a kill-resume drill:

1. Start incident workflow.
2. Mid-`containment` (after Approval granted, before action complete),
   kill the worker process with `SIGKILL`.
3. Restart the worker.
4. `Team.Resume(ctx, runID)` rehydrates:
   - Run state from RunStore + TaskStore
   - Step trace from EventStore (EventStepCompleted)
   - Scheduler decisions from TeamStateStore + EventStore
     (EventSchedulerTick)
   - AgentInstance.State from AgentInstanceStore
5. Workflow proceeds from the persisted `LastStepIdx` without
   re-executing the already-completed isolation action (idempotency
   ledger short-circuits via `ActionAttemptStore`).

This validates `11-boundaries.md` Principle 5 — three-surface
reconstruction.

## Eval assertions

`packs/aiops/incident-triage/demo_test.go` uses `eval.EvalCase` with
multi-agent-aware assertions (added in `10-evaluation.md`):

```go
eval.EvalCase{
    Name:    "p1-host-compromise-end-to-end",
    Setup:   setupIncidentTeam,
    Input:   api.RunInput{Request: fixtureP1Compromise},
    Timeout: 10 * time.Minute,
    Assertions: []eval.Assertion{
        eval.AgentInstanceSpawned("intake"),
        eval.AgentInstanceSpawned("forensics", eval.AtLeast(1)),
        eval.AgentInstanceSpawned("containment"),
        eval.HandoffOccurred("forensics", "forensics"),  // self-handoff for re-scope
        eval.BlackboardHasItem(api.BlackboardSelector{ItemTypes: []api.BlackboardItemType{"evidence"}}),
        eval.ToolCalled("isolate_host"),
        eval.ApprovalRequested("isolate_host"),                       // policy gated
        eval.NoNonIdempotentToolAutoRetried(),                         // ADR-015 § §
        eval.WithinBudget(50_000),
        eval.TeamTerminatedSuccessfully(),
    },
}
```

## Autoresearch borrowings exercised

This demo concretely exercises the four ports from autoresearch
(master spec §11):

| autoresearch concept | How the demo uses it |
|----------------------|----------------------|
| `program.md` | Each `AgentClass.Instructions` is a small, focused program-style instruction set |
| Fixed budget | `TaskBudget.MaxWallClock = 10 min` capped at Team level |
| `results.tsv` | `hydaelyn-audit` CLI on the Run's TraceStore produces a TSV of per-instance steps + outcomes |
| `val_bpb` | `eval.BPBLikeMetric` template optionally scores the IncidentReport against a ground-truth fixture |

## Non-goals

This demo deliberately does NOT:

- Connect to a real SIEM / EDR. All tools are stubbed.
- Persist to production storage. Uses `contract/internal/inmemfake`.
- Use a real LLM provider. Uses `provider/scripted` with canned responses.
- Cover full incident-response best practices. It's a framework exercise, not an IR playbook.

A user wanting the same shape against real systems would swap the
tool implementations and provider — the `multiagent/` layer, the
`agent/` loop, and the Runner stay identical.

## Verification

- `TestIncidentTriage_FullPath_P1Compromise`
- `TestIncidentTriage_RepairLoopRecoversFromInvalidDispatch`
- `TestIncidentTriage_NonIdempotentToolRoutesThroughApproval`
- `TestIncidentTriage_SelfHandoffReScopesForensics`
- `TestIncidentTriage_KillResumeReconstructsThreeSurfaces`
- `TestIncidentTriage_BudgetExhaustionTerminatesTeam`
- `TestIncidentTriage_DemoEvalSuitePasses`
