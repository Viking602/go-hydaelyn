# Task 10 - deepsearch compatibility

- Added `TestDeepsearchCompatibility_ResearchVerifySynthesize` to `patterns/deepsearch/deepsearch_test.go` to lock the legacy `research -> verify -> synthesize` sequencing, including verifier-task dependencies and supported-finding synthesis.
- Kept `patterns/deepsearch/deepsearch.go` behavior unchanged; rollout isolation is handled by separate `patterns/collab` registration and example wiring rather than by altering deepsearch startup or advancement semantics.
- Added `examples/collab/main.go` as an explicit opt-in example that registers both patterns but selects `Pattern: "collab"`, preserving `deepsearch.New()` as the unchanged default/reference path.
- Updated `README.md` to document that deepsearch remains the reference pattern and collaboration is additive/opt-in.
- Verification passed: `go test ./... -run '^(TestDeepsearchCompatibility_|TestMultiAgentCollaboration_)'` and `go build ./examples/...`.
