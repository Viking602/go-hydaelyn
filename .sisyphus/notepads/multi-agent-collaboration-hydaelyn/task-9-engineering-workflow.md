# Task 9 - engineering workflow

- Added `planner/engineering.go` with a concrete engineering workflow template that stays on the existing planner contract and emits ordinary `planner.TaskSpec` entries rather than a new workflow runtime.
- The template maps `plan -> implement -> review -> verify -> synthesize` into explicit task metadata: stage, namespace, dependencies, Reads/Writes, Publish targets, verifier requirements, and normal Hydaelyn roles (`supervisor`, `researcher`, `verifier`).
- Plan tasks publish `plan.*`, implementation tasks publish `impl.*` namespaces with `implement.*` write keys, review tasks publish `review.*`, verify tasks publish `verify.*`, and synthesis reads only verifier outputs before publishing `synthesize.final`.
- Added focused coverage in `planner/engineering_test.go` for phase mapping and explicit dataflow/verifier-gate enforcement.
- Verification passed: `go test ./planner ./host -run '^(TestEngineeringWorkflow_MapsPhasesToPlannerTasks|TestEngineeringWorkflow_UsesExplicitDataflowAndVerifierGates)$'`.
