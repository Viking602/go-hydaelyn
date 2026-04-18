# Task 6 - collab pattern

- Added `patterns/collab` as a generalized collaboration seed pattern with `Name`, `PlanTemplate`, `Start`, and `Advance` methods while keeping the runtime on the existing `research -> verify -> synthesize` phase model.
- Collaboration startup now specializes branch generation into `implement` tasks with `impl.*` namespaces and implementation write keys, while `Advance` appends `review`, optional `verify`, and final `synthesize` tasks using stage-aware metadata.
- Guarded synthesis now reads verifier outputs (`verify.<task>`) and carries `VerifierRequired` through the collaboration flow so blackboard filtering can stay generic and runtime-compatible.
- Added focused coverage for planned startup metadata propagation and the full `implement -> review -> verify -> synthesize` control loop in `patterns/collab/collab_test.go`.
- Verification passed: `go test ./patterns/collab -run '^(TestCollabPattern_StartBuildsPlannedCollaboration|TestCollabPattern_AdvanceCreatesVerifierAndSynthesis)$'`.
