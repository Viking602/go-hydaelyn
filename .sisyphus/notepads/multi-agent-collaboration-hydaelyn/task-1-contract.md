# Task 1 - collaboration contract

- Added `team.TaskStage` with v1 collaboration values: `plan`, `implement`, `review`, `verify`, `synthesize`.
- Extended `planner.TaskSpec` with `Stage`, `Namespace`, and `VerifierRequired` using `omitempty` JSON tags to preserve backward compatibility.
- Extended `team.Task` with the same collaboration metadata plus `IdempotencyKey` and `Version` for runtime-side idempotency/CAS hints.
- Kept existing runtime phases intact; legacy runtime tasks normalize to `implement` for research tasks, `verify` for verify tasks, and `synthesize` for synthesize tasks.
- Default normalization now fills `Namespace` and `IdempotencyKey` from `Task.ID`, and initializes `Version` to `1` for a stable v1 contract.
- Added focused coverage in `planner/planner_test.go` and `host/runtime_planning_test.go` to verify JSON contract preservation and planner-to-runtime metadata propagation.
- Verification passed: `go test ./planner ./team ./host -run '^(TestPlanTasksCarryCollaborationMetadata|TestBuildPlannedStatePreservesCollaborationContract)$'`.
